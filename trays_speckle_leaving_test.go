package pdiggerfactory

// The leaving's speckle answer (section 4.6.13 of the SDK design) beyond the golden files: tray 3 of
// trays-speckle-synthetic as values, which answer is the leaving's, which pairs link a tray stone and what linking
// does, a moved stone's world stone freed on a tray of its own, the evidence of a stone, and the self-check's C3, C4
// and C9 on accounts made broken on purpose. The rules themselves are golden-tested over trays-speckle-synthetic (its
// tray 3) and dry1-0018-0153.

import (
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"testing"
)

// speckleTrays are the trays of trays-speckle-synthetic.
func speckleTrays(t *testing.T) []Tray {
	t.Helper()
	path := filepath.Join("fixtures", "recordings", "trays-speckle-synthetic.ndjson.gz")
	trays, err := mustReplay(t, path).Trays(testCtx(t))
	if err != nil || len(trays) != 3 {
		t.Fatal(len(trays), err)
	}
	return trays
}

// speckleFind is a find of world stone id, scored score and second when scores holds both (else not checked).
func speckleFind(id int64, scores ...float64) SpeckleFind {
	f := SpeckleFind{ID: id}
	if len(scores) == 2 {
		f.Score, f.Second = Float64(scores[0]), Float64(scores[1])
	}
	return f
}

// TestTheLeavingsSpeckleAnswer replays the synthetic speckle recording and checks tray 3's leaving beyond its golden
// files: the identified facts of its answer as values, which stones the answer linked or confirmed, every stone's
// evidence and the confidence of every change; no stone has evidence while its tray is on, and trays 1 and 2, whose
// answers are none a leaving's, are left as without answers.
func TestTheLeavingsSpeckleAnswer(t *testing.T) {
	core := NewInterpreterCore()
	open := 0
	replaySpeckle(t, core, func(u *Update) {
		if tr := u.State.Tray; tr != nil {
			for _, s := range tr.Stones {
				if open++; s.Evidence != "" || s.BySpeckle {
					t.Errorf("g %d, the open tray %d: %+v", u.Frame.G, tr.N, s)
				}
			}
		}
	})
	trays := core.Trays()
	if len(trays) != 3 || open == 0 {
		t.Fatal(len(trays), open)
	}
	for _, tr := range trays[:2] {
		for _, s := range tr.Stones {
			if s.BySpeckle || s.Evidence != EvidenceUndecided {
				t.Errorf("tray %d: %+v", tr.N, s)
			}
		}
	}
	tr := trays[2]
	if bad := tr.Check(); len(bad) != 0 {
		t.Fatal(bad)
	}
	id := func(wid int64) string { return tr.Stones[stoneOf(t, tr, wid)].ID }
	type fact struct {
		code               string
		world              int64
		from               *int64
		score, margin, bar int64
	}
	want := map[string]fact{
		id(61): {SpeckleSame, 61, nil, 600, 500, 300}, id(62): {SpeckleSame, 62, nil, 620, 520, 300},
		id(63): {SpeckleSame, 63, nil, 600, 480, 300}, id(65): {SpeckleFound, 75, nil, 660, 560, 300},
		id(66): {SpeckleMoved, 67, Int64(66), 640, 540, 300}, id(67): {SpeckleMoved, 66, Int64(67), 630, 530, 300},
	}
	got := map[string]fact{}
	for _, x := range tr.Facts {
		if x.Kind != TrayIdentified {
			continue
		}
		if !bySpeckle(x) || x.Certain != nil || x.WorldID == nil || x.Score == nil || x.Margin == nil ||
			x.Bar == nil || x.G != tr.Leaving.G {
			t.Fatalf("%+v", x)
		}
		got[x.Stone] = fact{x.Code, *x.WorldID, x.FromWorld, *x.Score, *x.Margin, *x.Bar}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("identified by speckle:\n got  %+v\n want %+v", got, want)
	}
	// linked or confirmed by the answer: sure, by speckle; 68's pair would have moved it, which nothing disturbed
	evidence := map[int64]string{61: EvidenceFound, 62: EvidenceFound, 63: EvidenceFound, 64: EvidenceFound,
		65: EvidenceFound, 66: EvidenceFound, 67: EvidenceFound, 68: EvidenceFound, 69: EvidenceNotFound,
		71: EvidenceNotFound, 72: EvidenceFound, 73: EvidenceUndecided, 78: EvidenceFound}
	for wid, e := range evidence {
		s := tr.Stones[stoneOf(t, tr, wid)]
		bySpeckle := wid >= 61 && wid <= 67 && wid != 64
		if s.Evidence != e || s.BySpeckle != bySpeckle || (bySpeckle && !s.Sure) {
			t.Errorf("%d: %+v", wid, s)
		}
	}
	if s := tr.Stones[stoneOf(t, tr, 68)]; !s.Sure || s.Touched != nil || !sameID(s.WorldID, Int64(68)) {
		t.Errorf("68: %+v", s)
	}
	r := tr.Report()
	confidences := map[string]float64{}
	for _, c := range slices.Concat(r.Removed, r.Added, r.Moved) {
		confidences[c.Kind+" "+c.Stone] = c.Confidence
	}
	if want := map[string]float64{
		"removed " + id(64): ConfidenceRemovedCertainFound, "removed " + id(69): ConfidenceRemovedUnclearNotFound,
		"added " + id(71): ConfidenceAddedUnclearNotFound, "added " + id(72): ConfidenceAddedUnclearFound,
		"added " + id(73): ConfidenceAddedUnclearUndecided, "added " + id(78): ConfidenceAddedUnclearFound,
		"moved " + id(62): MovedConfidence, "moved " + id(65): MovedConfidence, "moved " + id(66): MovedConfidence,
		"moved " + id(67): MovedConfidence,
	}; !reflect.DeepEqual(confidences, want) {
		t.Errorf("confidences:\n got  %v\n want %v", confidences, want)
	}
}

