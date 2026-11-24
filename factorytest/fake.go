// Package factorytest is the testing module of the pDigger Factory SDK: a fake server that answers the v2 API
// from recorded replies (the fixtures/api layout: INDEX.json and files) and from a recording (pfr1), as an
// in-process http.RoundTripper or an httptest server. Everything is deterministic: no network, no real clock (the
// fake's wall clock stands still unless a test advances it or WithClock links it to a clock), and fault injection for errors, dropped connections, gaps, resets, delays and the
// unit's late close of finished streams.
//
//	fake, err := factorytest.New(factorytest.WithSampleRecording())
//	client, err := pdiggerfactory.New(fake.URL(), pdiggerfactory.WithTransport(fake.Transport()),
//		pdiggerfactory.WithClock(factorytest.NewClock(time.Time{})))
//
// WithSampleRecording serves a real recording embedded in this package (aug28, from a replay of the 20.20 unit's
// model and build on a Mac); the SDK's other fixtures (the other fixture recordings, the recorded replies of
// fixtures/api) are in the module's folder, which
// "go list -m -f '{{.Dir}}' github.com/eprojac/pdigger-factory-sdk-go" prints.
//
// What answers a GET, first match first:
//
//  1. FailNext and ReplyNext, for the requests they are given for;
//  2. a reply given by key with WithReplyMap, for its operation;
//  3. with a recording: the catalog (its header's source, plus the live camera with WithLiveCamera), the session
//     state (the recording's clock line in force where playback is, then the controls), frames/{g}, raw packets,
//     the world, its events, stone tracks and the focus are synthesised from the recording, and a frame raster is
//     checked against the recording's frames before it is answered (from a recorded reply, else a synthetic PNG);
//  4. the recorded replies of WithReplies: the other GET operations (model, topology, render draw lists, metrics,
//     ...), and everything when there is no recording;
//  5. the fake's own answers (health, readiness, status, the API root, ...).
//
// The frame streams (SSE and NDJSON, with the recording's clock, worker, gap and reset lines as side events in their
// place; NDJSON carries the gap lines only) and the long-poll come from the recording when there is one, else from
// recorded replies. They play it as fast as they read it, and the session state is the recording's clock line in
// force at the playhead, as on the unit (whose clock events say when its state changed): the first at the start,
// each later one (the unit's own rate, a pause, a seek) once a stream or a long poll has passed it; a long-poll reply
// ends before a clock line not passed yet, so its state is the one in force for its frames. Controls are validated
// like the server, dry_run honoured, and applied to the fake's state (from then on the state is the fake's own: the
// recording's later clock lines no longer change it): a select or seek starts a new fake session that replays the
// recording's frames from that time.
//
// With WithLiveCamera it also offers a live camera, LiveCamera ("cam-live"), as the 20.20 unit offers its plant
// cameras: listed after the recording, and shown by the session at the start, already live. The camera sends the
// recording's frames as a camera session (g = i from 0, t the arrival in seconds since the session started). A select
// of the camera starts a session as the unit does: connecting (for ConnectSeconds), then warming while the interpreter
// runs on the first hold (3) seconds of frames, which are never released, then live (LiveLagSeconds behind the
// camera); a play connects again (in its own reply) and warms up on the camera's next frames, after a break in g. The
// phases follow the fake's wall clock while the session plays (WithClock links it to the client's clock, so
// Session.UntilLive's sleeps move it); on a clock that stands still, a stream or long poll that passes one waits for
// it, which moves the clock on. The newest KeepPictures released frames have a picture (frames/{g}/image.jpg, a small
// JPEG); the camera has no media (404 not_found), a seek in it is refused (409 not_seekable, field "video": a seek
// naming a recording is applied while the session shows it) and so is a select with a time (400, field "time").
package factorytest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/eprojac/pdigger-factory-sdk-go/internal/pfr"
)

// Reply is one canned reply.
type Reply struct {
	Status      int
	ContentType string
	Headers     map[string]string
	Body        []byte
}

// JSONReply makes a reply with a JSON body.
func JSONReply(status int, v any) Reply {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return Reply{Status: status, ContentType: "application/json; charset=utf-8", Body: b}
}

