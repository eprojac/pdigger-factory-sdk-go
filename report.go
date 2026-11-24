package pdiggerfactory

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Moved is MOVED of section 4.7 of the SDK design: a stone the tray arrived with, that the SDK is sure of, saw at the
// leaving point and that something touched on the tray, moved when, once the tray's own shift is taken out, its place
// there is more than this many times its short side from where it was first seen.
const Moved = 0.75

// TrayMoved is the Kind of a TrayChange for a stone that moved (the others are TrayRemoved and TrayAdded).
const TrayMoved = "moved"

// The confidence of a change (section 4.6.13 of the SDK design, TrayChange.Confidence): the probability that it
// happened, the share of the four recordings' changes of its class that the maintainers' offline checks found right
// (their look tracker, each stone followed by its look both ways, on 148 trays; their camera check of all 52 certain
// removals; 150 trays in all), as (right + 1) / (checked + 2), to two places. A removal or an addition takes the
// number of its kind, whether it is certain and its stone's Evidence, which supports it (EvidenceNotFound), leaves
// it undecided (EvidenceUndecided) or contradicts it (EvidenceFound: a removed stone found on the tray as it left,
// an added one found on it at the arrival); a move takes MovedConfidence. The numbers are the same in every
// language: keep the changes at or above a threshold of your own.
const (
	ConfidenceRemovedCertainNotFound  = 0.98 // 48 of 48 right
	ConfidenceRemovedCertainUndecided = 0.50 // 1 of 2
	ConfidenceRemovedCertainFound     = 0.25 // 0 of 2
	ConfidenceRemovedUnclearNotFound  = 0.71 // 11 of 15
	ConfidenceRemovedUnclearUndecided = 0.58 // 35 of 60
	ConfidenceRemovedUnclearFound     = 0.09 // 1 of 21
	ConfidenceAddedUnclearNotFound    = 0.96 // 23 of 23
	ConfidenceAddedUnclearUndecided   = 0.52 // 32 of 62
	ConfidenceAddedUnclearFound       = 0.02 // 0 of 50
	// A certain addition takes the unclear additions' numbers: the four recordings have none to check.
	ConfidenceAddedCertainNotFound  = ConfidenceAddedUnclearNotFound
	ConfidenceAddedCertainUndecided = ConfidenceAddedUnclearUndecided
	ConfidenceAddedCertainFound     = ConfidenceAddedUnclearFound
	// MovedConfidence is the confidence of a move, which is always certain: 19 of 21 paired moves right.
	MovedConfidence = 0.87
)

// confidenceClass is a class of the confidence table: a change's kind, whether it is certain, and its stone's evidence.
type confidenceClass struct {
	kind     string
	certain  bool
	evidence string
}

// confidences is the confidence table of section 4.6.13 (CONFIDENCE).
var confidences = map[confidenceClass]float64{
	{TrayRemoved, true, EvidenceNotFound}:   ConfidenceRemovedCertainNotFound,
	{TrayRemoved, true, EvidenceUndecided}:  ConfidenceRemovedCertainUndecided,
	{TrayRemoved, true, EvidenceFound}:      ConfidenceRemovedCertainFound,
	{TrayRemoved, false, EvidenceNotFound}:  ConfidenceRemovedUnclearNotFound,
	{TrayRemoved, false, EvidenceUndecided}: ConfidenceRemovedUnclearUndecided,
	{TrayRemoved, false, EvidenceFound}:     ConfidenceRemovedUnclearFound,
	{TrayAdded, true, EvidenceNotFound}:     ConfidenceAddedCertainNotFound,
	{TrayAdded, true, EvidenceUndecided}:    ConfidenceAddedCertainUndecided,
	{TrayAdded, true, EvidenceFound}:        ConfidenceAddedCertainFound,
	{TrayAdded, false, EvidenceNotFound}:    ConfidenceAddedUnclearNotFound,
	{TrayAdded, false, EvidenceUndecided}:   ConfidenceAddedUnclearUndecided,
	{TrayAdded, false, EvidenceFound}:       ConfidenceAddedUnclearFound,
}

