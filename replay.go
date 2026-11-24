package pdiggerfactory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"net/http"
	"sync"
	"time"

	"github.com/eprojac/pdigger-factory-sdk-go/internal/pfr"
)

// RecordingHeader is the first line of a recording (format pfr1): its first line that is not blank, when that has
// "recording" and decodes (section 6 of the SDK design).
type RecordingHeader struct {
	Recording    string   `json:"recording"`
	Name         string   `json:"name,omitempty"`
	Video        *string  `json:"video"`
	Source       *Source  `json:"source,omitempty"`
	Build        string   `json:"build,omitempty"`
	API          string   `json:"api,omitempty"`
	Model        string   `json:"model,omitempty"`
	Backend      string   `json:"backend,omitempty"`
	Layers       []string `json:"layers,omitempty"`
	Redacted     []string `json:"redacted,omitempty"`
	RecordedFrom string   `json:"recorded_from,omitempty"`
	RecordedAt   string   `json:"recorded_at,omitempty"`
	Frames       int64    `json:"frames"`
	GFirst       *int64   `json:"g_first"`
	GLast        *int64   `json:"g_last"`
	Sessions     []int64  `json:"sessions,omitempty"`
	// BackfilledLayers is how many frames had late layers read back and merged in; FramesStillMissing how many
	// still miss a layer; HolesFilled how many frames the recorder read back after a gap; FramesLost how many it
	// could not get at all.
	BackfilledLayers   *int64 `json:"backfilled_layers,omitempty"`
	FramesStillMissing *int64 `json:"frames_still_missing,omitempty"`
	HolesFilled        *int64 `json:"holes_filled,omitempty"`
	FramesLost         *int64 `json:"frames_lost,omitempty"`
	// Complete says why and when the recording of the video ended (nil for an excerpt that stops before it).
	Complete *RecordingEnd `json:"complete,omitempty"`
	Notes    string        `json:"notes,omitempty"`
	// Raw is the header line as recorded.
	Raw json.RawMessage `json:"-"`
}

// RecordingEnd is why and when the recording of a video ended.
type RecordingEnd struct {
	// Why is "end-of-video" (the session went on to Next) or "max-media" (the recorder had its media time).
	Why string `json:"why"`
	// At is the recorder's wall time, epoch seconds.
	At float64 `json:"at"`
	// Next is the video the session played next; LastG and LastT the last frame recorded (end-of-video only).
	Next  string   `json:"next,omitempty"`
	LastG *int64   `json:"last_g,omitempty"`
	LastT *float64 `json:"last_t,omitempty"`
}

// Pace is how fast a Replay delivers frames.
type Pace int

const (
	// PaceNone delivers frames as fast as they are read (the default).
	PaceNone Pace = iota
	// PaceRealtime delivers frames at their media times divided by Speed.
	PaceRealtime
)

// ReplayOptions configure a Replay.
type ReplayOptions struct {
	// HTTPClient reads http(s) recordings (default http.DefaultClient); the requests carry the SDK's User-Agent.
	HTTPClient *http.Client
	// Pace and Speed (default 1.0) pace the iterators.
	Pace  Pace
	Speed float64
	// Clock is the clock of realtime pacing (default SystemClock).
	Clock Clock
}

// Replay runs the Interpreter over a recording file: a local .ndjson or .ndjson.gz path or an http(s) URL (read as
// a stream, never loaded whole), such as a published recording. Every iteration reads the recording from the start
// with a new InterpreterCore, under the context it is given: cancelling it ends the iteration at once, even in a read
// that waits on a stalled connection, and closes the connection. Close the Replay when done.
//
// Errors are *Error: transport when the recording cannot be opened or read (the file or the connection failed, or
// the context ended), not_found for a URL that answers 404 (another error status keeps its status, with the API's
// code when the reply has an API error body, else decode), and decode for bytes that are not a recording (a gzip
// stream that is corrupt or cut short).
type Replay struct {
	src  string
	opts ReplayOptions
	// Header is the recording's header (nil for a file without one).
	Header *RecordingHeader

	mu      sync.Mutex
	pending *replaySource
	skipped int64
}

type replaySource struct {
	src *pfr.Source
	r   *pfr.Reader
	// first is the first line that is not blank, read by OpenReplay: the first iteration reads it again
	first []byte
}