// ErrorReply makes an API error reply in the server's shape.
func ErrorReply(status int, code, message string) Reply {
	return JSONReply(status, map[string]any{"error": message, "code": code})
}

// Request is a request the fake answered, for assertions.
type Request struct {
	Op     string
	Method string
	Path   string
	Query  url.Values
	Header http.Header
	Body   []byte
}

type config struct {
	recording  string
	sample     bool
	lines      [][]byte
	repliesDir string
	replies    map[string]Reply
	live       bool
	boot       string
	clock      interface{ Now() time.Time }
}

// Option configures a Fake.
type Option func(*config)

// WithRecording serves a recording (a local .ndjson or .ndjson.gz pfr1 file).
func WithRecording(path string) Option {
	return func(c *config) { c.recording, c.sample = path, false }
}

// WithSampleRecording serves the sample recording embedded in this package (see SampleRecording), as WithRecording
// would serve the fixture file: the shared session plays aug28 from 21:10.0.
func WithSampleRecording() Option {
	return func(c *config) { c.recording, c.sample = "", true }
}

// WithLines serves a recording given as its lines: a header, frames and side lines, as JSON.
func WithLines(lines ...[]byte) Option {
	return func(c *config) { c.lines = append(c.lines, lines...) }
}

// WithFrames serves frames given as values (Frame structs, maps, json.RawMessage): a recording without a header.
func WithFrames(frames ...any) Option {
	return func(c *config) {
		for _, f := range frames {
			b, err := json.Marshal(f)
			if err != nil {
				panic(err)
			}
			c.lines = append(c.lines, b)
		}
	}
}

// WithReplies serves recorded replies from a directory in the fixtures/api layout (INDEX.json and files). With a
// recording they answer only what the recording does not (the model, the topology, the render draw lists, the
// metrics, ...): the directory's frames, world and session come from another run of the unit and would contradict
// the recording's.
func WithReplies(dir string) Option { return func(c *config) { c.repliesDir = dir } }

// WithReplyMap serves replies by key: an operationId, or operationId-variant (getFrame-404). A reply given here
// answers its operation even when a recording would (the key operationId every request of it, a 2xx
// operationId-variant the requests that name the variant, such as getFrame-latest for GET frames/latest), and wins
// over a WithReplies reply of the same key. Error variants answer only through ReplyNext.
func WithReplyMap(m map[string]Reply) Option {
	return func(c *config) {
		for k, v := range m {
			c.replies[k] = v
		}
	}
}

// WithLiveCamera offers the live camera LiveCamera ("cam-live") after the recording, as the 20.20 unit offers its plant
// cameras, and shows it at the start, already live (see the package doc); the catalog's default video stays the
// recording. A select of the camera connects and warms up on the fake's wall clock: give WithClock too when the
// client waits for it (Session.UntilLive).
func WithLiveCamera() Option { return func(c *config) { c.live = true } }

// WithClock makes the fake's wall clock follow clock from where it starts (the server_now of the recording's first
// clock line): give it the client's Clock (the SDK's WithClock), so the client's sleeps (Session.UntilLive, a
// stream's backoff) move the fake's time too, or pdiggerfactory.SystemClock{}, so a fake unit served to real clients
// runs in real time (its camera connects and warms up as a unit's does). Advance still moves it on. Without WithClock
// the wall clock stands still unless Advance moves it.
func WithClock(clock interface{ Now() time.Time }) Option { return func(c *config) { c.clock = clock } }

// WithBoot sets the server's boot id (event ids are "<boot>-<cursor>"; default "fakeboot01").
func WithBoot(boot string) Option { return func(c *config) { c.boot = boot } }

