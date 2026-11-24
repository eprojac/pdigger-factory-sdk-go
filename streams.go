package pdiggerfactory

import (
	"bytes"
	"context"
	"encoding/json"
	"iter"
	"net/url"
	"strings"
	"sync"
	"time"
)

// StreamItem is one item of a stream: *FrameItem, *ClockItem, *WorkerItem, *GapItem, *ResetItem, or *PacketItem
// (raw packet streams). Use a type switch.
type StreamItem interface {
	// Kind is the stream event name: frame, clock, worker, gap or reset.
	Kind() string
}

// FrameItem is a frame of a stream.
type FrameItem struct {
	Frame *Frame
	// ID is the event id "<boot>-<cursor>" (empty for long-poll and replays).
	ID string
	// Cursor is the frame's own cursor.
	Cursor int64
	// IsRevision: a higher world.rev of a frame already delivered for the same (session, g).
	IsRevision bool
	// IsBackfill: the frame's late layers were read back and merged in.
	IsBackfill bool
}

// ClockItem is the shared state, sent when it changes, with the server's wall clock.
type ClockItem struct {
	State     State
	ServerNow float64
}

// WorkerItem is the interpreter worker's status.
type WorkerItem struct {
	Worker WorkerStatus
}

// GapItem says that many items were skipped for a slow consumer.
type GapItem struct {
	Count int64
}

// ResetItem says the server restarted (another boot); the stream goes on from the newest.
type ResetItem struct {
	Boot   string
	Cursor int64
}

// PacketItem is a raw model packet of Streams.RawPackets.
type PacketItem struct {
	Packet *Sip1Packet
	ID     string
	Cursor int64
}

// Kind is "frame".
func (*FrameItem) Kind() string { return StreamEventFrame }

// Kind is "clock".
func (*ClockItem) Kind() string { return StreamEventClock }

// Kind is "worker".
func (*WorkerItem) Kind() string { return StreamEventWorker }

// Kind is "gap".
func (*GapItem) Kind() string { return StreamEventGap }

// Kind is "reset".
func (*ResetItem) Kind() string { return StreamEventReset }

// Kind is "frame".
func (*PacketItem) Kind() string { return StreamEventFrame }

// clockEvent is the item as a ClockEvent.
func (c *ClockItem) clockEvent() *ClockEvent {
	return &ClockEvent{State: c.State, ServerNow: c.ServerNow}
}

// Streams is the mid level: endless frame streams over SSE with resume, long-poll and raw packets.
type Streams struct{ c *Client }

// DefaultLayers returns the layers of a stream opened without Layers, the server's default: overlay, scene, cards and
// world (a new slice on every call).
func DefaultLayers() []string { return []string{LayerOverlay, LayerScene, LayerCards, LayerWorld} }

type streamConfig struct {
	layers        []string
	after         *int64
	observe       *bool
	precision     string
	resume        bool
	backfill      bool
	lastEventID   string
	maxReconnects int
	onReconnect   func(attempt int, err error)
	limit         int
	maxS          int
	format        string
	pollLimit     int
	pollWait      *int64
	clockOnOpen   bool
}

// StreamOption configures a stream or an Interpreter.
type StreamOption func(*streamConfig)

// Layers selects the layers (raw, overlay, scene, cards, world). Streams default to the server's default
// (overlay, scene, cards, world); the Interpreter to world and cards.
func Layers(layers ...string) StreamOption {
	return func(c *streamConfig) { c.layers = append([]string(nil), layers...) }
}

// After starts after this cursor (default: the newest).
func After(cursor int64) StreamOption { return func(c *streamConfig) { c.after = Int64(cursor) } }

// Observe sets whether the stream counts as a viewer: true does not (an idle unit stays idle). The default is the
// client's (WithObserve), false unless set.
func Observe(on bool) StreamOption { return func(c *streamConfig) { c.observe = Bool(on) } }

// Precision is "rounded" (default) or "full".
func Precision(p string) StreamOption { return func(c *streamConfig) { c.precision = p } }

// Resume sets whether a stream that the server ends (limit, max_s) or that drops is reconnected with
// Last-Event-ID, so the caller sees one endless stream (default true).
func Resume(on bool) StreamOption { return func(c *streamConfig) { c.resume = on } }

// Backfill sets whether a frame released with missing layers is held back, with every frame after it, until its
// layers are read back (default false for Streams.Frames, true for the Interpreter).
func Backfill(on bool) StreamOption { return func(c *streamConfig) { c.backfill = on } }

