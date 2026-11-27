package pdiggerfactory

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The unit's speckle answers, read as a tray's (section 4.6.12 of the SDK design). The tray rules use one of them, the
// answer for the tray's leaving (section 4.6.13): its pairs confirm stones, link again the ones the rules had lost or
// swapped, and say for every stone whether it was found at the other moment (TrayStone.Evidence), which gives each
// change of the report its confidence (TrayChange.Confidence). The other answers change no fact, count, report or open
// account.

// SpeckleClean is the least score of a clean find, in permille (the score rounded as the tray rules round a look's
// score): a clean find's score is also at least twice its second. It is the bar of the leaving's identified facts.
const SpeckleClean = 300

// SpeckleNear is how many frames an answer's reference may lie from its tray's arrival for the tray's links as it
// arrived to stand for the reference's, and an answer's later moment from the tray's leaving point for it to be the
// leaving's answer.
const SpeckleNear = 2

// The codes of the identified facts of the leaving's speckle answer (section 4.6.13): one of the unit's pairs links the
// stone to its world stone, sure.
const (
	// SpeckleSame is a stone already on that world stone, not sure, that is sure again ("confirmed by its speckle").
	SpeckleSame = "speckle_same"
	// SpeckleMoved is a stone on another world stone, linked to this one ("recognised by its speckle"); the fact's
	// FromWorld is the world stone it stood for before.
	SpeckleMoved = "speckle_moved"
	// SpeckleFound is a lost stone, linked to this world stone ("found again by its speckle").
	SpeckleFound = "speckle_found"
)

// The evidence of a tray stone (TrayStone.Evidence, section 4.6.13): what the leaving's speckle answer found of it at
// the other moment, for a stone the tray arrived with whether it is on the tray as it left (its arrival world id's
// find), for one added and on the tray as it left whether it was on it at the arrival (its world id's find).
const (
	// EvidenceFound is a clean find, in one of the unit's pairs or not.
	EvidenceFound = "found"
	// EvidenceNotFound is a stone checked without a find of SpeckleClean or more.
	EvidenceNotFound = "not_found"
	// EvidenceUndecided is no leaving answer, the stone not in it or not checked, or a find of SpeckleClean or more
	// that is not twice its second (look-alikes); and an added stone removed again.
	EvidenceUndecided = "undecided"
)

// speckleKept is how many answers InterpreterCore.Speckle keeps, the newest (1024); a variable so tests can make it
// small.
var speckleKept = 1024

// The sides of a StoneSpeckle.
const (
	// SpeckleArrival is the stone as it was at the answer's reference, checked at its later moment.
	SpeckleArrival = "arrival"
	// SpeckleLater is the stone as it was at the later moment, checked at the reference.
	SpeckleLater = "later"
)

// StoneSpeckle is a tray stone in one of the unit's speckle answers.
type StoneSpeckle struct {
	// Stone is the tray stone's id.
	Stone string
	// Side is SpeckleArrival or SpeckleLater.
	Side string
	// Score and Second are as the unit sent them (both nil: the stone was not checked; Second -1: no second place).
	Score, Second *float64
	// Clean says the score is at least 0.300 and at least twice the second (in permille: SpeckleClean).
	Clean bool
	// FoundWorld is the world stone at the other moment the best place lies on, Found the tray stone that stood for it
	// there (nil when there is none).
	Found      *string
	FoundWorld *int64
	// Pair is the tray stone the answer pairs it with (nil when there is none).
	Pair *string
}

// Same says the answer pairs the stone with itself: the same stone at both moments.
func (s StoneSpeckle) Same() bool { return s.Pair != nil && *s.Pair == s.Stone }