func (s *replaySource) Close() error { return s.src.Close() }

// OpenReplay opens a recording and reads its header. ctx bounds the opening and the header only: every iteration
// reads under the context it is given, and ending ctx after OpenReplay has returned changes nothing.
func OpenReplay(ctx context.Context, src string, opts *ReplayOptions) (*Replay, error) {
	r := &Replay{src: src}
	if opts != nil {
		r.opts = *opts
	}
	if r.opts.Speed <= 0 {
		r.opts.Speed = 1
	}
	if r.opts.Clock == nil {
		r.opts.Clock = SystemClock{}
	}
	s, err := r.open(ctx)
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(ctx, s.src.Abort)
	line, err := s.r.Next()
	if !stop() { // ctx ended while the header was read
		s.Close()
		return nil, transportError("replay", ctx, ctx.Err())
	}
	switch {
	case errors.Is(err, io.EOF):
	case err != nil:
		s.Close()
		return nil, r.readError(ctx, s, err)
	default:
		_, r.Header, _ = parseLine(line, true)
		s.first = line
	}
	r.pending = s
	return r, nil
}

func (r *Replay) open(ctx context.Context) (*replaySource, error) {
	src, err := pfr.Open(ctx, r.src, r.opts.HTTPClient, UserAgent)
	var se *pfr.StatusError
	var fe *pfr.FormatError
	switch {
	case errors.As(err, &se):
		return nil, statusError(r.src, se, r.opts.Clock.Now())
	case err != nil && ctx.Err() != nil:
		return nil, transportError("replay", ctx, err)
	case errors.As(err, &fe):
		return nil, &Error{Op: "replay", Code: CodeDecode, Message: r.src + " is not a readable recording", Err: err}
	case err != nil:
		return nil, &Error{Op: "replay", Code: CodeTransport, Message: "cannot open " + r.src, Err: err}
	}
	return &replaySource{src: src, r: pfr.NewReader(src)}, nil
}

// statusError is the error of a recording URL that answered a status outside 2xx: the API's error when the
// reply has an API error body, else the code of its status (not_found for a 404), with the status.
func statusError(src string, se *pfr.StatusError, now time.Time) *Error {
	e := readError("replay", &http.Response{StatusCode: se.Status, Header: se.Header,
		Body: io.NopCloser(bytes.NewReader(se.Body))}, now)
	e.Message = "cannot open " + src + ": " + e.Message
	return e
}

// readError classifies a read of the recording that failed: the context ended (transport, not retryable), the file
// or the connection failed (transport), or the bytes are not a recording (decode: a gzip stream that is corrupt or
// cut short).
func (r *Replay) readError(ctx context.Context, s *replaySource, err error) *Error {
	if ctx.Err() != nil {
		return transportError("replay", ctx, err)
	}
	if rerr := s.src.ReadErr(); rerr != nil {
		return &Error{Op: "replay", Code: CodeTransport, Message: "cannot read " + r.src, Err: rerr}
	}
	return &Error{Op: "replay", Code: CodeDecode, Message: r.src + " is not a readable recording", Err: err}
}

// take returns the source opened by OpenReplay for the first iteration, then a fresh one for every other.
func (r *Replay) take(ctx context.Context) (*replaySource, error) {
	r.mu.Lock()
	s := r.pending
	r.pending = nil
	r.mu.Unlock()
	if s != nil {
		return s, nil
	}
	return r.open(ctx)
}

// Close releases the recording.
func (r *Replay) Close() error {
	r.mu.Lock()
	s := r.pending
	r.pending = nil
	r.mu.Unlock()
	if s != nil {
		return s.Close()
	}
	return nil
}

// Skipped is how many lines of a damaged file the iterations so far skipped as not decoding (section 6): lines that
// are not a JSON object (not JSON, or JSON of another type), a first line with "recording" that is not a header that
// decodes, frame lines (with "g" and "cursor") that do not decode, and side lines (with "sse") whose sse is not a
// string, whose recv is not a number, or whose data does not decode as the event the SDK knows. The other lines a
// reader does not understand are ignored without being counted: blank lines, objects with neither "sse" nor both "g"
// and "cursor" (a line with "recording" after the first, say), and side lines of an event the SDK does not know.
func (r *Replay) Skipped() int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.skipped
}

