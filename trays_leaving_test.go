package pdiggerfactory

import "testing"

// Version 5 of the tray rules (SDK-DESIGN.md 4.6.1, Leaving): the leaving pairs a lost tray stone S with an
// unidentified world stone W only on evidence. Looks that agree (the tray's bar over at least nID cells) pair them and
// looks that disagree (below tDiff over at least nID cells) keep them apart, whatever the time and the hands; looks in
// between, too few cells or no look leave it to the time and the hands: W first seen no earlier than renameT before
// S's last sight, and no hand mark near S's place or W's since the earliest of S's last sight, S's loss and W's first
// sight, less renameT.
//
// Version 7 (4.6.1, Positive evidence at departure): an unidentified world stone that the leaving point's world keeps
// (lists, not detected), not held, with less than overlapMax of its box under one other unheld stone's box there, is
// counted as an unclear addition at its place there.

// leavingCase is one verdict's setting: W's first sight, a hand mark, and whether S has a chain and W a stored look.
type leavingCase struct {
	born     float64
	mark     *trayMark
	noChain  bool
	noStored bool
}

func leavingAt() leavingCase { return leavingCase{born: 7.2} }

// leavingVerdict is the leaving's verdict on S (last seen at 10.0, lost at 10.4, at (0, 0), its chain one look of world
// stone 3 at g10) and W (world stone 7 at (0.1, 0), its stored look read at g50), the pair's score taken from the
// tray's memo (ok false: no score).
func leavingVerdict(x trayScore, c leavingCase) bool {
	tr := &openTray{store: map[int64]*trayLook{}, memo: map[[4]int64]trayScore{{7, 50, 3, 10}: x}}
	if !c.noStored {
		tr.store[7] = &trayLook{wid: 7, g: 50}
	}
	if c.mark != nil {
		tr.marks = []trayMark{*c.mark}
	}
	s := &trayStone{seenT: 10.0, lostT: 10.4, place: vec2{0, 0}}
	if !c.noChain {
		s.chain = []*trayLook{{wid: 3, g: 10}}
	}
	return sameStone(tr, s, 7, &trayUnid{born: TrayPoint{T: c.born}, place: vec2{0.1, 0}})
}

func TestLeavingPairsLooksThatAgreeWhateverTheTimeAndTheHands(t *testing.T) {
	c := leavingCase{born: 1.0, mark: &trayMark{t: 9.0, p: vec2{0, 0}}}
	if !leavingVerdict(trayScore{tIDStart, nID, true}, leavingAt()) || !leavingVerdict(trayScore{tIDStart, nID, true}, c) {
		t.Fatal("looks that agree must pair")
	}
}

func TestLeavingNeverPairsLooksThatDisagree(t *testing.T) {
	c := leavingAt()
	c.born = 9.0
	if leavingVerdict(trayScore{tDiff - 1, nID, true}, leavingAt()) || leavingVerdict(trayScore{tDiff - 1, nID, true}, c) {
		t.Fatal("looks that disagree must not pair")
	}
}

func TestLeavingLeavesTheRestToTheTimeAndTheHands(t *testing.T) {
	scores := map[string]trayScore{
		"the lowest score in between":  {tDiff, nID, true},
		"the highest score in between": {tIDStart - 1, nID, true},
		"too few cells to disagree":    {tDiff - 1, nID - 1, true},
		"too few cells to agree":       {tIDStart, nID - 1, true},
		"no score":                     {},
	}
	mark := func(at float64, x float64) leavingCase {
		c := leavingAt()
		c.mark = &trayMark{t: at, p: vec2{x, 0}}
		return c
	}
	born := func(b float64) leavingCase { return leavingCase{born: b} }
	for name, x := range scores {
		for _, k := range []struct {
			what string
			c    leavingCase
			want bool
		}{
			{"W first seen 2.8 s before S's last sight, no hand near", leavingAt(), true},
			{"W first seen renameT before S's last sight", born(10.0 - renameT), true},
			{"W first seen earlier than that", born(10.0 - renameT - 0.1), false},
			{"a hand mark near S's place", mark(9.0, handR-0.05), false},
			{"a hand mark near W's place only", mark(9.0, 0.1+handR-0.05), false},
			{"a hand mark near neither", mark(9.0, 0.1+handR+0.05), true},
			{"a hand mark before the window (4.2 s)", mark(4.1, 0), true},
			{"a hand mark in the window", mark(4.3, 0), false},
		} {
			if got := leavingVerdict(x, k.c); got != k.want {
				t.Errorf("%s, %s: got %v, want %v", name, k.what, got, k.want)
			}
		}
	}
}

