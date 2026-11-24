package pdiggerfactory

import "testing"

// Two stones are established before any dropout. The first already belongs to the tray, the second
// is a later, still unidentified observation.
func blinkTray(t *testing.T) (*trayLedger, func(int64, []StoneView, ...WorldEvent), StoneView, StoneView) {
	t.Helper()
	ledger := newTrayLedger(&TrayCounts{}, 1, "dry1", func(Tray) {})
	stone := func(id int64, x float64) StoneView {
		return StoneView{ID: id, Detected: true, Shape: "frozen",
			Solid: &Solid{Pos: []float64{x, 0}, Dims: []float64{0.3, 0.2}, Q: []float64{0, 0, 0, 1}}}
	}
	first, added := stone(1, 0), stone(2, 1)
	feed := func(g int64, stones []StoneView, events ...WorldEvent) {
		count := int64(len(stones))
		ledger.feed(&WorldLayer{Motion: "static", Stones: stones, Events: events,
			Stats: WorldStats{Confirmed: &count, Stones: &count}}, TrayPoint{G: g, T: float64(g) / 5})
	}
	for g := int64(0); g <= 10; g++ {
		feed(g, []StoneView{first})
	}
	feed(11, []StoneView{first, added}, WorldEvent{Type: EventNew, ID: Int64(2)})
	for g := int64(12); g <= 15; g++ {
		feed(g, []StoneView{first, added})
	}
	if ledger.tray.link[1] == nil || ledger.tray.unid[2] == nil || ledger.tray.calmRef.G != 15 {
		t.Fatal("tray did not establish a link, candidate and calm leaving point")
	}
	return ledger, feed, first, added
}

func TestTrayBlinkKeepsLinkAndCandidate(t *testing.T) {
	ledger, feed, first, added := blinkTray(t)
	tr := ledger.tray
	linked, candidate := tr.link[1], tr.unid[2]
	feed(16, nil)
	feed(17, nil)
	if tr.link[1] != linked || tr.unid[2] != nil || tr.calmRef.G != 15 || linked.seenT != 3 {
		t.Fatal("missing frames changed the link, the last sight or the calm frame, or offered a missing candidate")
	}
	moved := *added.Solid
	moved.Pos = []float64{1.05, 0}
	added.Solid = &moved
	feed(18, []StoneView{first, added}, WorldEvent{Type: EventConfirm, ID: Int64(1)},
		WorldEvent{Type: EventNew, ID: Int64(3)})
	if tr.link[1] != linked || tr.unid[2] != candidate || candidate.born.G != 11 {
		t.Fatal("returning stones lost their identities or original birth frame")
	}
	facts := ledger.close()
	final := facts[len(facts)-1]
	if final.G != 15 || final.Stones != 2 || final.Sure != 1 || final.Unclear != 1 || final.Removed != 0 {
		t.Fatalf("%+v", final)
	}
	if added := tr.link[2]; added == nil || added.leftPlace == nil || *added.leftPlace != (vec2{1, 0}) {
		t.Fatalf("late addition has the wrong leaving place: %+v", added)
	}
}

func TestTrayBlinkExpiresOrEndsExplicitly(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  string
	}{
		{"three absent frames", ""},
		{"explicit ending", EventGone},
		{"tray closes before a return", "close"},
		{"unread frame before return", "gap"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ledger, feed, first, added := blinkTray(t)
			tr := ledger.tray
			linked := tr.link[1]
			feed(16, nil)
			switch tc.end {
			case EventGone:
				feed(17, nil, WorldEvent{Type: EventGone, ID: Int64(1)})
				if !linked.carriedAway || linked.lostG != 17 {
					t.Fatal("ending event lost its cause or time")
				}
			case "close":
				facts := ledger.close()
				end := facts[len(facts)-1]
				if end.G != 15 || end.Stones != 0 || end.Sure != 0 || end.Unclear != 1 {
					t.Fatalf("unconfirmed loss at close: %+v", end)
				}
			case "gap":
				ledger.skipped()
				feed(17, []StoneView{first, added})
				if tr.unid[2] == nil || tr.unid[2].born.G != 17 {
					t.Fatal("an absent candidate crossed an unread frame")
				}
			default:
				feed(17, nil)
				feed(18, nil)
				if linked.lostG != 16 || tr.unid[2] != nil {
					t.Fatal("a sustained absence did not expire at its first missing frame")
				}
			}
			if tc.end != "gap" && tr.link[1] != nil {
				t.Fatal("missing link survived its ending")
			}
		})
	}
}