// TestTheLeavingsAnswerIsTheNewestNearTheLeavingPoint: of the answers read while the tray was on, the newest whose
// later moment lies within SpeckleNear frames of the leaving point; none when none does.
func TestTheLeavingsAnswerIsTheNewestNearTheLeavingPoint(t *testing.T) {
	at := func(g int64) *WorldSpeckle { return &WorldSpeckle{At: SpeckleMoment{G: g}} }
	near, far, newer, later := at(31), at(27), at(28), at(33)
	tr := &openTray{}
	if leavingAnswer(tr, TrayPoint{G: 30}) != nil {
		t.Fatal("an answer without answers")
	}
	tr.answers = []*WorldSpeckle{near, far, newer, later}
	cases := map[int64]*WorldSpeckle{30: newer, 33: later, 29: newer, 26: newer, 25: far, 36: nil, 20: nil}
	for g, want := range cases {
		if got := leavingAnswer(tr, TrayPoint{G: g}); got != want {
			t.Errorf("leaving point g%d: %+v, want %+v", g, got, want)
		}
	}
}

// TestWhichPairsLinkATrayStone builds a tray at its leaving and an answer with a pair for each rule of section 4.6.13,
// in the answer's order: a pair links a stone the tray arrived with (on the tray or lost) to a world stone of the
// leaving point's world (or of the last frame's) that is not held, never a world stone or a stone twice, never to move
// a trusted stone or to take a trusted stone's world stone, and only with a scored find (but for a sure stone that
// stays, which needs no fact); then the links it makes and the facts it gives.
func TestWhichPairsLinkATrayStone(t *testing.T) {
	ref := TrayPoint{G: 30, T: 6}
	tr := &openTray{n: 1, k: 4, link: map[int64]*trayStone{}, unid: map[int64]*trayUnid{},
		arrivalLinks: map[int64]string{}}
	stone := func(wid int64, status string, doubted bool) *trayStone {
		s := &trayStone{id: "s-" + strconv.FormatInt(wid, 10), index: len(tr.stones), linked: status == TrayStoneOn,
			world: wid, status: status, sure: true, doubted: doubted, size: vec2{0.3, 0.2}, firstWorld: wid}
		tr.stones = append(tr.stones, s)
		tr.arrivalLinks[wid] = s.id
		if s.linked {
			tr.link[wid] = s
		}
		return s
	}
	a := stone(1, TrayStoneOn, false) // trusted
	b := stone(2, TrayStoneOn, true)  // doubted, in question
	b.question = &trayQuestion{cause: "pushed", why: "it was pushed", since: 21}
	lost := stone(3, TrayStoneLost, false)
	stone(4, TrayStoneRemoved, false)
	e := stone(5, TrayStoneOn, false)        // trusted
	stone(6, TrayStoneOn, true).added = true // never among the arrival links on a real tray
	stone(7, TrayStoneOn, true)              // no scored find for its pair
	stone(8, TrayStoneOn, true)              // its pair's later find not checked
	asked := stone(9, TrayStoneOn, true)     // in question on its pair's world stone
	asked.question = &trayQuestion{cause: "unseen", why: "not seen at two calm moments"}
	unsure := stone(15, TrayStoneOn, true) // not sure on its pair's world stone
	unsure.sure = false
	stays := stone(16, TrayStoneOn, true) // in question on its pair's world stone, with no find
	stays.question = &trayQuestion{cause: "unseen", why: "not seen at two calm moments"}
	tr.unid[10] = &trayUnid{born: TrayPoint{G: 25, T: 5}}
	world := func(g int64, ids ...int64) *trayWorld {
		var stones []StoneView
		for _, id := range ids {
			mode := "kept"
			if id == 11 {
				mode = "carry"
			}
			solid := &Solid{Pos: []float64{float64(id) / 10, 0}, Dims: []float64{0.3, 0.2}, Q: []float64{0, 0, 0, 1}}
			stones = append(stones, StoneView{ID: id, Mode: mode, Shape: "frozen", Detected: true, Solid: solid})
		}
		return newTrayWorld(&WorldLayer{Motion: "static", Stones: stones}, TrayPoint{G: g, T: float64(g) / 5})
	}
	tr.leaveWorld = world(30, 1, 2, 5, 6, 7, 8, 9, 10, 11, 13, 14, 15, 16) // 12 only in the last frame's world
	tw := world(31, 1, 2, 5, 6, 7, 8, 9, 10, 12, 13, 14, 15, 16)
	sp := &WorldSpeckle{At: SpeckleMoment{G: 30},
		Refs: []SpeckleFind{speckleFind(1, 0.6, 0.1), speckleFind(2, 0.7, 0.1), speckleFind(3, 0.4, 0.3),
			speckleFind(7), speckleFind(9, 0.65, 0.2), speckleFind(15, 0.55, 0.05)},
		Stones: []SpeckleFind{speckleFind(10, 0.5, 0.1), speckleFind(12, 0.6, 0.1), speckleFind(14)},
		Pairs: [][2]int64{
			{10, 99}, // no stone linked to 99 at the arrival
			{10, 6},  // an added stone
			{10, 4},  // a removed stone
			{20, 2},  // no world stone 20, in either world
			{11, 2},  // 11 is held
			{10, 1},  // 1 is trusted, on another world stone
			{5, 2},   // 5 is the trusted stone e's
			{12, 3},  // links the lost stone; its find is not clean, so the later find is the pair's
			{10, 2},  // links b
			{10, 3},  // 10 is taken, and so is the lost stone
			{13, 2},  // b is taken
			{13, 7},  // no find scored: 7 not checked, 13 not in the later finds
			{14, 8},  // no find scored: 8 not in the arrival finds, 14 not checked
			{1, 1},   // a stays where it is
			{9, 9},   // in question, stays: its question ends
			{15, 15}, // not sure, stays: sure again
			{16, 16}, // in question, stays, with no find: its question ends, no fact needs one
		}}
	var links []string
	for _, x := range speckleTargets(tr, sp, tw) {
		find := "-"
		if x.x != nil {
			find = strconv.FormatInt(x.x.ID, 10)
		}
		links = append(links, x.s.id+" "+strconv.FormatInt(x.c, 10)+" "+find+" "+x.status)
	}
	want := []string{"s-3 12 12 lost", "s-2 10 2 on", "s-1 1 1 on", "s-9 9 9 on", "s-15 15 15 on", "s-16 16 - on"}
	if !slices.Equal(links, want) {
		t.Fatalf("pairs %v, want %v", links, want)
	}
	tr.answers = []*WorldSpeckle{sp}
	l := newTrayLedger(&TrayCounts{}, 1, "v", func(Tray) {})
	var out []TrayFact
	l.speckleLeaving(tr, tw, ref, &out)
	if tr.leavingSpeckle != sp {
		t.Fatal("the leaving's answer is not kept")
	}
	var facts []string
	for _, x := range out {
		facts = append(facts, x.Describe()+" "+x.Code+" from "+idText(x.FromWorld)+" margin "+milli(*x.Margin)+
			" bar "+milli(*x.Bar))
	}
	if want := []string{
		"0:06.0 tray 1: stone s-3 found again by its speckle on world stone 12 (0.600) speckle_found from ? " +
			"margin 0.500 bar 0.300",
		"0:06.0 tray 1: stone s-2 recognised by its speckle on world stone 10 (0.700) speckle_moved from 2 " +
			"margin 0.600 bar 0.300",
		"0:06.0 tray 1: stone s-15 confirmed by its speckle on world stone 15 (0.550) speckle_same from ? " +
			"margin 0.500 bar 0.300",
	}; !slices.Equal(facts, want) {
		t.Fatalf("facts:\n%v\nwant\n%v", facts, want)
	}
	// the lost stone is on 12 as the last frame shows it; b on 10, which it takes with its first sight; b's world
	// stone 2, on the board, not held and in no pair, is unidentified again (freed)
	if !lost.linked || lost.status != TrayStoneOn || lost.world != 12 || lost.worldT != 6 ||
		lost.place != (vec2{1.2, 0}) || lost.seenT != 6 || !sameID(lost.touched, Int64(30)) || lost.atK != 4 ||
		lost.at != (vec2{1.2, 0}) {
		t.Errorf("the lost stone: %+v", lost)
	}
	if b.world != 10 || b.worldT != 5 || b.question != nil || tr.link[10] != b || tr.link[2] != nil ||
		tr.unid[10] != nil {
		t.Errorf("b: %+v", b)
	}
	if u := tr.unid[2]; u == nil || !u.freed || u.born != ref || u.place != (vec2{0.2, 0}) ||
		u.size != (vec2{0.3, 0.2}) {
		t.Errorf("world stone 2: %+v", u)
	}
	for _, s := range tr.stones {
		linked := slices.Contains([]*trayStone{lost, b, a, asked, unsure, stays}, s)
		if s.bySpeckle != linked || (linked && (!s.sure || s.question != nil)) {
			t.Errorf("%s: by speckle %v, sure %v, question %v", s.id, s.bySpeckle, s.sure, s.question)
		}
	}
	if e.bySpeckle || tr.link[5] != e || a.touched != nil || asked.touched != nil || stays.touched != nil {
		t.Errorf("trusted stones: %+v %+v", e, a)
	}
}