// confidence is the confidence of a removal or an addition (kind TrayRemoved or TrayAdded): its number in the table by
// whether it is certain and by its stone's evidence; an evidence that is neither EvidenceFound nor EvidenceNotFound
// (none: the stone of a tray made by hand) counts as EvidenceUndecided.
func confidence(kind string, certain bool, evidence string) float64 {
	if evidence != EvidenceFound && evidence != EvidenceNotFound {
		evidence = EvidenceUndecided
	}
	return confidences[confidenceClass{kind, certain, evidence}]
}

// TrayChange is one stone of a TrayReport (section 4.7): Kind is TrayRemoved, TrayAdded or TrayMoved, Stone the tray
// stone's id.
//
// For removed and added, At, Certain and Code are its fact's ("carried_away", "missing", "not_found"; "set_down",
// "not_recognised"); for moved, At is nil, Code "" and Certain true: only a stone the SDK is sure of, seen at the
// leaving point and touched on the tray is reported moved (one that is not sure may be another stone at that place).
// Place is the stone's final place on the tray, in the world's scene units (x right and y up as the camera sees the
// tray), for matching a report with another system that looks at the trays: for a removal, where the stone was last
// seen before it was taken; for an addition or a move, its place as the tray left (its LeftPlace when it was seen at
// the leaving point, else where it was last seen or, for a stone counted because the leaving point's world kept it, its
// place in that world). Size is its long and short side, as last known (for a stone counted from the leaving point's
// world, boxed or kept there, as that world has it). Distance is how far a moved stone moved once the tray's own shift
// is taken out (nil for the others). Confidence is the probability that the change happened (section 4.6.13: by its
// kind, whether it is certain and the stone's Evidence; MovedConfidence for a move), for a threshold of your own.
type TrayChange struct {
	Kind       string
	Stone      string
	At         *TrayPoint
	Certain    bool
	Code       string
	Place      [2]float64
	Size       [2]float64
	Distance   *float64
	Confidence float64
}

// Describe is one line: for removed and added, the clock of its fact, the kind, the stone and its final place ("from"
// it for a removal, "at" it for an addition), then its confidence, " (0.98)", or " (unclear, 0.71)" when it is not
// certain ("0:45.5 removed 20261122T023847.0Z-1-117 from (0.68, -1.06) (0.98)"); for moved, "moved", the stone, the
// distance, "to" its place and its confidence ("moved 20261122T023847.0Z-1-85 0.33 to (-0.12, -0.18) (0.87)").
func (c TrayChange) Describe() string {
	coord := func(v float64) string {
		if s := FormatFixed(v, 2); s != "-0.00" {
			return s
		}
		return "0.00"
	}
	where := "(" + coord(c.Place[0]) + ", " + coord(c.Place[1]) + ")"
	conf := FormatFixed(c.Confidence, 2)
	if c.Kind == TrayMoved {
		d := 0.0
		if c.Distance != nil {
			d = *c.Distance
		}
		return "moved " + c.Stone + " " + FormatFixed(d, 2) + " to " + where + " (" + conf + ")"
	}
	at := ""
	if c.At != nil {
		at = FormatClock(c.At.T) + " "
	}
	prep := " at "
	if c.Kind == TrayRemoved {
		prep = " from "
	}
	line := at + c.Kind + " " + c.Stone + prep + where
	if !c.Certain {
		return line + " (unclear, " + conf + ")"
	}
	return line + " (" + conf + ")"
}

// String is Describe.
func (c TrayChange) String() string { return c.Describe() }

// pointJSON is a TrayPoint as the other languages write it.
type pointJSON struct {
	G    int64    `json:"g"`
	T    float64  `json:"t"`
	Wall *float64 `json:"wall"`
}

func pointOf(p TrayPoint) pointJSON { return pointJSON(p) }

// MarshalJSON writes the change with the other languages' names: kind, stone, at ({g, t, wall} or null), certain, code
// (null for moved), place, size, distance, confidence.
func (c TrayChange) MarshalJSON() ([]byte, error) {
	var at *pointJSON
	if c.At != nil {
		p := pointOf(*c.At)
		at = &p
	}
	var code *string
	if c.Code != "" {
		code = &c.Code
	}
	return json.Marshal(struct {
		Kind       string     `json:"kind"`
		Stone      string     `json:"stone"`
		At         *pointJSON `json:"at"`
		Certain    bool       `json:"certain"`
		Code       *string    `json:"code"`
		Place      [2]float64 `json:"place"`
		Size       [2]float64 `json:"size"`
		Distance   *float64   `json:"distance"`
		Confidence float64    `json:"confidence"`
	}{c.Kind, c.Stone, at, c.Certain, code, c.Place, c.Size, c.Distance, c.Confidence})
}

