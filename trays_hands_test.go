package pdiggerfactory

import (
	"reflect"
	"testing"
)

// Version 4 of the tray rules (SDK-DESIGN.md 4.6.1): a certain removal needs a hand on the stone, and a visit continues
// the open tray only on the camera's word: of the stones no hand holds at its first frame, the tray saw one the world
// detects, and nine tenths of the detected ones or of them all.

type handStep struct {
	stones []StoneView
	events []WorldEvent
	touch  bool
	moving bool
}

func handStone(id int64, x float64) StoneView {
	return StoneView{ID: id, Mode: "kept", Family: "rect", Shape: "frozen", Detected: true,
		Solid: &Solid{Pos: []float64{x, 0, 0.046}, Dims: []float64{0.3, 0.2, 0.09}, Q: []float64{0, 0, 0, 1}}}
}

func handPlay(steps []handStep) []Tray {
	var trays []Tray
	ledger := newTrayLedger(&TrayCounts{}, 1, "dry1", func(tr Tray) { trays = append(trays, tr) })
	for g, s := range steps {
		n := int64(len(s.stones))
		left := "free"
		if s.touch {
			left = "touch"
		}
		motion := "static"
		if s.moving {
			motion = "moving"
		}
		ledger.feed(&WorldLayer{Motion: motion, Stones: s.stones, Events: s.events,
			Hands: WorldHands{Left: &HandState{State: left}, Right: &HandState{State: "free"}},
			Stats: WorldStats{Confirmed: &n, Stones: &n}}, TrayPoint{G: int64(g), T: float64(g) / 5})
	}
	ledger.close()
	return trays
}

// handRemoval: stones 1 and 2 arrive; touch lands on stone 2 at touchG; stone 2 leaves the world at lossG with loss; a
// hand works away from the stones at g20 and g40, so a calm moment follows at g43, more than 3 s after the loss.
func handRemoval(touch *WorldEvent, touchG int, loss WorldEvent, lossG int) Tray {
	var steps []handStep
	for g := 0; g < 50; g++ {
		stones := []StoneView{handStone(1, -0.6), handStone(2, 0.6)}
		if g >= lossG {
			stones = stones[:1]
		}
		s := handStep{stones: stones, touch: g == 20 || g == 40}
		if g == touchG && touch != nil {
			s.events, s.touch = []WorldEvent{*touch}, true
		} else if g == lossG {
			s.events = []WorldEvent{loss}
		}
		steps = append(steps, s)
	}
	return handPlay(steps)[0]
}

type removedAt struct {
	certain bool
	g       int64
}

func handRemoved(tr Tray) []removedAt {
	var out []removedAt
	for _, f := range tr.Facts {
		if f.Kind == TrayRemoved {
			out = append(out, removedAt{isTrue(f.Certain), f.G})
		}
	}
	return out
}

func absentLoss() WorldEvent { return WorldEvent{Type: EventDelete, ID: Int64(2), Why: "absent"} }

