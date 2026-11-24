package pdiggerfactory

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// wline is a frame line whose world has stone 7 and these events.
func wline(cursor, g int64, events string) string {
	return fline(cursor, 1, g, -1, `,"world":`+worldJSON(1, g, "", stone7, events))
}

// stone7 is stone 7 of a test world.
const stone7 = `{"id":7,"solid":{"pos":[0,0,0],"q":[0,0,0,1],"dims":[1,1,1]}}`

func TestInterpreterIterators(t *testing.T) {
	ctx := testCtx(t)
	f := linesFake(t, clockLine,
		wline(1, 1, `{"type":"new","id":7}`),
		wline(2, 2, `{"type":"lift","id":7,"side":"left"}`),
		wline(3, 4, ``),
		wline(4, 3, `{"type":"drop","id":7,"side":"left"}`), // older than g 4: the core drops it
		fline(5, 1, 5, -1, `,"world":5`),                    // does not decode: an error, and the stream goes on
		wline(6, 6, `{"type":"place","id":7,"side":"left"}`),
		wline(7, 7, `{"type":"lift","id":7,"side":"right"}`))
	c, _ := fakeClient(t, f)
	in := c.Interpreter(Resume(false))
	var errs, frames int
	for u, err := range in.Updates(ctx) {
		if err != nil {
			errs++
			continue
		}
		frames++
		if u.Frame.G == 7 {
			break
		}
	}
	if errs != 1 || frames != 5 {
		t.Fatalf("%d errors, %d frames", errs, frames)
	}
	st := in.State()
	if st.G != 7 || st.Wall == nil || len(st.OpenMoves) != 1 {
		t.Fatalf("%+v", st)
	}
	if s := in.Summary(); s.Moves.Placed != 1 || s.Events.Lift != 2 {
		t.Fatalf("%+v", s.Moves)
	}
	if out := in.Finish(); len(out) != 1 || out[0].Outcome != OutcomeInterrupted {
		t.Fatalf("%+v", out)
	}
	// Events and Activities, each from a fresh interpreter; an error is passed on, a break stops
	evs := 0
	for e, err := range c.Interpreter().Events(ctx) {
		if err != nil {
			continue
		}
		if evs++; e.Type == EventPlace {
			break
		}
	}
	if evs != 3 {
		t.Fatal(evs)
	}
	for a, err := range c.Interpreter().Activities(ctx) {
		if err != nil {
			continue
		}
		if a.Outcome != OutcomePlaced || a.Describe() != "0:00.4 Left hand lifts stone 7 and sets it down (0.8 s)" {
			t.Fatal(a.Describe())
		}
		break
	}
	for _, err := range c.Interpreter().Updates(ctx) {
		if err != nil {
			break
		}
	}
	for _, err := range c.Interpreter().Events(ctx) {
		if err != nil {
			break
		}
	}
	for _, err := range c.Interpreter().Activities(ctx) {
		if err != nil {
			break
		}
	}
}

// TestInterpreterFinalUpdate: a stream the server ends without resume ends with the final update, as a replay does;
// a loop that breaks or fails gets none, and the next loop goes on after the last frame.
func TestInterpreterFinalUpdate(t *testing.T) {
	ctx := testCtx(t)
	lines := []string{clockLine, wline(1, 1, `{"type":"new","id":7}`), wline(2, 2, `{"type":"lift","id":7,"side":"left"}`)}
	c, _ := fakeClient(t, linesFake(t, lines...))
	in := c.Interpreter(Resume(false), ConnectionLimit(2))
	var us []*Update
	for u, err := range in.Updates(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		us = append(us, u)
	}
	last := us[len(us)-1]
	if len(us) != 3 || !last.Final || last.Frame != nil || len(last.Activities) != 1 ||
		last.Activities[0].Outcome != OutcomeInterrupted || last.State.G != 2 {
		t.Fatalf("%d updates, the last %+v", len(us), last)
	}
	if u := in.End(); !u.Final || len(u.Activities) != 0 { // nothing is open any more
		t.Fatalf("%+v", u)
	}
	// a loop that breaks gets no final update; the next one goes on after the last frame fed
	in = c.Interpreter(Resume(false), ConnectionLimit(1))
	for u, err := range in.Updates(ctx) {
		if err != nil || u.Final {
			t.Fatal(err, u)
		}
		break
	}
	us = nil
	for u, err := range in.Updates(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		us = append(us, u)
	}
	if len(us) != 2 || us[0].Frame.G != 2 || !us[1].Final || len(us[1].Activities) != 1 {
		t.Fatalf("%+v", us)
	}
	// a stream that fails gets none
	f := linesFake(t, lines...)
	f.DropAfter("streamFrames", 2)
	c, _ = fakeClient(t, f)
	var errs, finals int
	for u, err := range c.Interpreter(Resume(false)).Updates(ctx) {
		if err != nil {
			errs++
		} else if u.Final {
			finals++
		}
	}
	if errs != 1 || finals != 0 {
		t.Fatal(errs, finals)
	}
}

