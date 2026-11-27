package pdiggerfactory

import (
	"encoding/json"
	"math"
	"slices"
	"testing"
)

func TestSheetRevisionsAndText(t *testing.T) {
	laid := WorldEvent{Type: EventSheet, Why: "laid", IDs: []int64{7, 1, 7}}
	other := WorldEvent{Type: EventSheet, Why: "laid", IDs: []int64{1, 7}}
	if keyOf(&laid) != keyOf(&other) || laid.IDs[0] != 7 {
		t.Fatal("sheet id order changed revision identity or input")
	}
	other.Why = "lifted"
	if keyOf(&laid) == keyOf(&other) {
		t.Fatal("lost distinct action")
	}
	for _, tc := range []struct {
		why  string
		ids  []int64
		want string
	}{
		{"laid", nil, "a sheet is laid"}, {"lifted", []int64{7}, "a sheet is lifted off stone 7"},
		{"rolled", nil, "a sheet event (rolled)"}, {"", nil, "a sheet event"},
		{"laid", []int64{1, 2, 3, 4, 5, 6, 7}, "a sheet is laid over stones 1, 2, 3, 4, 5, 6 and 1 more"},
	} {
		if got := (Event{Type: EventSheet, Why: tc.why, IDs: tc.ids}).Sentence(); got != tc.want {
			t.Errorf("%q: %s", tc.why, got)
		}
	}
	if got := stoneNames(nil); got != "stone ?" {
		t.Fatal(got)
	}
}