func TestLeavingScoresNothingWithoutAChainOrAStoredLook(t *testing.T) {
	x := trayScore{tDiff - 1, nID, true}
	if !leavingVerdict(x, leavingCase{born: 7.2, noChain: true}) || !leavingVerdict(x, leavingCase{born: 7.2, noStored: true}) {
		t.Fatal("without a chain or a stored look nothing is scored: the time and the hands decide")
	}
	if leavingVerdict(x, leavingCase{born: 1.0, noChain: true}) {
		t.Fatal("W first seen long before S's last sight must not pair")
	}
}

// TestLeavingCountsAKeptStoneAtTheLeavingPoint is version 7 on the ledger: stones 1 and 2 arrive with the tray; world
// stone 3 comes at g10, boxed at g10 and g11, then kept (held, in case "held"), and in some cases a neighbour 5 comes
// with it, at (0.20, y): its box (0.3 by 0.2, as 3's) then covers 6 (y 0.09: 0.24) or 8 (y 0.05: 0.32) of the 25
// points of 3's box. The tray leaves at its last frame, g17, a calm one; in the later cases a new event at g15 makes
// g14 the leaving point, after which 3 moves 0.05 or 5 first comes.
func TestLeavingCountsAKeptStoneAtTheLeavingPoint(t *testing.T) {
	type neighbour struct {
		mode string
		y    float64
	}
	for _, c := range []struct {
		name      string
		later     bool
		neighbour *neighbour
		counted   map[int64][2]float64
	}{
		{"alone", false, nil, map[int64][2]float64{3: {0, 0}}},
		// 0.24 of each box under the other
		{"beside kept", false, &neighbour{"kept", 0.09}, map[int64][2]float64{3: {0, 0}, 5: {0.2, 0.09}}},
		// 0.32: a kept stone hides one
		{"under kept", false, &neighbour{"kept", 0.05}, map[int64][2]float64{}},
		// so does a detected one, itself counted by its box on the last frame
		{"under seen", false, &neighbour{"seen", 0.05}, map[int64][2]float64{5: {0.2, 0.05}}},
		// a held stone hides none, and is not counted itself
		{"under held", false, &neighbour{"carry", 0.05}, map[int64][2]float64{3: {0, 0}}},
		{"held", false, nil, map[int64][2]float64{}},
		// its place at the leaving point, not at the last frame
		{"moved later", true, nil, map[int64][2]float64{3: {0, 0}}},
		// the stones of the leaving point hide it, not the last frame's
		{"rival later", true, &neighbour{"seen", 0.05}, map[int64][2]float64{3: {0, 0}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			stone := func(id int64, x, y float64, mode string) StoneView {
				return StoneView{ID: id, Detected: mode == "seen", Mode: mode, Shape: "frozen",
					Solid: &Solid{Pos: []float64{x, y}, Dims: []float64{0.3, 0.2}, Q: []float64{0, 0, 0, 1}}}
			}
			var closed []Tray
			ledger := newTrayLedger(&TrayCounts{}, 1, "sample", func(tr Tray) { closed = append(closed, tr) })
			for g := int64(0); g < 18; g++ {
				later := c.later && g >= 15
				stones := []StoneView{stone(1, -0.6, 0, "seen"), stone(2, 0.6, 0, "seen")}
				var events []WorldEvent
				if later && g == 15 {
					events = []WorldEvent{{Type: EventNew, ID: Int64(100)}}
				}
				if g >= 10 {
					x, mode := 0.0, "kept"
					if later && c.name == "moved later" {
						x = 0.05
					}
					if g < 12 {
						mode = "seen"
					} else if c.name == "held" {
						mode = "carry"
					}
					stones = append(stones, stone(3, x, 0, mode))
					if n := c.neighbour; n != nil && (c.name != "rival later" || later) {
						mode := n.mode
						if g < 12 {
							mode = "seen"
						}
						stones = append(stones, stone(5, 0.20, n.y, mode))
					}
				}
				n := int64(len(stones))
				ledger.feed(&WorldLayer{Motion: "static", Stones: stones, Events: events,
					Stats: WorldStats{Stones: &n, Confirmed: &n}}, TrayPoint{G: g, T: float64(g) * 0.2})
			}
			ledger.close()
			if len(closed) != 1 {
				t.Fatalf("%d trays closed, want 1", len(closed))
			}
			tray := closed[0]
			leaving := tray.Facts[len(tray.Facts)-1]
			if want := map[bool]int64{false: 17, true: 14}[c.later]; leaving.Kind != TrayLeaving || leaving.G != want {
				t.Fatalf("the leaving: %+v, want g%d", leaving, want)
			}
			places := map[int64][2]float64{}
			for _, s := range tray.Stones {
				if !s.Added {
					continue
				}
				places[*s.WorldID] = s.Place
				if s.Sure || s.Certain == nil || *s.Certain {
					t.Errorf("world stone %d: sure %v, certain %v; want an unclear addition", *s.WorldID, s.Sure, s.Certain)
				}
				var codes []string
				for _, f := range tray.Facts {
					if f.Stone == s.ID {
						codes = append(codes, f.Code)
					}
				}
				if len(codes) != 1 || codes[0] != "not_recognised" {
					t.Errorf("world stone %d: codes %v, want [not_recognised]", *s.WorldID, codes)
				}
				// only a detected stone has a place seen at the leaving point
				seen := c.name == "under seen" && *s.WorldID == 5
				if (s.LeftPlace != nil) != seen || seen && *s.LeftPlace != ([2]float64{0.2, 0.05}) {
					t.Errorf("world stone %d: left place %v", *s.WorldID, s.LeftPlace)
				}
			}
			if len(places) != len(c.counted) {
				t.Fatalf("added %v, want %v", places, c.counted)
			}
			for wid, place := range c.counted {
				if got, ok := places[wid]; !ok || got != place {
					t.Fatalf("added %v, want %v", places, c.counted)
				}
			}
			k := int64(len(c.counted))
			if leaving.Stones != 2+k || leaving.Sure != 2 || leaving.Added != 0 || leaving.Unclear != k {
				t.Fatalf("leaving %d stones (%d sure), %d added, %d unclear; want %d, 2, 0, %d",
					leaving.Stones, leaving.Sure, leaving.Added, leaving.Unclear, 2+k, k)
			}
		})
	}
}