// LastEventID resumes after an event id "<boot>-<cursor>" of an earlier stream.
func LastEventID(id string) StreamOption { return func(c *streamConfig) { c.lastEventID = id } }

// MaxReconnects ends the stream with an error after n failed reconnects in a row (default 0: never).
func MaxReconnects(n int) StreamOption { return func(c *streamConfig) { c.maxReconnects = n } }

// OnReconnect is called before each reconnect after a failure, with the failure count and the error.
func OnReconnect(fn func(attempt int, err error)) StreamOption {
	return func(c *streamConfig) { c.onReconnect = fn }
}

// ConnectionLimit ends each connection after n frames: the SDK asks the server for it (limit), counts the frames
// itself and closes the connection at the n-th, then reconnects at once with Last-Event-ID (the 20.20 unit closes a
// finished stream only about 10 s after its limit, and waiting for that would hold the next frames back).
func ConnectionLimit(n int) StreamOption { return func(c *streamConfig) { c.limit = n } }

// ConnectionMaxSeconds ends each connection after s seconds: the SDK asks the server for it (max_s), times it on the
// client's clock and closes the connection itself, then reconnects at once with Last-Event-ID.
func ConnectionMaxSeconds(s int) StreamOption { return func(c *streamConfig) { c.maxS = s } }

// ClockOnConnect reads the shared state (GET /session) after every (re)connect of Streams.Frames and passes it on
// first, as a ClockItem: the unit sends a clock event only when the state changes, none on connect, so without it
// the wall times of a new stream stay unknown until the next select, seek, pause or play. The read is one request
// of at most a second (a stream that waits longer would fall behind); when it fails the stream goes on without it.
// The Interpreter's default is true, Streams.Frames' false.
func ClockOnConnect(on bool) StreamOption { return func(c *streamConfig) { c.clockOnOpen = on } }

// RawFormat is the transport of Streams.RawPackets: FormatNDJSON (default) or FormatSSE.
func RawFormat(f string) StreamOption { return func(c *streamConfig) { c.format = f } }

// PollLimit is the frames per long-poll reply (1..16, default the server's 8).
func PollLimit(n int) StreamOption { return func(c *streamConfig) { c.pollLimit = n } }

// PollWait is the longest wait of a long-poll in ms (0..1000, default the server's 1000).
func PollWait(ms int64) StreamOption { return func(c *streamConfig) { c.pollWait = Int64(ms) } }

func (s *Streams) config(opts []StreamOption) *streamConfig {
	cfg := &streamConfig{resume: true}
	for _, o := range opts {
		o(cfg)
	}
	return cfg
}

func (c *streamConfig) observing(def bool) bool {
	if c.observe != nil {
		return *c.observe
	}
	return def
}

// arrival is an item from a stream source, with its arrival time.
type arrival struct {
	item StreamItem
	err  error
	at   time.Time
}

// send hands an arrival to the pipeline; false when ctx ended (nothing is handed over after that).
func send(ctx context.Context, out chan<- arrival, a arrival) bool {
	if ctx.Err() == nil {
		select {
		case out <- a:
			return true
		case <-ctx.Done():
		}
	}
	return false
}

// Frames follows the shared session's frames over SSE (section 2.3 of the SDK design): it reconnects with
// Last-Event-ID when the server ends a stream, the connection drops or a connection reaches its ConnectionLimit or
// ConnectionMaxSeconds, passes clock, worker, gap and reset events through, flags world revisions, drops duplicates
// and, with Backfill(true), completes late layers in order. A failed connection is tried again after the server's
// hint (retry_ms, else Retry-After), else the backoff. The connection closes when the loop ends or ctx is cancelled.
func (s *Streams) Frames(ctx context.Context, opts ...StreamOption) iter.Seq2[StreamItem, error] {
	cfg := s.config(opts)
	return func(yield func(StreamItem, error) bool) {
		s.pipeline(ctx, cfg, func(ctx context.Context, out chan<- arrival) { s.sseSource(ctx, cfg, out) }, yield)
	}
}

// Poll is Frames over the long-poll (pollFrames in a loop, continuing from the reply's cursor and state revision),
// for environments without streaming. A state change is a ClockItem, a worker change a WorkerItem.
func (s *Streams) Poll(ctx context.Context, opts ...StreamOption) iter.Seq2[StreamItem, error] {
	cfg := s.config(opts)
	return func(yield func(StreamItem, error) bool) {
		s.pipeline(ctx, cfg, func(ctx context.Context, out chan<- arrival) { s.pollSource(ctx, cfg, out) }, yield)
	}
}