// A place on a different stone can change what is on the tray, even if both hands are already free.
// The normal loss rules must handle this case on either side of an omission.
func TestTrayBlinkDuringHandWork(t *testing.T) {
	for _, workG := range []int64{16, 18} {
		ledger, feed, first, added := blinkTray(t)
		candidate := ledger.tray.unid[2]
		for g := int64(16); g <= 18; g++ {
			stones := []StoneView{first, added}
			var events []WorldEvent
			if g == 17 {
				stones = nil
			}
			if g == workG {
				events = []WorldEvent{{Type: EventPlace, ID: Int64(3), Side: "left"}}
			}
			feed(g, stones, events...)
		}
		if u := ledger.tray.unid[2]; u == nil || u == candidate || u.born.G != 18 {
			t.Fatalf("hand work at g %d carried the original candidate across the omission: %+v", workG, u)
		}
	}
}

func TestDeparturePositionUsesSelectedFrame(t *testing.T) {
	_, _, first, second := blinkTray(t)
	for _, mode := range []string{"calm", "worked", "arrival"} {
		var closed Tray
		ledger := newTrayLedger(&TrayCounts{}, 1, "example", func(tr Tray) { closed = tr })
		for g := int64(0); g <= 12; g++ {
			first.Detected = g != 12
			world := &WorldLayer{Motion: "static", Stones: []StoneView{first, second},
				Stats: WorldStats{Stones: Int64(2), Confirmed: Int64(2)}}
			if mode == "arrival" && g < 12 {
				world.Stats.Confirmed = Int64(1)
			}
			if mode == "worked" && g == 12 {
				world.Events = []WorldEvent{{Type: EventPlace, ID: Int64(2), Side: "left"}}
			}
			ledger.feed(world, TrayPoint{G: g, T: float64(min(g, 11)) / 5})
		}
		ledger.close()
		if mode == "arrival" && closed.Arrived.G != 12 {
			t.Fatalf("unexpected arrival: %+v", closed.Arrived)
		}
		if closed.Leaving.G != 12 || closed.Stones[0].LeftPlace != nil || closed.Stones[0].Place != ([2]float64{0, 0}) {
			t.Fatalf("departure retained an earlier observation (%s): %+v", mode, closed.Stones[0])
		}
		if p := closed.Stones[1].LeftPlace; p == nil || *p != ([2]float64{1, 0}) {
			t.Fatalf("departure lost a current observation (%s): %v", mode, p)
		}
	}
}

func TestTrayBlinkEndsBeforeNextStop(t *testing.T) {
	ledger, feed, first, added := blinkTray(t)
	tr := ledger.tray
	linked := tr.link[1]
	feed(16, []StoneView{added})
	ledger.feed(&WorldLayer{Motion: "moving"}, TrayPoint{G: 17, T: 3.4})
	for g := int64(18); g <= 35; g++ {
		if g == 18 {
			feed(g, []StoneView{added}, WorldEvent{Type: EventGone, ID: Int64(first.ID)})
		} else {
			feed(g, []StoneView{added})
		}
	}
	if ledger.tray != tr || linked.linked || linked.lostG != 16 || linked.carriedAway {
		t.Fatalf("an ending on a later stop changed the cause or time of the loss: %+v", linked)
	}
}

func TestTrayBlinkCompetingStone(t *testing.T) {
	for _, tc := range []struct {
		name  string
		near  [4]bool // last observation, two missing frames, then the return
		keeps bool
	}{
		{"stable neighbour", [4]bool{true, true, true, true}, true},
		{"new at first omission", [4]bool{false, true, true, true}, false},
		{"new at second omission", [4]bool{false, false, true, true}, false},
		{"new at recovery", [4]bool{false, false, false, true}, false},
		{"neighbour re-enters at recovery", [4]bool{true, true, false, true}, false},
		{"nearby IDs disappear and return together", [4]bool{true, false, false, true}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ledger, feed, first, added := blinkTray(t)
			candidate := ledger.tray.unid[added.ID]
			rival := added
			rival.ID = 3
			solid := *added.Solid
			solid.Pos = []float64{1.1, 0}
			rival.Solid = &solid
			for i, near := range tc.near {
				stones := []StoneView{first}
				if i == 0 || i == 3 {
					stones = append(stones, added)
				}
				if near {
					stones = append(stones, rival)
				}
				feed(int64(16+i), stones)
			}
			if u := ledger.tray.unid[added.ID]; u == nil || (u == candidate) != tc.keeps {
				t.Fatalf("unexpected identity after competing observation: %+v", u)
			}
		})
	}
}

