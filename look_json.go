package pdiggerfactory

import (
	"bytes"
	"encoding/json"
	"math"
	"strconv"
)

// UnmarshalJSON parses a world stone. Its look is read by value (section 4.6.2 of the SDK design): a number is a JSON
// number, never a boolean or a string, and never one too large for a float64; a whole number is a number whose value
// is an integer from -2^53 to 2^53 (3 and 3.0 alike). A look that does not fit StoneLook so is dropped (Look is nil),
// and the stone and its frame read as they would without it.
func (s *StoneView) UnmarshalJSON(b []byte) error {
	type plain StoneView
	var v struct {
		plain
		Look json.RawMessage `json:"look"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*s = StoneView(v.plain)
	s.Look = lookByValue(v.Look)
	return nil
}

// lookByValue is a look that fits StoneLook by value: Sp and Q numbers; V, G, Nu and Nv whole numbers; Px an array of
// numbers; At an object of the numbers cx, cy, theta, L and S; D a string. Nil for any other value.
func lookByValue(raw json.RawMessage) *StoneLook {
	var m map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if len(raw) == 0 || d.Decode(&m) != nil {
		return nil
	}
	at, atOK := m["at"].(map[string]any)
	px, pxOK := m["px"].([]any)
	cells, dOK := m["d"].(string)
	lk := &StoneLook{D: cells, Px: make([]float64, len(px))}
	fits := atOK && pxOK && dOK &&
		number(m["sp"], &lk.Sp) && number(m["q"], &lk.Q) &&
		whole(m["v"], &lk.V) && whole(m["g"], &lk.G) && whole(m["nu"], &lk.Nu) && whole(m["nv"], &lk.Nv) &&
		number(at["cx"], &lk.At.CX) && number(at["cy"], &lk.At.CY) && number(at["theta"], &lk.At.Theta) &&
		number(at["L"], &lk.At.L) && number(at["S"], &lk.At.S)
	for i, x := range px {
		fits = fits && number(x, &lk.Px[i])
	}
	if !fits {
		return nil
	}
	return lk
}

// number reads a JSON number by its value (ok false for a boolean, a string, anything else, or a number too large for
// a float64).
func number(v any, out *float64) bool {
	n, ok := v.(json.Number)
	if !ok {
		return false
	}
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil && (math.IsInf(f, 0) || math.IsNaN(f)) {
		return false
	}
	*out = f
	return true
}

// maxWhole is the largest whole number a look may hold: 2^53, below which every integer is exact in a float64.
const maxWhole = 1 << 53

// whole reads a whole number: a number whose value is an integer from -2^53 to 2^53.
func whole(v any, out *int64) bool {
	var f float64
	if !number(v, &f) || f != math.Trunc(f) || math.Abs(f) > maxWhole {
		return false
	}
	*out = int64(f)
	return true
}