// RawPackets follows the raw model codec stream (raw/stream, NDJSON or SSE) with the same resume rules; packets
// are PacketItems.
func (s *Streams) RawPackets(ctx context.Context, opts ...StreamOption) iter.Seq2[StreamItem, error] {
	cfg := s.config(opts)
	if cfg.format == "" {
		cfg.format = FormatNDJSON
	}
	return func(yield func(StreamItem, error) bool) {
		s.pipeline(ctx, cfg, func(ctx context.Context, out chan<- arrival) { s.rawSource(ctx, cfg, out) }, yield)
	}
}

// decodeEvent turns a raw event into a stream item; nil for an event name it does not know. An event whose data does
// not decode (not JSON, or not the shape of its type: 2.3 rule 9) is its own decode error (Op "decode"), which the
// stream yields and goes on after.
func decodeEvent(ev RawEvent, packets bool) (StreamItem, error) {
	switch {
	case ev.Event != StreamEventFrame: // an unnamed event ("message") too, as in the other languages
		return sideItem(ev.Event, ev.Data)
	case packets:
		var p Sip1Packet
		if err := decodeData(ev.Data, "a packet event", packetShape, &p); err != nil {
			return nil, err
		}
		pi := &PacketItem{Packet: &p, ID: ev.ID}
		if p.Cursor != nil {
			pi.Cursor = *p.Cursor
		}
		return pi, nil
	}
	var f Frame
	if err := decodeData(ev.Data, "a frame event", frameShape, &f); err != nil {
		return nil, err
	}
	return &FrameItem{Frame: &f, ID: ev.ID, Cursor: f.Cursor}, nil
}

// sideItem is the item of a side event (a stream's event, or a recording's side line); nil for an event name it
// does not know. Data that does not decode (2.3 rule 9) is a decode error.
func sideItem(name string, data []byte) (StreamItem, error) {
	switch name {
	case StreamEventClock:
		var c ClockEvent
		if err := decodeData(data, "a clock event", clockShape, &c); err != nil {
			return nil, err
		}
		return &ClockItem{State: c.State, ServerNow: c.ServerNow}, nil
	case StreamEventWorker:
		var w WorkerStatus
		if err := decodeData(data, "a worker event", workerShape, &w); err != nil {
			return nil, err
		}
		return &WorkerItem{Worker: w}, nil
	case StreamEventGap:
		var g GapEvent
		if err := decodeData(data, "a gap event", gapShape, &g); err != nil {
			return nil, err
		}
		return &GapItem{Count: g.Gap}, nil
	case StreamEventReset:
		var r ResetEvent
		if err := decodeData(data, "a reset event", resetShape, &r); err != nil {
			return nil, err
		}
		return &ResetItem{Boot: r.Boot, Cursor: r.Cursor}, nil
	}
	return nil, nil
}

// reconnector counts failed connections and waits before the next one.
type reconnector struct {
	s        *Streams
	cfg      *streamConfig
	failures int
}

// fail records a failure and waits; false means give up (the error was sent, or ctx ended).
func (r *reconnector) fail(ctx context.Context, out chan<- arrival, err error) bool {
	if e := AsError(err); e != nil && !e.IsRetryable() {
		send(ctx, out, arrival{err: err})
		return false
	}
	if !r.cfg.resume {
		send(ctx, out, arrival{err: err})
		return false
	}
	r.failures++
	if r.cfg.maxReconnects > 0 && r.failures > r.cfg.maxReconnects {
		send(ctx, out, arrival{err: &Error{Op: "stream", Code: CodeStreamClosed,
			Message: "reconnect budget used up", Err: err}})
		return false
	}
	if r.cfg.onReconnect != nil {
		r.cfg.onReconnect(r.failures, err)
	}
	wait := r.s.c.backoffDelay(r.failures - 1)
	if e := AsError(err); e != nil {
		if d, ok := e.RetryDelay(); ok {
			wait = d
		}
	}
	return r.s.c.sleep(ctx, wait) == nil
}