// leavingStone is a world stone of the leaving scenes, frozen, at (x, y) with a box of dims, in mode, detected or not.
func leavingStone(id int64, x, y float64, mode string, detected bool, dims [2]float64) StoneView {
	return StoneView{ID: id, Detected: detected, Mode: mode, Shape: "frozen",
		Solid: &Solid{Pos: []float64{x, y}, Dims: []float64{dims[0], dims[1]}, Q: []float64{0, 0, 0, 1}}}
}

// leavingBoard is the board of the synthetic tray recording: 2 m by 1.2 m around the origin.
var leavingBoard = &WorldDraw{Board: &Rect{HW: 1, HH: 0.6}}

// leavingAdded is a stone counted as a possible addition: its place, size, and place seen at the leaving point (Left,
// when Seen).
type leavingAdded struct {
	Place, Size, Left [2]float64
	Seen              bool
}

// checkLeavingAdded checks that tray's added stones, by world id, are want, each an unclear addition with only a
// not_recognised fact.
func checkLeavingAdded(t *testing.T, tray Tray, want map[int64]leavingAdded) {
	t.Helper()
	got := map[int64]leavingAdded{}
	for _, s := range tray.Stones {
		if !s.Added {
			continue
		}
		a := leavingAdded{Place: s.Place, Size: s.Size, Seen: s.LeftPlace != nil}
		if a.Seen {
			a.Left = *s.LeftPlace
		}
		got[*s.WorldID] = a
		if s.Sure || s.Certain == nil || *s.Certain {
			t.Errorf("world stone %d: sure %v, certain %v; want an unclear addition", *s.WorldID, s.Sure, s.Certain)
		}
		var codes []string
		for _, f := range tray.Facts {
			if f.Stone == s.ID {
				codes = append(codes, f.Code)
			}
		}
		if len(codes) != 1 || codes[0] != "not_recognised" {
			t.Errorf("world stone %d: codes %v, want [not_recognised]", *s.WorldID, codes)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("added %+v, want %+v", got, want)
	}
	for wid, a := range want {
		if g, ok := got[wid]; !ok || g != a {
			t.Fatalf("added %+v, want %+v", got, want)
		}
	}
}

// TestLeavingReadsTheLeavingPointWorld is where the leaving reads the leaving point's world, and which of its stones
// hide a kept one. Stones 1 and 2 arrive with the tray (1 at (0.2, 0.05) in case "under linked"); world stone 3 comes
// at g10 (at (0.95, 0) in case "under offboard", beside stone 9 off the board), boxed at g10 and g11, then kept, unless
// the case says otherwise. A hand event at g15 (the worked cases) makes the last frame, g17, the leaving point; a new
// event there (the others but "under linked" and "under offboard") makes it the calm g14; with neither it is g17, calm
// too.
func TestLeavingReadsTheLeavingPointWorld(t *testing.T) {
	box := [2]float64{0.3, 0.2}
	kept := func(x, y float64) leavingAdded { return leavingAdded{Place: [2]float64{x, y}, Size: box} }
	seen := func(x, y float64) leavingAdded {
		return leavingAdded{Place: [2]float64{x, y}, Size: box, Left: [2]float64{x, y}, Seen: true}
	}
	for _, c := range []struct {
		name string
		// leaving is the leaving fact's G, Stones, Sure, Added, Removed and Unclear
		leaving [6]int64
		counted map[int64]leavingAdded
	}{
		// a hand works at g15, after the calm g14: the leaving point is the last frame, g17, where 3 is kept at x 0.05
		{"worked", [6]int64{17, 3, 2, 0, 0, 1}, map[int64]leavingAdded{3: kept(0.05, 0)}},
		// the same, with a stone 5 boxed over 0.32 of 3's box from g15: 5 is counted (boxed on the last frame), 3 is
		// hidden
		{"worked under seen", [6]int64{17, 3, 2, 0, 0, 1}, map[int64]leavingAdded{5: seen(0.2, 0.05)}},
		// boxed but held at the leaving point g14, kept after: neither version 6 nor version 7 counts it
		{"held seen", [6]int64{14, 2, 2, 0, 0, 0}, map[int64]leavingAdded{}},
		// boxed at the leaving point g14 with 0.32 of its box under a kept 5: version 6 counts it whatever the overlap
		// (and 5, under 3, is hidden)
		{"seen under kept", [6]int64{14, 3, 2, 0, 0, 1}, map[int64]leavingAdded{3: seen(0, 0)}},
		// held only after the leaving point: counted, the leaving point's row decides
		{"held later", [6]int64{14, 3, 2, 0, 0, 1}, map[int64]leavingAdded{3: kept(0, 0)}},
		// its box grows to 0.36 after the leaving point: counted with the leaving point's size
		{"resized later", [6]int64{14, 3, 2, 0, 0, 1}, map[int64]leavingAdded{3: kept(0, 0)}},
		// 0.32 of its box under a tray stone's (stone 1, there since the arrival, itself doubted for the overlap):
		// hidden
		{"under linked", [6]int64{17, 2, 1, 0, 0, 0}, map[int64]leavingAdded{}},
		// 0.40 of its box under the box of a stone off the board (never on the tray): hidden all the same
		{"under offboard", [6]int64{17, 2, 2, 0, 0, 0}, map[int64]leavingAdded{}},
		// 0.32 under a kept 5 at the leaving point g14 that is gone by the last frame: hidden (and 5 is not on the
		// tray)
		{"gone later", [6]int64{14, 2, 2, 0, 0, 0}, map[int64]leavingAdded{}},
		// kept in another mode (transit, not detected): the world keeps it on the tray all the same
		{"transit", [6]int64{14, 3, 2, 0, 0, 1}, map[int64]leavingAdded{3: kept(0, 0)}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rock := func(id int64, x, y float64, mode string) StoneView {
				return leavingStone(id, x, y, mode, mode == "seen", box)
			}
			worked := c.name == "worked" || c.name == "worked under seen"
			var closed []Tray
			ledger := newTrayLedger(&TrayCounts{}, 1, "sample", func(tr Tray) { closed = append(closed, tr) })
			for g := int64(0); g < 18; g++ {
				later := g >= 15
				var events []WorldEvent
				if g == 15 && worked {
					events = []WorldEvent{{Type: EventPlace, Side: "left", ID: Int64(1)}}
				} else if g == 15 && c.name != "under linked" && c.name != "under offboard" {
					events = []WorldEvent{{Type: EventNew, ID: Int64(100)}}
				}
				one := rock(1, -0.6, 0, "seen")
				if c.name == "under linked" {
					one = rock(1, 0.2, 0.05, "seen")
				}
				stones := []StoneView{one, rock(2, 0.6, 0, "seen")}
				if g >= 10 {
					mode, detected := "kept", false
					switch {
					case g < 12:
						mode, detected = "seen", true
					case c.name == "held seen" && g <= 14:
						mode, detected = "carry", true
					case c.name == "transit":
						mode = "transit"
					}
					if c.name == "seen under kept" && g <= 14 {
						mode, detected = "seen", true
					}
					if c.name == "held later" && later {
						mode, detected = "carry", false
					}
					x, dims := 0.0, box
					if c.name == "under offboard" {
						x = 0.95
					} else if later && c.name == "worked" {
						x = 0.05
					}
					if c.name == "resized later" && later {
						dims = [2]float64{0.36, 0.2}
					}
					stones = append(stones, leavingStone(3, x, 0, mode, detected, dims))
					if c.name == "worked under seen" && later {
						stones = append(stones, rock(5, 0.2, 0.05, "seen"))
					}
					if c.name == "seen under kept" || (c.name == "gone later" && !later) {
						mode := "kept"
						if g < 12 {
							mode = "seen"
						}
						stones = append(stones, rock(5, 0.2, 0.05, mode))
					}
					if c.name == "under offboard" {
						stones = append(stones, rock(9, 1.15, 0, "seen"))
					}
				}
				n := int64(len(stones))
				ledger.feed(&WorldLayer{Motion: "static", Stones: stones, Events: events, Draw: leavingBoard,
					Stats: WorldStats{Stones: &n, Confirmed: &n}}, TrayPoint{G: g, T: float64(g) * 0.2})
			}
			ledger.close()
			if len(closed) != 1 {
				t.Fatalf("%d trays closed, want 1", len(closed))
			}
			tray := closed[0]
			l := tray.Facts[len(tray.Facts)-1]
			if got := [6]int64{l.G, l.Stones, l.Sure, l.Added, l.Removed, l.Unclear}; l.Kind != TrayLeaving || got != c.leaving {
				t.Fatalf("the leaving (g, stones, sure, added, removed, unclear): %v, want %v", got, c.leaving)
			}
			checkLeavingAdded(t, tray, c.counted)
		})
	}
}