// Fake is the fake server.
type Fake struct {
	mu      sync.Mutex
	replies map[string]Reply
	given   map[string]bool // the keys of WithReplyMap
	hasRec  bool
	header  map[string]any
	sources []map[string]any // the catalog, in order
	recSrc  string           // the recording's video id
	rec     []*entry         // the recording's frames, as recorded
	tl      []*entry         // the timeline: frames and side events of every session, in order
	start   int              // where a stream without after starts: the current program
	played  int              // the playhead: the timeline before it has been played (play)
	changed chan struct{}
	boot    string
	st      state
	now     float64
	worker  json.RawMessage
	session int64   // the newest session id used
	cursor  int64   // the newest event cursor
	rate    float64 // the playback rate of recordings
	viewers int
	streams int
	// the clock the wall clock follows (WithClock) and its time when the fake was made; nil: it stands still
	clock  interface{ Now() time.Time }
	clock0 time.Time
	// a camera session's phases still to come, in order, and its run's frames, released when it goes live; camG and
	// camT are the g and the session time after the camera's newest frame (the next run comes after them)
	phases []phase
	held   []*entry
	camG   int64
	camT   float64

	faults  map[string][]Reply
	drops   map[string]int
	hold    bool
	injects []*entry
	delay   func(op string, r *http.Request)
	reqs    []Request

	handler http.Handler
	server  *httptest.Server
}

// entry is a frame or a side event of the timeline.
type entry struct {
	side    string // "" for frames, else clock, worker, gap or reset
	later   bool   // a later clock line of the recording: the session state once playback passes it (play)
	data    json.RawMessage
	cursor  int64 // the event cursor (frames)
	obj     map[string]json.RawMessage
	session int64
	video   string
	g       int64
	t       float64
	rev     int64
	wv      *worldView // the parsed world layer (worldOf)
}

const fakeEpoch = 1794950000.0

// New makes a Fake.
func New(opts ...Option) (*Fake, error) {
	cfg := &config{replies: map[string]Reply{}, boot: "fakeboot01"}
	for _, o := range opts {
		o(cfg)
	}
	f := &Fake{replies: map[string]Reply{}, boot: cfg.boot, changed: make(chan struct{}), now: fakeEpoch,
		faults: map[string][]Reply{}, drops: map[string]int{}, clock: cfg.clock}
	if f.clock != nil {
		f.clock0 = f.clock.Now()
	}
	if cfg.repliesDir != "" {
		if err := f.loadReplies(cfg.repliesDir); err != nil {
			return nil, err
		}
	}
	f.given = map[string]bool{}
	for k, v := range cfg.replies {
		f.replies[k] = v
		f.given[k] = true
	}
	lines := cfg.lines
	switch {
	case cfg.sample:
		rl, err := linesOf(io.NopCloser(bytes.NewReader(sampleRecording)), SampleRecording)
		if err != nil {
			return nil, err
		}
		lines = append(rl, lines...)
	case cfg.recording != "":
		rc, err := pfr.Open(context.Background(), cfg.recording, nil, "")
		if err != nil {
			return nil, fmt.Errorf("factorytest: %w", err)
		}
		rl, err := linesOf(rc, cfg.recording)
		if err != nil {
			return nil, err
		}
		lines = append(rl, lines...)
	}
	f.loadRecording(lines, cfg.live)
	f.handler = http.HandlerFunc(f.serve)
	return f, nil
}

// linesOf reads the lines of a recording (gzip or not) and closes it.
func linesOf(r io.ReadCloser, name string) ([][]byte, error) {
	rc, err := pfr.Decompress(r)
	if err != nil {
		return nil, fmt.Errorf("factorytest: %s: %w", name, err)
	}
	defer rc.Close()
	lr := pfr.NewReader(rc)
	var lines [][]byte
	for {
		l, err := lr.Next()
		if errors.Is(err, io.EOF) {
			return lines, nil
		}
		if err != nil {
			return nil, fmt.Errorf("factorytest: %s: %w", name, err)
		}
		lines = append(lines, l)
	}
}

type indexEntry struct {
	File        string            `json:"file"`
	Status      int               `json:"status"`
	ContentType string            `json:"content_type"`
	Headers     map[string]string `json:"headers"`
}