// Items yields the recording's frames as FrameItems and its side lines as ClockItem, WorkerItem, GapItem and
// ResetItem, in the order recorded. Lines it does not understand are skipped (see Skipped).
func (r *Replay) Items(ctx context.Context) iter.Seq2[StreamItem, error] {
	return func(yield func(StreamItem, error) bool) {
		s, err := r.take(ctx)
		if err != nil {
			yield(nil, err)
			return
		}
		defer s.Close()
		// a read waiting on a stalled connection ends when ctx does: Abort cancels the request (or closes the file)
		defer context.AfterFunc(ctx, s.src.Abort)()
		pacer := newPacer(r.opts)
		first := true // the first line that is not blank: the only one that can be the header
		next := func() ([]byte, error) {
			if s.first != nil {
				b := s.first
				s.first = nil
				return b, nil
			}
			return s.r.Next()
		}
		for {
			if err := ctx.Err(); err != nil {
				yield(nil, transportError("replay", ctx, err))
				return
			}
			line, err := next()
			if err != nil {
				if !errors.Is(err, io.EOF) || ctx.Err() != nil {
					yield(nil, r.readError(ctx, s, err))
				}
				return
			}
			item, _, err := parseLine(line, first)
			first = false
			if err != nil {
				r.mu.Lock()
				r.skipped++
				r.mu.Unlock()
				continue
			}
			if item == nil { // the header, or a line a reader ignores
				continue
			}
			if fi, ok := item.(*FrameItem); ok {
				if err := pacer.wait(ctx, fi.Frame); err != nil {
					yield(nil, transportError("replay", ctx, err))
					return
				}
			}
			if !yield(item, nil) {
				return
			}
		}
	}
}

// parseLine reads a line of a recording by its keys (section 6 of the SDK design); bytes that are not UTF-8 read as
// U+FFFD. first is true for the first line that is not blank, the only one that can be the header: there a line with
// "recording" is the header, and one that does not decode is skipped, never a frame. Otherwise a line with "sse" is
// a side line (sse a string, recv a number when it is there, and data of its event's shape: 2.3 rule 9) and one with
// "g" and "cursor" is a frame, which decodes as a frame event does. parseLine gives the header, or the line's frame
// or side item; nothing for the lines a reader ignores without counting them (an object with neither "sse" nor both
// "g" and "cursor", a side line of an event the SDK does not know); and a decode error for a line that is not a JSON
// object, a header, side line or frame that does not decode (Skipped counts those).
func parseLine(line []byte, first bool) (StreamItem, *RecordingHeader, error) {
	fields, why := objectFields(line)
	if why != "" {
		return nil, nil, &Error{Op: "decode", Code: CodeDecode, Message: "a recording line that is " + why}
	}
	if _, ok := fields["recording"]; ok && first {
		h, err := decodeHeader(line)
		return nil, h, err
	}
	if _, ok := fields["sse"]; ok {
		var name string
		if sse, ok := member(fields, "sse"); !ok || json.Unmarshal(sse, &name) != nil {
			return nil, nil, &Error{Op: "decode", Code: CodeDecode, Message: "a side line whose sse is not a string"}
		}
		if recv, ok := member(fields, "recv"); ok && !isNumber(recv) {
			return nil, nil, &Error{Op: "decode", Code: CodeDecode, Message: "a side line whose recv is not a number"}
		}
		item, err := sideItem(name, fields["data"])
		return item, nil, err
	}
	_, hasG := fields["g"]
	_, hasCursor := fields["cursor"]
	if !hasG || !hasCursor {
		return nil, nil, nil
	}
	var f Frame
	if err := decodeData(line, "a frame line", frameShape, &f); err != nil {
		return nil, nil, err
	}
	return &FrameItem{Frame: &f, Cursor: f.Cursor}, nil, nil
}

