package pdiggerfactory

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// Tray reports (section 4.7 of the SDK design) beyond the golden files: the live stream, the trays that have none,
// the text without a clock, the tray's own shift, a tray where nothing it arrived with is left, and the JSON.

func reportsText(rs []TrayReport) string {
	var b strings.Builder
	for _, r := range rs {
		b.WriteString(r.Describe() + "\n")
	}
	return b.String()
}

func goldenReports(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("fixtures", "golden", name+".reports.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestInterpreterTrayReports follows the fake's stream of every recording to its end: one report as each tray
// leaves, the golden reports in all.
func TestInterpreterTrayReports(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("fixtures", "recordings", "*.ndjson*"))
	if err != nil || len(paths) == 0 {
		t.Fatal(paths, err)
	}
	for _, path := range paths {
		name := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(path), ".gz"), ".ndjson")
		t.Run(name, func(t *testing.T) {
			ctx := testCtx(t)
			r, err := OpenReplay(ctx, path, nil)
			if err != nil {
				t.Fatal(err)
			}
			frames := 0
			for range r.Frames(ctx) {
				frames++
			}
			r.Close()
			c, _ := fakeClient(t, newFake(t, factorytest.WithRecording(path)))
			var got []TrayReport
			for rep, err := range c.Interpreter(Resume(false), ConnectionLimit(frames)).TrayReports(ctx) {
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, rep)
			}
			if text, want := reportsText(got), goldenReports(t, name); text != want {
				t.Errorf("reports differ:\n%s\nwant:\n%s", text, want)
			}
		})
	}
	// a loop that stops at the first report leaves the stream there
	ctx := testCtx(t)
	c, _ := fakeClient(t, newFake(t, factorytest.WithRecording(synthPath)))
	n := 0
	for rep, err := range c.Interpreter().TrayReports(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		if n++; rep.Tray != 1 {
			t.Fatal(rep.Describe())
		}
		break
	}
	if n != 1 {
		t.Fatal(n)
	}
}

