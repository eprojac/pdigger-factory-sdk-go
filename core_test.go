package pdiggerfactory

import (
	"strings"
	"testing"
	"time"
)

// Synthetic frames for the interpreter core.

func wev(typ string, id int64, side string) WorldEvent {
	return WorldEvent{Type: typ, ID: Int64(id), Side: side}
}

func stoneAt(id int64, x, y float64) StoneView {
	return StoneView{ID: id, Solid: &Solid{Pos: []float64{x, y, 0}}}
}

func wframe(session, g int64, rev int64, stones []StoneView, events ...WorldEvent) *Frame {
	return &Frame{Cursor: g, Session: session, Video: "v", G: g, T: float64(g) / 5, World: &WorldLayer{Rev: rev,
		Stones: stones, Events: events}}
}

func describeAll(acts []Activity) string {
	var s []string
	for _, a := range acts {
		s = append(s, a.Describe())
	}
	return strings.Join(s, " | ")
}

func TestCoreMovesAndBursts(t *testing.T) {
	c := NewInterpreterCore()
	stones := []StoneView{stoneAt(1, 0, 0), stoneAt(2, 1, 1), {ID: 3}}
	moved := []StoneView{stoneAt(1, 3, 4), stoneAt(2, 1, 1), {ID: 3}}
	// no clock yet: no wall times
	u := c.Feed(wframe(7, 10, 0, stones, wev(EventLift, 1, HandLeft), wev(EventHinge, 2, HandRight),
		wev(EventLift, 3, "")))
	if len(u.Events) != 3 || u.Events[0].Wall != nil || u.Events[0].Stone == nil || u.Events[0].Stone.ID != 1 {
		t.Fatalf("%+v", u.Events)
	}
	c.SetClock(nil)
	c.SetClock(&ClockEvent{State: State{Session: 7, Playing: true, Rate: 0.8, AnchorWall: 1000, AnchorMedia: 0}})
	u = c.Feed(wframe(7, 11, 0, stones, wev(EventCarry, 1, HandRight), wev(EventLift, 1, HandLeft),
		wev(EventHinge, 2, ""), wev(EventCarry, 9, HandLeft)))
	if u.Events[0].Wall == nil || *u.Events[0].Wall != 1000+2.2/0.8 {
		t.Fatalf("wall %v", u.Events[0].Wall)
	}
	st := c.State()
	if len(st.OpenMoves) != 3 || st.OpenMoves[1].Hand != HandBoth || !st.OpenMoves[1].Carried ||
		!st.OpenMoves[2].Tipped || st.Stone(1) == nil || st.Stone(99) != nil || len(st.StoneIDs()) != 3 ||
		st.StoneIDs()[0] != 1 {
		t.Fatalf("state %+v", st.OpenMoves)
	}
	// stone 1 placed 5 units away (carried), stone 2 dropped, stone 3 removed, a gone with a delete in the same frame
	u = c.Feed(wframe(7, 12, 0, moved, wev(EventPlace, 1, HandLeft), wev(EventPlace, 2, HandRight),
		wev(EventDelete, 3, ""), wev(EventGone, 4, HandLeft), wev(EventDelete, 4, ""), wev(EventDelete, 5, "")))
	got := describeAll(u.Activities)
	want := "0:02.0 Both hands move stone 1 5.00 units in 0.4 s | 0:02.0 Right hand tips stone 2 and sets it down (0.4 s)" +
		" | 0:02.0 A hand lifts stone 3; it is removed after 0.4 s | 0:02.4 Left hand takes stone 4 away (0.0 s) (joined mid-move)"
	if got != want {
		t.Fatalf("activities\n%s\nwant\n%s", got, want)
	}
	// push bursts: joins within 1 s, a stale burst closes before the frame's events, pushes with a why do not count
	c.Feed(wframe(7, 13, 0, moved, wev(EventPush, 5, HandLeft), wev(EventPush, 5, HandLeft), wev(EventPush, 3, HandLeft),
		WorldEvent{Type: EventPush, Side: HandLeft}, WorldEvent{Type: EventPush, ID: Int64(1), Side: HandRight, Why: "course"},
		wev(EventPush, 8, "")))
	u = c.Feed(wframe(7, 20, 0, moved, wev(EventPush, 6, HandLeft)))
	if got := describeAll(u.Activities); got != "0:02.6 Left hand pushes stones 3 and 5 (4 pushes in 0.0 s)" {
		t.Fatalf("stale burst: %s", got)
	}
	// a frame without a world, then a revision whose push finds its burst stale and closes it in push()
	u = c.Feed(&Frame{Session: 7, Video: "v", G: 21, T: 4.2, Cards: &CardsLayer{Left: HandCard{Title: "Holding"}}})
	if u.State == nil || u.State.Hands.Left.Card != "Holding" || len(u.Events) != 0 {
		t.Fatal("frame without a world")
	}
	c.bursts[HandLeft].last.T = 0
	u = c.Feed(wframe(7, 20, 1, moved, wev(EventPush, 6, HandLeft), wev(EventPush, 7, HandLeft)))
	if got := describeAll(u.Activities); !u.IsRevision || got != "0:04.0 Left hand pushes stone 6 (1 push)" {
		t.Fatalf("revision push: %s", got)
	}
	c.Feed(wframe(7, 23, 0, moved, wev(EventLift, 2, HandLeft)))
	// a session change closes the open move (interrupted) and the open burst
	u = c.Feed(wframe(8, 1, 0, moved))
	if got := describeAll(u.Activities); !strings.Contains(got, "Left hand is still holding stone 2 (0.0 s so far)") ||
		!strings.Contains(got, "0:04.0 Left hand pushes stone 7 (1 push)") {
		t.Fatalf("session change: %s", got)
	}
	s := c.Summary()
	if s.Sessions != 2 || s.Gaps != 2 || s.MissingWorld != 1 || s.Moves.Total != 5 || s.Moves.Partial != 1 ||
		s.Moves.MeanDistance == nil || s.PushBursts.Left != 3 || s.Moves.Interrupted != 1 || s.Moves.Removed != 1 ||
		s.Moves.CarriedAway != 1 {
		t.Fatalf("summary\n%s", s.Describe())
	}
	if s.MediaSeconds != Round3(4.6-2.0) {
		t.Fatalf("media %v", s.MediaSeconds)
	}
}

