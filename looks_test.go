package pdiggerfactory

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"testing"
)

// lookVectors is fixtures/looks/looks-synthetic.json: synthetic looks (read by value, as the models read them), the
// ones the rules cannot read, the ones the tray rules do not use, the ones the models drop, and the reference's
// pairing of pairs of the others: [a, b, score as the shortest text of the double, its pair count, its permille]
// (null: no score).
type lookVectors struct {
	Looks        map[string]json.RawMessage `json:"looks"`
	Unreadable   []string                   `json:"unreadable"`
	TrayUnusable []string                   `json:"tray_unusable"`
	ModelNone    []string                   `json:"model_none"`
	Scores       [][]any                    `json:"scores"`
}

func readLookVectors(t *testing.T) (lookVectors, map[string]*StoneLook) {
	t.Helper()
	b, err := os.ReadFile("fixtures/looks/looks-synthetic.json")
	if err != nil {
		t.Fatal(err)
	}
	var v lookVectors
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	looks := map[string]*StoneLook{}
	for name, raw := range v.Looks {
		looks[name] = lookByValue(raw)
	}
	return v, looks
}

func in(names []string, name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func TestLookScoreMatchesTheReference(t *testing.T) {
	v, looks := readLookVectors(t)
	if len(v.Scores) < 300 || len(v.Unreadable) < 6 || len(v.TrayUnusable) < 5 || len(v.ModelNone) < 6 {
		t.Fatalf("too few vectors: %d scores, %d unreadable", len(v.Scores), len(v.Unreadable))
	}
	for name, lk := range looks {
		if (lk == nil) != in(v.ModelNone, name) {
			t.Errorf("%s: dropped by the models %v, want %v", name, lk == nil, in(v.ModelNone, name))
		}
		if (readLook(lk) == nil) != in(v.Unreadable, name) {
			t.Errorf("%s: unreadable %v", name, readLook(lk) == nil)
		}
		if UsableLook(lk) == (in(v.Unreadable, name) || in(v.TrayUnusable, name)) {
			t.Errorf("%s: usable %v", name, UsableLook(lk))
		}
	}
	scored := 0
	for _, s := range v.Scores {
		a, b := looks[s[0].(string)], looks[s[1].(string)]
		got, n, ok := LookPairing(a, b)
		if g, gok := LookScore(a, b); g != got || gok != ok {
			t.Errorf("%v: LookScore %v %v, LookPairing %v %v", s[:2], g, gok, got, ok)
		}
		if s[2] == nil {
			if ok || s[3] != nil || s[4] != nil {
				t.Errorf("%v: %v, want no score", s[:2], got)
			}
			continue
		}
		want, err := strconv.ParseFloat(s[2].(string), 64)
		if err != nil {
			t.Fatal(err)
		}
		if !ok || math.Float64bits(got) != math.Float64bits(want) || n != int64(s[3].(float64)) ||
			LookPermille(got) != int64(s[4].(float64)) {
			t.Errorf("%v: %v n %d (%v), want %v", s, got, n, ok, want)
		}
		scored++
	}
	if scored < 150 {
		t.Fatalf("only %d pairs scored", scored)
	}
}

func TestLookScoreRefusesWhatIsNotALook(t *testing.T) {
	_, looks := readLookVectors(t)
	good := looks["brick-b"]
	if _, ok := LookScore(nil, good); ok {
		t.Error("nil look scored")
	}
	if _, _, ok := LookPairing(good, nil); ok {
		t.Error("nil look paired")
	}
	empty := *good
	empty.Nu = 0
	if readLook(&empty) != nil {
		t.Error("a look of no cells was read")
	}
	wide := *good
	wide.Nu, wide.Nv = 1<<40, 1<<40 // the product overflows int64: still more than LookCells
	if readLook(&wide) != nil {
		t.Error("a look of 2^80 cells was read")
	}
	if s, ok := LookScore(good, good); !ok || s != 1 {
		t.Errorf("a look against itself: %v %v", s, ok)
	}
	// the length of nu x nv bytes is checked before anything is decoded; a length that fits still has to decode to them
	long := StoneLook{V: 1, Nu: 1, Nv: 1, D: strings.Repeat("A", 4_000_000)}
	short := StoneLook{V: 1, Nu: 3, Nv: 1, D: "AA=="}
	one := StoneLook{V: 1, Nu: 1, Nv: 1, D: "AA=="}
	if readLook(&long) != nil || readLook(&short) != nil || readLook(&one) == nil {
		t.Error("the length of the cells")
	}
	// a wrong length costs nothing: decoding (which allocates) never starts
	for _, d := range []string{long.D, "AA==AA==", "A", ""} {
		wrong := StoneLook{V: 1, Nu: 1, Nv: 1, D: d}
		if n := testing.AllocsPerRun(3, func() { readLook(&wrong) }); n != 0 {
			t.Errorf("%d characters for one cell were decoded (%v allocations)", len(d), n)
		}
	}
	if n := testing.AllocsPerRun(3, func() { readLook(&one) }); n == 0 {
		t.Error("decoding one cell allocates nothing: the check above proves nothing")
	}
	if floorHalf(-3) != -2 || floorHalf(3) != 1 || floorHalf(-2) != -1 {
		t.Error("floorHalf does not round down")
	}
	if LookPermille(0.8535) != 854 || LookPermille(-0.0004) != 0 || LookPermille(-0.0006) != -1 || LookPermille(1) != 1000 {
		t.Error("LookPermille")
	}
}

// TestALookIsReadByValue: numbers by value, whole numbers from -2^53 to 2^53; a look that does not fit is dropped
// and its stone still reads.
func TestALookIsReadByValue(t *testing.T) {
	v, _ := readLookVectors(t)
	stone := func(look string) StoneView {
		var s StoneView
		if err := json.Unmarshal([]byte(`{"id": 7, "mode": "kept", "look": `+look+`}`), &s); err != nil {
			t.Fatal(err)
		}
		if s.ID != 7 || s.Mode != "kept" {
			t.Fatalf("%+v", s)
		}
		return s
	}
	for _, name := range v.ModelNone {
		if s := stone(string(v.Looks[name])); s.Look != nil {
			t.Errorf("%s: kept", name)
		}
	}
	if s := stone(string(v.Looks["type-nu-whole-float"])); s.Look == nil || s.Look.Nu != 18 || readLook(s.Look) == nil {
		t.Errorf("nu 18.0: %+v", s.Look)
	}
	if s := stone(string(v.Looks["type-over-4096-cells"])); s.Look == nil || readLook(s.Look) != nil {
		t.Error("a look of 65 x 64 cells fits StoneLook and is not read")
	}
	for _, bad := range []string{`null`, `[1, 2]`, `"look"`, `{}`} {
		if s := stone(bad); s.Look != nil {
			t.Errorf("%s: kept", bad)
		}
	}
	var lk map[string]any
	if err := json.Unmarshal(v.Looks["brick-b"], &lk); err != nil {
		t.Fatal(err)
	}
	with := func(k, raw string) *StoneLook {
		m := map[string]json.RawMessage{}
		for key, x := range lk {
			b, _ := json.Marshal(x)
			m[key] = b
		}
		m[k] = json.RawMessage(raw)
		b, _ := json.Marshal(m)
		return stone(string(b)).Look
	}
	if with("sp", "1e400") != nil || with("g", "9007199254740994") != nil || with("g", "1.5") != nil {
		t.Error("a number too large, a whole number past 2^53 or a half was read")
	}
	if l := with("g", "9007199254740992"); l == nil || l.G != 1<<53 {
		t.Error("2^53 is a whole number")
	}
	if l := with("g", "9007199254740993"); l == nil || l.G != 1<<53 { // its double is 2^53
		t.Error("2^53 + 1 reads as 2^53")
	}
	if l := with("q", "1e-400"); l == nil || l.Q != 0 {
		t.Error("a number too small for a float64 reads as 0")
	}
	var s StoneView
	if err := json.Unmarshal([]byte(`{"id": "seven"}`), &s); err == nil {
		t.Error("a stone with a text id parsed")
	}
}