// speckleScene is a tray at 5 frames a second: stones 1 (x -0.6), 2 (x 0) and 3 (x 0.6) arrive at g4; a hand pushes 2
// at g15, so it is not sure from the settle at g18; world stone 7 comes at (0, -0.4) at g20, where the hand pushed 2,
// while 2's box stays (held in the last frame, when held); calm to g30, the leaving point; g31 moves and carries the
// answer, read while the tray is on: 1, 2 and 3 at the arrival are 1, 7 and 3 at g30.
func speckleScene(t *testing.T, held bool) Tray {
	t.Helper()
	var trays []Tray
	ledger := newTrayLedger(&TrayCounts{}, 1, "dry1", func(tr Tray) { trays = append(trays, tr) })
	for g := int64(0); g <= 30; g++ {
		stones := []StoneView{handStone(1, -0.6), handStone(2, 0), handStone(3, 0.6)}
		var events []WorldEvent
		left := "free"
		if g == 15 {
			events, left = []WorldEvent{{Type: EventPush, ID: Int64(2), Side: "left"}}, "touch"
		}
		if g >= 20 {
			seven := handStone(7, 0)
			seven.Solid.Pos = []float64{0, -0.4, 0.046}
			stones = append(stones, seven)
		}
		if g == 20 {
			events = []WorldEvent{{Type: EventNew, ID: Int64(7)}}
		}
		if g == 30 && held {
			stones[1].Mode = "carry"
		}
		n := int64(len(stones))
		ledger.feed(&WorldLayer{Motion: "static", Stones: stones, Events: events,
			Hands: WorldHands{Left: &HandState{State: left}, Right: &HandState{State: "free"}},
			Stats: WorldStats{Confirmed: &n, Stones: &n}}, TrayPoint{G: g, T: float64(g) / 5})
	}
	here := TrayPoint{G: 31, T: 6.2}
	ledger.feed(&WorldLayer{Motion: "moving"}, here)
	answer := &WorldSpeckle{V: 1, Tray: 1, Ref: SpeckleMoment{G: 4, T: 0.8}, At: SpeckleMoment{G: 30, T: 6},
		Refs: []SpeckleFind{speckleFind(1, 0.6, 0.1), speckleFind(2, 0.62, 0.1), speckleFind(3, 0.6, 0.1)},
		Stones: []SpeckleFind{speckleFind(1, 0.6, 0.1), speckleFind(7, 0.62, 0.1), speckleFind(3, 0.6, 0.1),
			speckleFind(2, 0.1, 0.05)},
		Pairs: [][2]int64{{1, 1}, {7, 2}, {3, 3}}}
	if a := ledger.speckle(answer, here, func(int64) map[int64]string { return nil }); a == nil || a.Tray != 1 {
		t.Fatalf("the answer is no tray's: %+v", a)
	}
	ledger.close()
	if len(trays) != 1 {
		t.Fatalf("%d trays", len(trays))
	}
	return trays[0]
}