func (f *Fake) loadReplies(dir string) error {
	b, err := os.ReadFile(filepath.Join(dir, "INDEX.json"))
	if err != nil {
		return fmt.Errorf("factorytest: replies: %w", err)
	}
	var index map[string]indexEntry
	if err := json.Unmarshal(b, &index); err != nil {
		return fmt.Errorf("factorytest: replies: INDEX.json: %w", err)
	}
	for name, e := range index {
		body, err := os.ReadFile(filepath.Join(dir, e.File))
		if err != nil {
			return fmt.Errorf("factorytest: replies: %w", err)
		}
		f.replies[name] = Reply{Status: e.Status, ContentType: e.ContentType, Headers: e.Headers, Body: body}
	}
	return nil
}

func decodeObject(b []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func num(m map[string]json.RawMessage, key string) (float64, bool) {
	raw, ok := m[key]
	if !ok {
		return 0, false
	}
	var v *float64
	if json.Unmarshal(raw, &v) != nil || v == nil {
		return 0, false
	}
	return *v, true
}

func str(m map[string]json.RawMessage, key string) string {
	var s string
	if raw, ok := m[key]; ok {
		_ = json.Unmarshal(raw, &s)
	}
	return s
}

func (f *Fake) loadRecording(lines [][]byte, live bool) {
	var clock json.RawMessage
	var firstFrame *entry
	for _, line := range lines {
		l, err := pfr.Classify(line)
		if err != nil {
			continue
		}
		obj, _ := decodeObject(line) // an object: Classify parsed it
		switch {
		case l.Recording && f.header == nil:
			var h map[string]any
			_ = json.Unmarshal(line, &h)
			f.header = h
		case l.IsSide:
			if l.SSE == "clock" || l.SSE == "worker" {
				f.tl = append(f.tl, &entry{side: l.SSE, data: l.Data, later: l.SSE == "clock" && clock != nil})
				if l.SSE == "worker" {
					f.worker = l.Data
				} else if clock == nil {
					clock = l.Data // the state at the start: a later line is the state once playback passes it (play)
				}
			} else if l.SSE == "gap" || l.SSE == "reset" {
				// frames the stream dropped, a restart: sent in their place (the tray rules read them, 4.6.1)
				if m, err := decodeObject(l.Data); err == nil && m != nil {
					f.tl = append(f.tl, &entry{side: l.SSE, data: l.Data})
				}
			}
		case l.IsFrame:
			e := frameEntry(obj)
			if e.cursor <= f.cursor {
				e.cursor = f.cursor + 1
			}
			f.cursor = e.cursor
			if e.session > f.session {
				f.session = e.session
			}
			f.tl = append(f.tl, e)
			f.rec = append(f.rec, e)
			if firstFrame == nil {
				firstFrame = e
			}
		}
	}
	f.hasRec = firstFrame != nil || f.header != nil
	f.buildCatalog(firstFrame, live)
	if clock == nil {
		clock = f.replyClock()
	}
	f.st = f.initialState(clock, firstFrame)
	if clock != nil {
		var c struct {
			ServerNow *float64 `json:"server_now"`
		}
		if json.Unmarshal(clock, &c) == nil && c.ServerNow != nil {
			f.now = *c.ServerNow
		}
	}
	if live {
		f.offerCamera(firstFrame)
	}
	f.rate = f.st.Rate
	if f.isLiveVideo(f.st.Video) {
		f.rate = 0.8 // the fake starts a recording selected after a camera at its default rate
	}
	if f.st.Session > f.session {
		f.session = f.st.Session
	}
}

func frameEntry(obj map[string]json.RawMessage) *entry {
	e := &entry{obj: obj, video: str(obj, "video")}
	if v, ok := num(obj, "cursor"); ok {
		e.cursor = int64(v)
	}
	if v, ok := num(obj, "session"); ok {
		e.session = int64(v)
	}
	if v, ok := num(obj, "g"); ok {
		e.g = int64(v)
	}
	e.t, _ = num(obj, "t")
	if w, ok := obj["world"]; ok {
		var wr struct {
			Rev int64 `json:"rev"`
		}
		_ = json.Unmarshal(w, &wr)
		e.rev = wr.Rev
	}
	return e
}

// replyClock is the state of a recorded getSession reply as a clock event, for a fake without a recording.
func (f *Fake) replyClock() json.RawMessage {
	rep, ok := f.replies["getSession"]
	if !ok {
		return nil
	}
	var s struct {
		State     map[string]any `json:"state"`
		ServerNow float64        `json:"server_now"`
	}
	if json.Unmarshal(rep.Body, &s) != nil || s.State == nil {
		return nil
	}
	s.State["server_now"] = s.ServerNow
	b, _ := json.Marshal(s.State)
	return b
}

func (f *Fake) buildCatalog(first *entry, live bool) {
	var src map[string]any
	if f.header != nil {
		src, _ = f.header["source"].(map[string]any)
	}
	if src == nil && first != nil && first.video != "" {
		src = map[string]any{"id": first.video, "title": first.video, "duration": nil, "fps": 10.0, "frames": nil,
			"grid_step": 2, "default_start": 0.0, "hold": 2.2, "proxy": "/api/v2/videos/" + first.video + "/media",
			"proxy_wh": []int{960, 540}, "native_wh": []int{1920, 1080}}
	}
	if src != nil {
		f.recSrc, _ = src["id"].(string)
		f.sources = append(f.sources, src)
	}
	if rep, ok := f.replies["listVideos"]; ok && src == nil {
		var cat struct {
			Videos []map[string]any `json:"videos"`
		}
		if json.Unmarshal(rep.Body, &cat) == nil {
			f.sources = append(f.sources, cat.Videos...)
		}
	}
	if live {
		var like map[string]any
		if len(f.sources) > 0 {
			like = f.sources[0]
		}
		f.sources = append(f.sources, cameraSource(like)) // after the recordings, as the unit lists its cameras
	}
}

func (f *Fake) source(id string) map[string]any {
	for _, s := range f.sources {
		if s["id"] == id {
			return s
		}
	}
	return nil
}

// ---- running the fake -----------------------------------------------------------------------------------------------

// URL is the base URL to give pdiggerfactory.New with Transport (any host works with the in-process transport).
func (f *Fake) URL() string { return "http://factory.test" }

// Handler is the fake as an http.Handler.
func (f *Fake) Handler() http.Handler { return f.handler }

// Transport is the fake as an in-process http.RoundTripper: no sockets; streams are delivered as they are written
// and a closed body or cancelled request ends the handler.
func (f *Fake) Transport() http.RoundTripper { return &transport{h: f.handler} }

// Client is an *http.Client over Transport.
func (f *Fake) Client() *http.Client { return &http.Client{Transport: f.Transport()} }

// Start serves the fake on a loopback httptest server and returns its URL; Close stops it.
func (f *Fake) Start() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.server == nil {
		f.server = httptest.NewServer(f.handler)
	}
	return f.server.URL
}

