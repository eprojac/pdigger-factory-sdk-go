package pdiggerfactory

import (
	"encoding/json"
	"errors"
	"math"
	"slices"
)

// SetTrayBounds configures the fixed tray rectangle in native pixels, ordered left, top, right, bottom.
// Call it before feeding a session. A nil rectangle restores the unbounded rules.
func (c *InterpreterCore) SetTrayBounds(bounds *[4]float64) error {
	if bounds != nil {
		r := *bounds
		for _, v := range r {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return errors.New("tray bounds must be finite")
			}
		}
		if r[0] >= r[2] || r[1] >= r[3] {
			return errors.New("tray bounds must have positive width and height")
		}
		bounds = &r
	}
	c.trayBounds, c.ledger.bounds = bounds, bounds
	return nil
}

func withinBounds(s *StoneView, b [4]float64) bool {
	return s.Mode == "seen" && s.Detected && !s.UnderSheet && len(s.Outline) >= 3 &&
		!slices.ContainsFunc(s.Outline, func(p []float64) bool {
			return !(len(p) == 2 && p[0] >= b[0] && p[0] <= b[2] && p[1] >= b[1] && p[1] <= b[3])
		})
}

func retiredOutside(s *StoneView, b [4]float64) bool {
	if s == nil || s.Mode != "kept" || s.Detected || s.UnderSheet || len(s.Outline) < 3 {
		return false
	}
	left, right, above, below := true, true, true, true
	for _, p := range s.Outline {
		if len(p) != 2 || math.IsNaN(p[0]) || math.IsNaN(p[1]) || math.IsInf(p[0], 0) || math.IsInf(p[1], 0) {
			return false
		}
		left, right = left && p[0] < b[0], right && p[0] > b[2]
		above, below = above && p[1] < b[1], below && p[1] > b[3]
	}
	return left || right || above || below
}

func (l *trayLedger) onlyBackgroundRetired(tr *openTray, tw *trayWorld) bool {
	if l.bounds == nil || tr.arrived != nil || tw.hole || tr.last == nil || tr.last.ref.G+1 != tw.ref.G {
		return false
	}
	for _, e := range tw.w.Events {
		switch e.Type {
		case EventDelete:
			if (e.Why != "outside" && e.Why != "stranded") || e.ID == nil || l.visitSeen[*e.ID] || !retiredOutside(tr.last.byID[*e.ID], *l.bounds) {
				return false
			}
		case EventNew, EventSplit:
			return false
		}
	}
	seen := 0
	for _, ws := range tw.stones {
		prev := tr.last.byID[ws.ID]
		if prev != nil && prev.Detected && !prev.UnderSheet && withinBounds(ws, *l.bounds) && dist(placeOf(ws), placeOf(prev)) <= nearD {
			seen++
		}
	}
	return seen >= 2
}

// The exposed faces have to establish continuity independently when a lift retires the last covered tracks.
func (l *trayLedger) sameExposedLayer(tr *openTray, tw *trayWorld) bool {
	if l.bounds == nil || tr.last.ref.G+1 != tw.ref.G {
		return false
	}
	before, same := 0, 0
	for id, s := range tr.link {
		prev := tr.last.byID[id]
		if s.underSheet || prev == nil || !withinBounds(prev, *l.bounds) {
			continue
		}
		before++
		curr := tw.byID[id]
		if curr != nil && withinBounds(curr, *l.bounds) && dist(placeOf(prev), placeOf(curr)) <= nearD {
			same++
		}
	}
	return same >= 2 && same*2 > before
}

func (l *trayLedger) sheetFact(tr *openTray, stones []*trayStone, code string, ref TrayPoint, out *[]TrayFact) {
	if len(stones) == 0 {
		return
	}
	names := make([]string, len(stones))
	for i, s := range stones {
		names[i] = s.id
	}
	slices.Sort(names)
	l.fact(TraySheet, tr, nil, ref, TrayFact{Code: code, Stones: int64(len(stones)), TrayStones: names}, out)
}

func (l *trayLedger) sheetLaid(tr *openTray, stones []*trayStone, ref TrayPoint, out *[]TrayFact) {
	var newly []*trayStone
	for _, s := range stones {
		if s.underSheet && s.sheetSince == nil {
			s.sheetSince = &ref
			newly = append(newly, s)
		}
	}
	if len(newly) != 0 {
		l.c.Sheets++
		l.sheetFact(tr, newly, "sheet_laid", ref, out)
	}
}