// TraySpeckle is one of the unit's speckle answers, read as a tray's: the tray, the frame it came in, the two moments it
// compares and the tray stones in it.
type TraySpeckle struct {
	// Tray is the SDK's tray number (Tray.N) in Session, of Video.
	Tray    int64
	Session int64
	Video   string
	// G, T and Wall are the frame the answer came in (Wall nil without a clock state).
	G    int64
	T    float64
	Wall *float64
	// UnitTray is the answer's own tray, the unit's count.
	UnitTray int64
	// Ref and At are the moments compared (G and T): the reference, the tray's arrival, and the later moment.
	Ref, At TrayPoint
	// Device is the GPU the unit checked on; GPUMS and WallMS the GPU time of the checks and the time the unit's
	// speckle service spent on the request, in milliseconds.
	Device        string
	GPUMS, WallMS float64
	// Stones are the tray stones in the answer, in tray order.
	Stones []StoneSpeckle
}

// Stone is the answer for that tray stone (nil when it has none).
func (a TraySpeckle) Stone(id string) *StoneSpeckle {
	for i := range a.Stones {
		if a.Stones[i].Stone == id {
			return &a.Stones[i]
		}
	}
	return nil
}

// Describe is "tray 2: speckle of 0:22.5 and 2:24.2: 42 stones, 33 clean, 26 paired, 24 the same": the stones, those
// clean, those with a pair and those paired with themselves.
func (a TraySpeckle) Describe() string {
	clean, paired, same := 0, 0, 0
	for _, e := range a.Stones {
		if e.Clean {
			clean++
		}
		if e.Pair != nil {
			paired++
		}
		if e.Same() {
			same++
		}
	}
	return fmt.Sprintf("tray %d: speckle of %s and %s: %d stones, %d clean, %d paired, %d the same", a.Tray,
		FormatClock(a.Ref.T), FormatClock(a.At.T), len(a.Stones), clean, paired, same)
}

// String is Describe.
func (a TraySpeckle) String() string { return a.Describe() }