func TestCoreRevisionsAndDuplicates(t *testing.T) {
	c := NewInterpreterCore()
	stones := []StoneView{stoneAt(1, 0, 0)}
	c.Feed(wframe(1, 5, 0, stones, wev(EventNew, 1, "")))
	c.Feed(wframe(1, 6, 0, stones))
	if u := c.Feed(wframe(1, 5, 0, stones)); !u.Duplicate {
		t.Fatal("duplicate")
	}
	if u := c.Feed(&Frame{Session: 1, G: 5}); !u.Duplicate {
		t.Fatal("an older frame without a world is a duplicate")
	}
	if u := c.Feed(wframe(1, 4, 1, stones)); !u.Duplicate {
		t.Fatal("a frame never seen below the newest is dropped")
	}
	// a revision of g 5: only new events come out
	u := c.Feed(wframe(1, 5, 1, stones, wev(EventNew, 1, ""), wev(EventConfirm, 1, "")))
	if !u.IsRevision || len(u.Events) != 1 || u.Events[0].Type != EventConfirm {
		t.Fatalf("revision %+v", u.Events)
	}
	// a revision of the newest frame takes its state
	u = c.Feed(wframe(1, 6, 2, []StoneView{stoneAt(1, 0, 0), stoneAt(2, 0, 0)}))
	if !u.IsRevision || len(u.State.Stones) != 2 {
		t.Fatal("revision of the newest frame")
	}
	if s := c.Summary(); s.Revisions != 2 || s.Frames != 2 {
		t.Fatalf("%+v", s)
	}
}

func TestCorePrunes(t *testing.T) {
	old := keepFrames
	keepFrames = 4
	defer func() { keepFrames = old }()
	c := NewInterpreterCore()
	for g := int64(0); g < 30; g++ {
		c.Feed(wframe(1, g, 0, nil))
	}
	if len(c.revs) > 2*keepFrames+1 || len(c.emitted) > 2*keepFrames+1 || len(c.links) > 2*keepFrames+1 {
		t.Fatalf("kept %d revisions", len(c.revs))
	}
	// a revision inside the window is one; the frames before it were forgotten, so theirs is dropped
	if u := c.Feed(wframe(1, 27, 1, nil, wev(EventConfirm, 1, ""))); !u.IsRevision || len(u.Events) != 1 {
		t.Fatalf("%+v", u)
	}
	if u := c.Feed(wframe(1, 2, 1, nil, wev(EventConfirm, 1, ""))); !u.Duplicate {
		t.Fatalf("%+v", u)
	}
}