func TestSheetReportsAndChecks(t *testing.T) {
	ctx := testCtx(t)
	rec, err := OpenReplay(ctx, recording("trays-sheet-synthetic"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rec.Close()
	trays, err := rec.Trays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tr := trays[0]
	report := tr.Report()
	bytes, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Under  []string         `json:"under_sheet"`
		Sheets int              `json:"sheets"`
		Facts  []map[string]any `json:"sheet_facts"`
	}
	if err := json.Unmarshal(bytes, &doc); err != nil || len(doc.Under) == 0 || doc.Sheets == 0 || doc.Facts[0]["kind"] != TraySheet || doc.Facts[0]["sure"] != nil {
		t.Fatal(string(bytes), err)
	}
	for _, tc := range []struct {
		edit func(*Tray)
		want string
	}{
		{func(v *Tray) { v.Facts[1].Stones++ }, "C10"},
		{func(v *Tray) { v.Facts[1].TrayStones = []string{"unknown"} }, "C10"},
		{func(v *Tray) { v.Facts[1].Code = "unexpected" }, "C10"},
		{func(v *Tray) { v.Facts[1].Code = "sheet_lifted" }, "C10"},
		{func(v *Tray) { v.Facts = append(v.Facts[:2:2], v.Facts[1:]...) }, "C10"},
		{func(v *Tray) { v.Facts[len(v.Facts)-1].UnderSheet++ }, "C11"},
	} {
		v := tr
		v.Facts = slices.Clone(tr.Facts)
		tc.edit(&v)
		if !slices.Contains(v.Check(), tc.want) {
			t.Fatalf("missing %s in %v", tc.want, v.Check())
		}
	}
}

func TestUnresolvedSheetStoneDoesNotOwnItsFormerWorldID(t *testing.T) {
	current := &trayStone{id: "current", status: TrayStoneOn, world: 8, linked: true, size: vec2{1, 1}}
	unresolved := &trayStone{id: "unresolved", status: TrayStoneOn, world: 8, index: 1, size: vec2{1, 1}}
	tr := &openTray{stones: []*trayStone{current, unresolved}, lastRun: &lookRun{}}
	visible := &StoneView{ID: 8, Mode: "seen", Detected: true, Solid: &Solid{
		Pos: []float64{0, 0, 0}, Dims: []float64{1, 1, 1}, Q: []float64{0, 0, 0, 1},
	}}
	tw := &trayWorld{stones: []*StoneView{visible}}
	entries := (&trayLedger{}).openAccount(tr, tw)
	got := entries[1]
	if got.WorldID != nil || len(got.Candidates) < 2 || got.Candidates[0].Kind != "unseen" ||
		got.Candidates[1].Stone != current.id {
		t.Fatalf("unresolved stone's candidates: %+v", got)
	}
}

func TestTraySheetBounds(t *testing.T) {
	c := NewInterpreterCore()
	bounds := [4]float64{0, 0, 100, 100}
	if c.SetTrayBounds(&bounds) != nil || c.newLedger(1, "").bounds == &bounds {
		t.Fatal("bounds must be copied per core")
	}
	for _, bad := range [][4]float64{{0, 0, 0, 5}, {0, math.NaN(), 3, 4}} {
		if c.SetTrayBounds(&bad) == nil {
			t.Fatal("invalid bounds")
		}
	}
	if c.SetTrayBounds(nil) != nil || c.ledger.bounds != nil {
		t.Fatal("clear bounds")
	}
	inside := [][]float64{{5, 5}, {8, 5}, {8, 8}}
	outside := [][]float64{{105, 5}, {108, 5}, {108, 8}}
	visible := func(id int64) *StoneView {
		return &StoneView{ID: id, Mode: "seen", Detected: true, Outline: inside, Solid: &Solid{Pos: []float64{0, 0, 0}, Dims: []float64{1, 1, 1}}}
	}
	a, b := visible(1), visible(2)
	old := &StoneView{ID: 3, Mode: "kept", Outline: outside}
	before := &trayWorld{byID: map[int64]*StoneView{1: a, 2: b, 3: old}, ref: TrayPoint{G: 0}}
	now := &trayWorld{w: &WorldLayer{Events: []WorldEvent{{Type: EventDelete, Why: "outside", ID: Int64(3)}}}, stones: []*StoneView{a, b}, byID: map[int64]*StoneView{1: a, 2: b}, ref: TrayPoint{G: 1}}
	tr := &openTray{last: before, link: map[int64]*trayStone{1: {}, 2: {}, 3: {underSheet: true}}}
	l := &trayLedger{bounds: &bounds}
	if !l.onlyBackgroundRetired(tr, now) || !l.sameExposedLayer(tr, now) {
		t.Fatal("two stationary sightings")
	}
	now.w.Events[0].Why = "absent"
	if l.onlyBackgroundRetired(tr, now) {
		t.Fatal("ordinary retirement")
	}
	now.w.Events[0].Type = EventNew
	if l.onlyBackgroundRetired(tr, now) {
		t.Fatal("new stone")
	}
	if retiredOutside(nil, bounds) || retiredOutside(a, bounds) {
		t.Fatal("observed stone")
	}
	old.Outline = [][]float64{{104, 0}, {math.Inf(1), 1}, {105, 2}}
	if retiredOutside(old, bounds) {
		t.Fatal("nonfinite outline")
	}
	delete(now.byID, 2)
	if l.sameExposedLayer(tr, now) {
		t.Fatal("one face cannot hold a layer")
	}
}

func TestBoundedArrivalAndVisibleUncover(t *testing.T) {
	ctx := testCtx(t)
	replay, err := OpenReplay(ctx, recording("trays-synthetic"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer replay.Close()
	core := NewInterpreterCore()
	bounds := [4]float64{0, 0, 100, 100}
	if err := core.SetTrayBounds(&bounds); err != nil {
		t.Fatal(err)
	}
	var facts []TrayFact
	for f, err := range replay.Frames(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		if f.G > 16 {
			break
		}
		w := f.World
		w.Stones = w.Stones[:2]
		for i := range w.Stones {
			s := &w.Stones[i]
			s.Outline = [][]float64{{1, 1}, {9, 1}, {9, 9}}
			s.Mode = "seen"
			s.Detected = true
		}
		w.Events = nil
		if f.G < 4 {
			old := w.Stones[0]
			old.ID = 99
			old.Detected = false
			old.Mode = "kept"
			old.Outline = [][]float64{{110, 0}, {120, 0}, {120, 10}}
			w.Stones = append(w.Stones, old)
		}
		if f.G == 4 {
			w.Events = []WorldEvent{{Type: EventDelete, Why: "stranded", ID: Int64(99)}}
		}
		if f.G >= 11 && f.G < 14 {
			w.Stones[0].UnderSheet = true
		}
		if f.G == 16 {
			w.Events = []WorldEvent{{Type: EventDelete, Why: "sheet", ID: Int64(9999)}}
		}
		w.Stats.Stones = Int64(int64(len(w.Stones)))
		w.Stats.Confirmed = Int64(int64(len(w.Stones)))
		if up := core.Feed(f); up != nil {
			facts = append(facts, up.TrayFacts...)
		}
	}
	if len(facts) < 3 || facts[0].G != 4 || facts[1].Code != "sheet_laid" || facts[2].Code != "sheet_lifted" {
		t.Fatalf("unexpected board account: %v", facts)
	}
}