// Close stops the httptest server, if started.
func (f *Fake) Close() {
	f.mu.Lock()
	s := f.server
	f.server = nil
	f.mu.Unlock()
	if s != nil {
		s.CloseClientConnections()
		s.Close()
	}
}

// ---- fault injection and inspection -----------------------------------------------------------------------------------

// FailNext answers the next n requests of an operation (an operationId, such as "getFrame") with r.
func (f *Fake) FailNext(op string, n int, r Reply) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := 0; i < n; i++ {
		f.faults[op] = append(f.faults[op], r)
	}
}

// ReplyNext answers the next n requests of an operation with a recorded reply by key (such as "getFrame-404").
func (f *Fake) ReplyNext(op string, n int, key string) error {
	f.mu.Lock()
	r, ok := f.replies[key]
	f.mu.Unlock()
	if !ok {
		return fmt.Errorf("factorytest: no reply %q", key)
	}
	f.FailNext(op, n, r)
	return nil
}

// DropAfter makes the next stream of an operation ("streamFrames" or "rawStream") drop its connection after k
// events.
func (f *Fake) DropAfter(op string, k int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.drops[op] = k
}

// HoldFinishedStreams makes a stream that reached its limit (or max_s, which the fake counts as five frames a
// second) send nothing more but stay open until the client closes it, as the 20.20 unit does: it closes a finished
// stream only about 10 s later. Off by default (the fake ends a finished stream at once).
func (f *Fake) HoldFinishedStreams(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hold = on
}