// TestCorePrunesTheTrayLinks: the tray links are pruned with the revisions: the oldest frame kept still gives a revision
// the tray stone from before it, a frame older than that is a duplicate, and the links kept stay bounded.
func TestCorePrunesTheTrayLinks(t *testing.T) {
	old := keepFrames
	keepFrames = 8
	defer func() { keepFrames = old }()
	brick := func(id int64, x float64) StoneView {
		return StoneView{ID: id, Shape: "frozen", Detected: true,
			Solid: &Solid{Pos: []float64{x, 0, 0.046}, Q: []float64{0, 0, 0, 1}, Dims: []float64{0.3, 0.2, 0.09}}}
	}
	on := []StoneView{brick(1, -0.6), brick(2, 0), brick(3, 0.6)}
	still := func(g, rev int64, events ...WorldEvent) *Frame {
		f := wframe(1, g, rev, on, events...)
		f.World.Motion = "static"
		return f
	}
	c := NewInterpreterCore()
	for g := int64(0); g < 25; g++ {
		c.Feed(still(g, 0))
	}
	// the tray arrives at g4, taken at g10 (its visit), so g11 is the first frame its links come before; more than
	// 2 x 8 frames kept prune at g16 (keeping g8 on) and at g24 (keeping g16 on)
	if tr := c.State().Tray; tr == nil || len(tr.Stones) != 3 || tr.Stones[0].ID != "s1t0.8-1-1" {
		t.Fatalf("%+v", tr)
	}
	_, edge := c.links[16]
	_, older := c.links[15]
	if len(c.links) != 9 || !edge || older {
		t.Fatalf("kept the links of %d frames", len(c.links))
	}
	if u := c.Feed(still(16, 1, wev(EventConfirm, 1, ""))); !u.IsRevision || len(u.Events) != 1 ||
		u.Events[0].TrayStone != "s1t0.8-1-1" {
		t.Fatalf("%+v", u)
	}
	if u := c.Feed(still(15, 1, wev(EventConfirm, 1, ""))); !u.Duplicate || len(u.Events) != 0 {
		t.Fatalf("%+v", u)
	}
}

// TestCoreCards: the cards come from new frames of the session; a new session starts without the last one's, and a
// revision does not change them.
func TestCoreCards(t *testing.T) {
	c := NewInterpreterCore()
	holding := &CardsLayer{Left: HandCard{Title: "Holding a stone"}}
	pushing := &CardsLayer{Left: HandCard{Title: "Pushing a stone"}}
	f := wframe(1, 0, 0, nil)
	f.Cards = holding
	if st := c.Feed(f).State; st.Hands.Left.Card != "Holding a stone" {
		t.Fatalf("%+v", st.Hands)
	}
	if st := c.Feed(wframe(2, 0, 0, nil)).State; st.Hands.Left.Card != "" { // session 2, no cards layer
		t.Fatalf("%+v", st.Hands)
	}
	rev := wframe(2, 0, 1, nil)
	rev.Cards = pushing
	if u := c.Feed(rev); !u.IsRevision || u.State.Hands.Left.Card != "" {
		t.Fatalf("%+v", u)
	}
	next := wframe(2, 1, 0, nil)
	next.Cards = pushing
	if st := c.Feed(next).State; st.Hands.Left.Card != "Pushing a stone" {
		t.Fatalf("%+v", st.Hands)
	}
}

func TestCoreHandsAndFinish(t *testing.T) {
	c := NewInterpreterCore()
	f := wframe(1, 0, 0, []StoneView{stoneAt(1, 0, 0)}, wev(EventLift, 1, ""))
	f.World.Hands = WorldHands{Left: &HandState{State: "grasp", Stone: Int64(1)}}
	f.World.Stats.Confirmed = Int64(1)
	f.World.Draw = &WorldDraw{Board: &Rect{}}
	c.Feed(f)
	f2 := wframe(1, 1, 0, nil, WorldEvent{Type: EventLift})
	f2.World.Hands, f2.World.Stats = f.World.Hands, f.World.Stats
	c.Feed(f2)
	st := c.State()
	if st.Hands.Left.State != "grasp" || *st.Hands.Left.Stone != 1 || st.Hands.Right.State != "" || st.Board != nil {
		t.Fatalf("%+v", st.Hands)
	}
	out := c.Finish()
	if len(out) != 2 || out[1].StoneID != nil || out[0].Outcome != OutcomeInterrupted {
		t.Fatalf("finish: %s", describeAll(out))
	}
	if c.Finish() != nil {
		t.Fatal("a second finish")
	}
	s := c.Summary()
	if s.Hands.Left.Grasp != 0.4 || *s.Stones.ConfirmedLast != 1 || s.Moves.MeanDuration != nil {
		t.Fatalf("%+v", s)
	}
}

func TestCoreHandMergeAndCloseOrder(t *testing.T) {
	c := NewInterpreterCore()
	stones := []StoneView{stoneAt(3, 0, 0), stoneAt(4, 0, 0)}
	c.Feed(wframe(1, 1, 0, stones, wev(EventLift, 4, HandRight), wev(EventLift, 3, "")))
	c.Feed(wframe(1, 2, 0, stones, wev(EventCarry, 3, HandLeft)))
	if st := c.State(); st.OpenMoves[3].Hand != HandLeft {
		t.Fatalf("%+v", st.OpenMoves[3])
	}
	out := c.Finish()
	if len(out) != 2 || *out[0].StoneID != 3 || *out[1].StoneID != 4 {
		t.Fatal(describeAll(out))
	}
}