func TestInventoryGapWithholdsSnapshotWithoutRestoringLink(t *testing.T) {
	ledger, feed, first, added := blinkTray(t)
	first.Detected = false
	feed(16, []StoneView{first, added})
	feed(17, []StoneView{added})
	feed(18, []StoneView{added})
	if tr := ledger.tray; tr.calmRef.G != 16 || tr.link[first.ID] != nil {
		t.Fatal("an undetected omission must pause the leaving point without preserving the link")
	}
	feed(19, []StoneView{added})
	if ledger.tray.calmRef.G != 19 {
		t.Fatal("a sustained absence must allow a new calm snapshot")
	}
}

func TestTrayBlinkReturnMustMatchItsNearestSource(t *testing.T) {
	for _, tc := range []struct {
		x     float64
		keeps bool
	}{{1.03125, true}, {1.0625, false}, {1.09375, false}, {1.3, false}} {
		ledger, feed, first, added := blinkTray(t)
		candidate := ledger.tray.unid[2]
		rival := added
		rival.ID = 3
		other := *added.Solid
		other.Pos = []float64{1.125, 0}
		rival.Solid = &other
		feed(16, []StoneView{first, added, rival})
		feed(17, []StoneView{first, rival})
		moved := *added.Solid
		moved.Pos = []float64{tc.x, 0}
		added.Solid = &moved
		feed(18, []StoneView{first, added, rival})
		if u := ledger.tray.unid[2]; u == nil || (u == candidate) != tc.keeps {
			t.Fatalf("incorrect continuity at return position %v: %+v", tc.x, u)
		}
	}
}

func TestArrivalWaitsForCompleteInventory(t *testing.T) {
	_, _, first, second := blinkTray(t)
	ledger := newTrayLedger(&TrayCounts{}, 1, "dry1", func(Tray) {})
	for g := int64(0); g <= 12; g++ {
		stones := []StoneView{first, second}
		if g == 4 {
			stones = []StoneView{first}
		}
		n := int64(len(stones))
		ledger.feed(&WorldLayer{Motion: "static", Stones: stones, Stats: WorldStats{Confirmed: &n, Stones: &n}},
			TrayPoint{G: g, T: float64(g) / 5})
	}
	if tr := ledger.tray; tr.arrived == nil || tr.arrived.G != 5 || len(tr.stones) != 2 {
		t.Fatalf("arrival used a frame with an incomplete inventory: %+v", tr.arrived)
	}
}

func TestReturningTrayUsesCompleteWorldFromLatestStop(t *testing.T) {
	for _, last := range []string{"same frame", "same quiet run", "new event"} {
		ledger, _, first, added := blinkTray(t)
		feed := func(g int64, mediaT float64, stones []StoneView, events ...WorldEvent) {
			n := int64(len(stones))
			ledger.feed(&WorldLayer{Motion: "static", Stones: stones, Events: events, Stats: WorldStats{Stones: &n, Confirmed: &n}},
				TrayPoint{G: g, T: mediaT})
		}
		tr := ledger.tray
		tracked := tr.link[1]
		ledger.feed(&WorldLayer{Motion: "moving"}, TrayPoint{G: 16, T: 3.2})
		feed(17, 4, []StoneView{first, added})
		feed(18, 5, []StoneView{first, added})
		first.Detected = false
		feed(19, 6.1, []StoneView{first})
		if last == "new event" {
			feed(20, 6.3, []StoneView{first}, WorldEvent{Type: EventNew, ID: Int64(100)})
		} else if last == "same quiet run" {
			feed(20, 6.3, []StoneView{first})
		}
		ledger.close()
		if tr.leaving.G != 18 || tracked.leftPlace == nil || *tracked.leftPlace != (vec2{0, 0}) {
			t.Fatalf("leaving must use the complete world from this stop: %+v, %+v", tr.leaving, tracked.leftPlace)
		}
		if tracked.sure != (last == "same frame") {
			t.Fatalf("the final check must use a distinct physical frame (%s): %+v", last, tracked)
		}
	}
}