// TestInterpreterFinalUpdateAfterABadEvent: a stream that ends right after an event that does not decode still ends
// with the final update (2.3 rule 9): the error is passed on, and the move still open ends as interrupted.
func TestInterpreterFinalUpdateAfterABadEvent(t *testing.T) {
	ctx := testCtx(t)
	f := linesFake(t, clockLine, wline(1, 1, `{"type":"new","id":7}`), wline(2, 2, `{"type":"lift","id":7,"side":"left"}`),
		fline(3, 1, 3, -1, `,"world":5`))
	c, _ := fakeClient(t, f)
	var got []string
	var final *Update
	for u, err := range c.Interpreter(Resume(false), ConnectionLimit(3)).Updates(ctx) {
		switch {
		case err != nil:
			got = append(got, "error "+AsError(err).Op)
		case u.Final:
			got, final = append(got, "final"), u
		default:
			got = append(got, fmt.Sprintf("g%d", u.Frame.G))
		}
	}
	if s := strings.Join(got, ","); s != "g1,g2,error decode,final" {
		t.Fatal(s)
	}
	if final.Frame != nil || len(final.Activities) != 1 || final.Activities[0].Outcome != OutcomeInterrupted {
		t.Fatalf("%+v", final)
	}
}

// TestWaitForPastAnEventThatDoesNotDecode: WaitFor waits on past an event that does not decode (2.3 rule 9), as the
// loops go on past it; so does a second WaitFor, whose stream resumes after the first one's frame, right before that
// event.
func TestWaitForPastAnEventThatDoesNotDecode(t *testing.T) {
	ctx := testCtx(t)
	f := linesFake(t, wline(1, 1, `{"type":"new","id":7}`), fline(2, 1, 2, -1, `,"world":5`),
		wline(3, 3, `{"type":"lift","id":7,"side":"left"}`))
	c, _ := fakeClient(t, f)
	if e, err := c.Interpreter().WaitFor(ctx, OfType(EventLift), 0); err != nil || e.G != 3 {
		t.Fatal(e, err)
	}
	in := c.Interpreter()
	if e, err := in.WaitFor(ctx, OfType(EventNew), 0); err != nil || e.G != 1 {
		t.Fatal(e, err)
	}
	if e, err := in.WaitFor(ctx, OfType(EventLift), 0); err != nil || e.G != 3 {
		t.Fatal(e, err)
	}
	if got := strings.Join(streamResumes(f, "streamFrames"), ","); got != ",,fakeboot01-1" {
		t.Fatal(got)
	}
}

func TestWaitFor(t *testing.T) {
	ctx := testCtx(t)
	f := linesFake(t, wline(1, 1, `{"type":"new","id":7}`), wline(2, 2, `{"type":"lift","id":7,"side":"right"}`))
	c, _ := fakeClient(t, f)
	e, err := c.Interpreter().WaitFor(ctx, OfType(EventLift), 0)
	if err != nil || e.Describe() != "0:00.4 Right hand lifts stone 7" || e.Stone == nil {
		t.Fatalf("%v %v", e, err)
	}
	if e, err = c.Interpreter().WaitFor(ctx, nil, time.Second); err != nil || e.Type != EventNew {
		t.Fatal(err)
	}
	// no match before the stream ends
	if _, err = c.Interpreter(Resume(false), ConnectionLimit(2)).WaitFor(ctx, AboutStone(9), 0); !HasCode(err, CodeStreamClosed) {
		t.Fatal(err)
	}
	// no match within the timeout
	if _, err = c.Interpreter().WaitFor(ctx, AboutStone(9), 5*time.Millisecond); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	// a stream error
	if _, err = c.Interpreter(Layers("depth")).WaitFor(ctx, nil, 0); !HasCode(err, CodeUnknownLayer) {
		t.Fatal(err)
	}
}

// TestInterpreterGoesOn: a later loop resumes after the last frame the interpreter was fed.
func TestInterpreterGoesOn(t *testing.T) {
	ctx := testCtx(t)
	f := linesFake(t, wline(1, 1, `{"type":"new","id":7}`), wline(2, 2, `{"type":"lift","id":7,"side":"left"}`),
		wline(3, 3, `{"type":"place","id":7,"side":"left"}`))
	c, _ := fakeClient(t, f)
	in := c.Interpreter()
	if _, err := in.WaitFor(ctx, OfType(EventLift), 0); err != nil {
		t.Fatal(err)
	}
	for e, err := range in.Events(ctx) {
		if err != nil || e.Type != EventPlace {
			t.Fatal(e, err)
		}
		break
	}
	var ids []string
	for _, r := range f.Requests() {
		if r.Op == "streamFrames" {
			ids = append(ids, r.Header.Get("Last-Event-ID"))
		}
	}
	if len(ids) != 2 || ids[0] != "" || ids[1] != "fakeboot01-2" {
		t.Fatal(ids)
	}
	// stone 7 never moved: one point, from the first frame
	if tr := in.Track(7); len(tr) != 1 || tr[0].G != 1 || tr[0].Pos != [3]float64{} {
		t.Fatal(tr)
	}
}

