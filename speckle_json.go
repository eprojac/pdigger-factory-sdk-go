package pdiggerfactory

import (
	"bytes"
	"encoding/json"
)

// UnmarshalJSON parses a world. Its speckle answer is read by value, as a stone's look is (section 4.6.12 of the SDK
// design): an answer that does not fit WorldSpeckle so is dropped (Speckle is nil), and the world and its frame read as
// they would without it.
func (w *WorldLayer) UnmarshalJSON(b []byte) error {
	type plain WorldLayer
	var v struct {
		plain
		Speckle json.RawMessage `json:"speckle"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*w = WorldLayer(v.plain)
	w.Speckle = speckleByValue(v.Speckle)
	return nil
}

// speckleByValue is a speckle answer that fits WorldSpeckle by value: V 1; Tray, both moments' G, each find's ID and At
// (or null) and both ids of each pair whole numbers; both moments' T, GPUMS and WallMS numbers; Device a string; each
// find's Score and Second both numbers or both null. Nil for any other value.
func speckleByValue(raw json.RawMessage) *WorldSpeckle {
	var m map[string]any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if len(raw) == 0 || d.Decode(&m) != nil {
		return nil
	}
	device, deviceOK := m["device"].(string)
	sp := &WorldSpeckle{Device: device}
	var stonesOK, refsOK, pairsOK bool
	sp.Stones, stonesOK = speckleFinds(m["stones"])
	sp.Refs, refsOK = speckleFinds(m["refs"])
	sp.Pairs, pairsOK = specklePairs(m["pairs"])
	if !deviceOK || !stonesOK || !refsOK || !pairsOK || !whole(m["v"], &sp.V) || sp.V != 1 ||
		!whole(m["tray"], &sp.Tray) || !speckleMoment(m["ref"], &sp.Ref) || !speckleMoment(m["at"], &sp.At) ||
		!number(m["gpu_ms"], &sp.GPUMS) || !number(m["wall_ms"], &sp.WallMS) {
		return nil
	}
	return sp
}

// speckleMoment reads a moment: an object of a whole G and a number T.
func speckleMoment(v any, out *SpeckleMoment) bool {
	m, ok := v.(map[string]any)
	return ok && whole(m["g"], &out.G) && number(m["t"], &out.T)
}

// speckleFinds reads an array of finds (ok false for anything else).
func speckleFinds(v any) ([]SpeckleFind, bool) {
	items, ok := v.([]any)
	out := make([]SpeckleFind, len(items))
	for i, x := range items {
		f, isObject := x.(map[string]any)
		ok = ok && isObject && whole(f["id"], &out[i].ID) && wholeOrNull(f["at"], &out[i].At) &&
			speckleScores(f["score"], f["second"], &out[i])
	}
	return out, ok
}

// wholeOrNull reads a whole number into a new *out, or null (or nothing) as nil.
func wholeOrNull(v any, out **int64) bool {
	if v == nil {
		return true
	}
	var n int64
	*out = &n
	return whole(v, &n)
}

// speckleScores reads a find's score and second: both numbers, or both null (or missing).
func speckleScores(s, q any, f *SpeckleFind) bool {
	if s == nil && q == nil {
		return true
	}
	var score, second float64
	f.Score, f.Second = &score, &second
	return number(s, &score) && number(q, &second)
}

// specklePairs reads the pairs: arrays of two whole numbers (ok false for anything else).
func specklePairs(v any) ([][2]int64, bool) {
	items, ok := v.([]any)
	out := make([][2]int64, len(items))
	for i, x := range items {
		p, isArray := x.([]any)
		ok = ok && isArray && len(p) == 2 && whole(p[0], &out[i][0]) && whole(p[1], &out[i][1])
	}
	return out, ok
}