func TestSeparateIncompleteCalmRunsRaiseDoubt(t *testing.T) {
	ledger, feed, first, added := blinkTray(t)
	tracked := ledger.tray.link[first.ID]
	first.Detected = false
	for g := int64(16); g <= 23; g++ {
		stones := []StoneView{first, added}
		var events []WorldEvent
		if g == 16 || g == 20 {
			events = []WorldEvent{{Type: EventNew, ID: Int64(100)}}
		}
		if g == 19 || g == 23 {
			stones = []StoneView{first}
		}
		feed(g, stones, events...)
		if tracked.sure != (g < 23) {
			t.Fatalf("unexpected certainty at frame %d: %+v", g, tracked)
		}
	}
}

func TestUnidentifiedStoneSeenOnLeavingBlink(t *testing.T) {
	for _, c := range []string{
		"positive", "later sight", "drift past anchor", "first sight", "born at anchor", "earlier sights interrupted",
		"hand", "gap", "replacement", "move", "rival", "split", "expiry", "expiry limit", "rewind before sight", "same time as sight",
	} {
		t.Run(c, func(t *testing.T) {
			_, _, first, second := blinkTray(t)
			var closed Tray
			ledger := newTrayLedger(&TrayCounts{}, 1, "dry1", func(tr Tray) { closed = tr })
			for g := int64(0); g < 18; g++ {
				stones := []StoneView{second}
				if g != 15 {
					stones = append(stones, first)
				}
				born := int64(10)
				if c == "born at anchor" {
					born = 14
				}
				if g >= born && !(c == "earlier sights interrupted" && g == 12) {
					s := first
					s.ID = 3
					s.Detected = (g <= 11 && c != "first sight") || g == 15
					geometry := *s.Solid
					geometry.Pos = []float64{0.5, 0}
					s.Solid = &geometry
					if g >= 16 && c == "replacement" {
						s.ID = 4
					}
					if g >= 16 && c == "move" {
						geometry.Pos[1] = 0.30
					}
					if c == "later sight" || c == "drift past anchor" {
						if g == 16 {
							s.Detected = true
							geometry.Pos[1] = 0.10
						} else if g == 17 {
							geometry.Pos[1] = 0.13
							if c == "drift past anchor" {
								geometry.Pos[1] = 0.20
							}
						}
					}
					stones = append(stones, s)
					if g >= 16 && c == "rival" {
						rival := s
						rival.ID = 4
						solid := geometry
						solid.Pos = []float64{0.6, 0}
						rival.Solid = &solid
						stones = append(stones, rival)
					}
				}
				var events []WorldEvent
				if g == 16 {
					events = []WorldEvent{{Type: EventNew, ID: Int64(100)}}
					switch c {
					case "hand":
						events = append(events, WorldEvent{Type: EventPlace, ID: Int64(2), Side: "left"})
					case "gap":
						ledger.skipped()
					case "split":
						events = append(events, WorldEvent{Type: EventSplit, ID: Int64(3)})
					}
				}
				mediaT := float64(g) / 5
				if g >= 16 && c == "same time as sight" {
					mediaT = 3 // a paused media clock gives a valid zero-age observation
				}
				if g == 17 {
					switch c {
					case "expiry":
						mediaT = 6.01 // more than three seconds since the positive sight at g15
					case "expiry limit":
						mediaT = 6
					case "rewind before sight":
						mediaT = 2.99 // frames advance, but the clock precedes the positive observation at 3.0
					}
				}
				n := int64(len(stones))
				ledger.feed(&WorldLayer{Motion: "static", Stones: stones, Events: events, Stats: WorldStats{Stones: &n, Confirmed: &n}},
					TrayPoint{G: g, T: mediaT})
			}
			ledger.close()
			var occupant *TrayStone
			for i := range closed.Stones {
				s := &closed.Stones[i]
				if s.WorldID != nil && *s.WorldID == 3 {
					occupant = s
				}
			}
			if c == "replacement" { // world stone 3 is gone by the end: nothing to count
				if occupant != nil {
					t.Fatalf("a replaced stone kept an occupant: %+v", occupant)
				}
				return
			}
			// where the sight cannot be used, the stone is counted at the leaving point's own place for it, where the
			// world kept it (not held, less than 0.30 of its box under another unheld stone's: version 7), never at the
			// sight's
			end := closed.Facts[len(closed.Facts)-1]
			if occupant == nil || !occupant.Added || occupant.Sure || occupant.Certain == nil || *occupant.Certain ||
				occupant.LeftPlace != nil || end.Stones != 3 || end.Added != 0 || end.Unclear != 1 {
				t.Fatalf("expected an occupant with uncertain origin: %+v, %+v", occupant, end)
			}
			want := [2]float64{0.5, 0}
			if c == "later sight" { // the last positive place
				want = [2]float64{0.5, 0.10}
			}
			if occupant.Place != want {
				t.Fatalf("expected the place %v, got %v", want, occupant.Place)
			}
		})
	}
}