// TrayReport is a tray's report, the SDK's simple mode (section 4.7): on tray Tray, from Arrived to Left, which
// stones were Removed, Added and Moved. A change in Added with Certain false is an occupant whose origin the SDK
// could not establish. It can be a stone already on the tray when it arrived.
//
// Complete says that is the tray's whole account: every change is certain and no stone is open. Otherwise Unclear
// counts the changes that are not certain and Unsure the stones on the tray as it left whose identity is not sure
// (those added unclear aside); the tray's facts and its open account (Tray.Open) say why, stone by stone. Stones is
// how many stones the tray arrived with. Arrived.Wall and Left.Wall are the unit's wall clock (UTC epoch seconds)
// when it is known. json.Marshal writes it with the other languages' names (session, video, tray, arrived, left, stones,
// removed, added, moved, unclear, unsure, complete), for a database, an MES or a spreadsheet.
type TrayReport struct {
	Session  int64
	Video    string
	Tray     int64
	Arrived  TrayPoint
	Left     TrayPoint
	Stones   int64
	Removed  []TrayChange
	Added    []TrayChange
	Moved    []TrayChange
	Unclear  int64
	Unsure   int64
	Complete bool
}

// Describe is "tray 1 (2026-11-22T02:38:47.0Z), 0:22.5 to 2:24.0: 11 removed, 13 added, 8 moved, not complete:
// 18 unclear, 1 not sure", then a line for each change, indented two spaces: removed, then added, then moved.
func (r TrayReport) Describe() string {
	var b strings.Builder
	b.WriteString("tray " + itoa(r.Tray))
	if r.Arrived.Wall != nil {
		b.WriteString(" (" + trayISO(*r.Arrived.Wall) + ")")
	}
	fmt.Fprintf(&b, ", %s to %s: %d removed, %d added, %d moved", FormatClock(r.Arrived.T), FormatClock(r.Left.T),
		len(r.Removed), len(r.Added), len(r.Moved))
	if !r.Complete {
		var parts []string
		if r.Unclear > 0 {
			parts = append(parts, itoa(r.Unclear)+" unclear")
		}
		if r.Unsure > 0 {
			parts = append(parts, itoa(r.Unsure)+" not sure")
		}
		b.WriteString(", not complete: " + strings.Join(parts, ", "))
	}
	for _, list := range [][]TrayChange{r.Removed, r.Added, r.Moved} {
		for _, c := range list {
			b.WriteString("\n  " + c.Describe())
		}
	}
	return b.String()
}

// String is Describe.
func (r TrayReport) String() string { return r.Describe() }

// MarshalJSON writes the report with the other languages' names.
func (r TrayReport) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Session  int64        `json:"session"`
		Video    string       `json:"video"`
		Tray     int64        `json:"tray"`
		Arrived  pointJSON    `json:"arrived"`
		Left     pointJSON    `json:"left"`
		Stones   int64        `json:"stones"`
		Removed  []TrayChange `json:"removed"`
		Added    []TrayChange `json:"added"`
		Moved    []TrayChange `json:"moved"`
		Unclear  int64        `json:"unclear"`
		Unsure   int64        `json:"unsure"`
		Complete bool         `json:"complete"`
	}{r.Session, r.Video, r.Tray, pointOf(r.Arrived), pointOf(r.Left), r.Stones, nonNil(r.Removed), nonNil(r.Added),
		nonNil(r.Moved), r.Unclear, r.Unsure, r.Complete})
}

func nonNil(c []TrayChange) []TrayChange {
	if c == nil {
		return []TrayChange{}
	}
	return c
}

// Report is the tray's report (section 4.7): which stones were removed, added and moved, and whether that is its
// whole account. Nil until the tray has left, and for a tray that never arrived.
func (t *Tray) Report() *TrayReport { return trayReport(t) }

func median(xs []float64) float64 {
	v := append([]float64(nil), xs...)
	sort.Float64s(v)
	k := len(v) / 2
	if len(v)%2 == 1 {
		return v[k]
	}
	return (v[k-1] + v[k]) / 2
}