func TestTrayRemovalNeedsAHandOnTheStone(t *testing.T) {
	for _, tc := range []struct {
		name    string
		loss    WorldEvent
		touched bool
	}{
		{"drop", WorldEvent{Type: EventDrop, ID: Int64(2), Side: "right", Why: "t-release"}, true},
		{"gone", WorldEvent{Type: EventGone, ID: Int64(2), Side: "right"}, true},
		{"delete by a hand", WorldEvent{Type: EventDelete, ID: Int64(2), Why: "hand"}, true},
		{"delete carried away", WorldEvent{Type: EventDelete, ID: Int64(2), Why: "carried-away"}, true},
		{"delete absent", absentLoss(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := []removedAt{{false, 49}}
			if tc.touched {
				want = []removedAt{{true, 43}}
			}
			if got := handRemoved(handRemoval(nil, -1, tc.loss, 30)); !reflect.DeepEqual(got, want) {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestTrayTouchBeforeASilentLoss(t *testing.T) {
	for _, tc := range []struct {
		name    string
		touch   WorldEvent
		touched bool
	}{
		{"lift", WorldEvent{Type: EventLift, ID: Int64(2), Side: "right"}, true},
		{"push with a side", WorldEvent{Type: EventPush, ID: Int64(2), Side: "left"}, true},
		{"push without a side", WorldEvent{Type: EventPush, ID: Int64(2), Why: "reseat"}, false},
		{"freeze", WorldEvent{Type: "freeze", ID: Int64(2)}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := []removedAt{{false, 49}}
			if tc.touched {
				want = []removedAt{{true, 43}}
			}
			touch := tc.touch
			if got := handRemoved(handRemoval(&touch, 24, absentLoss(), 28)); !reflect.DeepEqual(got, want) {
				t.Fatalf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestTrayOpenRemovalNoHand(t *testing.T) {
	tr := handRemoval(nil, -1, absentLoss(), 30)
	var codes []string
	for _, f := range tr.Facts {
		if f.Kind == TrayRemoved {
			codes = append(codes, f.Code)
		}
	}
	open := tr.Facts[len(tr.Facts)-1].Open
	if !reflect.DeepEqual(codes, []string{"not_found"}) || len(open) != 1 || open[0].Kind != TrayRemoved ||
		open[0].Cause != "no_hand" {
		t.Fatalf("codes %v, open %+v", codes, open)
	}
}

func TestTrayTouchTooOldDoesNotCount(t *testing.T) {
	// pushed at g5 (t 1.0); a calm moment at g23 (t 4.6); lost silently at g30 (t 6.0), last seen at g29: the window
	// starts at min(4.6, 5.8 - 3.0) = 2.8, after the push
	touch := WorldEvent{Type: EventPush, ID: Int64(2), Side: "left"}
	if got := handRemoved(handRemoval(&touch, 5, absentLoss(), 30)); !reflect.DeepEqual(got, []removedAt{{false, 49}}) {
		t.Fatalf("%+v", got)
	}
}

func TestTrayTouchAfterTheLastCalmMomentCounts(t *testing.T) {
	// a calm moment at g23 (t 4.6); pushed at g25 (t 5.0); hands work until stone 2 is lost silently at g44 (t 8.8),
	// last seen at g43: the window starts at min(4.6, 8.6 - 3.0) = 4.6, so the push counts
	var steps []handStep
	for g := 0; g < 66; g++ {
		stones := []StoneView{handStone(1, -0.6), handStone(2, 0.6)}
		if g >= 44 {
			stones = stones[:1]
		}
		s := handStep{stones: stones, touch: g == 20 || (g >= 25 && g <= 44) || g == 55}
		switch g {
		case 25:
			s.events = []WorldEvent{{Type: EventPush, ID: Int64(2), Side: "left"}}
		case 44:
			s.events = []WorldEvent{absentLoss()}
		}
		steps = append(steps, s)
	}
	if got := handRemoved(handPlay(steps)[0]); !reflect.DeepEqual(got, []removedAt{{true, 58}}) {
		t.Fatalf("%+v", got)
	}
}

// handBoard: ten stones arrive; the conveyor moves; then a board whose first frame shows known of them and 10-known
// stones the tray never saw.
func handBoard(known int) []Tray {
	var first, next []StoneView
	for i := 0; i < 10; i++ {
		x := -0.9 + 0.2*float64(i)
		first = append(first, handStone(int64(i), x))
		if i < known {
			next = append(next, handStone(int64(i), x))
		} else {
			next = append(next, handStone(int64(100+i), x))
		}
	}
	var steps []handStep
	for g := 0; g < 12; g++ {
		steps = append(steps, handStep{stones: first})
	}
	for g := 0; g < 3; g++ {
		steps = append(steps, handStep{stones: first, moving: true})
	}
	var news []WorldEvent
	for i := known; i < 10; i++ {
		news = append(news, WorldEvent{Type: EventNew, ID: Int64(int64(100 + i))})
	}
	steps = append(steps, handStep{stones: next, events: news})
	for g := 0; g < 11; g++ {
		steps = append(steps, handStep{stones: next})
	}
	return handPlay(steps)
}

// handDroveOff: ten stones arrive; the conveyor moves; the next stop's first frame shows seven of them where they stood,
// detected or only remembered, and the world loses them one after another.
func handDroveOff(detected bool) []Tray {
	var first []StoneView
	for i := 0; i < 10; i++ {
		first = append(first, handStone(int64(i), -0.9+0.2*float64(i)))
	}
	var steps []handStep
	for g := 0; g < 12; g++ {
		steps = append(steps, handStep{stones: first})
	}
	for g := 0; g < 3; g++ {
		steps = append(steps, handStep{stones: first, moving: true})
	}
	var left []StoneView
	for _, s := range first[3:] {
		s.Detected = detected
		left = append(left, s)
	}
	for g := 0; g < 2; g++ {
		steps = append(steps, handStep{stones: left})
	}
	for i := 3; i < 10; i++ {
		left = left[1:]
		steps = append(steps, handStep{stones: left,
			events: []WorldEvent{{Type: EventDelete, ID: Int64(int64(i)), Why: "absent"}}})
	}
	for g := 0; g < 10; g++ {
		steps = append(steps, handStep{stones: left})
	}
	return handPlay(steps)
}

func TestTrayVisitOnlyRememberingTheStonesIsANewTray(t *testing.T) {
	// a board that drove off: the stones the world remembers where it stood say nothing about the belt
	trays := handDroveOff(false)
	if len(trays) != 2 || trays[0].N != 1 || trays[1].N != 2 {
		t.Fatalf("%d trays", len(trays))
	}
	if len(trays[0].Stones) != 10 {
		t.Fatalf("%d stones", len(trays[0].Stones))
	}
	for _, s := range trays[0].Stones {
		if s.Status != TrayStoneOn {
			t.Fatalf("stone %s: %s", s.ID, s.Status)
		}
	}
	for _, f := range trays[0].Facts {
		if f.Kind == TrayRemoved || f.Kind == TrayAdded {
			t.Fatalf("fact %s %s", f.Kind, f.Stone)
		}
	}
}

func TestTrayVisitDetectingTheStonesGoesOn(t *testing.T) {
	trays := handDroveOff(true)
	if len(trays) != 1 || trays[0].N != 1 {
		t.Fatalf("%d trays", len(trays))
	}
	if len(trays[0].Stones) != 10 {
		t.Fatalf("%d stones", len(trays[0].Stones))
	}
	for _, s := range trays[0].Stones {
		if s.Status != TrayStoneRemoved {
			t.Fatalf("stone %s: %s", s.ID, s.Status)
		}
	}
	removals := 0
	for _, f := range trays[0].Facts {
		if f.Kind != TrayRemoved {
			continue
		}
		removals++
		if f.Certain == nil || *f.Certain {
			t.Fatalf("a certain removal of %s", f.Stone)
		}
	}
	if removals == 0 {
		t.Fatal("no removals")
	}
}

func TestTrayVisitContinuesWithNineTenths(t *testing.T) {
	trays := handBoard(9)
	if len(trays) != 1 || trays[0].N != 1 {
		t.Fatalf("%d trays", len(trays))
	}
}

func TestTrayVisitWithFewerThanNineTenthsIsANewTray(t *testing.T) {
	trays := handBoard(8)
	if len(trays) != 2 || trays[0].N != 1 || trays[1].N != 2 {
		t.Fatalf("%d trays", len(trays))
	}
	for _, tr := range trays {
		on := 0
		for _, s := range tr.Stones {
			if s.Status == TrayStoneOn {
				on++
			}
		}
		if on != 10 {
			t.Fatalf("tray %d: %d stones on", tr.N, on)
		}
	}
}

// handJolt: ten stones arrive; the conveyor jolts; the next stop's first frame shows kept of them only remembered (not
// detected) and the next detected of them detected where they stood, fresh detected stones with ids the tray never saw
// (a rename, or another board's), unknown remembered ids it never saw and held detected stones it never saw in a hand
// (mode carry, gone after that frame); after it the world detects them all.
func handJolt(kept, detected, fresh, unknown, held int) []Tray {
	var first []StoneView
	for i := 0; i < 10; i++ {
		first = append(first, handStone(int64(i), -0.9+0.2*float64(i)))
	}
	var steps []handStep
	for g := 0; g < 12; g++ {
		steps = append(steps, handStep{stones: first})
	}
	for g := 0; g < 3; g++ {
		steps = append(steps, handStep{stones: first, moving: true})
	}
	var next []StoneView
	var news []WorldEvent
	for i := 0; i < kept+detected; i++ {
		s := handStone(int64(i), -0.9+0.2*float64(i))
		s.Detected = i >= kept
		next = append(next, s)
	}
	for k := 0; k < fresh; k++ {
		next = append(next, handStone(int64(100+k), -0.9+0.2*float64(kept+detected+k)))
		news = append(news, WorldEvent{Type: EventNew, ID: Int64(int64(100 + k))})
	}
	for k := 0; k < unknown; k++ {
		s := handStone(int64(200+k), -0.9+0.2*float64(k))
		s.Solid.Pos[1] = 0.4
		s.Detected = false
		next = append(next, s)
	}
	first2 := append([]StoneView{}, next...)
	for k := 0; k < held; k++ {
		s := handStone(int64(300+k), -0.9+0.2*float64(k))
		s.Solid.Pos[1] = -0.4
		s.Mode = "carry"
		first2 = append(first2, s)
	}
	steps = append(steps, handStep{stones: first2, events: news})
	var all []StoneView
	for _, s := range next {
		s.Detected = true
		all = append(all, s)
	}
	for g := 0; g < 11; g++ {
		steps = append(steps, handStep{stones: all})
	}
	return handPlay(steps)
}

func TestTrayVisitAfterAJolt(t *testing.T) {
	for _, c := range []struct {
		name                                 string
		kept, detected, fresh, unknown, held int
		want                                 int
	}{
		// all the detected stones were seen, whatever else the world remembers: the same tray
		{"detected seen, unknown remembered", 0, 9, 0, 2, 0, 1},
		// no detected stone was seen, however many remembered ones were: a new tray
		{"only a new stone detected", 9, 0, 1, 0, 0, 2},
		// two of three detected stones seen (less than nine tenths), but nine of all ten: the same tray
		{"most remembered, one renamed", 7, 2, 1, 0, 0, 1},
		// two of four detected and two of all ten seen: a new tray
		{"few seen", 0, 2, 2, 6, 0, 2},
		// two held stones the tray never saw are left out: all nine detected ones were seen, the same tray
		{"held stones left out", 0, 9, 0, 0, 2, 1},
	} {
		if trays := handJolt(c.kept, c.detected, c.fresh, c.unknown, c.held); len(trays) != c.want {
			t.Errorf("%s: %d trays, want %d", c.name, len(trays), c.want)
		}
	}
}