// TestTwoStonesFirstSeenInOneTenthUnderOneWorldIDGetTwoIDs: tray stone 1 arrives at t 9.96; world 1 is missing at
// t 10.00 while a hand places stone 2, and is back at t 10.04 at x 0.3: nothing pairs it with stone 1, so that is
// removed and world 1 added, both unclear and both first seen in the tenth 10.0. The added one takes the id with -2
// (4.6).
func TestTwoStonesFirstSeenInOneTenthUnderOneWorldIDGetTwoIDs(t *testing.T) {
	times := []float64{9, 9.2, 9.4, 9.6, 9.96, 10, 10.04}
	for k := 0; k < 12; k++ {
		times = append(times, 10.2+0.2*float64(k))
	}
	box := [2]float64{0.3, 0.2}
	var closed []Tray
	ledger := newTrayLedger(&TrayCounts{}, 1, "sample", func(tr Tray) { closed = append(closed, tr) })
	for g, at := range times {
		x := -0.6
		if g >= 6 {
			x = 0.3
		}
		stones := []StoneView{leavingStone(1, x, 0, "kept", true, box), leavingStone(2, 0.6, 0, "kept", true, box)}
		var events []WorldEvent
		if g == 5 {
			stones = stones[1:]
			events = []WorldEvent{{Type: EventPlace, Side: "left", ID: Int64(2)}}
		}
		n := int64(len(stones))
		ledger.feed(&WorldLayer{Motion: "static", Stones: stones, Events: events, Draw: leavingBoard,
			Stats: WorldStats{Stones: &n, Confirmed: &n}}, TrayPoint{G: int64(g), T: at})
	}
	ledger.close()
	if len(closed) != 1 {
		t.Fatalf("%d trays closed, want 1", len(closed))
	}
	type row struct {
		id    string
		world int64 // 0 when none
		added bool
	}
	var stones []row
	for _, s := range closed[0].Stones {
		r := row{id: s.ID, added: s.Added}
		if s.WorldID != nil {
			r.world = *s.WorldID
		}
		stones = append(stones, r)
	}
	want := []row{{"s1t10.0-1-1", 0, false}, {"s1t10.0-1-2", 2, false}, {"s1t10.0-1-1-2", 1, true}}
	if len(stones) != len(want) || stones[0] != want[0] || stones[1] != want[1] || stones[2] != want[2] {
		t.Fatalf("stones %+v, want %+v", stones, want)
	}
	type said struct{ kind, code, stone string }
	var facts []said
	for _, f := range closed[0].Facts {
		if f.Stone != "" && f.Kind != TrayUnsure {
			facts = append(facts, said{f.Kind, f.Code, f.Stone})
		}
	}
	wantFacts := []said{{TrayRemoved, "not_found", "s1t10.0-1-1"}, {TrayAdded, "not_recognised", "s1t10.0-1-1-2"}}
	if len(facts) != 2 || facts[0] != wantFacts[0] || facts[1] != wantFacts[1] {
		t.Fatalf("facts %+v, want %+v", facts, wantFacts)
	}
}

// TestATrayGivesNoIDTwice: a second stone first seen under one world id in one tenth of a second takes -2, a third -3
// (4.6).
func TestATrayGivesNoIDTwice(t *testing.T) {
	tr := &openTray{}
	ws := leavingStone(7, 0, 0, "kept", true, [2]float64{0.3, 0.2})
	for i := 0; i < 3; i++ {
		tr.stones = append(tr.stones, newTrayStone(tr, "s7t1.0-1-7", &ws, TrayPoint{G: 5, T: 1}))
	}
	var ids []string
	for _, s := range tr.stones {
		ids = append(ids, s.id)
	}
	if len(ids) != 3 || ids[0] != "s7t1.0-1-7" || ids[1] != "s7t1.0-1-7-2" || ids[2] != "s7t1.0-1-7-3" {
		t.Fatalf("ids %v, want s7t1.0-1-7, then -2 and -3", ids)
	}
}