// decodeHeader is the header line b, or a decode error when it does not decode by its table (section 6); a member the
// header cannot hold is left out of it, and so is a source that is no catalog entry.
func decodeHeader(b []byte) (*RecordingHeader, error) {
	// these hide the header's members: each is decoded on its own, and one the header cannot hold is left out of it,
	// so that it never keeps the line from being the header (spec 6)
	var line struct {
		RecordingHeader
		Source             json.RawMessage `json:"source"`
		Layers             json.RawMessage `json:"layers"`
		Redacted           json.RawMessage `json:"redacted"`
		Sessions           json.RawMessage `json:"sessions"`
		Frames             json.RawMessage `json:"frames"`
		GFirst             json.RawMessage `json:"g_first"`
		GLast              json.RawMessage `json:"g_last"`
		BackfilledLayers   json.RawMessage `json:"backfilled_layers"`
		FramesStillMissing json.RawMessage `json:"frames_still_missing"`
		HolesFilled        json.RawMessage `json:"holes_filled"`
		FramesLost         json.RawMessage `json:"frames_lost"`
		Complete           json.RawMessage `json:"complete"`
	}
	if err := decodeData(b, "a header", headerShape, &line); err != nil {
		return nil, err
	}
	h := line.RecordingHeader
	if sourceShape.problem(line.Source) == "" { // a source that is no catalog entry is left out
		keepMember(line.Source, &h.Source)
	}
	keepMember(line.Layers, &h.Layers)
	keepMember(line.Redacted, &h.Redacted)
	keepMember(line.Sessions, &h.Sessions)
	keepMember(line.Frames, &h.Frames)
	keepMember(line.GFirst, &h.GFirst)
	keepMember(line.GLast, &h.GLast)
	keepMember(line.BackfilledLayers, &h.BackfilledLayers)
	keepMember(line.FramesStillMissing, &h.FramesStillMissing)
	keepMember(line.HolesFilled, &h.HolesFilled)
	keepMember(line.FramesLost, &h.FramesLost)
	keepMember(line.Complete, &h.Complete)
	h.Raw = append(json.RawMessage(nil), b...)
	return &h, nil
}

// keepMember decodes a header member into v; v keeps its zero value when the member is missing, null or does not fit.
func keepMember[T any](raw json.RawMessage, v *T) {
	var x T
	if len(raw) > 0 && json.Unmarshal(raw, &x) == nil {
		*v = x
	}
}

// Frames yields the recording's frames, without its side lines: to feed an InterpreterCore of your own, range over
// Items instead and give it the ClockItems (SetClock) and the GapItems and ResetItems (Gap) too.
func (r *Replay) Frames(ctx context.Context) iter.Seq2[*Frame, error] {
	return func(yield func(*Frame, error) bool) {
		for item, err := range r.Items(ctx) {
			if err != nil {
				yield(nil, err)
				return
			}
			if fi, ok := item.(*FrameItem); ok && !yield(fi.Frame, nil) {
				return
			}
		}
	}
}

// Updates feeds a new InterpreterCore with the recording (clock lines give the wall times; gap and reset lines go to
// InterpreterCore.Gap, as a live stream's events do) and yields an Update per frame (duplicates skipped), then a Final
// update (Frame nil) with the activities still open and the facts of the last tray leaving.
func (r *Replay) Updates(ctx context.Context) iter.Seq2[*Update, error] {
	return func(yield func(*Update, error) bool) {
		r.run(ctx, NewInterpreterCore(), yield)
	}
}

func (r *Replay) run(ctx context.Context, core *InterpreterCore, yield func(*Update, error) bool) bool {
	for item, err := range r.Items(ctx) {
		if err != nil {
			yield(nil, err)
			return false
		}
		switch it := item.(type) {
		case *ClockItem:
			core.SetClock(it.clockEvent())
		case *GapItem, *ResetItem: // frames dropped here; a line that does not decode is skipped (Items)
			core.Gap()
		case *FrameItem:
			u := core.Feed(it.Frame)
			if u.Duplicate {
				continue
			}
			if !yield(u, nil) {
				return false
			}
		}
	}
	return yield(core.End(), nil)
}

// Events yields the events of the recording, in order.
func (r *Replay) Events(ctx context.Context) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		for u, err := range r.Updates(ctx) {
			if err != nil {
				yield(Event{}, err)
				return
			}
			for _, e := range u.Events {
				if !yield(e, nil) {
					return
				}
			}
		}
	}
}

// Activities yields the activities of the recording in the order of section 4.3, ending with the ones still open
// at its end.
func (r *Replay) Activities(ctx context.Context) iter.Seq2[Activity, error] {
	return func(yield func(Activity, error) bool) {
		for u, err := range r.Updates(ctx) {
			if err != nil {
				yield(Activity{}, err)
				return
			}
			for _, a := range u.Activities {
				if !yield(a, nil) {
					return
				}
			}
		}
	}
}