func TestTrayReportsErrors(t *testing.T) {
	ctx := testCtx(t)
	// a frame that does not decode: the error comes through, and no tray left
	f := linesFake(t, clockLine, fline(1, 1, 1, -1, `,"world":5`), wline(2, 2, ``))
	c, _ := fakeClient(t, f)
	errs, reports := 0, 0
	for _, err := range c.Interpreter(Resume(false), ConnectionLimit(2)).TrayReports(ctx) {
		if err != nil {
			errs++
		} else {
			reports++
		}
	}
	if errs != 1 || reports != 0 {
		t.Fatal(errs, reports)
	}
	for _, err := range c.Interpreter().TrayReports(ctx) { // a loop may stop at the error
		if err == nil {
			t.Fatal("no error")
		}
		break
	}
	// a recording that breaks after its header: Replay.TrayReports returns the error
	r, err := OpenReplay(ctx, truncatedGzip(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if reports, err := r.TrayReports(ctx); reports != nil || !HasCode(err, CodeDecode) {
		t.Fatal(reports, err)
	}
}

func reportAt(t float64, wall *float64) *TrayPoint {
	return &TrayPoint{G: int64(t*5 + 0.5), T: t, Wall: wall}
}

func reportFact(kind string, t float64) TrayFact {
	return TrayFact{Kind: kind, Tray: 1, Session: 7, Video: "v", G: 10, T: t}
}

// reportStone is a tray stone the tray arrived with, seen at the leaving point at place; nothing touched it.
func reportStone(id string, place, first [2]float64, sure bool) TrayStone {
	left := place
	return TrayStone{ID: id, WorldID: Int64(1), Status: TrayStoneOn, Sure: sure, Born: TrayPoint{G: 5, T: 1},
		Place: place, Size: [2]float64{0.4, 0.2}, FirstPlace: first, LeftPlace: &left}
}

func reportTray(n int64, arrived, leaving *TrayPoint, stones []TrayStone, facts []TrayFact) *Tray {
	return &Tray{Session: 7, Video: "v", N: n, Arrived: arrived, Leaving: leaving, Stones: stones, Facts: facts}
}

func TestATrayThatHasNotLeftHasNoReport(t *testing.T) {
	arrived := []TrayFact{reportFact(TrayArrived, 1)}
	for i, tr := range []*Tray{
		reportTray(1, nil, nil, nil, nil),                           // never arrived
		reportTray(1, reportAt(1, nil), nil, nil, nil),              // still on
		reportTray(1, reportAt(1, nil), reportAt(9, nil), nil, nil), // no facts
		// its facts do not end in its leaving
		reportTray(1, reportAt(1, nil), reportAt(9, nil), nil, arrived),
		// left, never arrived
		reportTray(1, nil, reportAt(9, nil), nil, []TrayFact{reportFact(TrayLeaving, 9)}),
	} {
		if r := tr.Report(); r != nil {
			t.Errorf("%d: %s", i, r.Describe())
		}
	}
}

func TestATrayWithoutAClockAndNothingItArrivedWithLeft(t *testing.T) {
	gone := reportStone("s1t1.0-1-4", [2]float64{}, [2]float64{}, true)
	gone.Status, gone.Certain, gone.WorldID = TrayStoneRemoved, Bool(true), nil
	removed := reportFact(TrayRemoved, 4)
	removed.Stone, removed.Certain, removed.Code = gone.ID, Bool(true), "carried_away"
	other := reportFact(TrayRemoved, 5)
	other.Stone = "not-a-tray-stone" // a fact of no stone of the tray is left out
	// no stones, none sure, no unclear count
	leaving := reportFact(TrayLeaving, 9)
	facts := []TrayFact{reportFact(TrayArrived, 1), removed, other, leaving}
	r := reportTray(1, reportAt(1, nil), reportAt(9, nil), []TrayStone{gone}, facts).Report()
	if r == nil || r.Stones != 0 || r.Unclear != 0 || r.Unsure != 0 || !r.Complete || len(r.Moved) != 0 ||
		len(r.Removed) != 1 || r.Removed[0].Code != "carried_away" {
		t.Fatalf("%+v", r)
	}
	// a stone of a tray made by hand has no evidence: its change takes the undecided class's confidence (4.6.13)
	want := "tray 1, 0:01.0 to 0:09.0: 1 removed, 0 added, 0 moved\n" +
		"  0:04.0 removed s1t1.0-1-4 from (0.00, 0.00) (0.50)"
	if r.Describe() != want || r.String() != want ||
		r.Removed[0].String() != "0:04.0 removed s1t1.0-1-4 from (0.00, 0.00) (0.50)" ||
		r.Removed[0].Confidence != ConfidenceRemovedCertainUndecided {
		t.Fatal(r.Describe())
	}
	// an addition that is not certain: counted unclear by the leaving fact, "(unclear, ...)" in the text with its
	// confidence (its stone was found on the tray at the arrival: contradicted); it is not counted as not sure
	set := reportStone("s1t1.0-1-5", [2]float64{1, 1}, [2]float64{1, 1}, false)
	set.Added, set.Certain, set.Evidence = true, Bool(false), EvidenceFound
	added := reportFact(TrayAdded, 6)
	added.Stone, added.Certain, added.Code = set.ID, Bool(false), "not_recognised"
	leaving.Stones, leaving.Unclear = 1, 1
	facts = []TrayFact{reportFact(TrayArrived, 1), added, leaving}
	r = reportTray(1, reportAt(1, nil), reportAt(9, nil), []TrayStone{set}, facts).Report()
	if r.Describe() != "tray 1, 0:01.0 to 0:09.0: 0 removed, 1 added, 0 moved, not complete: 1 unclear\n"+
		"  0:06.0 added s1t1.0-1-5 at (1.00, 1.00) (unclear, 0.02)" || r.Unsure != 0 {
		t.Fatal(r.Describe())
	}
	// its final place is its place at the leaving point; not seen there, where it was last seen
	set.Place, set.LeftPlace = [2]float64{2, -0.001}, &[2]float64{1.5, -0.004}
	r = reportTray(1, reportAt(1, nil), reportAt(9, nil), []TrayStone{set}, facts).Report()
	if s := r.Added[0].Describe(); s != "0:06.0 added s1t1.0-1-5 at (1.50, 0.00) (unclear, 0.02)" ||
		r.Added[0].Place != [2]float64{1.5, -0.004} {
		t.Fatal(s)
	}
	set.LeftPlace = nil
	r = reportTray(1, reportAt(1, nil), reportAt(9, nil), []TrayStone{set}, facts).Report()
	if s := r.Added[0].Describe(); s != "0:06.0 added s1t1.0-1-5 at (2.00, 0.00) (unclear, 0.02)" {
		t.Fatal(s)
	}
}

// TestTheConfidenceOfAChange is the table of section 4.6.13: by kind, certainty and the stone's evidence; a stone
// without evidence counts as undecided; a certain addition takes the unclear additions' numbers.
func TestTheConfidenceOfAChange(t *testing.T) {
	for _, c := range []struct {
		kind     string
		certain  bool
		evidence string
		want     float64
	}{
		{TrayRemoved, true, EvidenceNotFound, 0.98}, {TrayRemoved, true, EvidenceUndecided, 0.50},
		{TrayRemoved, true, EvidenceFound, 0.25}, {TrayRemoved, false, EvidenceNotFound, 0.71},
		{TrayRemoved, false, EvidenceUndecided, 0.58}, {TrayRemoved, false, EvidenceFound, 0.09},
		{TrayAdded, true, EvidenceNotFound, 0.96}, {TrayAdded, true, EvidenceUndecided, 0.52},
		{TrayAdded, true, EvidenceFound, 0.02}, {TrayAdded, false, EvidenceNotFound, 0.96},
		{TrayAdded, false, EvidenceUndecided, 0.52}, {TrayAdded, false, EvidenceFound, 0.02},
		{TrayRemoved, true, "", 0.50}, {TrayAdded, false, "", 0.52}, {TrayRemoved, false, "something else", 0.58},
	} {
		if got := confidence(c.kind, c.certain, c.evidence); got != c.want {
			t.Errorf("%s, certain %v, %q: %v, want %v", c.kind, c.certain, c.evidence, got, c.want)
		}
	}
	if MovedConfidence != 0.87 || len(confidences) != 12 {
		t.Fatal(MovedConfidence, len(confidences))
	}
}

func TestTheTraysOwnShiftIsTakenOutBeforeAStoneIsCalledMoved(t *testing.T) {
	// stones the tray carried 0.1 along y as it left, one of them pushed 0.3 along x besides; d is not sure, so it
	// is counted as not sure and not as moved however far its place is
	touched := func(s TrayStone, g int64) TrayStone { s.Touched = Int64(g); return s }
	kept := []TrayStone{
		reportStone("a", [2]float64{0, 0.1}, [2]float64{0, 0}, true),
		reportStone("b", [2]float64{1, 0.1}, [2]float64{1, 0}, true),
		touched(reportStone("c", [2]float64{2.3, 0.1}, [2]float64{2, 0}, true), 40),
		touched(reportStone("d", [2]float64{3.9, 0.1}, [2]float64{3, 0}, false), 41),
		reportStone("e", [2]float64{4, 0.1}, [2]float64{4, 0}, true),
	}
	arrived := reportFact(TrayArrived, 2)
	arrived.Stones, arrived.Wall = 5, Float64(1_794_960_000.25)
	leaving := reportFact(TrayLeaving, 9)
	leaving.Stones, leaving.Sure = 5, 4
	report := func(stones ...TrayStone) *TrayReport {
		return reportTray(2, reportAt(1, nil), reportAt(9, nil), stones, []TrayFact{arrived, leaving}).Report()
	}
	names := func(r *TrayReport) string {
		var n []string
		for _, c := range r.Moved {
			n = append(n, c.Stone)
		}
		return strings.Join(n, ",")
	}
	r := report(kept...)
	if len(r.Moved) != 1 || r.Moved[0].Stone != "c" || *r.Moved[0].Distance != 0.3 || !r.Moved[0].Certain ||
		r.Moved[0].Place != [2]float64{2.3, 0.1} || r.Moved[0].At != nil || r.Moved[0].Confidence != MovedConfidence ||
		r.Unsure != 1 || r.Complete {
		t.Fatalf("%+v", r)
	}
	want := "tray 2 (2026-11-18T00:00:00.3Z), 0:02.0 to 0:09.0: 0 removed, 0 added, 1 moved, not complete: " +
		"1 not sure\n  moved c 0.30 to (2.30, 0.10) (0.87)"
	if r.Describe() != want {
		t.Fatal(r.Describe())
	}
	// the median of an odd half: one of two sure stones, three of five
	if n := names(report(kept[0], kept[2])); n != "c" {
		t.Fatal(n)
	}
	five := append(append([]TrayStone{}, kept[:3]...), reportStone("f", [2]float64{5, 0.1}, [2]float64{5, 0}, true),
		reportStone("g", [2]float64{6, 0.1}, [2]float64{6, 0}, true))
	if n := names(report(five...)); n != "c" {
		t.Fatal(n)
	}
	if r := report(kept[3]); len(r.Moved) != 0 { // nothing sure: nothing moved
		t.Fatal(names(r))
	}
	// the same place, but nothing touched it: an outline the world refined is not a move
	if r := report(kept[0], kept[1], reportStone("c", [2]float64{2.3, 0.1}, [2]float64{2, 0}, true)); len(r.Moved) != 0 {
		t.Fatal(names(r))
	}
	// not seen at the leaving point: its place there is not known, and it is left out of the shift as well
	unseen := kept[2]
	unseen.LeftPlace = nil
	if r := report(kept[0], kept[1], unseen); len(r.Moved) != 0 {
		t.Fatal(names(r))
	}
	if Moved != 0.75 {
		t.Fatal(Moved)
	}
}

func TestATrayChangeWithoutItsPointOrDistance(t *testing.T) {
	change := func(kind string, certain bool) TrayChange {
		return TrayChange{Kind: kind, Stone: "x", Certain: certain, Size: [2]float64{1, 1}}
	}
	if s := change(TrayRemoved, false).Describe(); s != "removed x from (0.00, 0.00) (unclear, 0.00)" {
		t.Fatal(s)
	}
	if s := change(TrayMoved, true).Describe(); s != "moved x 0.00 to (0.00, 0.00) (0.00)" {
		t.Fatal(s)
	}
	far := change(TrayRemoved, true)
	far.Place = [2]float64{-1.236, -0.0049} // a coordinate that rounds to zero prints without its sign
	far.Confidence = 0.58
	if s := far.Describe(); s != "removed x from (-1.24, 0.00) (0.58)" {
		t.Fatal(s)
	}
	if s := trayISO(1_794_960_000.04); s != "2026-11-18T00:00:00.0Z" {
		t.Fatal(s)
	}
}

func TestTrayReportJSON(t *testing.T) {
	// the other languages' names: at {g, t, wall} or null, code null for moved, empty lists never null
	removed := TrayChange{Kind: TrayRemoved, Stone: "s-1", At: &TrayPoint{G: 20, T: 4, Wall: Float64(1.5)}, Certain: true,
		Code: "carried_away", Place: [2]float64{1, 2}, Size: [2]float64{0.4, 0.2}, Confidence: 0.98}
	moved := TrayChange{Kind: TrayMoved, Stone: "s-2", Certain: true, Place: [2]float64{3, 4}, Size: [2]float64{0.4, 0.2},
		Distance: Float64(0.3), Confidence: MovedConfidence}
	r := TrayReport{Session: 7, Video: "v", Tray: 2, Arrived: TrayPoint{G: 5, T: 1}, Left: TrayPoint{G: 45, T: 9,
		Wall: Float64(2.5)}, Stones: 3, Removed: []TrayChange{removed}, Moved: []TrayChange{moved}, Unsure: 1}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var got, want any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal([]byte(`{"session":7,"video":"v","tray":2,"arrived":{"g":5,"t":1,"wall":null},
		"left":{"g":45,"t":9,"wall":2.5},"stones":3,
		"removed":[{"kind":"removed","stone":"s-1","at":{"g":20,"t":4,"wall":1.5},"certain":true,"code":"carried_away",
			"place":[1,2],"size":[0.4,0.2],"distance":null,"confidence":0.98}],
		"added":[],
		"moved":[{"kind":"moved","stone":"s-2","at":null,"certain":true,"code":null,"place":[3,4],"size":[0.4,0.2],
			"distance":0.3,"confidence":0.87}],
		"unclear":0,"unsure":1,"complete":false}`), &want)
	if !reflect.DeepEqual(got, want) {
		t.Fatal(string(b))
	}
	// a report of a recording as JSON: every key, in the order the other languages write them, and every key of a
	// change
	reports, err := mustReplay(t, synthPath).TrayReports(testCtx(t))
	if err != nil || len(reports) == 0 || len(reports[0].Removed) == 0 {
		t.Fatal(err)
	}
	inOrder := func(b []byte, keys ...string) {
		t.Helper()
		last := -1
		for _, k := range keys {
			i := strings.Index(string(b), `"`+k+`":`)
			if i <= last {
				t.Fatal(k, string(b))
			}
			last = i
		}
	}
	b, _ = json.Marshal(reports[0])
	inOrder(b, "session", "video", "tray", "arrived", "left", "stones", "removed", "added", "moved", "unclear",
		"unsure", "complete")
	b, _ = json.Marshal(reports[0].Removed[0])
	inOrder(b, "kind", "stone", "at", "certain", "code", "place", "size", "distance", "confidence")
}

func mustReplay(t *testing.T, path string) *Replay {
	t.Helper()
	r, err := OpenReplay(testCtx(t), path, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}