// ended handles a connection that ended without an error (the server ended it, or the SDK did at its limit or
// max_s): reconnect at once when it delivered something, else back off.
func (r *reconnector) ended(ctx context.Context, delivered int) bool {
	if !r.cfg.resume {
		return false
	}
	if delivered > 0 {
		r.failures = 0
		return true
	}
	r.failures++
	return r.s.c.sleep(ctx, r.s.c.backoffDelay(r.failures-1)) == nil
}

// clockReadTimeout bounds the state read of ClockOnConnect, on the client's clock.
const clockReadTimeout = time.Second

// sendClock reads the shared state once and hands it on as a ClockItem (nothing when the read fails).
func (s *Streams) sendClock(ctx context.Context, out chan<- arrival) {
	rctx, cancel := withTimeout(ctx, s.c.clock, clockReadTimeout)
	defer cancel()
	reply, err := s.c.api.getSession(rctx, true)
	if err == nil {
		send(ctx, out, arrival{item: &ClockItem{State: reply.State, ServerNow: reply.ServerNow}, at: s.c.clock.Now()})
	}
}

func (s *Streams) sseSource(ctx context.Context, cfg *streamConfig, out chan<- arrival) {
	defer close(out)
	rc := &reconnector{s: s, cfg: cfg}
	lastID := cfg.lastEventID
	for {
		p := &StreamFramesParams{Layers: cfg.layers, Format: FormatSSE, Limit: cfg.limit, MaxS: cfg.maxS,
			Observe: cfg.observing(s.c.observe), Precision: cfg.precision}
		if lastID == "" {
			p.After = cfg.after
		}
		q := p.query()
		lim := &connLimits{limit: cfg.limit, maxS: cfg.maxS}
		if cfg.clockOnOpen {
			lim.onOpen = func() { s.sendClock(ctx, out) }
		}
		delivered := 0
		var connErr error
		for ev, err := range s.c.rawStream(ctx, "streamFrames", apiRoot+"/session/frames/stream", q, FormatSSE, lastID,
			true, lim) {
			if err != nil {
				connErr = err
				break
			}
			delivered++
			rc.failures = 0
			if ev.ID != "" {
				lastID = ev.ID
			}
			a := arrival{at: s.c.clock.Now()}
			a.item, a.err = decodeEvent(ev, false)
			if ri, ok := a.item.(*ResetItem); ok && ri.Boot != "" { // the resume id becomes the reset's (2.3)
				lastID = ri.Boot + "-" + itoa(ri.Cursor)
			}
			if (a.item != nil || a.err != nil) && !send(ctx, out, a) {
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		if connErr != nil {
			if !rc.fail(ctx, out, connErr) {
				return
			}
			continue
		}
		if !rc.ended(ctx, delivered) {
			return
		}
	}
}

func (s *Streams) rawSource(ctx context.Context, cfg *streamConfig, out chan<- arrival) {
	defer close(out)
	rc := &reconnector{s: s, cfg: cfg}
	lastID := cfg.lastEventID
	after := cfg.after
	for {
		q := url.Values{}
		q.Set("format", cfg.format)
		if lastID == "" {
			setInt(q, "after", after)
		}
		setPositive(q, "limit", cfg.limit)
		setPositive(q, "max_s", cfg.maxS)
		if cfg.observing(s.c.observe) {
			q.Set("observe", "1")
		}
		delivered := 0
		var connErr error
		lim := &connLimits{limit: cfg.limit, maxS: cfg.maxS}
		for ev, err := range s.c.rawStream(ctx, "rawStream", apiRoot+"/raw/stream", q, cfg.format, lastID, true, lim) {
			if err != nil {
				connErr = err
				break
			}
			delivered++
			rc.failures = 0
			if ev.ID != "" {
				lastID = ev.ID
			}
			a := arrival{at: s.c.clock.Now()}
			a.item, a.err = decodeEvent(ev, true)
			if pi, ok := a.item.(*PacketItem); ok && pi.Packet.Cursor != nil {
				after = Int64(*pi.Packet.Cursor)
			}
			if ri, ok := a.item.(*ResetItem); ok && ri.Boot != "" { // the resume id becomes the reset's (2.3)
				lastID, after = ri.Boot+"-"+itoa(ri.Cursor), Int64(ri.Cursor)
			}
			if (a.item != nil || a.err != nil) && !send(ctx, out, a) {
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		if connErr != nil {
			if !rc.fail(ctx, out, connErr) {
				return
			}
			continue
		}
		if !rc.ended(ctx, delivered) {
			return
		}
	}
}

func (s *Streams) pollSource(ctx context.Context, cfg *streamConfig, out chan<- arrival) {
	defer close(out)
	rc := &reconnector{s: s, cfg: cfg}
	p := &PollParams{Layers: cfg.layers, Limit: cfg.pollLimit, WaitMS: cfg.pollWait, Precision: cfg.precision}
	if cfg.after != nil {
		p.Cursor = *cfg.after
	}
	if !cfg.observing(s.c.observe) {
		p.CID = s.c.newCID()
	}
	first := true
	var worker []byte
	for {
		started := s.c.clock.Now()
		reply, err := s.c.api.PollFrames(ctx, p)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if !rc.fail(ctx, out, err) {
				return
			}
			continue
		}
		rc.failures = 0
		news := false
		if first || reply.State.Revision != p.Rev {
			first, news = false, true
			p.Rev = reply.State.Revision
			if !send(ctx, out, arrival{item: &ClockItem{State: reply.State, ServerNow: reply.ServerNow},
				at: s.c.clock.Now()}) {
				return
			}
		}
		if w, _ := json.Marshal(reply.Worker); !bytes.Equal(w, worker) {
			worker = w
			if !send(ctx, out, arrival{item: &WorkerItem{Worker: reply.Worker}, at: s.c.clock.Now()}) {
				return
			}
		}
		for i := range reply.Frames {
			f := &reply.Frames[i]
			news = true
			if !send(ctx, out, arrival{item: &FrameItem{Frame: f, Cursor: f.Cursor}, at: s.c.clock.Now()}) {
				return
			}
		}
		p.Cursor = reply.Cursor
		// a reply with nothing new that came back early (a server that does not wait): back off
		if !news && s.c.clock.Now().Sub(started) < 100*time.Millisecond {
			rc.failures++
			if s.c.sleep(ctx, s.c.backoffDelay(rc.failures-1)) != nil {
				return
			}
		}
	}
}

// held is a frame waiting in the release queue.
type held struct {
	fi    *FrameItem
	ready bool
}

type sessG struct{ session, g int64 }

// dedupe flags revisions and drops duplicates: a frame whose (session, g) was delivered with the same or a higher
// world.rev. It remembers the newest session's newest keepFrames grid frames (frames of other sessions are forgotten:
// the core starts again on a session change), so it stays bounded on a stream that runs for days.
type dedupe struct {
	delivered       map[sessG]int64
	session, newest int64
}

// admit is false for a duplicate; it flags a revision.
func (d *dedupe) admit(fi *FrameItem) bool {
	f := fi.Frame
	k := sessG{f.Session, f.G}
	var rev int64
	if f.World != nil {
		rev = f.World.Rev
	}
	if prev, ok := d.delivered[k]; ok {
		if rev <= prev {
			return false
		}
		fi.IsRevision = true
	}
	d.delivered[k] = rev
	if f.Session != d.session || len(d.delivered) == 1 {
		d.session, d.newest = f.Session, f.G
	} else if f.G > d.newest {
		d.newest = f.G
	}
	if len(d.delivered) > 2*keepFrames {
		for key := range d.delivered {
			if key.session != d.session || key.g < d.newest-int64(keepFrames) {
				delete(d.delivered, key)
			}
		}
	}
	return true
}

// pipeline runs a source and releases its items: side events at once, frames in arrival order behind a frame
// that waits for its backfill, revisions flagged and duplicates dropped.
func (s *Streams) pipeline(ctx context.Context, cfg *streamConfig, src func(context.Context, chan<- arrival),
	yield func(StreamItem, error) bool) {
	ictx, cancel := context.WithCancel(ctx)
	out := make(chan arrival, 16)
	results := make(chan *held)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		src(ictx, out)
	}()
	defer func() {
		cancel()
		wg.Wait()
	}()
	d := &dedupe{delivered: map[sessG]int64{}}
	emit := func(fi *FrameItem) bool {
		if !d.admit(fi) {
			return true
		}
		return yield(fi, nil)
	}
	var queue []*held
	in := (<-chan arrival)(out)
	for {
		if err := ctx.Err(); err != nil {
			yield(nil, transportError("stream", ctx, err))
			return
		}
		for len(queue) > 0 && queue[0].ready {
			h := queue[0]
			queue = queue[1:]
			if !emit(h.fi) {
				return
			}
		}
		if in == nil && len(queue) == 0 {
			return
		}
		select {
		case <-ctx.Done(): // the loop ends at its top
		case a, ok := <-in:
			if !ok {
				in = nil
				continue
			}
			if a.err != nil {
				if !yield(nil, a.err) {
					return
				}
				continue
			}
			fi, isFrame := a.item.(*FrameItem)
			if !isFrame {
				if !yield(a.item, nil) {
					return
				}
				continue
			}
			need := cfg.backfill && len(fi.Frame.Missing) > 0
			if !need && len(queue) == 0 {
				if !emit(fi) {
					return
				}
				continue
			}
			h := &held{fi: fi, ready: !need}
			queue = append(queue, h)
			if need {
				wg.Add(1)
				go func() {
					defer wg.Done()
					s.backfill(ictx, cfg, h, a.at)
					select {
					case results <- h:
					case <-ictx.Done():
					}
				}()
			}
		case h := <-results:
			h.ready = true
		}
	}
}

// The late-layer backfill: a held frame's missing layers are read backfillDelay after it arrived, up to
// backfillTries times backfillInterval apart, each read bounded by backfillTimeout, so a late world holds the frames
// behind it for about 4.5 s at the most. All on the client's clock.
const (
	backfillDelay    = 1500 * time.Millisecond
	backfillInterval = time.Second
	backfillTimeout  = time.Second
	backfillTries    = 3
)

// backfill reads a frame's missing layers (see backfillDelay) and merges them in.
func (s *Streams) backfill(ctx context.Context, cfg *streamConfig, h *held, arrived time.Time) {
	f := h.fi.Frame
	for try := 0; try < backfillTries; try++ {
		due := arrived.Add(backfillDelay + time.Duration(try)*backfillInterval)
		if s.c.sleep(ctx, due.Sub(s.c.clock.Now())) != nil {
			return
		}
		tctx, cancel := withTimeout(ctx, s.c.clock, backfillTimeout)
		got, err := s.c.api.getFrame(tctx, FrameAt(f.G), &FrameParams{Layers: append([]string(nil), f.Missing...),
			Session: Int64(f.Session), Precision: cfg.precision}, true)
		cancel()
		if err != nil {
			if IsNotRetained(err) || IsSessionGone(err) || ctx.Err() != nil {
				return
			}
			continue
		}
		if got.Session == f.Session && got.G == f.G && mergeLayers(f, got) {
			h.fi.IsBackfill = true
		}
		if len(f.Missing) == 0 {
			return
		}
	}
}

// mergeLayers copies the layers of src that dst misses into dst and removes them from dst.Missing.
func mergeLayers(dst, src *Frame) bool {
	merged := false
	var still []string
	for _, layer := range dst.Missing {
		ok := true
		switch strings.TrimSpace(layer) {
		case LayerRaw:
			ok = src.Raw != nil
			if ok {
				dst.Raw = src.Raw
			}
		case LayerOverlay:
			ok = src.Overlay != nil
			if ok {
				dst.Overlay = src.Overlay
			}
		case LayerScene:
			ok = src.Scene != nil
			if ok {
				dst.Scene = src.Scene
			}
		case LayerCards:
			ok = src.Cards != nil
			if ok {
				dst.Cards = src.Cards
			}
		case LayerWorld:
			ok = src.World != nil
			if ok {
				dst.World = src.World
			}
		default:
			ok = false
		}
		if ok {
			merged = true
		} else {
			still = append(still, layer)
		}
	}
	dst.Missing = still
	return merged
}

// newCID makes a per-client id for the long-poll (^[A-Za-z0-9_-]{8,40}$).
func (c *Client) newCID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-"
	b := make([]byte, 16)
	for i := range b {
		b[i] = alphabet[int(c.jitter()*float64(len(alphabet)))%len(alphabet)]
	}
	return "go-" + string(b)
}

// ChanResult is one value or error of a stream consumed as a channel.
type ChanResult[T any] struct {
	Value T
	Err   error
}

// Chan consumes an iterator in a goroutine and delivers its values on a channel (closed when the iterator ends or
// ctx is cancelled), for select-based code. Cancel ctx to stop it early: that also closes the stream.
func Chan[T any](ctx context.Context, seq iter.Seq2[T, error]) <-chan ChanResult[T] {
	ch := make(chan ChanResult[T])
	go func() {
		defer close(ch)
		for v, err := range seq {
			select {
			case ch <- ChanResult[T]{Value: v, Err: err}:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}
