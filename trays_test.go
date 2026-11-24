package pdiggerfactory

// Trays and tray stones (section 4.6 of the SDK design) beyond the golden files: the values, ids without a clock,
// the live streams, the state, the events' tray stones and the trays the core keeps. The rules themselves are
// golden-tested over every recording, trays-synthetic among them (the rules the real excerpts never reach), and a
// rename that two lost stones fit on an edited copy of it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

var synthPath = filepath.Join("fixtures", "recordings", "trays-synthetic.ndjson.gz")

// feedSynth feeds core the frames of trays-synthetic (and its clock line when clocked), calling each with every update.
func feedSynth(t *testing.T, core *InterpreterCore, clocked bool, each func(*Update)) {
	t.Helper()
	ctx := testCtx(t)
	r, err := OpenReplay(ctx, synthPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for item, err := range r.Items(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		switch it := item.(type) {
		case *ClockItem:
			if clocked {
				core.SetClock(it.clockEvent())
			}
		case *FrameItem:
			if u := core.Feed(it.Frame); each != nil {
				each(u)
			}
		}
	}
}

func synthGolden(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("fixtures", "golden", "trays-synthetic.trays.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func factsText(facts []TrayFact) string {
	var b strings.Builder
	for _, f := range facts {
		b.WriteString(f.Describe() + "\n")
	}
	return b.String()
}

func trayNumbers(trays []Tray) []int64 {
	var ns []int64
	for _, t := range trays {
		ns = append(ns, t.N)
	}
	return ns
}

func f64(v float64) *float64 { return &v }

func TestTrayValues(t *testing.T) {
	ctx := testCtx(t)
	r, err := OpenReplay(ctx, synthPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	trays, err := r.Trays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ns := trayNumbers(trays); !slices.Equal(ns, []int64{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatal(ns)
	}
	first := trays[0]
	if a, l, ok := first.Window(); !ok || a != 0.8 || l != 5.2 {
		t.Fatal(a, l, ok)
	}
	if _, _, ok := trays[3].Window(); ok || trays[3].Arrived != nil || trays[3].Leaving != nil ||
		len(trays[3].Stones) != 0 { // a stop that never settled
		t.Fatalf("%+v", trays[3])
	}
	if first.Session != 1794684199 || first.Video != "dry1" || first.N != 1 || first.Leaving.G != 26 ||
		!reflect.DeepEqual(*first.Arrived, TrayPoint{G: 4, T: 0.8, Wall: f64(1794960000.8)}) {
		t.Fatalf("%+v", first)
	}
	// removed: not on the tray when it left (no LeftPlace); Touched is the frame a hand last touched it (it carried
	// it away); the recording has no speckle answer, so nothing decides whether it was found as the tray left
	want := TrayStone{ID: "20261118T000000.8Z-1-2", Status: TrayStoneRemoved, Sure: true, Certain: Bool(true),
		Born: TrayPoint{G: 4, T: 0.8, Wall: f64(1794960000.8)}, Place: [2]float64{0, 0}, Size: [2]float64{0.3, 0.2},
		FirstPlace: [2]float64{0, 0}, Touched: Int64(13), Evidence: EvidenceUndecided}
	if !reflect.DeepEqual(first.Stones[1], want) {
		t.Fatalf("%+v", first.Stones[1])
	}
	// seen in the leaving frame, never touched
	if s := first.Stones[0]; s.LeftPlace == nil || *s.LeftPlace != [2]float64{-0.6, 0} || s.Touched != nil {
		t.Fatalf("%+v", s)
	}
	fact := TrayFact{Kind: TrayRemoved, Tray: 1, Session: 1794684199, Video: "dry1", Stone: "20261118T000000.8Z-1-2",
		Certain: Bool(true), Why: "carried away", Code: "carried_away", G: 22, T: 4.4, Wall: f64(1794960004.4)}
	if !reflect.DeepEqual(first.Facts[1], fact) || first.Facts[1].String() != first.Facts[1].Describe() ||
		first.Facts[1].Clock() != "0:04.4" {
		t.Fatalf("%+v", first.Facts[1])
	}
	added := first.Stone("20261118T000003.8Z-1-7")
	if added == nil || !added.Added || added.Certain == nil || !*added.Certain || FormatClock(added.Born.T) != "0:03.8" {
		t.Fatalf("%+v", added)
	}
	if first.Stone("no such stone") != nil {
		t.Fatal("a stone that is not there")
	}
	leaving := first.Facts[len(first.Facts)-1]
	if leaving.Kind != TrayLeaving || leaving.Clock() != "0:05.2" || leaving.Stones != 3 || leaving.Sure != 3 ||
		leaving.Added != 1 || leaving.Removed != 1 || leaving.Unclear != 0 {
		t.Fatalf("%+v", leaving)
	}
	// every fact of the trays, in order, is what TrayFacts yields
	var all []TrayFact
	for _, tr := range trays {
		all = append(all, tr.Facts...)
	}
	var streamed []TrayFact
	for f, err := range r.TrayFacts(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		streamed = append(streamed, f)
	}
	if !reflect.DeepEqual(all, streamed) || factsText(all) != synthGolden(t) {
		t.Fatal(factsText(streamed))
	}
	for range r.TrayFacts(ctx) {
		break // a loop that stops
	}
}

// TestACertainAdditionCounts: tray 5's slab 47 is set down for certain, then gone after the last calm moment, so the
// tray's leaving finds it removed unclear; the leaving still counts its certain addition, as its facts and the
// summary do.
func TestACertainAdditionCounts(t *testing.T) {
	ctx := testCtx(t)
	r, err := OpenReplay(ctx, synthPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	trays, err := r.Trays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	five := trays[4]
	slab := five.Stone("20261118T000025.8Z-5-47")
	if slab == nil || !slab.Added || slab.Status != TrayStoneRemoved || slab.Certain == nil || *slab.Certain {
		t.Fatalf("%+v", slab) // its certain is its removal's
	}
	var said []string
	for _, f := range five.Facts {
		if f.Stone == slab.ID {
			said = append(said, f.Kind+" "+strconv.FormatBool(isTrue(f.Certain)))
		}
	}
	if !slices.Equal(said, []string{"added true", "removed false"}) {
		t.Fatal(said)
	}
	leaving := five.Facts[len(five.Facts)-1]
	if leaving.Stones != 2 || leaving.Sure != 2 || leaving.Added != 1 || leaving.Removed != 0 || leaving.Unclear != 1 {
		t.Fatalf("%+v", leaving)
	}
	if bad := five.Check(); len(bad) != 0 {
		t.Fatal(bad)
	}
	// C5 compares each of the leaving's counts with the account: one wrong count, and only C5 fails
	for name, wrong := range map[string]func(leaving *TrayFact){
		"added":   func(leaving *TrayFact) { leaving.Added = 0 },
		"removed": func(leaving *TrayFact) { leaving.Removed = 1 },
		"unclear": func(leaving *TrayFact) { leaving.Unclear = 0 },
	} {
		broken := five
		broken.Facts = slices.Clone(five.Facts)
		wrong(&broken.Facts[len(broken.Facts)-1])
		if bad := broken.Check(); !slices.Equal(bad, []string{"C5"}) {
			t.Fatal(name, bad)
		}
	}
	if s, err := r.Summary(ctx); err != nil || s.Trays.Added != 2 { // tray 1's slab and this one
		t.Fatal(s, err)
	}
}

// TestTrayStoneKeepsItsID: the world renames stone 12 to 17; the tray stone keeps its id and stands for 17 (still sure).
func TestTrayStoneKeepsItsID(t *testing.T) {
	ctx := testCtx(t)
	r, err := OpenReplay(ctx, synthPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	trays, err := r.Trays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tray := trays[1]
	s := tray.Stone("20261118T000006.4Z-2-12")
	if s == nil || s.WorldID == nil || *s.WorldID != 17 || s.Status != TrayStoneOn || !s.Sure {
		t.Fatalf("%+v", s)
	}
	i := slices.IndexFunc(tray.Facts, func(f TrayFact) bool { return f.Why == "found again, not for sure" })
	if f := tray.Facts[i]; f.Stone != "20261118T000006.4Z-2-13" || f.WorldID == nil || *f.WorldID != 18 {
		t.Fatalf("%+v", f) // the world id it stands for now
	}
}

// TestTrayRenameThatTwoLostStonesFit: a world stone that two lost tray stones fit is a rename of neither. On a copy of
// trays-synthetic edited from g 50 to the end of tray 2 (g 68), stones 15 and 16, whose boxes overlap, are gone and one
// stone, 19, stands between them (0.025 from each place). Each lost stone fits 19 alone, and 19 fits both: neither is
// found again by a rename; as the tray leaves, 16 is matched to 19 (not sure, as it was) and 15 is not found. No
// fixture recording reaches this rule; the lines are those of the reference interpreter and of the Python SDK for the
// same copy.
func TestTrayRenameThatTwoLostStonesFit(t *testing.T) {
	presEditCheck(t, presEdit{rec: "trays-synthetic", edit: func(g int64, w map[string]any) {
		s := edStone(w, "15")
		if g < 50 || g > 68 || s == nil {
			return
		}
		merged := edCopy(t, s)
		merged["id"] = json.Number("19")
		edSolid(merged, "pos")[0] = -0.575
		edWithout(w, "15")
		edWithout(w, "16")
		w["stones"] = append(w["stones"].([]any), merged)
		if g == 50 {
			edEvent(w, map[string]any{"type": "delete", "id": json.Number("15"), "why": "absent"})
			edEvent(w, map[string]any{"type": "delete", "id": json.Number("16"), "why": "absent"})
			edEvent(w, map[string]any{"type": "new", "id": json.Number("19")})
		}
	}, was: []string{"0:13.4 tray 2 leaving: 5 stones (1 sure), 0 added, 0 removed, 0 unclear"},
		now: []string{"0:13.4 tray 2: stone 20261118T000006.4Z-2-15 not found (unclear)",
			"0:13.4 tray 2 leaving: 4 stones (1 sure), 0 added, 0 removed, 1 unclear"}})
}

// TestTrayIDsWithoutAClock: without a clock state the stamp is the session and the media time, so ids stay unique
// after a seek back and in another session.
func TestTrayIDsWithoutAClock(t *testing.T) {
	for wall, want := range map[float64]string{1794960000.84: "20261118T000000.8Z", 1767225600: "20260101T000000.0Z",
		1772323199.96: "20260301T000000.0Z", -0.04: "19700101T000000.0Z", -0.06: "19691231T235959.9Z"} {
		if got := trayStamp(f64(wall), 0, 1); got != want {
			t.Errorf("%v: %s, want %s", wall, got, want)
		}
	}
	if got := trayStamp(nil, 22.5, 1795314878); got != "s1795314878t22.5" {
		t.Fatal(got)
	}
	core := NewInterpreterCore() // no clock fed
	var facts []TrayFact
	feedSynth(t, core, false, func(u *Update) { facts = append(facts, u.TrayFacts...) })
	facts = append(facts, core.End().TrayFacts...)
	lines := strings.Split(factsText(facts), "\n")
	if lines[1] != "0:04.4 tray 1: stone s1794684199t0.8-1-2 was removed (carried away)" ||
		lines[2] != "0:05.2 tray 1: stone s1794684199t3.8-1-7 was set down" {
		t.Fatal(lines[:3])
	}
	for _, f := range facts {
		if f.Wall != nil {
			t.Fatalf("%+v", f)
		}
	}
}

func TestEventsNameTheirTrayStone(t *testing.T) {
	core := NewInterpreterCore()
	seen := map[string]string{}
	trays := map[int64]bool{}
	feedSynth(t, core, true, func(u *Update) {
		for _, e := range u.Events {
			seen[e.Type+" "+idText(e.StoneID)] = e.TrayStone
		}
		if tr := u.State.Tray; tr != nil {
			trays[tr.N] = true
			if tr.Leaving != nil { // the open tray
				t.Fatalf("%+v", tr)
			}
		}
	})
	for k, want := range map[string]string{
		"gone 2":   "20261118T000000.8Z-1-2", // it stood for 2 before this frame
		"new 7":    "",                       // a stone not on the tray yet
		"lift 13":  "20261118T000006.4Z-2-13",
		"place 18": "20261118T000006.4Z-2-13", // renamed: the same tray stone
		// revisions: the tray stone before the revised frame, not the links of the newest one
		"confirm 2":  "20261118T000000.8Z-1-2", // frame 13, when 2 still stood for its tray stone
		"confirm 7":  "",                       // frame 26, still the newest, which set 7 down: none before it
		"confirm 17": "",                       // frame 58, whose rename linked 17
	} {
		if v, ok := seen[k]; !ok || v != want {
			t.Errorf("%s: %q (%v), want %q", k, v, ok, want)
		}
	}
	if len(trays) != 7 {
		t.Fatal(trays)
	}
	core.End()
	if core.State().Tray != nil {
		t.Fatal("a tray after the end")
	}
}

func TestCoreKeepsTheNewestTrays(t *testing.T) {
	core := NewInterpreterCore()
	core.KeepTrays(2)
	var last []int64
	feedSynth(t, core, true, func(u *Update) {
		if u.Frame.G == 80 { // tray 3 is on
			if ns := trayNumbers(core.Trays()); !slices.Equal(ns, []int64{1, 2, 3}) {
				t.Fatal(ns)
			}
		}
		last = trayNumbers(core.Trays())
	})
	if !slices.Equal(last, []int64{5, 6, 7}) { // tray 7 is on, the 2 newest that have left before it
		t.Fatal(last)
	}
	core.End()
	if ns := trayNumbers(core.Trays()); !slices.Equal(ns, []int64{6, 7}) {
		t.Fatal(ns)
	}
	one := NewInterpreterCore()
	one.KeepTrays(1)
	feedSynth(t, one, true, nil)
	one.End()
	if ns := trayNumbers(one.Trays()); !slices.Equal(ns, []int64{7}) {
		t.Fatal(ns)
	}
	all := NewInterpreterCore()
	all.KeepTrays(0)
	feedSynth(t, all, true, nil)
	if ns := trayNumbers(all.Trays()); !slices.Equal(ns, []int64{1, 2, 3, 4, 5, 6, 7}) || all.State().Tray.N != 7 {
		t.Fatal(ns) // tray 7 is still on
	}
}

func TestFinishLeavesTheTrayFactsInTrays(t *testing.T) {
	core := NewInterpreterCore()
	feedSynth(t, core, true, nil)
	if out := core.Finish(); out != nil {
		t.Fatal(out) // no activity was open
	}
	var facts []TrayFact
	for _, tr := range core.Trays() {
		facts = append(facts, tr.Facts...)
	}
	if got := factsText(facts); got != synthGolden(t) {
		t.Fatal(got)
	}
}

func TestInterpreterTrayFacts(t *testing.T) {
	ctx := testCtx(t)
	golden := synthGolden(t)
	r, err := OpenReplay(ctx, synthPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	frames := 0
	for range r.Frames(ctx) {
		frames++
	}
	r.Close()
	fake := newFake(t, factorytest.WithRecording(synthPath))
	c, _ := fakeClient(t, fake)
	// the connection ends after the last frame; without resume, so does the stream, with the final update (tray 7 leaves)
	in := c.Interpreter(Resume(false), ConnectionLimit(frames))
	var got []TrayFact
	for f, err := range in.TrayFacts(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, f)
	}
	if factsText(got) != golden {
		t.Fatal(factsText(got))
	}
	if ns := trayNumbers(in.Trays()); !slices.Equal(ns, []int64{1, 2, 3, 4, 5, 6, 7}) {
		t.Fatal(ns)
	}
	if u := in.End(); !u.Final || len(u.TrayFacts) != 0 { // the stream's end made the last tray leave
		t.Fatalf("%+v", u)
	}
	// three facts come with one frame (0:08.6); a loop that stops after the first leaves the other two to the next
	in = c.Interpreter()
	for f, err := range in.TrayFacts(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		if f.Clock() == "0:08.6" {
			break
		}
	}
	var rest []TrayFact
	for f, err := range in.TrayFacts(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		if rest = append(rest, f); rest[len(rest)-1].Kind == TrayLeaving {
			break // tray 2 leaves
		}
	}
	if rest[0].Describe() != "0:08.6 tray 2: stone 20261118T000006.4Z-2-15 is not sure any more (its box overlaps another stone)" ||
		!strings.Contains(golden, factsText(rest)) || len(rest) != 4 {
		t.Fatal(factsText(rest))
	}
}

func TestTrayFactsErrors(t *testing.T) {
	ctx := testCtx(t)
	// a recording that breaks after its header: TrayFacts yields the error, Trays returns it
	r, err := OpenReplay(ctx, truncatedGzip(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	var last error
	for _, err := range r.TrayFacts(ctx) {
		last = err
	}
	mustCode(t, last, CodeDecode)
	r, err = OpenReplay(ctx, truncatedGzip(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if trays, err := r.Trays(ctx); trays != nil || !HasCode(err, CodeDecode) {
		t.Fatal(trays, err)
	}
	// a frame that does not decode: the Interpreter's TrayFacts passes the error on and goes on, or stops at it
	f := linesFake(t, clockLine, fline(1, 1, 1, -1, `,"world":5`), wline(2, 2, ``))
	c, _ := fakeClient(t, f)
	errs := 0
	for _, err := range c.Interpreter(Resume(false), ConnectionLimit(2)).TrayFacts(ctx) {
		if err != nil {
			errs++
		}
	}
	if errs != 1 {
		t.Fatal(errs)
	}
	for _, err := range c.Interpreter().TrayFacts(ctx) {
		if err != nil {
			break
		}
	}
}

func TestTrayHelpers(t *testing.T) {
	if usableStone(&StoneView{}) || usableStone(&StoneView{Solid: &Solid{Pos: []float64{0, 0}, Q: []float64{0, 0, 0, 1},
		Dims: []float64{1}}}) {
		t.Fatal("a stone without a usable solid")
	}
	if !sameCount(nil, nil) || sameCount(Int64(1), nil) || sameCount(nil, Int64(1)) || !sameCount(Int64(2), Int64(2)) {
		t.Fatal("sameCount")
	}
	if !containsID([]int64{3, 5}, 5) || containsID(nil, 5) {
		t.Fatal("containsID")
	}
	if !handFree(nil) || !handFree(&HandState{}) || !handFree(&HandState{State: "free"}) ||
		handFree(&HandState{State: "touch"}) {
		t.Fatal("handFree")
	}
	removed := TrayFact{Kind: TrayRemoved, Tray: 2, Stone: "x", Certain: Bool(true), Why: "missing", T: 61.2}
	if got := removed.Describe(); got != "1:01.2 tray 2: stone x was removed" {
		t.Fatal(got)
	}
}

// TestTrayLeavingPairs: when a tray leaves, what is left is paired nearest first (in tray order, then by world id, at
// equal distances), never sure; the rest is unclear.
func TestTrayLeavingPairs(t *testing.T) {
	var counts TrayCounts
	var closed []Tray
	l := newTrayLedger(&counts, 7, "v", func(tr Tray) { closed = append(closed, tr) })
	brick := func(id int64, x float64) StoneView {
		return StoneView{ID: id, Shape: "frozen", Detected: true,
			Solid: &Solid{Pos: []float64{x, 0, 0}, Q: []float64{0, 0, 0, 1}, Dims: []float64{0.3, 0.2, 0.1}}}
	}
	ref := TrayPoint{G: 1, T: 1}
	w := &WorldLayer{Motion: "static", Stones: []StoneView{brick(21, 0.5), brick(22, 0.5), brick(23, 3)}}
	tw := newTrayWorld(w, ref)
	tr := &openTray{n: 1, ids: map[int64]struct{}{}, arrived: &ref, link: map[int64]*trayStone{},
		unid: map[int64]*trayUnid{}, k: 0, last: tw}
	for i, x := range []float64{0, 1, 0.6} { // stones 0 and 1 are 0.5 from 21 and 22; stone 2 is 0.1 from both
		b := brick(int64(i+1), x)
		s := newTrayStone(tr, "s"+itoa(int64(i)), &b, ref)
		tr.stones = append(tr.stones, s)
		lose(tr, s, ref)
	}
	for _, id := range []int64{22, 21, 23} {
		ws := tw.byID[id]
		tr.unid[id] = &trayUnid{born: ref, place: placeOf(ws), size: sizeOf(ws)}
	}
	l.tray = tr
	var out []TrayFact
	l.closeTray(&out)
	var lines []string
	for _, f := range out {
		lines = append(lines, f.Describe())
	}
	wantLines := []string{
		"0:01.0 tray 1: stone s2 is not sure any more (matched when the tray left)", // 0.1 from 21 and 22: 21 first
		"0:01.0 tray 1: stone s0 is not sure any more (matched when the tray left)", // 0.5 from 22: tray order first
		"0:01.0 tray 1: stone s1 not found (unclear)",
		"0:01.0 tray 1: stone s7t1.0-1-23 not recognised (unclear)", // 2 from stone 1: too far to pair
		"0:01.0 tray 1 leaving: 3 stones (0 sure), 0 added, 0 removed, 2 unclear",
	}
	if !slices.Equal(lines, wantLines) {
		t.Fatal(strings.Join(lines, "\n"))
	}
	if st := closed[0].Stones; *st[2].WorldID != 21 || *st[0].WorldID != 22 {
		t.Fatalf("%+v", st)
	}
	if counts.RemovedUnclear != 1 || counts.AddedUnclear != 1 || counts.Unsure != 2 {
		t.Fatalf("%+v", counts)
	}
}