func TestCoreTracks(t *testing.T) {
	oldPoints, oldStones := trackPoints, trackStones
	trackPoints, trackStones = 3, 2
	defer func() { trackPoints, trackStones = oldPoints, oldStones }()
	c := NewInterpreterCore()
	if c.Track(1) != nil {
		t.Fatal("a track before any frame")
	}
	at := func(id int64, x float64) StoneView { return stoneAt(id, x, 0) }
	short := StoneView{ID: 9, Solid: &Solid{Pos: []float64{1, 2}}}
	c.Feed(wframe(1, 1, 0, []StoneView{at(1, 0), {ID: 8}, short}))
	c.Feed(wframe(1, 2, 0, []StoneView{at(1, 0)})) // did not move: no point
	for g := int64(3); g <= 9; g++ {
		c.Feed(wframe(1, g, 0, []StoneView{at(1, float64(g))}))
	}
	tr := c.Track(1)
	if len(tr) != 3 || tr[0].G != 7 || tr[2].G != 9 || tr[2].Pos != [3]float64{9, 0, 0} || tr[2].T != 1.8 {
		t.Fatalf("the newest points: %+v", tr)
	}
	tr[0].G = -1 // a copy
	if c.Track(1)[0].G != 7 || c.Track(8) != nil || c.Track(9) != nil {
		t.Fatal("copy, or a stone without a full solid")
	}
	// a revision adds no point; the least recently moved stone's track goes when a third stone comes
	c.Feed(wframe(1, 9, 1, []StoneView{at(1, 50)}))
	c.Feed(wframe(1, 10, 0, []StoneView{at(1, 9), at(2, 0)}))
	c.Feed(wframe(1, 11, 0, []StoneView{at(1, 9), at(2, 1), at(3, 0)}))
	if c.Track(1) != nil || len(c.Track(2)) != 2 || len(c.Track(3)) != 1 {
		t.Fatalf("eviction: %v %v %v", c.Track(1), c.Track(2), c.Track(3))
	}
	// a new session starts without tracks
	c.Feed(wframe(2, 1, 0, nil))
	if c.Track(2) != nil {
		t.Fatal("a track of the last session")
	}
}

// TestCoreWall: wall times come from the newest clock state of the frame's session; after a select or seek the
// playhead runs from the end of the worker's hold, so the wall of a media time is SessionClock.WallOf's.
func TestCoreWall(t *testing.T) {
	c := NewInterpreterCore()
	if c.wall(7, 12) != nil {
		t.Fatal("a wall without a clock")
	}
	st := State{Session: 7, Playing: true, Rate: 0.8, AnchorWall: 1000, HoldUntil: 1002.5, AnchorMedia: 10}
	c.SetClock(&ClockEvent{State: st})
	w := c.wall(7, 12)
	if w == nil || *w != 1002.5+2/0.8 || c.wall(8, 12) != nil {
		t.Fatal(w)
	}
	start := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	if sc := NewSessionClock(&SessionReply{State: st}, start, start, nil).WallOf(12); sc == nil || *sc != *w {
		t.Fatal(sc, *w)
	}
	// paused: mapped back from the pause, as SessionClock.WallOf
	paused := st
	paused.Playing, paused.Revision = false, 1
	c.SetClock(&ClockEvent{State: paused})
	if w := c.wall(7, 12); w == nil || *w != 1000+2/0.8 {
		t.Fatal(w)
	}
	if sc := NewSessionClock(&SessionReply{State: paused}, start, start, nil).WallOf(12); sc == nil || *sc != 1000+2/0.8 {
		t.Fatal(sc)
	}
	// an older state of the session is ignored; the next session's state does not take the walls of this one
	c.SetClock(&ClockEvent{State: st})
	next := State{Session: 8, Playing: true, Rate: 1, AnchorWall: 2000, AnchorMedia: 0, Revision: 2}
	c.SetClock(&ClockEvent{State: next})
	if w := c.wall(7, 12); w == nil || *w != 1000+2/0.8 {
		t.Fatal("session 7 keeps its newest state:", w)
	}
	if w := c.wall(8, 3); w == nil || *w != 2003 {
		t.Fatal(w)
	}
	for s := int64(9); s < 16; s++ { // 8 sessions kept: session 7's state goes
		c.SetClock(&ClockEvent{State: State{Session: s, Playing: true, Rate: 1, AnchorWall: 3000}})
	}
	if c.wall(7, 12) != nil || c.wall(8, 3) == nil {
		t.Fatal("the states of the last 8 sessions are kept")
	}
	st.Rate, st.Revision = 0, 5
	c.SetClock(&ClockEvent{State: st})
	if c.wall(7, 12) != nil {
		t.Fatal("a wall at rate 0")
	}
}