// TestASecondStopCountsWhatItsCleanFrameKept: motion at g13 separates two visits; at the second stop a silent absence
// of stone 2 at g23 interrupts its one three-frame quiet run, so the tray leaves at its nominated clean frame, g22. An
// unidentified stone 5, boxed at (0.30, 0.30) in the first visit, kept at (0.32, 0.30) through g22 and at (0.36, 0.30)
// after, is counted at its place there (version 7); boxed there, with that place as its LeftPlace too (version 6);
// under a kept stone 6 that comes at g21, it is not, and nor is 6.
func TestASecondStopCountsWhatItsCleanFrameKept(t *testing.T) {
	small := [2]float64{0.05, 0.04}
	for _, c := range []struct {
		name    string
		counted map[int64]leavingAdded
	}{
		{"kept", map[int64]leavingAdded{5: {Place: [2]float64{0.32, 0.3}, Size: small}}},
		{"seen", map[int64]leavingAdded{5: {Place: [2]float64{0.32, 0.3}, Size: small, Left: [2]float64{0.32, 0.3},
			Seen: true}}},
		{"hidden", map[int64]leavingAdded{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			rock := func(id int64, x, y float64, seen bool) StoneView {
				if seen {
					return leavingStone(id, x, y, "seen", true, small)
				}
				return leavingStone(id, x, y, "kept", false, small)
			}
			var closed []Tray
			ledger := newTrayLedger(&TrayCounts{}, 1, "sample", func(tr Tray) { closed = append(closed, tr) })
			for g := int64(0); g < 26; g++ {
				first := -0.5
				if g > 22 {
					first = -0.48
				}
				stones := []StoneView{rock(1, first, 0, true)}
				if g != 23 {
					stones = append(stones, rock(2, 0, 0, true))
				}
				stones = append(stones, rock(3, 0.6, 0, g < 21))
				if g >= 8 {
					x := 0.36
					if g <= 12 {
						x = 0.30
					} else if g <= 22 {
						x = 0.32
					}
					stones = append(stones, rock(5, x, 0.30, g <= 12 || (c.name == "seen" && g == 22)))
					if c.name == "hidden" && g >= 21 {
						stones = append(stones, rock(6, 0.33, 0.30, false))
					}
				}
				var events []WorldEvent
				if (g >= 14 && g <= 20) || g >= 24 {
					events = []WorldEvent{{Type: EventNew, ID: Int64(100)}}
				}
				motion := "static"
				if g == 13 {
					motion = "moving"
				}
				n := int64(len(stones))
				ledger.feed(&WorldLayer{Motion: motion, Stones: stones, Events: events, Draw: leavingBoard,
					Stats: WorldStats{Stones: &n, Confirmed: &n}}, TrayPoint{G: g, T: float64(g) / 5})
			}
			ledger.close()
			if len(closed) != 1 {
				t.Fatalf("%d trays closed, want 1", len(closed))
			}
			tray := closed[0]
			l := tray.Facts[len(tray.Facts)-1]
			k := int64(len(c.counted))
			if l.Kind != TrayLeaving || l.G != 22 || l.Stones != 3+k || l.Sure != 2 || l.Unclear != k {
				t.Fatalf("the leaving: g%d, %d stones (%d sure), %d unclear; want g22, %d, 2, %d", l.G, l.Stones, l.Sure,
					l.Unclear, 3+k, k)
			}
			checkLeavingAdded(t, tray, c.counted)
		})
	}
}
