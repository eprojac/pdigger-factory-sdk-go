package pdiggerfactory

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"
)

// speckleAnswer is a well-formed answer of the unit (section 4.6.12), as JSON: whole numbers written with a fraction
// and finds without at, score or second (null) still fit.
const speckleAnswer = `{"v":1,"tray":2.0,"ref":{"g":4,"t":0.8},"at":{"g":20,"t":4},"device":"d","gpu_ms":1.5,` +
	`"wall_ms":2,"stones":[{"id":1,"at":null,"score":null,"second":null},{"id":2}],` +
	`"refs":[{"id":3,"at":4.0,"score":0.5,"second":-1}],"pairs":[[1,3]]}`

// TestWorldSpeckleByValue checks that a world's speckle answer is read by value: an answer that does not fit
// WorldSpeckle is dropped, and its world reads as it would without it.
func TestWorldSpeckleByValue(t *testing.T) {
	var w WorldLayer
	if err := json.Unmarshal([]byte(`{"g":7,"speckle":`+speckleAnswer+`}`), &w); err != nil || w.Speckle == nil {
		t.Fatalf("%v %+v", err, w)
	}
	sp := w.Speckle
	if w.G != 7 || sp.V != 1 || sp.Tray != 2 || sp.Ref != (SpeckleMoment{G: 4, T: 0.8}) || sp.At.G != 20 ||
		sp.Device != "d" || sp.GPUMS != 1.5 || sp.WallMS != 2 || len(sp.Stones) != 2 || sp.Stones[0].At != nil ||
		sp.Stones[1].Score != nil || *sp.Refs[0].At != 4 || *sp.Refs[0].Score != 0.5 || *sp.Refs[0].Second != -1 ||
		!slices.Equal(sp.Pairs, [][2]int64{{1, 3}}) {
		t.Fatalf("%+v", sp)
	}
	var base map[string]any
	if err := json.Unmarshal([]byte(speckleAnswer), &base); err != nil {
		t.Fatal(err)
	}
	bad := []string{`null`, `5`, `"x"`, `[]`, `{}`}
	for _, change := range []map[string]any{
		{"v": 2}, {"v": true}, {"tray": "2"}, {"tray": 1.5}, {"ref": map[string]any{"g": 4}},
		{"at": map[string]any{"g": 1.5, "t": 0}}, {"at": 3}, {"device": 5}, {"gpu_ms": "1"}, {"wall_ms": nil},
		{"stones": map[string]any{}}, {"stones": []any{5}}, {"refs": []any{map[string]any{"id": 1.5}}},
		{"refs": []any{map[string]any{"id": 1, "at": "x"}}}, {"refs": []any{map[string]any{"id": 1, "score": 0.5}}},
		{"refs": []any{map[string]any{"id": 1, "score": 0.5, "second": "x"}}}, {"pairs": []any{[]any{1}}},
		{"pairs": []any{5}}, {"pairs": []any{[]any{1, 2.5}}}, {"pairs": map[string]any{}},
	} {
		m := map[string]any{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range change {
			if v == nil {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
		b, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		bad = append(bad, string(b))
	}
	for _, s := range bad {
		var w WorldLayer
		if err := json.Unmarshal([]byte(`{"g":7,"speckle":`+s+`}`), &w); err != nil || w.G != 7 || w.Speckle != nil {
			t.Errorf("%s: %v %+v", s, err, w.Speckle)
		}
	}
	if err := json.Unmarshal([]byte(`{"g":"x"}`), &w); err == nil {
		t.Fatal("a world that does not decode decoded")
	}
}

// replaySpeckle feeds core with the synthetic speckle recording (fixtures/recordings/trays-speckle-synthetic) and
// calls each for every update.
func replaySpeckle(t *testing.T, core *InterpreterCore, each func(*Update)) {
	t.Helper()
	ctx := context.Background()
	r, err := OpenReplay(ctx, filepath.Join("fixtures", "recordings", "trays-speckle-synthetic.ndjson.gz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.run(ctx, core, func(u *Update, err error) bool {
		if err != nil {
			t.Fatal(err)
		}
		each(u)
		return true
	})
}

// TestSpeckleTrayStones replays the synthetic speckle recording and checks what its golden speckle.txt does not show:
// the frames that gave an answer (not the revision of one), the answers the tray stones hold as each tray leaves (the
// newest read while the tray was on), the late answer kept out of its tray, and the answers of the Interpreter.
func TestSpeckleTrayStones(t *testing.T) {
	core := NewInterpreterCore()
	var gs []int64
	revised := 0
	replaySpeckle(t, core, func(u *Update) {
		for _, a := range u.Speckle {
			gs = append(gs, a.G)
		}
		if u.IsRevision && u.Frame.World.Speckle != nil {
			revised++
			if len(u.Speckle) != 0 {
				t.Error("a revision's answer was read again")
			}
		}
	})
	if !slices.Equal(gs, []int64{24, 30, 32, 36, 46, 50, 123}) || revised != 1 {
		t.Fatal(gs, revised)
	}
	trays := core.Trays()
	if len(trays) != 3 {
		t.Fatal(len(trays))
	}
	stone := func(tr int, id string) *StoneSpeckle {
		t.Helper()
		s := trays[tr].Stone(id)
		if s == nil || s.Speckle == nil {
			t.Fatalf("tray %d stone %s: %+v", tr+1, id, s)
		}
		return s.Speckle
	}
	one, two := "20261118T000000.8Z-1-", "20261118T000007.8Z-2-"
	if s := stone(0, one+"1"); s.Found != nil || *s.FoundWorld != 1 || !s.Clean { // g 32's
		t.Errorf("%+v", s)
	}
	if s := stone(0, one+"2"); *s.Score != 0.8 || !s.Same() { // g 36's: the tray was still on
		t.Errorf("%+v", s)
	}
	if s := stone(0, one+"4"); *s.Pair != one+"5" || s.Same() || *s.Score != 0.5 { // g 30's, not g 46's
		t.Errorf("%+v", s)
	}
	if s := stone(1, two+"13"); s.Side != SpeckleLater || *s.Found != two+"13" {
		t.Errorf("%+v", s)
	}
	if s := stone(1, two+"12"); s.Pair != nil || s.Clean {
		t.Errorf("%+v", s)
	}
	answers := (&Interpreter{core: core}).Speckle()
	late := answers[4]
	if len(answers) != 7 || late.G != 46 || late.Tray != 1 || late.UnitTray != 1 || late.Stone(one+"4") == nil ||
		late.Stone(one+"1") != nil || late.String() != late.Describe() || late.Wall == nil {
		t.Fatalf("%d %+v", len(answers), late)
	}
}

// TestSpeckleKept checks that the core keeps the newest answers only.
func TestSpeckleKept(t *testing.T) {
	defer func(n int) { speckleKept = n }(speckleKept)
	speckleKept = 2
	core := NewInterpreterCore()
	var gs []int64
	replaySpeckle(t, core, func(u *Update) {
		for _, a := range u.Speckle {
			gs = append(gs, a.G)
			var kept []int64
			for _, k := range core.Speckle() {
				kept = append(kept, k.G)
			}
			if want := gs[max(0, len(gs)-2):]; !slices.Equal(kept, want) {
				t.Errorf("kept %v, want %v", kept, want)
			}
		}
	})
	if len(gs) != 7 || len(core.speckle) > 3 {
		t.Fatal(gs, len(core.speckle))
	}
}