// TestInterpreterKeepsTheRestOfAFrame: a loop that stops inside a frame (WaitFor at its match, a break) leaves the
// rest of that frame, what it did not yield, to the next loop, which yields it before the frames after it.
func TestInterpreterKeepsTheRestOfAFrame(t *testing.T) {
	ctx := testCtx(t)
	world := func(cursor, g int64, events string) string {
		stones := stone7 + `,{"id":9,"solid":{"pos":[1,0,0],"q":[0,0,0,1],"dims":[1,1,1]}}`
		return fline(cursor, 1, g, -1, `,"world":`+worldJSON(1, g, "", stones, events))
	}
	f := linesFake(t, clockLine,
		world(1, 1, `{"type":"new","id":7},{"type":"new","id":9}`),
		world(2, 2, `{"type":"lift","id":7,"side":"left"}`),
		world(3, 3, `{"type":"place","id":7,"side":"left"},{"type":"push","id":9,"side":"right"}`),
		world(4, 4, `{"type":"confirm","id":9}`))
	c, _ := fakeClient(t, f)
	const move = "0:00.4 Left hand lifts stone 7 and sets it down (0.2 s)"
	// first takes the first n sentences of a loop
	first := func(n int, seq func(func(string, error) bool)) string {
		var out []string
		for s, err := range seq {
			if err != nil {
				t.Fatal(err)
			}
			if out = append(out, s); len(out) == n {
				break
			}
		}
		return strings.Join(out, "|")
	}
	events := func(in *Interpreter) func(func(string, error) bool) {
		return func(yield func(string, error) bool) {
			for e, err := range in.Events(ctx) {
				if !yield(e.Describe(), err) {
					return
				}
			}
		}
	}
	activities := func(in *Interpreter) func(func(string, error) bool) {
		return func(yield func(string, error) bool) {
			for a, err := range in.Activities(ctx) {
				if !yield(a.Describe(), err) {
					return
				}
			}
		}
	}
	waitFor := func(in *Interpreter, typ string) {
		if _, err := in.WaitFor(ctx, OfType(typ), 0); err != nil {
			t.Fatal(err)
		}
	}
	// WaitFor, then Events: the push of the place's frame comes first
	in := c.Interpreter()
	waitFor(in, EventPlace)
	if got := first(2, events(in)); got != "0:00.6 Right hand pushes stone 9|0:00.8 stone 9 is confirmed" {
		t.Fatal(got)
	}
	// WaitFor, then Activities: the move the place closed comes first; Finish closes the push burst
	in = c.Interpreter()
	waitFor(in, EventPlace)
	if got := first(1, activities(in)); got != move {
		t.Fatal(got)
	}
	if out := in.Finish(); len(out) != 1 || out[0].Describe() != "0:00.6 Right hand pushes stone 9 (1 push)" {
		t.Fatalf("%+v", out)
	}
	// WaitFor, then Updates: the first update is the rest of the place's frame, then the stream goes on
	in = c.Interpreter()
	waitFor(in, EventPlace)
	for u, err := range in.Updates(ctx) {
		if err != nil || u.Frame.G != 3 || len(u.Events) != 1 || u.Events[0].Type != EventPush ||
			len(u.Activities) != 1 || u.Activities[0].Kind != KindMove || u.State.G != 3 {
			t.Fatalf("%+v %v", u, err)
		}
		break
	}
	for u, err := range in.Updates(ctx) {
		if err != nil || u.Frame.G != 4 {
			t.Fatalf("%+v %v", u, err)
		}
		break
	}
	// an Activities loop that stops inside a frame leaves that frame's events
	in = c.Interpreter()
	if got := first(1, activities(in)); got != move {
		t.Fatal(got)
	}
	want := "0:00.6 Left hand places stone 7|0:00.6 Right hand pushes stone 9|0:00.8 stone 9 is confirmed"
	if got := first(3, events(in)); got != want {
		t.Fatal(got)
	}
	// WaitFor at the last event of its frame leaves the frame's activities to an Activities loop; an Events loop goes
	// on with the next frame
	in = c.Interpreter()
	waitFor(in, EventPush)
	if got := first(1, activities(in)); got != move {
		t.Fatal(got)
	}
	in = c.Interpreter()
	waitFor(in, EventPush)
	if got := first(1, events(in)); got != "0:00.8 stone 9 is confirmed" {
		t.Fatal(got)
	}
	// a frame that is used up leaves nothing
	in = c.Interpreter()
	waitFor(in, EventConfirm)
	if in.pending != nil {
		t.Fatalf("%+v", in.pending)
	}
}