// TestAMovedStoneFreesItsWorldStone: the leaving's answer links stone 2, not sure, to world stone 7; 2's own world
// stone, free on the board, is unidentified again, so the leaving counts it as an unclear addition (cause freed) that
// the answer did not find at the arrival; held, it is not counted. Stone 2 is reported moved.
func TestAMovedStoneFreesItsWorldStone(t *testing.T) {
	for _, held := range []bool{false, true} {
		tr := speckleScene(t, held)
		want := []string{
			"0:00.8 tray 1 arrived: 3 stones",
			"0:03.6 tray 1: stone s1t0.8-1-2 is not sure any more (it was pushed)",
			"0:06.0 tray 1: stone s1t0.8-1-2 recognised by its speckle on world stone 7 (0.620)",
			"0:06.0 tray 1: stone s1t6.0-1-2 not recognised (unclear)",
			"0:06.0 tray 1 leaving: 4 stones (3 sure), 0 added, 0 removed, 1 unclear",
		}
		report := "tray 1, 0:00.8 to 0:06.0: 0 removed, 1 added, 1 moved, not complete: 1 unclear\n" +
			"  0:06.0 added s1t6.0-1-2 at (0.00, 0.00) (unclear, 0.96)\n" +
			"  moved s1t0.8-1-2 0.40 to (0.00, -0.40) (0.87)"
		if held {
			want = append(want[:3], "0:06.0 tray 1 leaving: 3 stones (3 sure), 0 added, 0 removed, 0 unclear")
			report = "tray 1, 0:00.8 to 0:06.0: 0 removed, 0 added, 1 moved\n" +
				"  moved s1t0.8-1-2 0.40 to (0.00, -0.40) (0.87)"
		}
		var facts []string
		for _, x := range tr.Facts {
			facts = append(facts, x.Describe())
		}
		if !slices.Equal(facts, want) {
			t.Fatalf("held %v: facts\n%v\nwant\n%v", held, facts, want)
		}
		if r := tr.Report().Describe(); r != report {
			t.Fatalf("held %v: report\n%s\nwant\n%s", held, r, report)
		}
		if bad := tr.Check(); len(bad) != 0 {
			t.Fatalf("held %v: %v", held, bad)
		}
		moved := tr.Stones[1]
		if !moved.Sure || !moved.BySpeckle || !sameID(moved.WorldID, Int64(7)) || moved.Evidence != EvidenceFound {
			t.Errorf("held %v: %+v", held, moved)
		}
		if !held && (len(tr.Open) != 1 || tr.Open[0].Cause != "freed" || tr.Stones[3].Evidence != EvidenceNotFound) {
			t.Errorf("the freed world stone: %+v %+v", tr.Open, tr.Stones[3])
		}
	}
}