// TrayFacts yields the tray facts of the recording, in order (section 4.6 of the SDK design): trays arriving and
// leaving, tray stones removed, set down or no longer sure.
func (r *Replay) TrayFacts(ctx context.Context) iter.Seq2[TrayFact, error] {
	return func(yield func(TrayFact, error) bool) {
		for u, err := range r.Updates(ctx) {
			if err != nil {
				yield(TrayFact{}, err)
				return
			}
			for _, f := range u.TrayFacts {
				if !yield(f, nil) {
					return
				}
			}
		}
	}
}

// Trays consumes the recording and returns every tray, oldest first, with its Arrived and Leaving points, its tray
// stones and its facts. Tray.Window is the span to replay or to show; on the shared session Session.Seek(ctx,
// tray.Arrived.T, ...) shows the arrival.
func (r *Replay) Trays(ctx context.Context) ([]Tray, error) {
	core := NewInterpreterCore()
	core.KeepTrays(0)
	if err := r.consume(ctx, core); err != nil {
		return nil, err
	}
	return core.Trays(), nil
}

// TrayReports consumes the recording and returns the report of every tray that arrived, oldest first (section 4.7 of
// the SDK design): which stones were removed, added and moved, and whether that is the tray's whole account.
func (r *Replay) TrayReports(ctx context.Context) ([]TrayReport, error) {
	trays, err := r.Trays(ctx)
	if err != nil {
		return nil, err
	}
	reports := []TrayReport{}
	for i := range trays {
		if rep := trays[i].Report(); rep != nil {
			reports = append(reports, *rep)
		}
	}
	return reports, nil
}

// consume feeds the core with the whole recording and keeps nothing else (the first error, if any).
func (r *Replay) consume(ctx context.Context, core *InterpreterCore) error {
	var ferr error
	r.run(ctx, core, func(_ *Update, err error) bool {
		ferr = err
		return err == nil
	})
	return ferr
}

// Result is everything a replay gives at once.
type Result struct {
	Events     []Event
	Activities []Activity
	TrayFacts  []TrayFact
	// Speckle are the unit's speckle answers read, in order (section 4.6.12 of the SDK design).
	Speckle []TraySpeckle
	// Trays are every tray of the recording, oldest first.
	Trays   []Tray
	Summary *Summary
}

// Run consumes the recording and returns its events, activities, tray facts, speckle answers, trays and summary.
func (r *Replay) Run(ctx context.Context) (*Result, error) {
	core := NewInterpreterCore()
	core.KeepTrays(0)
	res := &Result{}
	var ferr error
	r.run(ctx, core, func(u *Update, err error) bool {
		if err != nil {
			ferr = err
			return false
		}
		res.Events = append(res.Events, u.Events...)
		res.Activities = append(res.Activities, u.Activities...)
		res.TrayFacts = append(res.TrayFacts, u.TrayFacts...)
		res.Speckle = append(res.Speckle, u.Speckle...)
		return true
	})
	if ferr != nil {
		return nil, ferr
	}
	res.Summary = core.Summary()
	res.Trays = core.Trays()
	return res, nil
}

// Summary consumes the recording and returns its summary.
func (r *Replay) Summary(ctx context.Context) (*Summary, error) {
	core := NewInterpreterCore()
	if err := r.consume(ctx, core); err != nil {
		return nil, err
	}
	return core.Summary(), nil
}

// pacer sleeps so frames come at their media times divided by the speed.
type pacer struct {
	on      bool
	speed   float64
	clock   Clock
	anchorT float64
	anchorW time.Time
	started bool
	session int64
}

func newPacer(o ReplayOptions) *pacer {
	return &pacer{on: o.Pace == PaceRealtime, speed: o.Speed, clock: o.Clock}
}

func (p *pacer) wait(ctx context.Context, f *Frame) error {
	if !p.on {
		return nil
	}
	now := p.clock.Now()
	if !p.started || f.Session != p.session || f.T < p.anchorT {
		p.started, p.session, p.anchorT, p.anchorW = true, f.Session, f.T, now
		return nil
	}
	due := p.anchorW.Add(time.Duration((f.T - p.anchorT) / p.speed * float64(time.Second)))
	return p.clock.Sleep(ctx, due.Sub(now))
}
