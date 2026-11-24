package pdiggerfactory

import (
	"context"
	"iter"
	"sync"
	"time"
)

// Interpreter follows the shared session (a recording, or a plant camera live) and turns its frames into typed events,
// activities, hand states and the stone inventory. Its streams use the layers world and cards, count as a viewer
// (unless Observe(true) or WithObserve), backfill late worlds, so events come complete and in order, and read the
// shared state on every connect, so wall times are known from the first frame.
//
// Loops can follow one another: WaitFor, then Events, Activities or Updates, and so on. A later loop goes on where the
// last one stopped, and misses nothing in between: it first yields the rest of the frame the last loop stopped in
// (the events and the activities that loop did not yield; after WaitFor, the events after the match and all of that
// frame's activities), then the frames after it (the stream resumes after the last frame fed, while the server still
// holds it). One loop at a time should consume an Interpreter; State and Summary may be read from any goroutine.
type Interpreter struct {
	c    *Client
	opts []StreamOption

	mu      sync.Mutex
	core    *InterpreterCore
	lastID  string  // the event id of the newest frame fed to the core
	pending *Update // the rest of the frame where the last loop stopped (what it did not yield), nil when none
}

// Interpreter makes an Interpreter over the client's frame stream. Options are those of Streams.Frames; the
// defaults are Layers(world, cards), Backfill(true) and ClockOnConnect(true).
func (c *Client) Interpreter(opts ...StreamOption) *Interpreter {
	all := append([]StreamOption{Layers(LayerWorld, LayerCards), Backfill(true), ClockOnConnect(true)}, opts...)
	return &Interpreter{c: c, opts: all, core: NewInterpreterCore()}
}

// Updates follows the session and yields an Update per frame: its events, the activities it completed and the
// state after it. The first update of a loop that follows another may be the rest of the frame that loop stopped in:
// only the events and activities it did not yield. Connection drops, streams the server ends and server restarts are
// handled inside the stream. An event that does not decode (2.3 rule 9: not JSON, or not the shape of its type) is
// yielded as an error (Op "decode") and the stream goes on; an error the stream cannot recover from (a reply such as
// 400 or 404, or a used-up MaxReconnects budget) is yielded and ends it.
// The loop also ends when it breaks or ctx is cancelled (then it yields the context's error). A stream the server
// ends with Resume(false) ends with the final update, as a replay does (Final, Frame nil: the activities still open
// as interrupted and the open tray's leaving); a loop that breaks, fails or is cancelled gets none.
func (in *Interpreter) Updates(ctx context.Context) iter.Seq2[*Update, error] {
	return func(yield func(*Update, error) bool) {
		in.mu.Lock()
		rest := in.pending
		in.pending = nil
		opts := in.opts[:len(in.opts):len(in.opts)]
		if in.lastID != "" {
			opts = append(opts, LastEventID(in.lastID))
		}
		in.mu.Unlock()
		if rest != nil && !yield(rest, nil) {
			return
		}
		failed := false // an error other than an event that does not decode ends the stream
		for item, err := range in.c.streams.Frames(ctx, opts...) {
			if err != nil {
				if e := AsError(err); e == nil || e.Op != "decode" {
					failed = true
				}
				if !yield(nil, err) {
					return
				}
				continue
			}
			switch it := item.(type) {
			case *ClockItem:
				in.mu.Lock()
				in.core.SetClock(it.clockEvent())
				in.mu.Unlock()
			case *GapItem, *ResetItem: // frames dropped here: the tray rules could not read them (4.6.1)
				in.mu.Lock()
				in.core.Gap()
				in.mu.Unlock()
			case *FrameItem:
				in.mu.Lock()
				u := in.core.Feed(it.Frame)
				in.lastID = it.ID
				in.mu.Unlock()
				if u.Duplicate {
					continue
				}
				u.IsBackfill = it.IsBackfill
				if !yield(u, nil) {
					return
				}
			}
		}
		if !failed && ctx.Err() == nil {
			yield(in.End(), nil)
		}
	}
}

// keep saves the rest of u, after its first events events, first activities activities and first facts tray facts,
// for the next loop.
func (in *Interpreter) keep(u *Update, events, activities, facts int) {
	rest := *u
	rest.Events = u.Events[events:]
	rest.Activities = u.Activities[activities:]
	rest.TrayFacts = u.TrayFacts[facts:]
	if len(rest.Events) == 0 && len(rest.Activities) == 0 && len(rest.TrayFacts) == 0 {
		return
	}
	in.mu.Lock()
	defer in.mu.Unlock()
	in.pending = &rest
}