// TestTheEvidenceOfAStone: what the leaving's answer says of a world id on each side (a pair, then a clean find, then
// a find of SpeckleClean or more, then none; the first find of an id counts), and which world id a tray stone's
// evidence reads.
func TestTheEvidenceOfAStone(t *testing.T) {
	sp := &WorldSpeckle{
		Refs: []SpeckleFind{speckleFind(1, 0.6, 0.1), speckleFind(2, 0.45, 0.3), speckleFind(3, 0.2, 0.1),
			speckleFind(4), speckleFind(5, 0.2, 0.15), speckleFind(1, 0.1, 0.05), speckleFind(8, 0.3, -1)},
		Stones: []SpeckleFind{speckleFind(11, 0.6, 0.1), speckleFind(12, 0.1, 0.05), speckleFind(15, 0.25, 0.2)},
		Pairs:  [][2]int64{{15, 5}, {16, 6}},
	}
	for _, c := range []struct {
		arrival bool
		wid     int64
		want    string
	}{
		{true, 1, EvidenceFound},     // clean; its second find does not count
		{true, 2, EvidenceUndecided}, // 0.450 is not twice 0.300: look-alikes
		{true, 3, EvidenceNotFound},  // checked, no find of SpeckleClean
		{true, 4, EvidenceUndecided}, // not checked
		{true, 5, EvidenceFound},     // paired, though its own find is not clean
		{true, 6, EvidenceUndecided}, // paired, but not in the answer's arrival finds
		{true, 8, EvidenceFound},     // 0.300 with no second place (-1)
		{true, 11, EvidenceUndecided},
		{false, 11, EvidenceFound},
		{false, 12, EvidenceNotFound},
		{false, 15, EvidenceFound}, // paired by its later id
		{false, 5, EvidenceUndecided},
	} {
		if got := speckleEvidence(sp, c.arrival, c.wid); got != c.want {
			t.Errorf("arrival %v, world stone %d: %s, want %s", c.arrival, c.wid, got, c.want)
		}
	}
	if got := speckleEvidence(nil, true, 1); got != EvidenceUndecided {
		t.Errorf("no answer: %s", got)
	}
	tr := &openTray{leavingSpeckle: sp}
	for _, c := range []struct {
		s    trayStone
		want string
	}{
		{trayStone{firstWorld: 3, world: 11, status: TrayStoneOn}, EvidenceNotFound}, // arrived as 3, now on 11
		{trayStone{firstWorld: 1, status: TrayStoneRemoved}, EvidenceFound},          // removed, found as the tray left
		{trayStone{firstWorld: 12, world: 11, status: TrayStoneOn, added: true}, EvidenceFound},
		{trayStone{firstWorld: 12, world: 12, status: TrayStoneRemoved, added: true}, EvidenceUndecided},
		{trayStone{firstWorld: 12, world: 12, status: TrayStoneLost, added: true}, EvidenceUndecided},
	} {
		if got := stoneEvidence(tr, &c.s); got != c.want {
			t.Errorf("%+v: %s, want %s", c.s, got, c.want)
		}
	}
	if got := stoneEvidence(&openTray{}, &trayStone{firstWorld: 1}); got != EvidenceUndecided {
		t.Errorf("no leaving answer: %s", got)
	}
}