func (l *trayLedger) observeSheets(tr *openTray, tw *trayWorld, calm bool, out *[]TrayFact) {
	var uncovered []*trayStone
	for id, s := range tr.link {
		if ws := tw.byID[id]; ws != nil {
			if ws.UnderSheet {
				s.underSheet = true
			} else if s.underSheet {
				uncovered = append(uncovered, s)
			}
		}
	}
	l.sheetLaid(tr, uncovered, tw.ref, out)
	l.sheetFact(tr, uncovered, "sheet_lifted", tw.ref, out)
	for _, s := range uncovered {
		s.underSheet, s.sheetSince = false, nil
	}
	if calm {
		l.sheetLaid(tr, tr.stones, tw.ref, out)
	}
}

// retireSheet runs on moving packets too. A whole layer departing keeps the inventory of the board that just left.
func (l *trayLedger) retireSheet(tw *trayWorld, out *[]TrayFact) bool {
	tr := l.tray
	if tr == nil || tr.arrived == nil || tr.last == nil {
		return false
	}
	retired, lift := map[int64]bool{}, map[int64]bool{}
	for _, e := range tw.w.Events {
		if e.Type == EventDelete && e.Why == "sheet" && e.ID != nil {
			retired[*e.ID] = true
		}
		if e.Type == EventSheet && e.Why == "lifted" {
			for _, id := range e.IDs {
				lift[id] = true
			}
		}
	}
	if len(retired) == 0 {
		return false
	}
	var affected, lifted []*trayStone
	allNamed, anchors := true, 0
	for id, s := range tr.link {
		if !s.underSheet {
			continue
		}
		if lift[id] {
			lifted = append(lifted, s)
		}
		if retired[id] {
			affected = append(affected, s)
			allNamed = allNamed && lift[id]
		} else if ws := tw.byID[id]; ws != nil && ws.UnderSheet && dist(placeOf(ws), s.place) <= nearD {
			anchors++
		}
	}
	if len(affected) == 0 {
		return false
	}
	if l.on && !tw.hole && tw.w.Motion == "static" && allNamed && (anchors >= 2 || l.sameExposedLayer(tr, tw)) {
		l.sheetLaid(tr, affected, tw.ref, out)
		l.sheetFact(tr, affected, "sheet_lifted", tw.ref, out)
		for _, s := range affected {
			delete(tr.link, s.world)
			lose(tr, s, tw.ref)
			s.sheetLoss = true
		}
		tr.worked = true
		return false
	}
	l.closeAt(out, &tw.ref, lifted)
	l.stop, l.on = nil, false
	return true
}

func checkSheets(t *Tray, byID map[string]*TrayStone, bad map[string]bool) map[string]bool {
	covered, lifted := map[string]bool{}, map[string]bool{}
	for _, f := range t.Facts {
		if f.Kind != TraySheet {
			continue
		}
		if len(f.TrayStones) == 0 || int64(len(f.TrayStones)) != f.Stones {
			bad["C10"] = true
		}
		for i, id := range f.TrayStones {
			s := byID[id]
			if (i > 0 && f.TrayStones[i-1] >= id) || s == nil || s.Born.G > f.G {
				bad["C10"] = true
			}
			switch f.Code {
			case "sheet_laid":
				if covered[id] {
					bad["C10"] = true
				}
				covered[id] = true
			case "sheet_lifted":
				if !covered[id] {
					bad["C10"] = true
				}
				delete(covered, id)
				lifted[id] = true
			default:
				bad["C10"] = true
			}
		}
	}
	if n := len(t.Facts); n > 0 && t.Facts[n-1].Kind == TrayLeaving {
		var under int64
		for _, s := range t.Stones {
			if s.Status == TrayStoneOn && s.UnderSheet {
				under++
			}
		}
		if f := t.Facts[n-1]; f.UnderSheet != under || under > f.Stones {
			bad["C11"] = true
		}
	}
	return lifted
}

// MarshalJSON keeps the two sheet counts together whenever either is present.
func (t TrayCounts) MarshalJSON() ([]byte, error) {
	type counts TrayCounts
	if t.Sheets == 0 && t.UnderSheet == 0 {
		return json.Marshal(counts(t))
	}
	return json.Marshal(struct {
		counts
		Sheets     int64 `json:"sheets"`
		UnderSheet int64 `json:"under_sheet"`
	}{counts(t), t.Sheets, t.UnderSheet})
}