// Events yields just the events, in world order. A loop that stops inside a frame leaves the rest of it (its later
// events and its activities) to the next loop.
func (in *Interpreter) Events(ctx context.Context) iter.Seq2[Event, error] {
	return func(yield func(Event, error) bool) {
		for u, err := range in.Updates(ctx) {
			if err != nil {
				if !yield(Event{}, err) {
					return
				}
				continue
			}
			for i, e := range u.Events {
				if !yield(e, nil) {
					in.keep(u, i+1, 0, 0)
					return
				}
			}
		}
	}
}

// Activities yields just the activities, as frames complete them. A loop that stops inside a frame leaves the rest of
// it (its later activities and its events) to the next loop.
func (in *Interpreter) Activities(ctx context.Context) iter.Seq2[Activity, error] {
	return func(yield func(Activity, error) bool) {
		for u, err := range in.Updates(ctx) {
			if err != nil {
				if !yield(Activity{}, err) {
					return
				}
				continue
			}
			for i, a := range u.Activities {
				if !yield(a, nil) {
					in.keep(u, 0, i+1, 0)
					return
				}
			}
		}
	}
}

// TrayFacts yields just the tray facts (section 4.6 of the SDK design): trays arriving and leaving, tray stones
// removed, set down or no longer sure. A loop that stops inside a frame leaves the rest of it to the next loop.
func (in *Interpreter) TrayFacts(ctx context.Context) iter.Seq2[TrayFact, error] {
	return func(yield func(TrayFact, error) bool) {
		for u, err := range in.Updates(ctx) {
			if err != nil {
				if !yield(TrayFact{}, err) {
					return
				}
				continue
			}
			for i, f := range u.TrayFacts {
				if !yield(f, nil) {
					in.keep(u, 0, 0, i+1)
					return
				}
			}
		}
	}
}

// TrayReports yields a report as each tray leaves (section 4.7 of the SDK design): which stones were removed, added
// and moved, and whether that is the tray's whole account. A tray leaves when the next one arrives, the session
// changes or the frames end. Errors come through as TrayFacts gives them.
func (in *Interpreter) TrayReports(ctx context.Context) iter.Seq2[TrayReport, error] {
	return func(yield func(TrayReport, error) bool) {
		for f, err := range in.TrayFacts(ctx) {
			if err != nil {
				if !yield(TrayReport{}, err) {
					return
				}
				continue
			}
			if f.Kind != TrayLeaving {
				continue
			}
			trays := in.Trays()
			for i := len(trays) - 1; i >= 0; i-- {
				if t := &trays[i]; t.Session == f.Session && t.N == f.Tray {
					if r := t.Report(); r != nil && !yield(*r, nil) {
						return
					}
					break
				}
			}
		}
	}
}

// WaitFor follows the session until an event matches (OfType, AboutStone, or any func(Event) bool; nil matches
// any event) and returns it. The rest of the matching event's frame (its later events and its activities) is left
// to the next loop. It waits on past an event that does not decode (2.3 rule 9), as the loops go on past it; another
// error ends the wait. A timeout above zero bounds the wait; the error then wraps context.DeadlineExceeded.
func (in *Interpreter) WaitFor(ctx context.Context, match EventMatch, timeout time.Duration) (Event, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	for ev, err := range in.Events(ctx) {
		if e := AsError(err); e != nil && e.Op == "decode" {
			continue
		}
		if err != nil {
			return Event{}, err
		}
		if match == nil || match(ev) {
			return ev, nil
		}
	}
	return Event{}, &Error{Op: "waitFor", Code: CodeStreamClosed, Message: "the stream ended without a match"}
}

// State is the state after the newest frame.
func (in *Interpreter) State() *InterpreterState {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.core.State()
}

// Summary is the summary so far (open activities are not counted until they end).
func (in *Interpreter) Summary() *Summary {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.core.Summary()
}

// Track is the path of a stone's solid in this session from the frames this interpreter was fed (see
// InterpreterCore.Track); the server's own track since the session began is API.GetStone.
func (in *Interpreter) Track(stoneID int64) []TrackPoint {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.core.Track(stoneID)
}

// Finish closes what is still open (moves as interrupted, push bursts ended) and returns those activities; call it
// when a program stops following. Activities a loop left unyielded (the rest of the frame it stopped in) are not
// among them: the next loop yields those. The open tray leaves too: its facts are in Trays (End returns them).
func (in *Interpreter) Finish() []Activity {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.core.Finish()
}

// End is Finish as the final update: the activities still open and the facts of the open tray leaving.
func (in *Interpreter) End() *Update {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.core.End()
}

// Trays are the trays so far, oldest first (see InterpreterCore.Trays): jump between their Arrived and Leaving points
// with Session.Seek on a recording.
func (in *Interpreter) Trays() []Tray {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.core.Trays()
}

// Speckle are the unit's speckle answers read so far, oldest first (see InterpreterCore.Speckle).
func (in *Interpreter) Speckle() []TraySpeckle {
	in.mu.Lock()
	defer in.mu.Unlock()
	return in.core.Speckle()
}