// DescribeSpeckleTxt is the golden speckle.txt form of answers: for each, "{g} {clock} " and its Describe; then for each
// of its stones two spaces, the stone, its side, its score and second to 3 places ("-" when not checked), " clean"
// when clean, " found {tray stone}" (or " found world {id}" when no tray stone stood there), " pair {tray stone}" when
// it has one; a newline after every line.
func DescribeSpeckleTxt(answers []TraySpeckle) string {
	var b strings.Builder
	for _, a := range answers {
		b.WriteString(strconv.FormatInt(a.G, 10) + " " + FormatClock(a.T) + " " + a.Describe() + "\n")
		for _, e := range a.Stones {
			b.WriteString("  " + e.Stone + " " + e.Side + " ")
			if e.Score != nil && e.Second != nil {
				b.WriteString(FormatFixed(*e.Score, 3) + " " + FormatFixed(*e.Second, 3))
			} else {
				b.WriteString("-")
			}
			if e.Clean {
				b.WriteString(" clean")
			}
			if e.Found != nil {
				b.WriteString(" found " + *e.Found)
			} else if e.FoundWorld != nil {
				b.WriteString(" found world " + strconv.FormatInt(*e.FoundWorld, 10))
			}
			if e.Pair != nil {
				b.WriteString(" pair " + *e.Pair)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// speckle reads one of the unit's speckle answers, from the world of frame here (fed already): its tray and the tray
// stones in it, or nil when it is no tray's. linksBefore(g) gives the tray links before frame g (the core keeps them
// for revisions; nil when it does not). An answer read while its tray is on is kept on the tray, for its leaving
// (section 4.6.13).
func (l *trayLedger) speckle(sp *WorldSpeckle, here TrayPoint, linksBefore func(int64) map[int64]string) *TraySpeckle {
	rg, ag := sp.Ref.G, sp.At.G
	tr := l.tray
	if tr == nil || tr.arrived == nil || rg < tr.arrived.G-SpeckleNear {
		tr = l.lastClosed // a tray that left has arrived and has a leaving point
		if tr == nil || rg < tr.arrived.G-SpeckleNear || rg > tr.leaving.G {
			return nil
		}
	}
	mine := make(map[string]bool, len(tr.stones))
	for _, s := range tr.stones {
		mine[s.id] = true
	}
	own := func(links map[int64]string) map[int64]string { // the links to this tray's stones
		out := map[int64]string{}
		for wid, sid := range links {
			if mine[sid] {
				out[wid] = sid
			}
		}
		return out
	}
	refLinks := tr.arrivalLinks
	if rg-tr.arrived.G > SpeckleNear || tr.arrived.G-rg > SpeckleNear {
		refLinks = linksBefore(rg)
	}
	refLinks = own(refLinks)
	atLinks := own(linksBefore(ag))
	byRef, byAt := map[int64]int64{}, map[int64]int64{}
	for _, p := range sp.Pairs {
		byAt[p[0]], byRef[p[1]] = p[1], p[0]
	}
	got := map[string]StoneSpeckle{}
	read := func(finds []SpeckleFind, side string, links, other map[int64]string, pairs map[int64]int64) {
		for _, x := range finds {
			sid, ok := links[x.ID]
			if _, done := got[sid]; !ok || done {
				continue
			}
			e := StoneSpeckle{Stone: sid, Side: side, Score: copyNum(x.Score), Second: copyNum(x.Second),
				Clean: speckleClean(&x), FoundWorld: copyID(x.At)}
			if x.At != nil {
				if f, ok := other[*x.At]; ok {
					e.Found = &f
				}
			}
			if w, ok := pairs[x.ID]; ok {
				if p, ok := other[w]; ok {
					e.Pair = &p
				}
			}
			got[sid] = e
		}
	}
	read(sp.Refs, SpeckleArrival, refLinks, atLinks, byRef) // a stone as it was at the reference, checked later
	read(sp.Stones, SpeckleLater, atLinks, refLinks, byAt)  // a stone at the later moment, checked at the reference
	a := &TraySpeckle{Tray: tr.n, Session: l.session, Video: l.video, G: here.G, T: here.T, Wall: here.Wall,
		UnitTray: sp.Tray, Ref: TrayPoint{G: rg, T: sp.Ref.T}, At: TrayPoint{G: ag, T: sp.At.T}, Device: sp.Device,
		GPUMS: sp.GPUMS, WallMS: sp.WallMS}
	if tr == l.tray { // the answers read while the tray is on, for its leaving (4.6.13)
		tr.answers = append(tr.answers, sp)
	}
	for _, s := range tr.stones {
		if e, ok := got[s.id]; ok {
			a.Stones = append(a.Stones, e)
			if tr == l.tray { // while the tray is on, the newest answer is its stones'
				s.speckle = &e
			}
		}
	}
	return a
}

// copyNum is a copy of a number that may be absent.
func copyNum(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// speckleClean says a find is clean (section 4.6.12): scored, p(score) >= SpeckleClean and p(score) >= 2 * p(second).
func speckleClean(x *SpeckleFind) bool {
	return x.Score != nil && x.Second != nil && permilleOf(*x.Score) >= SpeckleClean &&
		permilleOf(*x.Score) >= 2*permilleOf(*x.Second)
}

// ---- the leaving's speckle answer (section 4.6.13) -----------------------------------------------------------------

// leavingAnswer is the leaving's answer: the newest answer read while the tray was on whose later moment lies within
// SpeckleNear frames of the leaving point ref, else nil. (A later moment further from the leaving point comes before
// hand work on the tray: the leaving point is the last calm frame unless hands worked after it.)
func leavingAnswer(tr *openTray, ref TrayPoint) *WorldSpeckle {
	for i := len(tr.answers) - 1; i >= 0; i-- {
		if d := tr.answers[i].At.G - ref.G; d >= -SpeckleNear && d <= SpeckleNear {
			return tr.answers[i]
		}
	}
	return nil
}

// speckleTarget is a pair of the leaving's answer that links a tray stone: the stone, the later world id c, the pair's
// clean find, and the stone's status and world id before (w0, when it was on the tray).
type speckleTarget struct {
	s      *trayStone
	c      int64
	x      *SpeckleFind
	status string
	w0     int64
}

// same says the stone was on the pair's world stone already.
func (t speckleTarget) same() bool { return t.status == TrayStoneOn && t.w0 == t.c }

// findsByID are an answer's finds by world id (of two finds of one id, the later).
func findsByID(finds []SpeckleFind) map[int64]*SpeckleFind {
	m := make(map[int64]*SpeckleFind, len(finds))
	for i := range finds {
		m[finds[i].ID] = &finds[i]
	}
	return m
}

// pointStone is world stone id as the leaving point's world has it, else as the last frame's (tw) has it; nil when
// neither has it.
func pointStone(tr *openTray, id int64, tw *trayWorld) *StoneView {
	if tr.leaveWorld != nil {
		if ws, ok := tr.leaveWorld.byID[id]; ok {
			return ws
		}
	}
	return tw.byID[id]
}

// speckleTargets are the leaving answer's pairs [c, r] that link a tray stone, in the answer's order: r is linked at
// the arrival to a stone that was not added and is on the tray or lost, and c is a world stone of the leaving point's
// world (else of the last frame's), not held; a world id or a stone in an earlier such pair is skipped, and so is a
// pair that would move a trusted stone to another world stone or give a trusted stone's world stone to another. Its
// clean find is r's find when clean, else c's. (A pair whose clean find is missing or not scored is skipped too, unless
// its stone is sure and on c already and so needs no fact: the unit never sends such a pair, and the reference has no
// reading for the others.)
func speckleTargets(tr *openTray, sp *WorldSpeckle, tw *trayWorld) []speckleTarget {
	byTray := make(map[string]*trayStone, len(tr.stones))
	for _, s := range tr.stones {
		byTray[s.id] = s
	}
	refs, later := findsByID(sp.Refs), findsByID(sp.Stones)
	var targets []speckleTarget
	claimed, used := map[int64]bool{}, map[*trayStone]bool{}
	for _, p := range sp.Pairs {
		c, r := p[0], p[1]
		s := byTray[tr.arrivalLinks[r]]
		ws := pointStone(tr, c, tw)
		if s == nil || s.underSheet || s.added || (s.status != TrayStoneOn && s.status != TrayStoneLost) || ws == nil || ws.UnderSheet || inHand(ws) ||
			claimed[c] || used[s] {
			continue
		}
		// a stone nothing disturbed (trusted) keeps its world stone, and keeps it from others: on look-alike pavers a
		// pair can join two neighbours, while an undisturbed stone has not moved
		if y := tr.link[c]; (trusted(s) && s.world != c) || (y != nil && y != s && trusted(y)) {
			continue
		}
		x := refs[r]
		if x == nil || !speckleClean(x) {
			x = later[c]
		}
		if (x == nil || x.Score == nil) && !(s.status == TrayStoneOn && s.world == c && s.sure) {
			continue
		}
		targets = append(targets, speckleTarget{s: s, c: c, x: x, status: s.status, w0: s.world})
		claimed[c], used[s] = true, true
	}
	return targets
}

// speckleLeaving is the unit's pairs at the leaving (section 4.6.13), after the leaving's look work and before its
// questions end: a stone already on its pair's world stone is confirmed (its question ends; one that was not sure is
// sure again, SpeckleSame); any other is linked to it, sure (SpeckleMoved or SpeckleFound): the stone that stood there
// is lost, and the world stone the moved stone stood on is unidentified again (freed) when it is on the board, not held
// and in no pair. Each fact holds the pair's clean find's score and margin in permille, and the bar SpeckleClean.
func (l *trayLedger) speckleLeaving(tr *openTray, tw *trayWorld, ref TrayPoint, out *[]TrayFact) {
	sp := leavingAnswer(tr, ref)
	tr.leavingSpeckle = sp
	if sp == nil {
		return
	}
	targets := speckleTargets(tr, sp, tw)
	claimed := make(map[int64]bool, len(targets))
	for _, t := range targets {
		claimed[t.c] = true
	}
	for _, t := range targets {
		if t.same() {
			continue
		}
		s, c := t.s, t.c
		y, u := tr.link[c], tr.unid[c]
		delete(tr.link, c)
		delete(tr.unid, c)
		if y != nil && y != s {
			lose(tr, y, ref)
		}
		if s.status == TrayStoneOn && tr.link[s.world] == s {
			w0 := s.world
			delete(tr.link, w0)
			if ws0 := tw.byID[w0]; !claimed[w0] && ws0 != nil && !inHand(ws0) && insideBoard(tr.board, placeOf(ws0)) {
				tr.unid[w0] = &trayUnid{born: ref, place: placeOf(ws0), size: sizeOf(ws0), freed: true}
			}
		}
		switch {
		case u != nil:
			s.worldT = u.born.T
		case y != nil:
			s.worldT = y.worldT
		default:
			s.worldT = ref.T
		}
		ws := pointStone(tr, c, tw)
		s.linked, s.world, s.status = true, c, TrayStoneOn
		s.touch(ref.G)
		s.carriedAway, s.dropped, s.emptied = false, false, false
		s.place = placeOf(ws)
		if ws.Detected {
			s.seenT = ref.T
		}
		tr.link[c] = s
		s.atK, s.at = tr.k, s.place
	}
	for _, t := range targets {
		s := t.s
		s.question, s.bySpeckle = nil, true
		var from *int64
		code, why := SpeckleMoved, "recognised by its speckle"
		switch {
		case t.same() && s.sure: // sure, or a question settled: no fact
			continue
		case t.same():
			code, why = SpeckleSame, "confirmed by its speckle"
		case t.status == TrayStoneLost:
			code, why = SpeckleFound, "found again by its speckle"
		default:
			from = Int64(t.w0)
		}
		s.sure = true
		p := permilleOf(*t.x.Score)
		var margin *int64
		if t.x.Second != nil {
			margin = Int64(p - permilleOf(*t.x.Second))
		}
		l.fact(TrayIdentified, tr, s, ref, TrayFact{Why: why, Code: code, FromWorld: from, Score: Int64(p),
			Margin: margin, Bar: Int64(SpeckleClean)}, out)
	}
}

// speckleEvidence is what the leaving's answer sp says of world stone wid, on its arrival side (its refs) or its later
// side (its stones): EvidenceFound when wid is in one of the unit's pairs or its find is clean, EvidenceNotFound when
// it was checked without a find of SpeckleClean or more, EvidenceUndecided otherwise (no answer, wid not in it or not
// checked, or a find of SpeckleClean or more that is not twice its second: look-alikes).
func speckleEvidence(sp *WorldSpeckle, arrival bool, wid int64) string {
	if sp == nil {
		return EvidenceUndecided
	}
	finds, side := sp.Stones, 0
	if arrival {
		finds, side = sp.Refs, 1
	}
	i := slices.IndexFunc(finds, func(x SpeckleFind) bool { return x.ID == wid })
	if i < 0 || finds[i].Score == nil {
		return EvidenceUndecided
	}
	switch x := &finds[i]; {
	case slices.ContainsFunc(sp.Pairs, func(p [2]int64) bool { return p[side] == wid }), speckleClean(x):
		return EvidenceFound
	case permilleOf(*x.Score) >= SpeckleClean:
		return EvidenceUndecided
	}
	return EvidenceNotFound
}

// stoneEvidence is TrayStone.Evidence as the tray leaves (section 4.6.13): a stone the tray arrived with by its
// arrival world id's find, one added and on the tray by its world id's; an added stone removed again is undecided.
func stoneEvidence(tr *openTray, s *trayStone) string {
	if !s.added {
		return speckleEvidence(tr.leavingSpeckle, true, s.firstWorld)
	}
	if s.status != TrayStoneOn {
		return EvidenceUndecided
	}
	return speckleEvidence(tr.leavingSpeckle, false, s.world)
}