// trayReport is the report of a tray that has left, from its facts and its final tray stones; nil for any other.
func trayReport(t *Tray) *TrayReport {
	facts := t.Facts
	if t.Arrived == nil || t.Leaving == nil || len(facts) == 0 || facts[len(facts)-1].Kind != TrayLeaving {
		return nil
	}
	arrival, leaving := facts[0], facts[len(facts)-1]
	byID := make(map[string]*TrayStone, len(t.Stones))
	for i := range t.Stones {
		byID[t.Stones[i].ID] = &t.Stones[i]
	}
	r := &TrayReport{Session: t.Session, Video: t.Video, Tray: t.N,
		Arrived: TrayPoint{G: arrival.G, T: arrival.T, Wall: arrival.Wall},
		Left:    TrayPoint{G: leaving.G, T: leaving.T, Wall: leaving.Wall},
		Stones:  arrival.Stones, Removed: []TrayChange{}, Added: []TrayChange{}, Moved: []TrayChange{},
		Unclear: leaving.Unclear}
	for _, x := range facts {
		s := byID[x.Stone]
		if s == nil || (x.Kind != TrayRemoved && x.Kind != TrayAdded) {
			continue
		}
		certain := x.Certain != nil && *x.Certain
		c := TrayChange{Kind: x.Kind, Stone: s.ID, At: &TrayPoint{G: x.G, T: x.T, Wall: x.Wall}, Certain: certain,
			Code: x.Code, Place: s.Place, Size: s.Size, Confidence: confidence(x.Kind, certain, s.Evidence)}
		if x.Kind == TrayAdded && s.LeftPlace != nil {
			c.Place = *s.LeftPlace // its final place: as the tray left
		}
		if x.Kind == TrayRemoved {
			r.Removed = append(r.Removed, c)
		} else {
			r.Added = append(r.Added, c)
		}
	}
	// moved: only the stones it arrived with that are sure (a stone not sure counts as not sure, not as moved), at
	// their place at the leaving point (where they were seen), and only those something touched
	type keptStone struct {
		s *TrayStone
		p vec2
	}
	var kept []keptStone
	for i := range t.Stones {
		s := &t.Stones[i]
		if !s.Added && s.Status == TrayStoneOn && s.Sure && s.LeftPlace != nil {
			kept = append(kept, keptStone{s, vec2(*s.LeftPlace)})
		}
	}
	if len(kept) > 0 {
		// the tray's own shift: the median over the half of them that moved least (ties in tray order)
		order := make([]int, len(kept))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool {
			return dist(vec2(kept[order[a]].s.FirstPlace), kept[order[a]].p) <
				dist(vec2(kept[order[b]].s.FirstPlace), kept[order[b]].p)
		})
		var dx, dy []float64
		for _, i := range order[:(len(kept)+1)/2] {
			dx = append(dx, kept[i].p[0]-kept[i].s.FirstPlace[0])
			dy = append(dy, kept[i].p[1]-kept[i].s.FirstPlace[1])
		}
		mx, my := median(dx), median(dy)
		for _, k := range kept {
			d := dist(vec2{k.s.FirstPlace[0] + mx, k.s.FirstPlace[1] + my}, k.p)
			if k.s.Touched != nil && d > Moved*k.s.Size[1] {
				r.Moved = append(r.Moved, TrayChange{Kind: TrayMoved, Stone: k.s.ID, Certain: true, Place: k.p,
					Size: k.s.Size, Distance: Float64(Round3(d)), Confidence: MovedConfidence})
			}
		}
	}
	for i := range t.Stones {
		s := &t.Stones[i]
		if s.Status == TrayStoneOn && !s.Sure && !(s.Added && s.Certain != nil && !*s.Certain) {
			r.Unsure++
		}
	}
	r.Complete = leaving.Unclear == 0 && leaving.Sure == leaving.Stones
	return r
}

// trayISO is a wall time (UTC epoch seconds) to the tenth of a second, rounded as trayStamp rounds it:
// "2026-11-22T02:38:47.0Z".
func trayISO(wall float64) string {
	n := int64(math.Floor(wall*10 + 0.5))
	secs, tenth := floorDiv(n, 10), floorMod(n, 10)
	y, m, d := civilFromDays(floorDiv(secs, 86400))
	r := floorMod(secs, 86400)
	return fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d.%dZ", y, m, d, r/3600, r%3600/60, r%60, tenth)
}