// InjectGap makes the next stream start with a gap event of n skipped items.
func (f *Fake) InjectGap(n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, _ := json.Marshal(map[string]int64{"gap": n})
	f.injects = append(f.injects, &entry{side: "gap", data: b})
}

// InjectReset makes the next stream start with a reset event (as for a Last-Event-ID of another boot).
func (f *Fake) InjectReset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, _ := json.Marshal(map[string]any{"boot": f.boot, "cursor": f.cursor})
	f.injects = append(f.injects, &entry{side: "reset", data: b})
}

// SetDelay sets a hook called before every answer (it may block; nothing sleeps by default).
func (f *Fake) SetDelay(fn func(op string, r *http.Request)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.delay = fn
}

// Requests are the requests answered so far.
func (f *Fake) Requests() []Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Request(nil), f.reqs...)
}

// Ops are the operation ids of the requests answered so far.
func (f *Fake) Ops() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.reqs))
	for i, r := range f.reqs {
		out[i] = r.Op
	}
	return out
}

// Advance moves the fake's wall clock forward by seconds (the playhead moves when the session plays, and a camera
// session takes the phases whose time has come).
func (f *Fake) Advance(seconds float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now += seconds
	f.advance()
}

// Now is the fake's wall clock (epoch seconds).
func (f *Fake) Now() float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.wall()
}

// wall is the fake's wall clock now, epoch seconds: where it started, moved by Advance (and by streams that waited
// for a camera's phases), plus the time its clock has run since the fake was made. The caller holds f.mu.
func (f *Fake) wall() float64 {
	if f.clock == nil {
		return f.now
	}
	return f.now + f.clock.Now().Sub(f.clock0).Seconds()
}

// SessionState is the fake's shared session state (a camera session first takes the phases whose time has come).
func (f *Fake) SessionState() (session int64, video string, playing bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.advance()
	return f.st.Session, f.st.Video, f.st.Playing
}

// LivePhase is the phase of the fake's camera session (connecting, warming or live; "" on a recording), after the
// phases whose time has come.
func (f *Fake) LivePhase() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.advance()
	if f.st.Live == nil {
		return ""
	}
	return f.st.Live.Phase
}

// Boot is the fake's boot id.
func (f *Fake) Boot() string { return f.boot }

// ReplyKeys are the keys of the recorded replies, sorted.
func (f *Fake) ReplyKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	keys := make([]string, 0, len(f.replies))
	for k := range f.replies {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// appendTimeline adds entries and wakes the streams. The caller holds f.mu.
func (f *Fake) appendTimeline(es ...*entry) {
	f.tl = append(f.tl, es...)
	close(f.changed)
	f.changed = make(chan struct{})
}

// reply picks a recorded reply for an operation: the 2xx variant whose suffix appears in the request (a query
// value or a path segment, as in getFrame-latest or renderScene3d-compact), else the key itself, else the first 2xx
// variant.
func (f *Fake) reply(op string, r *http.Request) (Reply, bool) {
	return f.pick(op, r, false)
}

// givenReply picks a reply given with WithReplyMap for an operation, as reply does, but never a variant that the
// request does not name: a reply given by key answers an operation that the recording would otherwise answer.
func (f *Fake) givenReply(op string, r *http.Request) (Reply, bool) {
	return f.pick(op, r, true)
}

func (f *Fake) pick(op string, r *http.Request, given bool) (Reply, bool) {
	var keys []string
	for k, rep := range f.replies {
		if strings.HasPrefix(k, op+"-") && rep.Status < 300 && (!given || f.given[k]) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	words := map[string]bool{}
	for _, vs := range r.URL.Query() {
		for _, v := range vs {
			for _, w := range strings.Split(v, ",") {
				words[w] = true
			}
		}
	}
	for _, seg := range strings.Split(r.URL.Path, "/") {
		words[strings.TrimSuffix(seg, ".png")] = true
	}
	for _, k := range keys {
		if words[strings.TrimPrefix(k, op+"-")] {
			return f.replies[k], true
		}
	}
	if rep, ok := f.replies[op]; ok && (!given || f.given[op]) {
		return rep, true
	}
	if len(keys) == 0 || given {
		return Reply{}, false
	}
	return f.replies[keys[0]], true
}