// TestCheckTheLeavingsSpeckleFacts: the self-check (section 4.6.10) of tray 3 of trays-speckle-synthetic, whose
// stones not sure are sure again through the leaving's answer, on copies broken on purpose.
func TestCheckTheLeavingsSpeckleFacts(t *testing.T) {
	tr := speckleTrays(t)[2]
	if got := tr.Check(); got == nil || len(got) != 0 {
		t.Fatalf("%#v", got)
	}
	i := slices.IndexFunc(tr.Facts, func(x TrayFact) bool { return x.Code == SpeckleSame })
	same := tr.Facts[i] // 61's, after its unsure fact
	s := stoneOf(t, tr, 61)
	if same.Stone != tr.Stones[s].ID || !tr.Stones[s].Sure {
		t.Fatalf("%+v %+v", same, tr.Stones[s])
	}
	insert := func(xs ...TrayFact) []TrayFact { return slices.Concat(tr.Facts[:i+1], xs, tr.Facts[i+1:]) }
	first := func(match func(TrayFact) bool) TrayFact { return tr.Facts[slices.IndexFunc(tr.Facts, match)] }
	unsure := first(func(x TrayFact) bool { return x.Kind == TrayUnsure && x.Stone == same.Stone })
	gone := first(func(x TrayFact) bool { return x.Kind == TrayRemoved && isTrue(x.Certain) }) // 64's
	look, late := same, same
	look.Code, late.Stone = "look_found", gone.Stone
	n := len(tr.Facts)
	wantCode(t, "C3", map[string][]string{
		"two identified facts of the speckle": withFacts(tr, insert(same)),
		"identified by look after unsure": withFacts(tr, slices.Concat(tr.Facts[:i], []TrayFact{look},
			tr.Facts[i+1:])),
		"identified by the speckle after a removal": withFacts(tr, slices.Concat(tr.Facts[:n-1], []TrayFact{late},
			tr.Facts[n-1:])),
	})
	wantCode(t, "C4", map[string][]string{
		"not sure after its speckle fact":       withStone(tr, s, func(x *TrayStone) { x.Sure = false }),
		"an unsure fact after its speckle fact": withFacts(tr, insert(unsure)),
		"sure with no speckle fact":             withFacts(tr, slices.Delete(slices.Clone(tr.Facts), i, i+1)),
	})
	checks := map[string][]string{}
	for name, change := range map[string]func(*TrayFact){
		"a look's bar":  func(x *TrayFact) { x.Bar, x.Score = Int64(750), Int64(760) },
		"below its bar": func(x *TrayFact) { x.Score = Int64(SpeckleClean - 1) },
		"certain":       func(x *TrayFact) { x.Certain = Bool(true) },
		"no score":      func(x *TrayFact) { x.Score = nil },
		"no bar":        func(x *TrayFact) { x.Bar = nil },
	} {
		facts := slices.Clone(tr.Facts)
		change(&facts[i])
		checks[name] = withFacts(tr, facts)
	}
	wantCode(t, "C9", checks)
	// a speckle fact has no margin rule, unlike a look's
	facts := slices.Clone(tr.Facts)
	facts[i].Margin = Int64(10)
	if got := withFacts(tr, facts); len(got) != 0 {
		t.Fatal(got)
	}
}
