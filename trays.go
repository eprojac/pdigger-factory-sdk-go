package pdiggerfactory

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"slices"
	"strconv"
	"strings"
)

// Trays and tray stones (section 4.6 of the SDK design): the SDK's own stone ids, one per stone, never reused.
//
// The world's stone ids are the engine tracker's: it renames a stone it lost and found again, and it can keep an id on
// another stone (a stone set down where a removed one lay, a neighbour pushed into its place, a row of equal stones
// pushed one place along while the ids stay on the places). The interpreter follows each tray from the moment it has
// arrived and settled to the moment it leaves, and keeps its own stones on it, the tray stones. A tray stone says
// whether its identity is still sure; a fact about it is certain or unclear: an identity stays sure, and a fact is
// certain, only when what the world shows at calm moments leaves no other reading. A certain fact is never revised.
//
// A stone without looks of its own (TrayStone.Looks empty) that the world stops detecting while nothing could have
// changed it is not given up at once: its doubt is a question (a presence question, section 4.6.1: the stone is not
// sure meanwhile, and no fact is given), which seeing it again where it was settles when no hand was at work, no push
// came on the tray, no frame went unread and no other stone of its size stood at its place while it was out of sight.
//
// With looks (sections 4.6.3-4.6.10; the 20.20 unit sends them since build claude-20261120-looks1) a doubt about a
// stone that has a look is a question a later look can settle (a stone with looks that is not seen at two calm moments
// included: only a look settles its question), a look can find a lost stone again or tell which stone a world id stands
// for now (fact identified), and the account of a tray that leaves lists every stone the rules leave open with the
// readings the evidence leaves (its open account). A look never makes a fact certain: a tray's certain facts with looks
// are the first of those without them (4.6.7). Without usable looks the rules are those of the first version (4.6.1,
// presence questions included), line for line.
//
// What the SDK can not see: a box the model sees on an empty slot when the tray arrives becomes a tray stone, and a
// stone set into that slot later keeps its id.
//
// Every product below is converted to float64 before it is added to, so Go does not fuse it into one multiply-add
// (see FormatFixed): the rules must decide exactly as in the other languages.

// The rules' numbers.
const (
	trayStopMin  = 2.0  // a static stop of at least this many seconds is a tray visit
	trayCalm     = 3    // a calm moment: this many frames in a row with both hands free, no hand event, no change
	blinkFrames  = 2    // consecutive quiet worlds a detected stone may be absent before its loss takes effect
	trayQuiet    = 5    // frames since the last new before a tray can arrive
	renameT      = 3.0  // a world rename: an unidentified stone first seen within this many seconds of the loss ...
	renameD      = 0.15 // ... and this close to the lost stone's place
	sizeTol      = 0.25 // two sizes are compatible when each side differs by at most this share of the larger
	nearD        = 0.12 // an unidentified stone at the same place at two calm moments: within this distance
	handR        = 0.40 // "next to": hand work, a pushed neighbour, a stone that appeared
	leaveD       = 1.0  // when a tray leaves, what is left is paired within this distance
	ghostMoments = 2    // calm moments in a row a stone's world stone may go unseen before the stone is not sure
	overlapMax   = 0.30 // share of a stone's box another world box may cover before the stone is not sure
	emptyMargin  = 0.03 // margin of the empty-footprint check of a removal
)

// The rules by look (section 4.6.11); scores are permille integers.
const (
	tIDStart = 900  // the tray's bar while its gauge holds fewer than gaugeMin pairs
	tIDMin   = 750  // the lowest bar: max(tIDMin, alike + gaugeGap)
	gaugeGap = 100  // how far the bar lies above the most alike pair of two different stones of the tray
	gaugeMin = 20   // gauge pairs a tray needs before its bar can drop
	gaugeK   = 2    // gauge partners each taken look is scored with, both ways
	tFirm    = 800  // the bar for a link change, or with a stone without a chain near
	mID      = 150  // the margin a decision needs over every scored rival
	nID      = 100  // paired cells a decision or a doubt by look needs
	tDiff    = 400  // below this an untouched stone's own look changed
	dMatch   = 1.0  // how far a pair reaches
	dNeigh   = 0.40 // neighbours of a column, and stones without a chain near
	kPair    = 4    // pairs per row
	kNeigh   = 4    // neighbours per column
	nTake    = 16   // looks taken per look work
)

// TraysKept is how many trays that have left a new InterpreterCore keeps for Trays (the newest); see KeepTrays.
const TraysKept = 256

// Kinds of tray facts.
const (
	TrayArrived    = "arrived"
	TrayUnsure     = "unsure"
	TrayIdentified = "identified"
	TrayRemoved    = "removed"
	TrayAdded      = "added"
	TraySheet      = "sheet"
	TrayLeaving    = "leaving"
)

// Statuses of a tray stone.
const (
	TrayStoneOn      = "on"
	TrayStoneLost    = "lost"
	TrayStoneRemoved = "removed"
)

// The codes of the open account (section 4.6.8): the codes of unsure facts (the causes of open unsure entries), why
// an open removal or addition was not certain, and why the looks could not settle an entry, by kind.
var (
	trayUnsureCodes = []string{"overlap", "unseen", "pushed", "neighbour_pushed", "appeared", "size_changed",
		"found_again", "matched_at_leaving", "look_changed"}
	trayRemovedCauses = []string{"look_lost", "by_look", "twins", "relinked", "lost_late", "covered", "rivals",
		"no_hand", "unchecked"}
	trayAddedCauses = []string{"freed", "size_changed", "relinked", "doubtful", "seen_once", "unchecked"}
	trayLookCodes   = map[string][]string{
		TrayUnsure:  {"none", "late", "box", "stale", "tie", "low"},
		TrayRemoved: {"none", "late", "box", "stale", "tie", "low"},
		TrayAdded:   {"none", "tie", "new"},
	}
)

// TrayPoint is a point of a tray: grid frame, media time and wall time (nil before a clock state is known).
type TrayPoint = Moment

// TrayQuestion is a doubt held open for a look, or for the stone's next sight (a presence question, section 4.6.1), to
// settle (section 4.6.4): its cause code ("pushed", "unseen", ...), why, Since (the first frame G whose looks count for
// it) and where it opened.
type TrayQuestion struct {
	Cause  string
	Why    string
	Since  int64
	Opened TrayPoint
}

// TrayCandidate is one reading the evidence leaves for an open stone (section 4.6.8): Kind "world" (a world stone,
// with the tray stone linked to it in Stone, if any), "stone" (a tray stone), "gone" (with Why "carried_away",
// "emptied", "dropped" or "hand", or none), "unseen" or "new". Score is a look score in permille, when the last look
// run had one.
type TrayCandidate struct {
	Kind    string
	WorldID *int64
	Stone   string
	Score   *int64
	Why     string
}

// Describe is the candidate as text: "world 95 0.248", "stone 20261122T023847.0Z-1-87", "gone (hand)", "unseen" or
// "new".
func (c TrayCandidate) Describe() string {
	switch c.Kind {
	case "world", "stone":
		head := "stone " + c.Stone
		if c.Kind == "world" {
			head = "world " + idText(c.WorldID)
		}
		if c.Score == nil {
			return head
		}
		return head + " " + milli(*c.Score)
	case "gone":
		if c.Why == "" {
			return "gone"
		}
		return "gone (" + c.Why + ")"
	}
	return c.Kind
}

// TrayOpen is a stone the rules leave open when its tray leaves (section 4.6.8): Kind TrayUnsure (on the tray, not
// sure), TrayRemoved (removed unclear) or TrayAdded (added unclear), with the cause, why the looks could not settle it
// (Look, with its Score in permille), and every reading the evidence leaves (Candidates, ranked). HandNear and
// SeenAtArrival are set for added entries only.
type TrayOpen struct {
	Stone         string
	WorldID       *int64
	Kind          string
	Cause         string
	Look          string
	Score         *int64
	HandNear      *bool
	SeenAtArrival *bool
	Candidates    []TrayCandidate
}

// Describe is the entry as one line: "stone 20261122T023847.0Z-1-91: unsure neighbour_pushed, look low 0.248: world 91
// or world 95 0.248 or ... or gone".
func (o TrayOpen) Describe() string {
	s := "stone " + o.Stone + ": " + o.Kind + " " + o.Cause + ", look " + o.Look
	if o.Score != nil {
		s += " " + milli(*o.Score)
	}
	if o.HandNear != nil && *o.HandNear {
		s += ", hand near"
	}
	if o.SeenAtArrival != nil && *o.SeenAtArrival {
		s += ", seen at arrival"
	}
	parts := make([]string, len(o.Candidates))
	for i, c := range o.Candidates {
		parts[i] = c.Describe()
	}
	return s + ": " + strings.Join(parts, " or ")
}

// TrayStone is a stone of a tray, with the SDK's own id: {when first seen}-{tray}-{world id}
// ("20261122T023847.0Z-2-84"; without a clock "s{session}t{media seconds}-{tray}-{world id}"; a second stone first
// seen on the tray in the same tenth of a second under the same world id takes "-2" at the end, a third "-3"). An id is
// never given to two stones.
type TrayStone struct {
	// ID is the tray stone's id.
	ID string
	// WorldID is the world stone it stands for now (nil while lost and once removed).
	WorldID *int64
	// Status is TrayStoneOn, TrayStoneLost or TrayStoneRemoved.
	Status string
	// Sure says its identity is sure now: false while a Question is open, and once the stone is not sure, until the
	// leaving's speckle answer confirms or links it (section 4.6.13).
	Sure bool
	// Added means the SDK created this stone after arrival. If Certain is false, it could be an arrival stone
	// observed under a different world ID.
	Added bool
	// Certain says whether its fact is certain: for a removed stone its removal's (also when it was added first), for
	// an added stone that was not removed its addition's, nil for the others. So an added and removed stone that was
	// set down for certain can be Certain false: count certain additions from the facts.
	Certain *bool
	// Born is when it was first seen on the tray.
	Born TrayPoint
	// Place (x, y) and Size (long, short side) are as last known (for a stone counted from the leaving point's world,
	// boxed or kept there, as that world has it), in scene units.
	Place [2]float64
	Size  [2]float64
	// Question is the doubt a look, or the stone's next sight (a presence question, section 4.6.1), may still settle
	// (nil when none).
	Question *TrayQuestion
	// Looks are the frames G of its anchor and latest look (with looks only).
	Looks []int64
	// ByLook says a look decision linked it; LookLost that a look decision ended its link.
	ByLook   bool
	LookLost bool
	// Open is its open entry once the tray has left (nil when it was settled).
	Open *TrayOpen
	// FirstPlace is its place when the tray stone was made (at the arrival for the stones the tray arrived with, as
	// it was set down or as the tray left for an added one); LeftPlace its place at the tray's leaving point when it
	// was on the tray and seen there (nil otherwise, and while the tray is on); Touched the frame G where something
	// last touched it on this tray (a hand, a push on it or next to it, a link to another world stone; nil when
	// nothing did). The report reads these (section 4.7).
	FirstPlace [2]float64
	LeftPlace  *[2]float64
	Touched    *int64
	// Speckle is the stone in the newest of the unit's speckle answers read while its tray was on (section 4.6.12),
	// nil without one.
	Speckle *StoneSpeckle
	// BySpeckle says the leaving's speckle answer linked or confirmed it (section 4.6.13). Evidence, set as the tray
	// leaves ("" while it is on), is what that answer found of it at the other moment: EvidenceFound, EvidenceNotFound
	// or EvidenceUndecided; a report's change of the stone takes its Confidence by it.
	BySpeckle  bool
	Evidence   string
	UnderSheet bool
	SheetSince *TrayPoint
}

// TrayFact is something the interpreter says about a tray: arrived, unsure, identified, removed, added or leaving.
type TrayFact struct {
	// Kind is TrayArrived, TrayUnsure, TrayIdentified, TrayRemoved, TrayAdded or TrayLeaving.
	Kind string
	// Tray is the tray's number in its session, from 1.
	Tray    int64
	Session int64
	Video   string
	// Stone is the tray stone's id ("" for arrived and leaving).
	Stone string
	// WorldID is the world id the stone stands for now (nil when none).
	WorldID *int64
	// Certain says, for removed and added, that the fact is certain; an unclear one is reported as such, never as a
	// fact (nil for the other kinds).
	Certain *bool
	// Why is the reason of unsure, identified, removed and added ("" for the others), and Code its code ("pushed",
	// "look_found", SpeckleFound, "carried_away", ...).
	Why  string
	Code string
	// G, T and Wall are the frame of the fact (the leaving facts carry the leaving point).
	G    int64
	T    float64
	Wall *float64
	// Stones are the tray stones at the arrival (arrived), or on the tray (leaving); Sure, Added, Removed (its
	// certain facts) and Unclear (its unclear facts) are counts of leaving.
	Stones, Sure, Added, Removed, Unclear int64
	TrayStones                            []string
	UnderSheet                            int64
	// Opened is where the question an unsure fact ends opened (nil when it ends none).
	Opened *TrayPoint
	// FromWorld (the world id the stone stood for before, nil when it was lost), Score, Margin (nil without a scored
	// rival) and Bar, in permille, are set for identified (by its look; or by the leaving's speckle answer: its clean
	// find's score, the margin over its second, and the bar SpeckleClean).
	FromWorld *int64
	Score     *int64
	Margin    *int64
	Bar       *int64
	// TID is the tray's bar, in permille, when it took a look, and Open the open account (leaving only).
	TID  *int64
	Open []TrayOpen
}

// Clock is the fact's media time as M:SS.d.
func (f TrayFact) Clock() string { return FormatClock(f.T) }

// Describe is the fact as one line: "0:45.5 tray 1: stone 20261122T023847.0Z-1-117 was removed".
func (f TrayFact) Describe() string {
	head := FormatClock(f.T) + " tray " + itoa(f.Tray)
	certain := f.Certain != nil && *f.Certain
	switch f.Kind {
	case TrayArrived:
		return head + " arrived: " + itoa(f.Stones) + " stones"
	case TraySheet:
		action := "lifted off"
		if f.Code == "sheet_laid" {
			action = "laid over"
		}
		return head + ": a sheet was " + action + " " + stoneNames(f.TrayStones)
	case TrayLeaving:
		extra := ""
		if f.UnderSheet > 0 {
			extra = ", " + itoa(f.UnderSheet) + " under a sheet"
		}
		return head + " leaving: " + itoa(f.Stones) + " stones (" + itoa(f.Sure) + " sure), " + itoa(f.Added) +
			" added, " + itoa(f.Removed) + " removed, " + itoa(f.Unclear) + " unclear" + extra
	case TrayUnsure:
		if f.Opened != nil {
			return head + ": stone " + f.Stone + " is not sure any more (" + f.Why + "; open since " +
				FormatClock(f.Opened.T) + ")"
		}
		return head + ": stone " + f.Stone + " is not sure any more (" + f.Why + ")"
	case TrayIdentified:
		how, by, score := "recognised", " by its look", "-"
		switch f.Code {
		case "look_found":
			how = "found again"
		case SpeckleSame:
			how, by = "confirmed", " by its speckle"
		case SpeckleMoved:
			by = " by its speckle"
		case SpeckleFound:
			how, by = "found again", " by its speckle"
		}
		if f.Score != nil {
			score = milli(*f.Score)
		}
		return head + ": stone " + f.Stone + " " + how + by + " on world stone " + idText(f.WorldID) + " (" + score +
			")"
	case TrayRemoved:
		if !certain {
			return head + ": stone " + f.Stone + " not found (unclear)"
		}
		if f.Why == "carried away" {
			return head + ": stone " + f.Stone + " was removed (carried away)"
		}
		return head + ": stone " + f.Stone + " was removed"
	}
	if certain {
		return head + ": stone " + f.Stone + " was set down"
	}
	return head + ": stone " + f.Stone + " not recognised (unclear)"
}

// String is Describe.
func (f TrayFact) String() string { return f.Describe() }

// DescribeOpenTxt is the golden open.txt form (section 4.6.9 of the SDK design): every leaving fact's Describe line
// (with " (bar 0.xxx)" when the tray took a look), then its open entries indented two spaces, "\n" after every line.
func DescribeOpenTxt(facts []TrayFact) string {
	var b strings.Builder
	for _, f := range facts {
		if f.Kind != TrayLeaving {
			continue
		}
		b.WriteString(f.Describe())
		if f.TID != nil {
			b.WriteString(" (bar " + milli(*f.TID) + ")")
		}
		b.WriteString("\n")
		for _, o := range f.Open {
			b.WriteString("  " + o.Describe() + "\n")
		}
	}
	return b.String()
}

// milli is an integer in permille with three decimals, from integers only: 853 is "0.853", 1000 is "1.000", -5 is
// "-0.005".
func milli(p int64) string {
	if p < 0 {
		return "-" + milli(-p)
	}
	return strconv.FormatInt(p/1000, 10) + "." + fmt.Sprintf("%03d", p%1000)
}

// Tray is a tray: Arrived (it has arrived and settled; nil until then) and Leaving (just before it moved out; nil
// while it is on, and for a tray that never arrived), its stones in tray order and every fact about it. Open is the
// open account of its leaving (nil until it has left) and TID its bar by look now (permille; nil while it has taken
// no look).
type Tray struct {
	Session int64
	Video   string
	// N is the tray's number in its session, from 1.
	N       int64
	Arrived *TrayPoint
	Leaving *TrayPoint
	Stones  []TrayStone
	Facts   []TrayFact
	Open    []TrayOpen
	TID     *int64
}

// Window is the media span from the arrival to the leaving point, the span to replay or to show for this tray; ok is
// false until it has left.
func (t *Tray) Window() (arrived, leaving float64, ok bool) {
	if t.Arrived == nil || t.Leaving == nil {
		return 0, 0, false
	}
	return t.Arrived.T, t.Leaving.T, true
}

// Stone is the tray stone with that id, or nil.
func (t *Tray) Stone(id string) *TrayStone {
	for i := range t.Stones {
		if t.Stones[i].ID == id {
			return &t.Stones[i]
		}
	}
	return nil
}

// Check is the self-check of a tray that has left (section 4.6.10 of the SDK design): the codes ("C1" ... "C9") it
// finds broken in the tray's facts and final tray stones, sorted; empty when the account is consistent. A tray that
// never arrived passes; one that has not left yet breaks C1 (it has no leaving fact).
func (t *Tray) Check() []string { return checkTray(t) }

// TrayCounts are the trays counts of the summary: trays opened and arrived, tray stones at the arrivals, certain and
// unclear facts, and tray stones that stopped being sure.
type TrayCounts struct {
	Sheets         int64 `json:"sheets,omitempty"`
	UnderSheet     int64 `json:"under_sheet,omitempty"`
	Total          int64 `json:"total"`
	Arrived        int64 `json:"arrived"`
	Stones         int64 `json:"stones"`
	Added          int64 `json:"added"`
	AddedUnclear   int64 `json:"added_unclear"`
	Removed        int64 `json:"removed"`
	RemovedUnclear int64 `json:"removed_unclear"`
	Unsure         int64 `json:"unsure"`
}

// ---- the self-check (section 4.6.10) ---------------------------------------------------------------------------

func isTrue(b *bool) bool { return b != nil && *b }

func sameBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// candKey is a candidate's (kind, world id, stone): no two candidates of an entry share one.
type candKey struct {
	kind    string
	hasID   bool
	worldID int64
	stone   string
}

func candKeyOf(c TrayCandidate) candKey {
	k := candKey{kind: c.Kind, stone: c.Stone}
	if c.WorldID != nil {
		k.hasID, k.worldID = true, *c.WorldID
	}
	return k
}

// bySpeckle says a fact is an identified fact of the leaving's speckle answer (section 4.6.13).
func bySpeckle(x TrayFact) bool {
	return x.Kind == TrayIdentified && (x.Code == SpeckleSame || x.Code == SpeckleMoved || x.Code == SpeckleFound)
}

func checkTray(t *Tray) []string {
	bad := map[string]bool{}
	facts := t.Facts
	if t.Arrived == nil && len(facts) == 0 && len(t.Stones) == 0 {
		return []string{}
	}
	if len(facts) == 0 || facts[0].Kind != TrayArrived || facts[len(facts)-1].Kind != TrayLeaving {
		bad["C1"] = true
	}
	for i := 0; i+1 < len(facts); i++ {
		if facts[i].Kind == TrayLeaving {
			bad["C1"] = true
		}
	}
	byID := map[string]*TrayStone{}
	for i := range t.Stones {
		byID[t.Stones[i].ID] = &t.Stones[i]
	}
	if len(byID) != len(t.Stones) {
		bad["C2"] = true
	}
	life := map[string][]TrayFact{}
	for _, x := range facts {
		if x.Stone == "" {
			continue
		}
		if byID[x.Stone] == nil {
			bad["C2"] = true
		}
		life[x.Stone] = append(life[x.Stone], x)
	}
	for _, xs := range life {
		kinds := make([]string, len(xs))
		for i, x := range xs {
			kinds[i] = x.Kind
		}
		count := func(k string) (n int) {
			for _, x := range kinds {
				if x == k {
					n++
				}
			}
			return
		}
		if slices.Contains(kinds[1:], TrayAdded) || count(TrayUnsure) > 1 || count(TrayRemoved) > 1 {
			bad["C3"] = true
		}
		for a, x := range xs {
			// after unsure only a removal or an identified fact of the leaving's speckle; after removed nothing; an
			// identified fact never after removed, one by look never after unsure; one of the speckle at most
			after, speckle := xs[a+1:], bySpeckle(x)
			if x.Kind == TrayUnsure && slices.ContainsFunc(after, func(y TrayFact) bool {
				return y.Kind != TrayRemoved && !bySpeckle(y)
			}) {
				bad["C3"] = true
			}
			if x.Kind == TrayRemoved && len(after) > 0 {
				bad["C3"] = true
			}
			if x.Kind == TrayIdentified && (slices.Contains(kinds[:a], TrayRemoved) ||
				(!speckle && slices.Contains(kinds[:a], TrayUnsure))) {
				bad["C3"] = true
			}
			if speckle && slices.ContainsFunc(xs[:a], bySpeckle) {
				bad["C3"] = true
			}
		}
	}
	var on []*TrayStone
	for i := range t.Stones {
		s := &t.Stones[i]
		xs := life[s.ID]
		var rem []TrayFact
		unsure := false // an unsure fact after its identified fact of the leaving's speckle, if any
		for _, x := range xs {
			if x.Kind == TrayRemoved {
				rem = append(rem, x)
			}
			unsure = !bySpeckle(x) && (unsure || x.Kind == TrayUnsure)
		}
		if (s.Status == TrayStoneRemoved) != (len(rem) > 0) || (len(rem) > 0 && !sameBool(rem[0].Certain, s.Certain)) {
			bad["C4"] = true
		}
		if s.Status == TrayStoneOn && s.Sure != (!unsure && !(s.Added && !isTrue(s.Certain))) {
			bad["C4"] = true
		}
		if s.Question != nil {
			bad["C4"] = true
		}
		if s.Status == TrayStoneOn {
			on = append(on, s)
		}
	}
	if len(facts) > 0 && facts[0].Kind == TrayArrived {
		var n int64
		for _, s := range t.Stones {
			if !s.Added {
				n++
			}
		}
		if facts[0].Stones != n {
			bad["C5"] = true
		}
	}
	if len(facts) > 0 && facts[len(facts)-1].Kind == TrayLeaving {
		checkLeaving(t, facts, on, byID, bad)
	}
	lifted := checkSheets(t, byID, bad)
	worlds := map[int64]bool{}
	for _, s := range on {
		if s.WorldID == nil {
			if s.Sure || s.UnderSheet || !lifted[s.ID] {
				bad["C6"] = true
			}
			continue
		}
		if worlds[*s.WorldID] {
			bad["C6"] = true
			continue
		}
		worlds[*s.WorldID] = true
	}
	for _, x := range facts { // by its look: score >= bar >= tIDMin, margin >= mID; by the speckle: bar SpeckleClean
		cleared := x.Score != nil && x.Bar != nil && *x.Score >= *x.Bar && x.Certain == nil
		if bySpeckle(x) && !(cleared && *x.Bar == SpeckleClean) {
			bad["C9"] = true
		}
		if x.Kind == TrayIdentified && !bySpeckle(x) &&
			!(cleared && *x.Bar >= tIDMin && (x.Margin == nil || *x.Margin >= mID)) {
			bad["C9"] = true
		}
	}
	out := make([]string, 0, len(bad))
	for code := range bad {
		out = append(out, code)
	}
	slices.Sort(out)
	return out
}

// checkLeaving checks the leaving fact: its counts and open list (C5), its entries (C7) and their candidates (C8).
func checkLeaving(t *Tray, facts []TrayFact, on []*TrayStone, byID map[string]*TrayStone, bad map[string]bool) {
	leave := facts[len(facts)-1]
	var sure, added, removed, unclear int64
	for _, s := range on {
		if s.Sure {
			sure++
		}
	}
	for _, x := range facts {
		switch {
		case x.Kind != TrayRemoved && x.Kind != TrayAdded:
		case !isTrue(x.Certain):
			unclear++
		case x.Kind == TrayAdded:
			added++
		default:
			removed++
		}
	}
	if int64(len(on)) != leave.Stones || sure != leave.Sure || added != leave.Added || removed != leave.Removed ||
		unclear != leave.Unclear {
		bad["C5"] = true
	}
	// one entry per open stone, in tray order
	var open, want []string
	for _, o := range leave.Open {
		open = append(open, o.Stone)
	}
	for _, s := range t.Stones {
		if !((s.Status == TrayStoneOn && s.Sure) || (s.Status == TrayStoneRemoved && isTrue(s.Certain))) {
			want = append(want, s.ID)
		}
	}
	if !slices.Equal(open, want) {
		bad["C5"] = true
	}
	sureWorld, sureNone := map[int64]bool{}, false
	for _, s := range on {
		if s.Sure && s.WorldID == nil {
			sureNone = true
		} else if s.Sure {
			sureWorld[*s.WorldID] = true
		}
	}
	for _, o := range leave.Open {
		c := o.Candidates
		keys := map[candKey]bool{}
		for _, x := range c {
			keys[candKeyOf(x)] = true
		}
		codes := trayAddedCauses
		switch o.Kind {
		case TrayUnsure:
			codes = trayUnsureCodes
		case TrayRemoved:
			codes = trayRemovedCauses
		}
		if !slices.Contains(codes, o.Cause) || !slices.Contains(trayLookCodes[o.Kind], o.Look) || len(c) == 0 ||
			len(keys) != len(c) {
			bad["C7"] = true
		}
		if len(c) == 0 { // the checks below read the first and last candidate; C8 has nothing to check
			continue
		}
		firstKind := "world"
		if o.WorldID == nil {
			firstKind = "unseen"
		}
		if o.Kind == TrayUnsure && (c[0].Kind != firstKind || !sameID(c[0].WorldID, o.WorldID)) {
			bad["C7"] = true
		}
		if o.Kind != TrayAdded && !slices.ContainsFunc(c, func(x TrayCandidate) bool { return x.Kind == "gone" }) {
			bad["C7"] = true
		}
		if (o.Kind == TrayRemoved && c[len(c)-1].Kind != "unseen") || (o.Kind == TrayAdded && c[len(c)-1].Kind != "new") {
			bad["C7"] = true
		}
		for _, x := range c {
			var z *TrayStone
			if x.Kind == "stone" && x.Stone != "" {
				z = byID[x.Stone]
			}
			settled := z != nil && ((z.Status == TrayStoneOn && z.Sure) || (z.Status == TrayStoneRemoved && isTrue(z.Certain)))
			taken := x.Kind == "world" && ((x.WorldID == nil && sureNone) || (x.WorldID != nil && sureWorld[*x.WorldID]))
			if settled || taken {
				bad["C8"] = true
			}
		}
	}
}

func sameID(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// ---- geometry --------------------------------------------------------------------------------------------------

type vec2 [2]float64

func dist(a, b vec2) float64 {
	dx, dy := a[0]-b[0], a[1]-b[1]
	return math.Sqrt(float64(dx*dx) + float64(dy*dy))
}

// usableStone is a world stone the tray rules can use: a solid with at least 2 numbers in pos and dims and 4 in q.
func usableStone(ws *StoneView) bool {
	s := ws.Solid
	return s != nil && len(s.Pos) >= 2 && len(s.Dims) >= 2 && len(s.Q) >= 4
}

func sizeOf(ws *StoneView) vec2 {
	d := ws.Solid.Dims
	return vec2{math.Max(d[0], d[1]), math.Min(d[0], d[1])}
}

func placeOf(ws *StoneView) vec2 { return vec2{ws.Solid.Pos[0], ws.Solid.Pos[1]} }

func compatible(a, b vec2) bool {
	return math.Abs(a[0]-b[0]) <= sizeTol*math.Max(a[0], b[0]) && math.Abs(a[1]-b[1]) <= sizeTol*math.Max(a[1], b[1])
}

// turnOf is the cosine and sine of the stone's turn about the vertical, from its quaternion [x, y, z, w], without
// trigonometry.
func turnOf(ws *StoneView) (float64, float64) {
	q := ws.Solid.Q
	z, w := q[2], q[3]
	n := float64(w*w) + float64(z*z)
	if n == 0 {
		return 1, 0
	}
	return (float64(w*w) - float64(z*z)) / n, float64(2*z*w) / n
}

// covers says the stone's box, grown by m, covers the point p.
func covers(ws *StoneView, p vec2, m float64) bool {
	c, s := turnOf(ws)
	pos, d := ws.Solid.Pos, ws.Solid.Dims
	dx, dy := p[0]-pos[0], p[1]-pos[1]
	return math.Abs(float64(c*dx)+float64(s*dy)) <= d[0]/2+m && math.Abs(float64(-s*dx)+float64(c*dy)) <= d[1]/2+m
}

func insideBoard(b *Rect, p vec2) bool {
	if b == nil {
		return true
	}
	c, s := math.Cos(b.Theta), math.Sin(b.Theta)
	dx, dy := p[0]-b.CX, p[1]-b.CY
	return math.Abs(float64(c*dx)+float64(s*dy)) <= float64(b.HW*1.05)+0.02 &&
		math.Abs(float64(-s*dx)+float64(c*dy)) <= float64(b.HH*1.05)+0.02
}

// overlapShare is the largest share of this world stone's box (5 x 5 points) that one other world stone's box covers.
func overlapShare(ws *StoneView, world []*StoneView) float64 {
	c, s := turnOf(ws)
	d := ws.Solid.Dims
	x0, y0 := ws.Solid.Pos[0], ws.Solid.Pos[1]
	var pts [25]vec2
	for i := 0; i < 5; i++ {
		for j := 0; j < 5; j++ {
			u := float64((float64(i)+0.5)/5*d[0]) - d[0]/2
			v := float64((float64(j)+0.5)/5*d[1]) - d[1]/2
			pts[i*5+j] = vec2{x0 + float64(c*u) - float64(s*v), y0 + float64(s*u) + float64(c*v)}
		}
	}
	reach := math.Max(d[0], d[1])
	best := 0
	for _, o := range world {
		if o.ID == ws.ID || o.UnderSheet {
			continue
		}
		od := o.Solid.Dims
		if dist(placeOf(o), vec2{x0, y0}) > (reach+math.Max(od[0], od[1]))/2 {
			continue
		}
		n := 0
		for _, p := range pts {
			if covers(o, p, 0) {
				n++
			}
		}
		best = max(best, n)
	}
	return float64(best) / 25
}

// civilFromDays is the proleptic Gregorian date of a day count since 1970-01-01.
func civilFromDays(z int64) (int64, int64, int64) {
	z += 719468
	era := floorDiv(z, 146097)
	doe := z - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	m := mp + 3
	if mp >= 10 {
		m = mp - 9
	}
	y := yoe + era*400
	if m <= 2 {
		y++
	}
	return y, m, d
}

// trayStamp is when a stone was first seen: the UTC wall time to the tenth ("20261122T023847.0Z"), else the session
// and the media seconds ("s1795314878t22.5": world ids start again in a new session, and a seek can show the same
// time again).
func trayStamp(wall *float64, t float64, session int64) string {
	if wall == nil {
		return "s" + strconv.FormatInt(session, 10) + "t" + FormatFixed(t, 1)
	}
	n := int64(math.Floor(float64(*wall*10) + 0.5))
	secs, tenth := floorDiv(n, 10), floorMod(n, 10)
	y, m, d := civilFromDays(floorDiv(secs, 86400))
	r := floorMod(secs, 86400)
	return fmt.Sprintf("%04d%02d%02dT%02d%02d%02d.%dZ", y, m, d, r/3600, r%3600/60, r%60, tenth)
}

// ---- the rules -------------------------------------------------------------------------------------------------

// trayWorld is a frame's world as the rules see it: the stones with a usable solid. hole: frames the rules could not
// read came before this one (section 4.6.1, presence questions).
type trayWorld struct {
	w      *WorldLayer
	ref    TrayPoint
	stones []*StoneView
	byID   map[int64]*StoneView
	hole   bool
	// quietHands excludes hand work on the observation before a blink and on its return.
	quietHands bool
}

// newTrayWorld is the world as the tray rules read it: a stone without a usable solid is left out, and so is every
// later stone of a world id the frame already had.
func newTrayWorld(w *WorldLayer, ref TrayPoint) *trayWorld {
	tw := &trayWorld{w: w, ref: ref, byID: make(map[int64]*StoneView, len(w.Stones))}
	seen := make(map[int64]bool, len(w.Stones))
	for i := range w.Stones {
		s := &w.Stones[i]
		if usableStone(s) && !seen[s.ID] {
			tw.stones = append(tw.stones, s)
			tw.byID[s.ID] = s
		}
		seen[s.ID] = true
	}
	return tw
}

// trayUnid is a world stone no tray stone stands for (yet). freed: a look decision unlinked it; resized: a size
// change of a linked world stone made it (neither is ever added for certain).
type trayUnid struct {
	born    TrayPoint
	place   vec2
	size    vec2
	seen    int
	hasLast bool
	last    vec2
	freed   bool
	resized bool
	// Positive observations while this ID remained unidentified on consecutive frames.
	lastFrame int64
	positives int
	boundary  *boundarySight
}

// trayBlink keeps the last observation and the first missing frame. An unidentified stone waits here so
// it cannot take part in a match until the world shows it again.
type trayBlink struct {
	first      TrayPoint
	prior      *StoneView
	priorWorld *trayWorld
	unid       *trayUnid
}

// The returning stone must be strictly closer to its own prior place than to any compatible visible source.
func (b trayBlink) matchesReturn(stone *StoneView) bool {
	place, size := placeOf(stone), sizeOf(stone)
	d := dist(place, placeOf(b.prior))
	if d > renameD || !compatible(size, sizeOf(b.prior)) {
		return false
	}
	for _, other := range b.priorWorld.stones {
		if other.ID != stone.ID && !other.UnderSheet && other.Detected && !inHand(other) && compatible(size, sizeOf(other)) && dist(place, placeOf(other)) <= d {
			return false
		}
	}
	return true
}

// trayLook is a stored look of a world stone (section 4.6.3): the frame it came on, the stone's place and size there.
type trayLook struct {
	wid      int64
	cells    *lookCells
	g        int64
	t        float64
	place    vec2
	size     vec2
	taken    bool
	detected bool // the world stone was detected on the look's frame: only such a look is an anchor
}

// trayAgain is an unseen run's first sight (section 4.6.1, presence questions): where its stone was first detected
// after the run's first calm moment, whether a busy frame or a push frame came from its last sight up to then, and
// whether another world stone of its size stood within nearD of the place where it was last seen (crowd).
type trayAgain struct {
	place             vec2
	busy, push, crowd bool
}

// trayWatch is a stone's unseen run (section 4.6.1, presence questions): its world id, the place and the time it was
// last seen (seenT, s1), the time of the run's first calm moment (c1T), at which its world stone was not detected, and
// its first sight after that calm moment. spoilt: a busy or push frame later than c1 came while its world stone was
// not detected, or frames the rules could not read came.
type trayWatch struct {
	wid    int64
	place  vec2
	seenT  float64
	c1T    float64
	again  *trayAgain
	spoilt bool
}

// trayQuestion is a stone's question; watch, for a presence question (section 4.6.1), is the unseen run it was opened
// with, kept until the question is settled or ends: seeing the stone again settles it.
type trayQuestion struct {
	cause, why string
	since      int64
	opened     TrayPoint
	watch      *trayWatch
}

// lookPair is a pair of the look step: a world id (a row's pairs) or a tray index (a column's neighbours), the
// permille score and its pair count (ok false: no score).
type lookPair struct {
	key   int64
	sc, n int64
	ok    bool
}

type lookRow struct {
	index int
	since int64
}

type rowWorld struct {
	row int
	wid int64
}

// pairInfo is a pair's bar and best rival (top, when hasTop).
type pairInfo struct {
	bar    int64
	top    int64
	hasTop bool
}

type lookBest struct {
	row    int
	wid    int64
	score  int64
	margin *int64
	bar    int64
}

// lookRun is one run of the look step (section 4.6.5): its rows and columns, each row's pairs, each pair's bar and
// best rival, and the decided pair with the highest score.
type lookRun struct {
	rows  []lookRow
	cols  []int64
	pairs map[int][]lookPair
	info  map[rowWorld]pairInfo
	best  *lookBest
}

// trayScore is a memoised score of two looks.
type trayScore struct {
	sc, n int64
	ok    bool
}

type trayStone struct {
	id          string
	index       int
	linked      bool // world is the world id it stands for
	world       int64
	worldT      float64
	size        vec2
	frozen      bool
	place       vec2
	seenT       float64
	born        TrayPoint
	underSheet  bool
	sheetSince  *TrayPoint
	sheetLoss   bool
	status      string
	sure        bool // not given up: a stone in question keeps it (its view's Sure is false meanwhile)
	added       bool
	certain     *bool
	lostT       float64
	lostK       int
	carriedAway bool
	dropped     bool
	emptied     bool
	handled     bool
	pushed      bool
	ghost       int
	atK         int // the snapshot the stone was last on the tray at, and its place there
	at          vec2
	// by look (sections 4.6.3-4.6.8)
	chain        []*trayLook
	question     *trayQuestion
	doubted      bool
	byLook       bool
	lookLost     bool
	lostByLook   bool
	lifted       bool
	hasDisturbed bool // disturbed is the frame of its latest disturbance since the snapshot
	disturbed    int64
	lostG        int64
	late         bool
	box          bool
	unsureCode   string
	unsureFromT  float64
	unidRec      *trayUnid
	leftNow      bool
	addedNow     bool
	open         *TrayOpen
	freedPush    bool // a world stone a look unlinked was pushed next to it since the snapshot
	// the report's (section 4.7): its place when made, at the latest calm frame and at the leaving point (when it was
	// on the tray and seen in that frame), and the frame where something last touched it
	firstPlace vec2
	calmPlace  *vec2
	leftPlace  *vec2
	touched    *int64
	// presence questions (section 4.6.1): its current unseen run (it ends when the stone is seen at a calm moment or
	// lost); once: that run is on probation (it began at a calm moment that settled the stone's presence question
	// without showing it, so the calm moment that shows it again must settle that run too); handledT: the latest time
	// it was handled (a lift, carry, hinge or place on its link) or held, when hasHandledT
	run1        *trayWatch
	once        bool
	hasHandledT bool
	handledT    float64
	// certain removals (section 4.6.1): handT, the latest time a hand touched its link (a hand event, a push with a
	// side, or a delete with why hand or carried-away on its world id), when hasHandT; lostCalmT, the time of the last
	// calm moment before its latest loss
	hasHandT  bool
	handT     float64
	lostCalmT float64
	// the stone in the newest speckle answer read while the tray was on (section 4.6.12); for the leaving's answer
	// (4.6.13): the world id the stone was made with (its arrival world id for a stone the tray arrived with), whether
	// the answer linked or confirmed it, and what it found of it at the other moment (set as the tray leaves)
	speckle    *StoneSpeckle
	firstWorld int64
	bySpeckle  bool
	evidence   string
}

// watches are the stone's open unseen runs: its own (run1) and its presence question's, nil where there is none.
func (s *trayStone) watches() [2]*trayWatch {
	var w *trayWatch
	if s.question != nil {
		w = s.question.watch
	}
	return [2]*trayWatch{s.run1, w}
}

// handle notes time t as the latest at which the stone was handled or held (section 4.6.1).
func (s *trayStone) handle(t float64) { s.hasHandledT, s.handledT = true, t }

// disturb marks a disturbance of the stone in frame g (which also touches it).
func (s *trayStone) disturb(g int64) {
	s.hasDisturbed, s.disturbed = true, g
	s.touch(g)
}

// touch notes frame g as the latest where something touched the stone: a disturbance, or a link to another world
// stone.
func (s *trayStone) touch(g int64) { s.touched = Int64(g) }

func (s *trayStone) view() TrayStone {
	v := TrayStone{ID: s.id, Status: s.status, Sure: s.sure && s.question == nil, Added: s.added, Born: s.born,
		Place: s.place, Size: s.size, ByLook: s.byLook, LookLost: s.lookLost, Open: s.open, FirstPlace: s.firstPlace,
		Speckle: s.speckle, BySpeckle: s.bySpeckle, Evidence: s.evidence, UnderSheet: s.underSheet, SheetSince: s.sheetSince}
	if s.leftPlace != nil {
		p := [2]float64(*s.leftPlace)
		v.LeftPlace = &p
	}
	if s.touched != nil {
		v.Touched = Int64(*s.touched)
	}
	if s.linked {
		v.WorldID = Int64(s.world)
	}
	if s.certain != nil {
		v.Certain = Bool(*s.certain)
	}
	if q := s.question; q != nil {
		v.Question = &TrayQuestion{Cause: q.cause, Why: q.why, Since: q.since, Opened: q.opened}
	}
	for _, e := range s.chain {
		v.Looks = append(v.Looks, e.g)
	}
	return v
}

type trayMark struct {
	t float64
	p vec2
}

// openTray is the open tray's working state (dropped when it leaves; what is kept is its Tray).
type openTray struct {
	visit     trayVisit
	n         int64
	ids       map[int64]struct{}
	arrived   *TrayPoint
	leaving   *TrayPoint
	board     *Rect
	stones    []*trayStone
	link      map[int64]*trayStone
	unid      map[int64]*trayUnid
	blinks    map[int64]trayBlink
	missing   map[int64]int64 // first missing frame of each short inventory gap
	marks     []trayMark
	k         int
	settleT   float64
	calmRef   *TrayPoint
	calmWorld *trayWorld
	// leaveWorld is the leaving point's world (set as the tray leaves: its positive evidence at departure).
	leaveWorld *trayWorld
	worked     bool
	last       *trayWorld
	unclear    int64
	added      int64 // certain added facts: the leaving's count, also of a stone removed unclear later
	facts      []TrayFact
	// by look: the look store, the gauge, twins, a memo of scores, the leaving's last run, the open account
	store        map[int64]*trayLook
	hasAlike     bool
	alike        int64
	alikeN       int
	took         bool
	twins        map[string]bool
	memo         map[[4]int64]trayScore
	idsAtArrival map[int64]struct{}
	lastRun      *lookRun
	left         bool
	open         []TrayOpen
	relinked     bool    // links and trust may differ from v1's: no certain removal or addition after it (4.6.7)
	hasLastMark  bool    // lastMarkT is the time of the tray's latest hand mark
	lastMarkT    float64 // a look older than it settles no question
	// presence questions (section 4.6.1): the time of the latest busy frame (a hand not free or flagged untrusted, a
	// hand event, a held world stone, or a delete why hand or carried-away) and of the latest push frame (a push event,
	// any id, with a side or not), when hasBusy and hasPush (a frame after frames the rules could not read is both);
	// settleG is the frame G of the last snapshot
	hasBusy bool
	busyT   float64
	hasPush bool
	pushT   float64
	settleG int64
	// arrivalLinks are the links as the tray arrived (world id -> tray stone id), for the speckle answers (4.6.12);
	// answers the unit's answers read while the tray was on, oldest first, and leavingSpeckle the leaving's (4.6.13)
	arrivalLinks   map[int64]string
	answers        []*WorldSpeckle
	leavingSpeckle *WorldSpeckle
}

// trayLedger runs the rules of section 4.6 for one session: feed it the worlds of the session's frames in order (not
// revisions), tell it where frames it could not read came (skipped), close it when the session ends. Each tray that
// leaves goes to onClose as its Tray.
type trayLedger struct {
	bounds    *[4]float64
	visitSeen map[int64]bool
	session   int64
	video     string
	c         *TrayCounts
	onClose   func(Tray)
	n         int64
	tray      *openTray
	stop      []*trayWorld
	on        bool
	calm      int
	calmG     int64 // the first frame of the calm run
	quiet     int
	hole      bool // frames the rules could not read came before the next world fed (4.6.1)
	// lastClosed is the tray that left last: a speckle answer that comes after it left is for it (4.6.12)
	lastClosed *openTray
}

func newTrayLedger(c *TrayCounts, session int64, video string, onClose func(Tray)) *trayLedger {
	return &trayLedger{session: session, video: video, c: c, onClose: onClose}
}

// links are the open tray's links now: world id -> tray stone id (nil between trays).
func (l *trayLedger) links() map[int64]string {
	if l.tray == nil {
		return nil
	}
	out := make(map[int64]string, len(l.tray.link))
	for id, s := range l.tray.link {
		out[id] = s.id
	}
	return out
}

// current is the open tray (nil between trays).
func (l *trayLedger) current() *Tray {
	if l.tray == nil {
		return nil
	}
	t := l.view(l.tray)
	return &t
}

func (l *trayLedger) view(tr *openTray) Tray {
	t := Tray{Session: l.session, Video: l.video, N: tr.n, Arrived: tr.arrived, Leaving: tr.leaving,
		Stones: make([]TrayStone, len(tr.stones)), Facts: append([]TrayFact(nil), tr.facts...)}
	for i, s := range tr.stones {
		t.Stones[i] = s.view()
	}
	if tr.left {
		t.Open = append([]TrayOpen{}, tr.open...)
	}
	if tr.took {
		t.TID = Int64(tidOf(tr))
	}
	return t
}

// skipped says frames the rules could not read came here (a frame without a world, a G gap, a gap or reset side
// event, a revision): the next world fed counts as a busy and a push frame (section 4.6.1).
func (l *trayLedger) skipped() { l.hole = true }

// feed takes the world of the session's next frame and returns the tray facts it made.
func (l *trayLedger) feed(w *WorldLayer, ref TrayPoint) []TrayFact {
	var out []TrayFact
	hole := l.hole
	l.hole = false
	tw := newTrayWorld(w, ref)
	tw.hole = hole
	if l.retireSheet(tw, &out) {
		return out
	}
	if w.Motion != "static" {
		l.stop, l.on = nil, false
		return out
	}
	if l.on {
		l.frame(tw, &out)
		return out
	}
	l.stop = append(l.stop, tw)
	if l.stop[len(l.stop)-1].ref.T-l.stop[0].ref.T < trayStopMin {
		return out
	}
	l.begin(&out)
	for _, s := range l.stop {
		l.frame(s, &out)
	}
	l.stop = nil
	return out
}

// close makes the open tray leave (the session ended, or the replay or stream).
func (l *trayLedger) close() []TrayFact {
	var out []TrayFact
	l.closeTray(&out)
	l.stop, l.on = nil, false
	return out
}

func (l *trayLedger) fact(kind string, tr *openTray, s *trayStone, ref TrayPoint, f TrayFact, out *[]TrayFact) {
	f.Kind, f.Tray, f.Session, f.Video = kind, tr.n, l.session, l.video
	f.G, f.T, f.Wall = ref.G, ref.T, ref.Wall
	if s != nil {
		f.Stone = s.id
		if s.linked {
			f.WorldID = Int64(s.world)
		}
	}
	tr.facts = append(tr.facts, f)
	*out = append(*out, f)
}

func (tw *trayWorld) observedPlace(s *trayStone) *vec2 {
	ws := tw.byID[s.world]
	if !s.linked || s.status != TrayStoneOn || ws == nil || ws.UnderSheet || !ws.Detected || inHand(ws) {
		return nil
	}
	p := placeOf(ws)
	return &p
}

func inHand(ws *StoneView) bool { return ws.Mode == "carry" || ws.Mode == "hinge" }

// handFree says a hand is free: free, or no state at all.
func handFree(h *HandState) bool { return h == nil || h.State == "" || h.State == "free" }

// handUntrusted says a hand is flagged untrusted (its frame is busy, section 4.6.1).
func handUntrusted(h *HandState) bool { return h != nil && h.Untrusted != nil && *h.Untrusted }

// handTouch says a hand touched the event's stone: a hand event, a push with a side, or a delete with why hand or
// carried-away (section 4.6.1, certain removals).
func handTouch(e *WorldEvent) bool {
	return handEvent(e) || (e.Type == EventDelete && (e.Why == "hand" || e.Why == "carried-away"))
}

func handEvent(e *WorldEvent) bool {
	switch e.Type {
	case EventLift, EventHinge, EventCarry, EventPlace, EventDrop, EventGone:
		return true
	}
	return e.Type == EventPush && e.Side != ""
}

// lookClearer is an event after which a world stone's stored look no longer describes it.
func lookClearer(typ string) bool {
	switch typ {
	case EventPush, EventLift, EventHinge, EventCarry, EventPlace, EventDrop, EventGone, EventDelete, EventSplit:
		return true
	}
	return false
}

func (l *trayLedger) begin(out *[]TrayFact) {
	l.visitSeen = make(map[int64]bool)
	ids := map[int64]struct{}{}
	unheld := map[int64]struct{}{}
	for _, s := range l.stop[0].stones {
		if s.UnderSheet || inHand(s) {
			continue
		}
		unheld[s.ID] = struct{}{}
		if s.Detected {
			ids[s.ID] = struct{}{}
		}
	}
	seen, seenAll := 0, 0
	if tr := l.tray; tr != nil {
		for id := range unheld {
			if _, ok := tr.ids[id]; ok {
				seenAll++
				if _, detected := ids[id]; detected {
					seen++
				}
			}
		}
	}
	// a visit continues the open tray only on the camera's word (section 4.6.1): at least one of its first frame's
	// detected stones was seen on the tray, and nine tenths of those detected stones were, or nine tenths of all its
	// stones that no hand holds (a jolt can leave most of a board's stones only remembered and rename one of the few the
	// camera sees). A new board under the camera may carry many of the last board's world ids, and a board that drove
	// off may leave its stones remembered (not detected) where it stood.
	if !(seen > 0 && (10*seen >= 9*len(ids) || 10*seenAll >= 9*len(unheld))) {
		l.closeTray(out)
		l.n++
		l.c.Total++
		l.tray = &openTray{n: l.n, ids: map[int64]struct{}{}, link: map[int64]*trayStone{},
			unid: map[int64]*trayUnid{}, k: -1, store: map[int64]*trayLook{}, twins: map[string]bool{},
			memo: map[[4]int64]trayScore{}, idsAtArrival: map[int64]struct{}{}}
	} else if l.tray.arrived != nil {
		// the tray goes on after moving frames the rules never read: the stop's first frame counts as a busy and a
		// push frame, so no unseen run from before them is settled by a sight after them (4.6.1)
		unread(l.tray, l.stop[0].ref.T)
		l.tray.finishBlinks()
		l.tray.last = nil
	}
	l.on = true
	clear(l.tray.missing)
	l.tray.visit = trayVisit{startG: l.stop[0].ref.G}
	l.calm, l.quiet = 0, 0
}

func sortedKeys[V any](m map[int64]V) []int64 {
	keys := make([]int64, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (tr *openTray) finishBlinks() {
	for id, b := range tr.blinks {
		tr.loseBlink(id, b)
	}
	clear(tr.blinks)
}

// incompleteInventory withholds a quiet world with recent omissions from arrival and calm snapshots.
// All usable IDs count here, even undetected or unlinked ones. This timing rule does not restore links.
func (tr *openTray) incompleteInventory(tw *trayWorld) bool {
	if !tw.quietHands || len(tw.w.Events) != 0 || tw.hole || tr.last == nil || tr.last.ref.G+1 != tw.ref.G {
		clear(tr.missing)
		return false
	}
	for id, first := range tr.missing {
		if tw.byID[id] != nil || tw.ref.G-first >= blinkFrames {
			delete(tr.missing, id)
		}
	}
	for id, prev := range tr.last.byID {
		if tw.byID[id] == nil && !prev.UnderSheet {
			if tr.missing == nil {
				tr.missing = make(map[int64]int64)
			}
			tr.missing[id] = tw.ref.G
		}
	}
	return len(tr.missing) != 0
}

// gainedBlinkNeighbour reports whether another compatible stone entered the neighbourhood of the last sight.
// A stable neighbour is allowed. A new one could stand for the stone whose observation is missing.
func gainedBlinkNeighbour(anchor *StoneView, previous, current *trayWorld) bool {
	size, place := sizeOf(anchor), placeOf(anchor)
	near := func(s *StoneView) bool {
		return s != nil && !s.UnderSheet && s.Detected && !inHand(s) && compatible(sizeOf(s), size) && dist(placeOf(s), place) <= renameD
	}
	for _, s := range current.stones {
		if s.ID != anchor.ID && near(s) && !near(previous.byID[s.ID]) {
			return true
		}
	}
	return false
}

// resolveBlinks runs before this frame changes links. Only an uninterrupted run of quiet, event-free missing
// frames can preserve an observation. A return must show a compatible stone at the previous place.
func (tr *openTray) resolveBlinks(tw *trayWorld) bool {
	contiguous := tr.last != nil && tr.last.ref.G+1 == tw.ref.G && !tw.hole
	canMiss := contiguous && tw.quietHands && len(tw.w.Events) == 0
	for id, b := range tr.blinks {
		ws := tw.byID[id]
		// Compare with the immediately preceding frame: a neighbour that left and came back is a new contender.
		uncontested := contiguous && !gainedBlinkNeighbour(b.prior, tr.last, tw)
		if ws == nil && canMiss && uncontested && tw.ref.G-b.first.G < blinkFrames {
			continue
		}
		delete(tr.blinks, id)
		if ws != nil && ws.Detected && uncontested && tw.quietHands && b.matchesReturn(ws) &&
			!slices.ContainsFunc(tw.w.Events, func(e WorldEvent) bool {
				return e.ID != nil && *e.ID == id && e.Type != EventConfirm && e.Type != EventFreeze
			}) {
			if b.unid != nil {
				tr.unid[id] = b.unid
			}
			continue
		}
		// Step 3 applies an explicit ending with its own frame and cause.
		if !slices.ContainsFunc(tw.w.Events, func(e WorldEvent) bool {
			return e.ID != nil && *e.ID == id && (e.Type == EventGone || e.Type == EventDrop || e.Type == EventDelete)
		}) {
			tr.loseBlink(id, b)
		}
	}
	if canMiss && tr.last.quietHands {
		for id, ws := range tr.last.byID {
			u := tr.unid[id]
			if tw.byID[id] != nil || ws.UnderSheet || !ws.Detected || inHand(ws) || (tr.link[id] == nil && u == nil) ||
				gainedBlinkNeighbour(ws, tr.last, tw) {
				continue
			}
			if tr.blinks == nil {
				tr.blinks = make(map[int64]trayBlink)
			}
			tr.blinks[id] = trayBlink{first: tw.ref, prior: ws, priorWorld: tr.last, unid: u}
			delete(tr.unid, id)
		}
	}
	return len(tr.blinks) != 0
}

func (tr *openTray) loseBlink(id int64, b trayBlink) {
	if s := tr.link[id]; s != nil {
		delete(tr.link, id)
		lose(tr, s, b.first)
	}
}

func (l *trayLedger) frame(tw *trayWorld, out *[]TrayFact) {
	tr, w, ref := l.tray, tw.w, tw.ref
	defer tr.observeBoundary(tr.last, tw)
	t := ref.T
	for _, ws := range tw.stones {
		if !ws.UnderSheet {
			tr.ids[ws.ID] = struct{}{}
			if ws.Detected {
				l.visitSeen[ws.ID] = true
			}
		}
	}
	storeLooks(tr, tw)
	hand, change, isNew, took, pushed := false, false, false, false, false
	for i := range w.Events {
		e := &w.Events[i]
		hand = hand || handEvent(e)
		change = change || e.Type == EventNew || e.Type == EventDelete || e.Type == EventSplit
		isNew = isNew || e.Type == EventNew
		took = took || (e.Type == EventDelete && (e.Why == "hand" || e.Why == "carried-away"))
		pushed = pushed || e.Type == EventPush
	}
	if change && l.onlyBackgroundRetired(tr, tw) {
		change = false
	}
	if isNew {
		l.quiet = 0
	} else {
		l.quiet++
	}
	free := handFree(w.Hands.Left) && handFree(w.Hands.Right)
	untrusted := handUntrusted(w.Hands.Left) || handUntrusted(w.Hands.Right)
	held := slices.ContainsFunc(tw.stones, inHand)
	busy := !free || untrusted || held || hand || took
	tw.quietHands = !busy && !pushed
	incomplete := tr.incompleteInventory(tw)
	if tr.arrived != nil && tr.resolveBlinks(tw) {
		incomplete = true
	}
	tr.visit.frame(tw.quietHands && !change && !tw.hole, incomplete, tr.calmRef)
	tr.last = tw
	if !incomplete {
		if free && !hand && !change {
			l.calm++
		} else {
			l.calm = 0
		}
		if l.calm == 1 {
			l.calmG = ref.G
		}
	}
	if busy {
		// a busy frame (4.6.1, presence questions): a hand at work, an untrusted hand, a held world stone, or a stone
		// a hand took away
		tr.hasBusy, tr.busyT = true, t
	}
	if pushed { // a push frame
		tr.hasPush, tr.pushT = true, t
	}
	if tw.hole { // frames the rules could not read came before this one (4.6.1)
		unread(tr, t)
	}
	if tr.arrived == nil {
		if !incomplete && l.calm >= trayCalm && l.quiet >= trayQuiet && sameCount(w.Stats.Confirmed, w.Stats.Stones) {
			// looks read before the calm run that ends here may show another stone: hands worked since
			for id, e := range tr.store {
				if e.g < l.calmG {
					delete(tr.store, id)
				}
			}
			l.arrive(tr, tw, out)
			l.sheetLaid(tr, tr.stones, ref, out)
			tr.arrivalLinks = l.links() // the links as it arrived, for the speckle answers (4.6.12)
			tr.idsAtArrival = maps.Clone(tr.ids)
			l.lookWork(tr, tw, ref, out)
		}
		return
	}
	l.observeSheets(tr, tw, !incomplete && l.calm >= trayCalm, out)
	// 1. where hands worked
	for i := range w.Events {
		e := &w.Events[i]
		if !(handEvent(e) || (e.Type == EventDelete && e.Why == "hand")) || e.ID == nil {
			continue
		}
		if ws := tw.byID[*e.ID]; ws != nil {
			tr.marks = append(tr.marks, trayMark{t, placeOf(ws)})
		} else if s := tr.link[*e.ID]; s != nil {
			tr.marks = append(tr.marks, trayMark{t, s.place})
		} else {
			continue
		}
		tr.hasLastMark, tr.lastMarkT = true, t
	}
	// 2. the leaving point
	if hand {
		tr.worked = true
	}
	calmNow := !incomplete && l.calm >= trayCalm
	if calmNow {
		r := ref
		tr.calmRef, tr.calmWorld, tr.worked = &r, tr.last, false
	}
	// 3. the world's events on linked stones (and the disturbances and lifts the look rules read)
	var ends, late []int64
	for i := range w.Events {
		e := &w.Events[i]
		var s *trayStone
		if e.ID != nil {
			s = tr.link[*e.ID]
		}
		if s == nil {
			if e.Type == EventPush && e.ID != nil {
				late = append(late, *e.ID)
				if u := tr.unid[*e.ID]; u != nil && u.freed {
					freedPush(tr, u, ref)
				}
			}
			continue
		}
		if handTouch(e) {
			s.hasHandT, s.handT = true, t
		}
		switch e.Type {
		case EventLift, EventCarry, EventHinge, EventPlace:
			s.handled, s.lifted = true, true
			s.disturb(ref.G)
			// the time it was handled (4.6.1); the event's hand mark (step 1) is where its world stone is now, which
			// may be away from both places watchOpen tests the marks against
			s.handle(t)
		case EventPush:
			push(tr, s, ref)
		case EventGone, EventDrop, EventDelete:
			if !containsID(ends, *e.ID) {
				ends = append(ends, *e.ID)
			}
		}
	}
	for _, id := range ends {
		s := tr.link[id]
		delete(tr.link, id)
		for i := range w.Events {
			if e := &w.Events[i]; e.ID != nil && *e.ID == id {
				s.carriedAway = s.carriedAway || e.Type == EventGone
				s.dropped = s.dropped || e.Type == EventDrop
				s.emptied = s.emptied || (e.Type == EventDelete && e.Why == "hand")
			}
		}
		lose(tr, s, ref)
	}
	// a busy frame or a push frame spoils the unseen run of a linked stone not detected on it (4.6.1)
	if (tr.hasBusy && tr.busyT == t) || (tr.hasPush && tr.pushT == t) {
		for _, id := range sortedKeys(tr.link) {
			if ws := tw.byID[id]; !tr.link[id].underSheet && ws != nil && !ws.Detected {
				for _, rec := range tr.link[id].watches() {
					if rec != nil && t > rec.seenT {
						rec.spoilt = true
					}
				}
			}
		}
	}
	// 4. linked stones in this frame's world
	for _, id := range sortedKeys(tr.link) {
		s, ws := tr.link[id], tw.byID[id]
		if s.underSheet {
			continue
		}
		if ws == nil {
			if _, pending := tr.blinks[id]; !pending {
				delete(tr.link, id)
				lose(tr, s, ref)
			}
			continue
		}
		if inHand(ws) {
			s.handled, s.lifted = true, true
			s.disturb(ref.G)
			s.handle(t)
			continue
		}
		if ws.Shape == "frozen" {
			if s.frozen && !compatible(sizeOf(ws), s.size) {
				delete(tr.link, id)
				l.doubt(tr, s, "its world stone changed size", "size_changed", ref, ref.G+1, out)
				lose(tr, s, ref)
				tr.unid[id] = &trayUnid{born: ref, place: placeOf(ws), size: sizeOf(ws), resized: true}
				continue
			}
			if !s.frozen {
				s.size, s.frozen = sizeOf(ws), true
			}
		}
		if ws.Detected {
			s.place, s.seenT = placeOf(ws), t
			for _, rec := range s.watches() { // the run's first sight (4.6.1)
				if rec != nil && rec.again == nil && t > rec.c1T && !rec.spoilt {
					rec.again = &trayAgain{place: s.place, busy: tr.hasBusy && tr.busyT >= rec.seenT,
						push: tr.hasPush && tr.pushT >= rec.seenT, crowd: crowded(s, rec, tw)}
				}
			}
		}
	}
	// 5. world stones no tray stone stands for
	for _, ws := range tw.stones {
		if tr.link[ws.ID] != nil || tr.unid[ws.ID] != nil || inHand(ws) || ws.UnderSheet {
			continue
		}
		if insideBoard(tr.board, placeOf(ws)) {
			tr.unid[ws.ID] = &trayUnid{born: ref, place: placeOf(ws), size: sizeOf(ws)}
		}
	}
	for _, id := range sortedKeys(tr.unid) {
		ws := tw.byID[id]
		if ws == nil || ws.UnderSheet {
			delete(tr.unid, id)
			continue
		}
		u := tr.unid[id]
		if ws.Detected {
			u.place = placeOf(ws)
		}
		u.size = sizeOf(ws)
	}
	// 6. renames, then a push on an id linked only now (lost at step 3, renamed at step 6) as step 3 reads it;
	// 7. the settle at a calm moment, the look work in a later calm frame
	l.renames(tr, tw, out)
	for i, id := range late {
		if s := tr.link[id]; s != nil && !slices.Contains(late[:i], id) {
			push(tr, s, ref)
		}
	}
	if incomplete {
		l.rememberUnseen(tr, tw, out)
		return
	}
	if l.calm == trayCalm {
		l.settle(tr, tw, ref, false, out)
	} else if l.calm > trayCalm {
		l.lookWork(tr, tw, ref, out)
	}
	tr.rememberVisitPoint(tw)
	if calmNow { // a calm frame: each stone's place in it, if it was seen in it (section 4.7)
		for _, s := range tr.stones {
			s.calmPlace = tw.observedPlace(s)
		}
	}
}

func sameCount(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func (l *trayLedger) arrive(tr *openTray, tw *trayWorld, out *[]TrayFact) {
	ref := tw.ref
	tr.arrived = &ref
	if tw.w.Draw != nil {
		tr.board = tw.w.Draw.Board
	}
	st := trayStamp(ref.Wall, ref.T, l.session)
	for _, ws := range tw.stones {
		if inHand(ws) || (ws.UnderSheet && !l.visitSeen[ws.ID]) || !insideBoard(tr.board, placeOf(ws)) {
			continue
		}
		s := newTrayStone(tr, st+"-"+itoa(tr.n)+"-"+itoa(ws.ID), ws, ref)
		s.underSheet = ws.UnderSheet
		s.calmPlace = tw.observedPlace(s)
		tr.stones = append(tr.stones, s)
		tr.link[ws.ID] = s
	}
	l.c.Arrived++
	l.c.Stones += int64(len(tr.stones))
	l.fact(TrayArrived, tr, nil, ref, TrayFact{Stones: int64(len(tr.stones))}, out)
	snapshot(tr, ref)
}

// push is a push on tray stone s's world stone: s is pushed, and every tray stone of a compatible size next to it in
// the last snapshot is disturbed.
func push(tr *openTray, s *trayStone, ref TrayPoint) {
	s.pushed = true
	s.disturb(ref.G)
	if s.atK == tr.k {
		for _, o := range tr.stones {
			if o != s && o.atK == tr.k && compatible(o.size, s.size) && dist(o.at, s.at) <= handR {
				o.disturb(ref.G)
			}
		}
	}
}

// freedPush is a push on a world stone a look unlinked (freed): every tray stone of a compatible size next to it in
// the last snapshot is doubted at the settle as a pushed stone's neighbour is (by looks only).
func freedPush(tr *openTray, u *trayUnid, ref TrayPoint) {
	for _, o := range tr.stones {
		if o.atK == tr.k && compatible(o.size, u.size) && dist(o.at, u.place) <= handR {
			o.freedPush = true
			o.disturb(ref.G)
		}
	}
}

// uniqueStoneID is the tray stone id id, or, when the tray already has a stone with it (a stone and its world id seen
// again within the same tenth of a second), the first of id-2, id-3, ... that the tray does not have (4.6).
func uniqueStoneID(tr *openTray, id string) string {
	taken := make(map[string]bool, len(tr.stones))
	for _, s := range tr.stones {
		taken[s.id] = true
	}
	uid := id
	for k := int64(2); taken[uid]; k++ {
		uid = id + "-" + itoa(k)
	}
	return uid
}

func newTrayStone(tr *openTray, id string, ws *StoneView, born TrayPoint) *trayStone {
	return &trayStone{id: uniqueStoneID(tr, id), index: len(tr.stones), linked: true, world: ws.ID, worldT: born.T,
		size: sizeOf(ws), frozen: ws.Shape == "frozen", place: placeOf(ws), seenT: born.T, born: born,
		status: TrayStoneOn, sure: true, atK: -1, firstPlace: placeOf(ws), firstWorld: ws.ID}
}

func snapshot(tr *openTray, ref TrayPoint) {
	tr.k++
	for _, s := range tr.stones {
		s.handled, s.pushed, s.hasDisturbed, s.freedPush = false, false, false, false
		if s.status == TrayStoneOn {
			s.atK, s.at = tr.k, s.place
		}
	}
	tr.settleT, tr.settleG = ref.T, ref.G
	r := ref
	tr.calmRef, tr.calmWorld, tr.worked = &r, tr.last, false
}

func lose(tr *openTray, s *trayStone, ref TrayPoint) {
	s.underSheet, s.sheetSince, s.sheetLoss = false, nil, false
	s.run1 = nil // a lost stone's unseen run ends (4.6.1): never spoilt while unlinked; on probation, it fails
	s.linked, s.status, s.lostT, s.lostK = false, TrayStoneLost, ref.T, tr.k
	s.lostG, s.lostByLook = ref.G, false
	s.lostCalmT = tr.settleT
}

// handTook says a hand touched the lost stone's link from its last calm moment (or renameT before it was last seen,
// if that is earlier) up to its loss: a stone does not leave a tray by itself (section 4.6.1, certain removals).
func handTook(s *trayStone) bool {
	return s.hasHandT && s.handT >= math.Min(s.lostCalmT, s.seenT-renameT)
}

// ---- doubts and questions (section 4.6.4) -------------------------------------------------------------------------

// trusted is a stone on the tray and sure, never in question or not sure on this tray, and not linked by a look:
// without looks exactly v1's "on the tray and sure".
func trusted(s *trayStone) bool { return s.status == TrayStoneOn && s.sure && !s.doubted && !s.byLook }

// doubt is where a reason of 4.6.1 holds: a question for a stone with a chain (not for a box doubt), else unsure. A
// later unseen doubt changes nothing in a presence question; a doubt of any other cause ends it.
func (l *trayLedger) doubt(tr *openTray, s *trayStone, why, cause string, ref TrayPoint, since int64, out *[]TrayFact) {
	l.doubtWith(tr, s, why, cause, ref, since, nil, out)
}

// doubtWith is doubt with the watch of an unseen doubt (a copy of the stone's unseen run, for a stone without a chain
// that nothing could have changed since it was last seen): such a doubt is a presence question (4.6.1, 4.6.4).
func (l *trayLedger) doubtWith(tr *openTray, s *trayStone, why, cause string, ref TrayPoint, since int64,
	w *trayWatch, out *[]TrayFact) {
	if !s.sure {
		return
	}
	s.doubted = true
	q := s.question
	if q != nil && q.watch != nil {
		if cause == "unseen" { // still not seen: nothing new
			return
		}
		q.watch = nil // any other doubt: seeing it again no longer settles it
	}
	if (len(s.chain) > 0 && cause != "overlap") || (w != nil && q == nil) {
		if q == nil {
			s.question = &trayQuestion{cause: cause, why: why, since: since, opened: ref, watch: w}
		} else if since > q.since {
			q.since = since
		}
		return
	}
	if cause == "overlap" {
		s.box = true
	}
	l.unsure(tr, s, why, cause, ref, out)
}

// unsure is the fact unsure: the stone is not sure (sure again only through the leaving's speckle answer); a question
// ends undecided with its own why and code.
func (l *trayLedger) unsure(tr *openTray, s *trayStone, why, cause string, ref TrayPoint, out *[]TrayFact) {
	q := s.question
	s.sure, s.question = false, nil
	l.c.Unsure++
	var opened *TrayPoint
	s.unsureFromT = ref.T
	if q != nil {
		why, cause = q.why, q.cause
		o := q.opened
		opened = &o
		s.unsureFromT = o.T
	}
	s.unsureCode = cause
	l.fact(TrayUnsure, tr, s, ref, TrayFact{Why: why, Code: cause, Opened: opened}, out)
}

// endQuestion ends a question undecided (before a certain removal, or when the tray leaves).
func (l *trayLedger) endQuestion(tr *openTray, s *trayStone, ref TrayPoint, out *[]TrayFact) {
	if q := s.question; q != nil {
		l.unsure(tr, s, q.why, q.cause, ref, out)
	}
}

// sinceOf is the first frame whose looks count for a hand doubt: after the stone's latest disturbance.
func sinceOf(s *trayStone, ref TrayPoint) int64 {
	if s.hasDisturbed {
		return s.disturbed + 1
	}
	return ref.G
}

// ---- presence questions (section 4.6.1) -------------------------------------------------------------------------

// unread: frames the rules could not read came before the frame at t: it counts as a busy and a push frame, and every
// open unseen run (a stone's, and a presence question's) is spoilt, its stone detected or not.
func unread(tr *openTray, t float64) {
	if !tr.hasBusy || tr.busyT < t {
		tr.busyT = t
	}
	if !tr.hasPush || tr.pushT < t {
		tr.pushT = t
	}
	tr.hasBusy, tr.hasPush = true, true
	for _, s := range tr.stones {
		for _, rec := range s.watches() {
			if rec != nil {
				rec.spoilt = true
			}
		}
	}
}

// crowded says another world stone of the stone's size, not held, stands within nearD of the place where it was last
// seen before its unseen run: a sight there can not tell the two apart.
func crowded(s *trayStone, w *trayWatch, tw *trayWorld) bool {
	for _, ws := range tw.stones {
		if ws.ID != w.wid && !ws.UnderSheet && !inHand(ws) && compatible(sizeOf(ws), s.size) && dist(placeOf(ws), w.place) <= nearD {
			return true
		}
	}
	return false
}

// lookAgainst says a look of the stone's world stone read since it was last seen, still fresh, reaches the bar against
// the chain of another tray stone the look step pairs looks with (lost, or on the tray in question): that stone may be
// on this world stone, so no sight settles the question.
func lookAgainst(tr *openTray, s *trayStone, w *trayWatch, tw *trayWorld) bool {
	e := tr.store[w.wid]
	if e == nil || e.t <= w.seenT || !fresh(tr, e, tw) {
		return false
	}
	bar := tidOf(tr)
	for _, o := range tr.stones {
		if o == s || len(o.chain) == 0 || !o.sure || !compatible(o.size, e.size) ||
			!(o.status == TrayStoneLost || (o.status == TrayStoneOn && o.question != nil)) {
			continue
		}
		if sc, n, ok := chainScore(tr, o, e); ok && sc >= bar && n >= nID {
			return true
		}
	}
	return false
}

// watchOpen says nothing could have changed the stone since it was last seen: its run is not spoilt, it was not
// handled or held since, no hand mark since lies within handR of its last place or of its place now, and no look of
// its world stone since shows another open stone (lookAgainst).
func watchOpen(tr *openTray, s *trayStone, w *trayWatch, tw *trayWorld) bool {
	return !(s.hasHandledT && s.handledT >= w.seenT) && !w.spoilt && !handsNear(tr, w.seenT, w.place, s.place) &&
		!lookAgainst(tr, s, w, tw)
}

// seenAgain is the sight half of a settle: linked to the run's world stone; first seen after the run's first calm
// moment within nearD of where it was last seen before the run, with no busy or push frame from that last sight up to
// then and no other stone of its size within nearD of that place then; seen within nearD of that place last, after
// that calm moment; and no other stone of its size within nearD of it now.
func seenAgain(s *trayStone, w *trayWatch, tw *trayWorld) bool {
	a := w.again
	if a == nil || dist(a.place, w.place) > nearD || a.busy || a.push || a.crowd {
		return false
	}
	return s.status == TrayStoneOn && s.linked && s.world == w.wid && s.seenT > w.c1T &&
		dist(s.place, w.place) <= nearD && !crowded(s, w, tw)
}

// cleanSince says the leaving may leave the last snapshot's own frame uncounted for this stone (4.6.1, Leaving): its
// unseen run is open (watchOpen), and no busy frame and no push frame came since it was last seen.
func cleanSince(tr *openTray, s *trayStone, tw *trayWorld) bool {
	r := s.run1
	if r == nil || !watchOpen(tr, s, r, tw) {
		return false
	}
	return !((tr.hasBusy && tr.busyT >= r.seenT) || (tr.hasPush && tr.pushT >= r.seenT))
}

// watchOf is the watch of an unseen doubt (a copy of the stone's unseen run): none for a stone with a chain (only a
// look settles its question), when another cause of the settle's step 1 holds now, or when something could have
// changed the stone since it was last seen. (appearedNext needs a stone handled since the last snapshot, which is
// later than the run's s1, so watchOpen refuses such a stone too; the cause is kept as 4.6.1 lists it.)
func watchOf(tr *openTray, s *trayStone, k int, births []birth, tw *trayWorld) *trayWatch {
	r := s.run1
	if len(s.chain) > 0 || r == nil || !s.linked || r.wid != s.world || r.spoilt || s.pushed || s.freedPush ||
		pushedNext(tr, s, k) || appearedNext(s, births) {
		return nil
	}
	w := &trayWatch{wid: r.wid, place: r.place, seenT: r.seenT, c1T: r.c1T, again: r.again}
	if !watchOpen(tr, s, w, tw) {
		return nil
	}
	return w
}

// pushedNext says a stone of a compatible size next to s in snapshot k was pushed.
func pushedNext(tr *openTray, s *trayStone, k int) bool {
	for _, o := range tr.stones {
		if o != s && o.pushed && o.atK == k && compatible(o.size, s.size) && dist(o.at, s.at) <= handR {
			return true
		}
	}
	return false
}

// appearedNext says a stone of a compatible size appeared next to s while a hand held it.
func appearedNext(s *trayStone, births []birth) bool {
	if !s.handled {
		return false
	}
	for _, b := range births {
		if compatible(b.size, s.size) && math.Min(dist(b.place, s.place), dist(b.place, s.at)) <= handR {
			return true
		}
	}
	return false
}

func handsNear(tr *openTray, since float64, places ...vec2) bool {
	for i := len(tr.marks) - 1; i >= 0; i-- {
		m := tr.marks[i]
		if m.t < since {
			break
		}
		for _, q := range places {
			if dist(m.p, q) <= handR {
				return true
			}
		}
	}
	return false
}

// relink links a lost tray stone to an unidentified world stone of this frame (its fact names the new world id).
func (l *trayLedger) relink(tr *openTray, s *trayStone, id int64, tw *trayWorld, sure bool, why, cause string,
	ref TrayPoint, out *[]TrayFact) {
	u := tr.unid[id]
	delete(tr.unid, id)
	s.sheetLoss = false
	s.linked, s.world, s.status = true, id, TrayStoneOn
	s.touch(ref.G)
	s.worldT = u.born.T
	s.carriedAway, s.dropped, s.emptied = false, false, false
	ws := tw.byID[id]
	s.place = placeOf(ws)
	if ws.Detected {
		s.seenT = ref.T
	}
	tr.link[id] = s
	if sure {
		return
	}
	if cause == "matched_at_leaving" { // never a question
		if s.sure {
			s.doubted = true
			l.unsure(tr, s, why, cause, ref, out)
		}
		return
	}
	l.doubt(tr, s, why, cause, ref, ref.G+1, out)
}

// renames: a lost stone and one unidentified stone at its place, within renameT s of the loss: the world renamed it.
func (l *trayLedger) renames(tr *openTray, tw *trayWorld, out *[]TrayFact) {
	var lost []*trayStone
	for _, s := range tr.stones {
		if s.status == TrayStoneLost && !s.dropped {
			lost = append(lost, s)
		}
	}
	if len(lost) == 0 || len(tr.unid) == 0 {
		return
	}
	fits := func(o *trayStone, u *trayUnid) bool {
		return math.Abs(u.born.T-o.lostT) <= renameT && compatible(u.size, o.size) && dist(u.place, o.place) <= renameD
	}
	for _, s := range lost {
		var near []int64
		for _, id := range sortedKeys(tr.unid) {
			if fits(s, tr.unid[id]) {
				near = append(near, id)
			}
		}
		if len(near) != 1 {
			continue
		}
		id := near[0]
		u := tr.unid[id]
		rival := false
		for _, o := range lost {
			if o != s && o.status == TrayStoneLost && fits(o, u) {
				rival = true
				break
			}
		}
		if rival {
			continue
		}
		sure := s.sure && s.question == nil && s.lostK == tr.k && !s.carriedAway && !s.emptied &&
			!handsNear(tr, math.Min(s.lostT, u.born.T)-renameT, s.place, u.place)
		l.relink(tr, s, id, tw, sure, "found again, not for sure", "found_again", tw.ref, out)
	}
}

// rivals says a world stone could be this lost stone: unidentified, or standing for a tray stone that is not trusted,
// of a compatible size, first seen after it was last seen (less renameT) or at its place.
func rivals(tr *openTray, s *trayStone) bool {
	for _, u := range tr.unid {
		if compatible(u.size, s.size) && (u.born.T >= s.seenT-renameT || dist(u.place, s.place) <= renameD) {
			return true
		}
	}
	for _, o := range tr.stones {
		if o.status == TrayStoneOn && !trusted(o) && compatible(o.size, s.size) &&
			(o.worldT >= s.seenT-renameT || dist(o.place, s.place) <= renameD) {
			return true
		}
	}
	return false
}

type birth struct{ place, size vec2 }

func (l *trayLedger) settle(tr *openTray, tw *trayWorld, ref TrayPoint, final bool, out *[]TrayFact) {
	t, k := ref.T, tr.k
	var world []*StoneView
	for _, ws := range tw.stones {
		if !ws.UnderSheet && !inHand(ws) {
			world = append(world, ws)
		}
	}
	var births []birth
	for _, id := range sortedKeys(tr.unid) {
		if u := tr.unid[id]; u.born.T > tr.settleT {
			births = append(births, birth{u.place, u.size})
		}
	}
	for _, s := range tr.stones {
		if s.status == TrayStoneOn && !trusted(s) && s.worldT > tr.settleT {
			births = append(births, birth{s.place, s.size})
		}
	}
	// 1. sure stones (and stones in question), one by one in tray order: is the world still showing them? The leaving
	//    tests the last frame again only when it is not the last snapshot's own frame, or when something could have
	//    changed the stone since it was last seen (4.6.1, Leaving)
	sameFrame := final && tr.last != nil && tr.last.ref.G == tr.settleG
	// at the leaving, the stones its speckle answer pairs are not tested: the answer settles them (4.6.13)
	var paired map[*trayStone]bool
	if sp := leavingAnswer(tr, ref); final && sp != nil {
		paired = map[*trayStone]bool{}
		for _, x := range speckleTargets(tr, sp, tw) {
			paired[x.s] = true
		}
	}
	for _, s := range tr.stones {
		if s.underSheet || s.status != TrayStoneOn || !s.sure || s.atK != k || paired[s] {
			continue
		}
		var ws *StoneView
		if s.linked {
			ws = tw.byID[s.world]
		}
		if tr.visit.stillUnseen(s, ws, tw.ref.G, final) {
			l.doubt(tr, s, "not seen at two calm moments", "unseen", ref, ref.G, out)
			continue
		}
		settled := false
		if q := s.question; q != nil && q.watch != nil { // a presence question (4.6.1)
			if !watchOpen(tr, s, q.watch, tw) { // something could have changed it: it ends
				l.unsure(tr, s, q.why, q.cause, ref, out)
				continue
			}
			if seenAgain(s, q.watch, tw) { // seen again: settled, sure again, no fact
				s.question, s.ghost, s.run1, settled = nil, 0, nil, true
			}
		}
		if ws != nil && overlapShare(ws, world) >= overlapMax {
			l.doubt(tr, s, "its box overlaps another stone", "overlap", ref, ref.G, out)
			continue
		}
		if ws != nil && !ws.Detected {
			// the leaving on the last snapshot's own frame, nothing since its last sight: tested there
			if !(sameFrame && cleanSince(tr, s, tw)) {
				s.ghost++
				if s.ghost == 1 {
					s.run1 = &trayWatch{wid: ws.ID, place: s.place, seenT: s.seenT, c1T: t}
					if settled { // settled on a calm moment that does not show it: its next run is on probation
						s.once = true
					}
				}
				if s.ghost >= ghostMoments {
					if q := s.question; q != nil && q.watch != nil { // still watched, still not seen
						l.doubt(tr, s, "not seen at two calm moments", "unseen", ref, ref.G, out)
					} else {
						w := watchOf(tr, s, k, births, tw)
						s.once = false
						l.doubtWith(tr, s, "not seen at two calm moments", "unseen", ref, ref.G, w, out)
						if q := s.question; w != nil && q != nil && q.watch == w && seenAgain(s, w, tw) {
							// seen since the first unseen calm moment: settled; not detected now, so this calm
							// moment starts its next unseen run, on probation
							s.question, s.ghost, s.once = nil, 1, true
							s.run1 = &trayWatch{wid: ws.ID, place: s.place, seenT: s.seenT, c1T: t}
						}
						continue
					}
				}
			}
		} else {
			r, once := s.run1, s.once
			s.ghost, s.run1, s.once = 0, nil, false
			if once && !(r != nil && watchOpen(tr, s, r, tw) && seenAgain(s, r, tw)) {
				// its run on probation ends with a sight that does not settle it: something could have changed the
				// stone while it was out of sight (4.6.1)
				l.doubt(tr, s, "not seen at two calm moments", "unseen", ref, ref.G, out)
				continue
			}
		}
		if s.pushed {
			l.doubt(tr, s, "it was pushed", "pushed", ref, sinceOf(s, ref), out)
			continue
		}
		if pushedNext(tr, s, k) || s.freedPush {
			l.doubt(tr, s, "a stone next to it was pushed", "neighbour_pushed", ref, sinceOf(s, ref), out)
			continue
		}
		if appearedNext(s, births) {
			l.doubt(tr, s, "a stone appeared next to it while a hand held it", "appeared", ref, sinceOf(s, ref), out)
		}
	}
	// 2. the look work
	l.lookWork(tr, tw, ref, out)
	if final {
		// the leaving's speckle answer (4.6.13): the unit's pairs confirm, relink and find stones again; then the rest
		// of the leaving
		l.speckleLeaving(tr, tw, ref, out)
		l.leave(tr, tw, ref, out)
		return
	}
	// 3. certain removals, on a tray that is not relinked: v1's rule; a twin keeps a stone v1 removes, and relinks
	//    the tray
	for _, s := range tr.stones {
		if tr.relinked {
			break
		}
		if s.status != TrayStoneLost || s.sheetLoss || (s.lostK >= k && t-s.lostT < renameT) {
			continue
		}
		empty := true
		for _, ws := range world {
			if covers(ws, s.place, emptyMargin) {
				empty = false
				break
			}
		}
		if !empty || rivals(tr, s) || !handTook(s) {
			continue
		}
		if tr.twins[s.id] {
			tr.relinked = true
			break
		}
		l.endQuestion(tr, s, ref, out)
		s.status, s.certain = TrayStoneRemoved, Bool(true)
		l.c.Removed++
		code := "missing"
		if s.carriedAway {
			code = "carried_away"
		}
		l.fact(TrayRemoved, tr, s, ref, TrayFact{Certain: Bool(true), Why: strings.ReplaceAll(code, "_", " "),
			Code: code}, out)
	}
	// 4. certain additions
	var doubtful []vec2
	for _, s := range tr.stones {
		if s.status == TrayStoneLost || (s.status == TrayStoneOn && !trusted(s)) {
			doubtful = append(doubtful, s.size)
		}
	}
	for _, id := range sortedKeys(tr.unid) {
		u, ws := tr.unid[id], tw.byID[id]
		if ws == nil || !ws.Detected {
			u.seen, u.hasLast = 0, false
			continue
		}
		if u.hasLast && dist(u.last, u.place) <= nearD {
			u.seen++
		} else {
			u.seen = 1
		}
		u.hasLast, u.last = true, u.place
		if u.seen < 2 || u.freed || u.resized || tr.relinked {
			continue
		}
		blocked := false
		for _, z := range doubtful {
			if compatible(u.size, z) {
				blocked = true
				break
			}
		}
		if !blocked {
			l.mint(tr, id, u, ws, true, ref, out)
		}
	}
	// 5. the snapshot
	snapshot(tr, ref)
}

// sameStone is the leaving's evidence (sections 4.6.1, Leaving; 4.6.7, step 4) that the unidentified world stone W is
// the lost tray stone S, which a pair needs: their looks agree (S's chain against W's stored look: at least the tray's
// bar over at least nID cells); or, when the looks do not disagree (below tDiff over at least nID cells), nothing could
// have put another stone there: W was first seen at or after S was last seen, less renameT, and no hand mark from the
// earliest of S's last sight, S's loss and W's first sight, less renameT, lies within handR of S's place or of W's.
func sameStone(tr *openTray, s *trayStone, id int64, u *trayUnid) bool {
	if e := tr.store[id]; e != nil && len(s.chain) > 0 {
		if sc, n, ok := chainScore(tr, s, e); ok && n >= nID {
			if sc >= tidOf(tr) {
				return true
			}
			if sc < tDiff {
				return false
			}
		}
	}
	return u.born.T >= s.seenT-renameT &&
		!handsNear(tr, math.Min(math.Min(s.seenT, s.lostT), u.born.T)-renameT, s.place, u.place)
}

// leave is the rest of the leaving (section 4.6.7): questions end, v1's pairing (only pairs with evidence), unclear
// removals and additions, the open account, the snapshot.
func (l *trayLedger) leave(tr *openTray, tw *trayWorld, ref TrayPoint, out *[]TrayFact) {
	for _, s := range tr.stones {
		l.endQuestion(tr, s, ref, out)
	}
	type pair struct {
		d     float64
		index int
		id    int64
	}
	var pairs []pair
	for _, s := range tr.stones {
		if s.status != TrayStoneLost {
			continue
		}
		for id, u := range tr.unid {
			if compatible(u.size, s.size) && dist(u.place, s.place) <= leaveD && sameStone(tr, s, id, u) {
				pairs = append(pairs, pair{dist(u.place, s.place), s.index, id})
			}
		}
	}
	slices.SortFunc(pairs, func(a, b pair) int { // nearest first, then in tray order, then by world id
		return cmp.Or(cmp.Compare(a.d, b.d), cmp.Compare(a.index, b.index), cmp.Compare(a.id, b.id))
	})
	usedS, usedW := map[int]bool{}, map[int64]bool{}
	for _, p := range pairs {
		if usedS[p.index] || usedW[p.id] {
			continue
		}
		usedS[p.index], usedW[p.id] = true, true
		l.relink(tr, tr.stones[p.index], p.id, tw, false, "matched when the tray left", "matched_at_leaving", ref, out)
	}
	for _, s := range tr.stones {
		if s.status == TrayStoneLost {
			if s.sheetLoss {
				s.status = TrayStoneOn
				if s.sure {
					l.unsure(tr, s, "its track ended when the paper lifted", "unseen", ref, out)
				}
				continue
			}
			s.status, s.certain, s.leftNow = TrayStoneRemoved, Bool(false), true
			tr.unclear++
			l.c.RemovedUnclear++
			l.fact(TrayRemoved, tr, s, ref, TrayFact{Certain: Bool(false), Why: "not found when the tray left",
				Code: "not_found"}, out)
		}
	}
	var unheld []*StoneView // the leaving point's world stones not held: those that can hide a kept stone
	if tr.leaveWorld != nil {
		for _, o := range tr.leaveWorld.stones {
			if !o.UnderSheet && !inHand(o) {
				unheld = append(unheld, o)
			}
		}
	}
	for _, id := range sortedKeys(tr.unid) {
		u, ws := tr.unid[id], tw.byID[id]
		if ws != nil && !ws.Detected && u.boundary != nil && u.boundary.calmG == ref.G {
			ws = u.boundary.latest
		}
		kept := false
		if ws != nil && !ws.Detected && tr.leaveWorld != nil {
			if at := tr.leaveWorld.byID[id]; at != nil && !inHand(at) {
				if at.Detected {
					// detected, not held, in the leaving point's world: positive evidence too (no hand worked since)
					ws = at
				} else if overlapShare(at, unheld) < overlapMax {
					// kept, not held, in the leaving point's world, less than overlapMax of its box under another
					// unheld stone's: the world kept it on the tray although the model did not box it there (version 7)
					ws, kept = at, true
				}
			}
		}
		if ws != nil && (ws.Detected || kept) {
			l.mint(tr, id, u, ws, false, ref, out)
		}
	}
	tr.open, tr.left = l.openAccount(tr, tw), true
	tr.unid = map[int64]*trayUnid{}
	snapshot(tr, ref)
}

func (l *trayLedger) mint(tr *openTray, id int64, u *trayUnid, ws *StoneView, certain bool, ref TrayPoint,
	out *[]TrayFact) {
	s := newTrayStone(tr, trayStamp(u.born.Wall, u.born.T, l.session)+"-"+itoa(tr.n)+"-"+itoa(id), ws, u.born)
	s.sure, s.added, s.certain = certain, true, Bool(certain)
	s.doubted, s.unidRec, s.addedNow = !certain, u, !certain
	tr.stones = append(tr.stones, s)
	tr.link[id] = s
	delete(tr.unid, id)
	why, code := "set down", "set_down"
	if certain {
		tr.added++
		l.c.Added++
	} else {
		tr.unclear++
		l.c.AddedUnclear++
		why, code = "not recognised when the tray left", "not_recognised"
	}
	l.fact(TrayAdded, tr, s, ref, TrayFact{Certain: Bool(certain), Why: why, Code: code}, out)
}

// ---- looks on the tray (section 4.6.3) -------------------------------------------------------------------------

// storeLooks is the look store: forget the ids of this frame's look clearers and of stones gone, then keep this
// frame's usable looks of stones not held inside the board (any place before the arrival).
func storeLooks(tr *openTray, tw *trayWorld) {
	store := tr.store
	for i := range tw.w.Events {
		if e := &tw.w.Events[i]; lookClearer(e.Type) && e.ID != nil {
			delete(store, *e.ID)
		}
	}
	for id := range store {
		if tw.byID[id] == nil {
			delete(store, id)
		}
	}
	for _, ws := range tw.stones {
		if ws.UnderSheet || inHand(ws) || !insideBoard(tr.board, placeOf(ws)) {
			continue
		}
		if c := usableLook(ws.Look); c != nil {
			store[ws.ID] = &trayLook{wid: ws.ID, cells: c, g: tw.ref.G, t: tw.ref.T, place: placeOf(ws),
				size: sizeOf(ws), detected: ws.Detected}
		}
	}
}

// fresh says a stored look still describes its stone: the stone is here, not held, within nearD of where the look
// was read, and no hand mark after the look lies within handR of that place.
func fresh(tr *openTray, e *trayLook, tw *trayWorld) bool {
	ws := tw.byID[e.wid]
	if ws == nil || ws.UnderSheet || inHand(ws) || dist(placeOf(ws), e.place) > nearD {
		return false
	}
	for i := len(tr.marks) - 1; i >= 0; i-- {
		m := tr.marks[i]
		if m.t <= e.t {
			break
		}
		if dist(m.p, e.place) <= handR {
			return false
		}
	}
	return true
}

func tidOf(tr *openTray) int64 {
	if tr.alikeN < gaugeMin || !tr.hasAlike {
		return tIDStart
	}
	return max(tIDMin, tr.alike+gaugeGap)
}

// pScore is the permille score and pair count of look a against look b (ok false: none); each pair of looks is
// scored once a tray.
func pScore(tr *openTray, a, b *trayLook) (int64, int64, bool) {
	key := [4]int64{a.wid, a.g, b.wid, b.g}
	m, done := tr.memo[key]
	if !done {
		if s, n, ok := pairCells(a.cells, b.cells); ok {
			m = trayScore{permilleOf(s), n, true}
		}
		tr.memo[key] = m
	}
	return m.sc, m.n, m.ok
}

// chainScore is a tray stone's chain against a look L: against its anchor, or its latest when that scores higher; a
// chain look that is L itself (the same world stone and frame) is not scored.
func chainScore(tr *openTray, s *trayStone, look *trayLook) (int64, int64, bool) {
	var sc, n int64
	ok := false
	for _, c := range s.chain {
		if c.wid == look.wid && c.g == look.g {
			continue
		}
		if x, xn, xok := pScore(tr, look, c); xok && (!ok || x > sc) {
			sc, n, ok = x, xn, true
		}
	}
	return sc, n, ok
}

// setLatest makes look the latest of the stone's chain, after its anchor. Only a stone with a chain gets one: a sure
// stone re-read, or a row of the look run.
func setLatest(s *trayStone, look *trayLook) {
	s.chain = []*trayLook{s.chain[0], look}
}

// ---- the look work (section 4.6.5) -----------------------------------------------------------------------------

func (l *trayLedger) lookWork(tr *openTray, tw *trayWorld, ref TrayPoint, out *[]TrayFact) {
	l.takeLooks(tr, tw, ref, out)
	anchors(tr, tw)
	for {
		run := lookRunOf(tr, tw)
		tr.lastRun = run
		if run.best == nil {
			return
		}
		l.apply(tr, run.best, tw, ref, out)
	}
}

func (l *trayLedger) takeLooks(tr *openTray, tw *trayWorld, ref TrayPoint, out *[]TrayFact) {
	store := tr.store
	var take []int64
	for _, id := range sortedKeys(store) {
		if e := store[id]; !e.taken && fresh(tr, e, tw) {
			take = append(take, id)
		}
	}
	take = take[:min(len(take), nTake)]
	type partner struct {
		d  float64
		id int64
	}
	for _, id := range take {
		e := store[id]
		e.taken, tr.took = true, true
		ws := tw.byID[id]
		here, size := placeOf(ws), sizeOf(ws)
		// the gauge: how alike two different stones of this tray look, both ways
		var partners []partner
		for oid, o := range tw.byID {
			x := store[oid]
			if oid == id || x == nil || !x.taken || !fresh(tr, x, tw) || !insideBoard(tr.board, placeOf(o)) ||
				!compatible(sizeOf(o), size) {
				continue
			}
			if d := dist(placeOf(o), here); d >= 0.5*size[0] {
				partners = append(partners, partner{d, oid})
			}
		}
		slices.SortFunc(partners, func(a, b partner) int { return cmp.Or(cmp.Compare(a.d, b.d), cmp.Compare(a.id, b.id)) })
		for i, p := range partners {
			if i >= gaugeK {
				break
			}
			var v []int64
			if sc, _, ok := pScore(tr, e, store[p.id]); ok {
				v = append(v, sc)
			}
			if sc, _, ok := pScore(tr, store[p.id], e); ok {
				v = append(v, sc)
			}
			if len(v) > 0 {
				if m := slices.Max(v); !tr.hasAlike || m > tr.alike {
					tr.alike, tr.hasAlike = m, true
				}
				tr.alikeN++
			}
		}
		// the own compare of a sure stone with a chain
		if s := tr.link[id]; s != nil && len(s.chain) > 0 && untouched(s) {
			sc, n, ok := chainScore(tr, s, e)
			if ok && sc >= tidOf(tr) && n >= nID {
				setLatest(s, e)
			} else if ok && sc < tDiff && n >= nID {
				if trusted(s) { // v1 trusts it: from here on what the tray trusts may differ from v1's
					tr.relinked = true
				}
				l.doubt(tr, s, "its look changed", "look_changed", ref, e.g, out)
			}
		}
	}
}

func anchors(tr *openTray, tw *trayWorld) {
	for _, s := range tr.stones {
		if s.underSheet || s.status != TrayStoneOn || len(s.chain) > 0 || !s.linked {
			continue
		}
		e := tr.store[s.world]
		if e == nil || !e.taken || !fresh(tr, e, tw) {
			continue
		}
		if untouched(s) && e.detected && s.ghost == 0 {
			s.chain = []*trayLook{e}
		} else {
			s.late = true
		}
	}
}

// untouched: sure, not in question, never lifted on this tray and not disturbed since the last snapshot.
func untouched(s *trayStone) bool { return s.sure && s.question == nil && !s.lifted && !s.hasDisturbed }

// firstSeen is when an open world stone was first seen on this tray: its unidentified record, or its tray stone's
// link.
func firstSeen(tr *openTray, id int64) float64 {
	if u := tr.unid[id]; u != nil {
		return u.born.T
	}
	return tr.link[id].worldT
}

// unscored says a tray stone without a chain could be on world stone id: linked to it, within dNeigh of it, or
// disturbed since the last snapshot within dMatch of it. No score rules it out, so no decision on id.
func unscored(tr *openTray, s *trayStone, id int64, here, size vec2) bool {
	for _, o := range tr.stones {
		if o == s || o.status == TrayStoneRemoved || len(o.chain) > 0 || !compatible(o.size, size) {
			continue
		}
		d := dist(o.place, here)
		if (o.status == TrayStoneOn && o.linked && o.world == id) || d <= dNeigh || (o.hasDisturbed && d <= dMatch) {
			return true
		}
	}
	return false
}

// lookRunOf is one run of the look step: rows (open tray stones with a chain), columns (open world stones with a
// fresh taken look), their pairs, neighbours, hits and twins, and the decided pair with the highest score.
func lookRunOf(tr *openTray, tw *trayWorld) *lookRun {
	tid, store := tidOf(tr), tr.store
	type row struct {
		s     *trayStone
		since int64
	}
	var rows []row
	for _, s := range tr.stones {
		if s.underSheet || len(s.chain) == 0 || !s.sure {
			continue
		}
		q := s.question
		switch {
		case s.status == TrayStoneOn && q != nil:
			rows = append(rows, row{s, q.since})
		case s.status == TrayStoneLost && s.lostByLook && q != nil:
			rows = append(rows, row{s, q.since})
		case s.status == TrayStoneLost && q != nil:
			rows = append(rows, row{s, max(s.lostG+1, q.since)})
		case s.status == TrayStoneLost:
			rows = append(rows, row{s, s.lostG + 1})
		}
	}
	var cols []*StoneView
	for _, ws := range tw.stones {
		if ws.UnderSheet || inHand(ws) || !insideBoard(tr.board, placeOf(ws)) {
			continue
		}
		e := store[ws.ID]
		if e == nil || !e.taken || !fresh(tr, e, tw) {
			continue
		}
		y := tr.link[ws.ID]
		if (y == nil && tr.unid[ws.ID] == nil) || (y != nil && y.sure && y.question == nil) {
			continue
		}
		cols = append(cols, ws)
	}
	type near struct {
		d   float64
		key int64
	}
	byNear := func(a, b near) int { return cmp.Or(cmp.Compare(a.d, b.d), cmp.Compare(a.key, b.key)) }
	pairs := map[int][]lookPair{} // row index -> [(world id, S, n)] nearest first
	for _, r := range rows {
		var ns []near
		gone := r.s.status == TrayStoneLost && (r.s.carriedAway || r.s.emptied)
		for _, ws := range cols {
			e := store[ws.ID]
			if d := dist(r.s.place, placeOf(ws)); compatible(r.s.size, sizeOf(ws)) && d <= dMatch &&
				e.g >= r.since && (!tr.hasLastMark || e.t >= tr.lastMarkT) &&
				!(gone && firstSeen(tr, ws.ID) <= r.s.lostT) {
				ns = append(ns, near{d, ws.ID})
			}
		}
		slices.SortFunc(ns, byNear)
		ps := []lookPair{}
		for _, nw := range ns[:min(len(ns), kPair)] {
			sc, n, ok := chainScore(tr, r.s, store[nw.key])
			ps = append(ps, lookPair{nw.key, sc, n, ok})
		}
		pairs[r.s.index] = ps
	}
	neigh := map[int64][]lookPair{} // world id -> [(tray index, S, n)] nearest first
	hits := map[int64]map[int]bool{}
	for _, ws := range cols {
		id, here, size := ws.ID, placeOf(ws), sizeOf(ws)
		var cs []near
		for _, o := range tr.stones {
			if d := dist(o.place, here); o.status == TrayStoneOn && len(o.chain) > 0 && compatible(o.size, size) &&
				d <= dNeigh {
				cs = append(cs, near{d, int64(o.index)})
			}
		}
		slices.SortFunc(cs, byNear)
		ns := []lookPair{}
		h := map[int]bool{}
		for _, c := range cs[:min(len(cs), kNeigh)] {
			sc, n, ok := chainScore(tr, tr.stones[c.key], store[id])
			ns = append(ns, lookPair{c.key, sc, n, ok})
			if ok && sc >= tid && n >= nID {
				h[int(c.key)] = true
			}
		}
		neigh[id] = ns
		for i, ps := range pairs {
			for _, p := range ps {
				if p.key == id && p.ok && p.sc >= tid && p.n >= nID {
					h[i] = true
				}
			}
		}
		hits[id] = h
	}
	twins := map[int64]bool{}
	for id, h := range hits {
		if len(h) >= 2 {
			twins[id] = true
			for i := range h {
				tr.twins[tr.stones[i].id] = true
			}
		}
	}
	// decided pairs
	type decision struct {
		sc     int64
		row    int
		wid    int64
		margin *int64
		bar    int64
	}
	var decided []decision
	info := map[rowWorld]pairInfo{} // (row, world id) -> (bar, best rival)
	for _, r := range rows {
		s, i := r.s, r.s.index
		for _, p := range pairs[i] {
			ws := tw.byID[p.key]
			here, size := placeOf(ws), sizeOf(ws)
			bar := tid
			firm := !(s.status == TrayStoneOn && s.linked && s.world == p.key)
			for _, o := range tr.stones {
				if firm {
					break
				}
				firm = o != s && o.status != TrayStoneRemoved && len(o.chain) == 0 && compatible(o.size, size) &&
					dist(o.place, here) <= dNeigh
			}
			if firm {
				bar = max(tid, tFirm)
			}
			var rivals []int64
			for _, q := range pairs[i] {
				if q.key != p.key && q.ok {
					rivals = append(rivals, q.sc)
				}
			}
			for j, ps := range pairs {
				for _, q := range ps {
					if j != i && q.key == p.key && q.ok {
						rivals = append(rivals, q.sc)
					}
				}
			}
			for _, q := range neigh[p.key] {
				if int(q.key) != i && q.ok {
					rivals = append(rivals, q.sc)
				}
			}
			pi := pairInfo{bar: bar}
			if len(rivals) > 0 {
				pi.top, pi.hasTop = slices.Max(rivals), true
			}
			info[rowWorld{i, p.key}] = pi
			clear := !twins[p.key] && p.ok && p.n >= nID && p.sc >= bar && !unscored(tr, s, p.key, here, size)
			for _, rv := range rivals {
				clear = clear && p.sc-rv >= mID
			}
			if clear {
				d := decision{sc: p.sc, row: i, wid: p.key, bar: bar}
				if pi.hasTop {
					d.margin = Int64(p.sc - pi.top)
				}
				decided = append(decided, d)
			}
		}
	}
	run := &lookRun{pairs: pairs, info: info}
	if len(decided) > 0 {
		d := slices.MinFunc(decided, func(a, b decision) int {
			return cmp.Or(cmp.Compare(b.sc, a.sc), cmp.Compare(a.row, b.row), cmp.Compare(a.wid, b.wid))
		})
		run.best = &lookBest{row: d.row, wid: d.wid, score: d.sc, margin: d.margin, bar: d.bar}
	}
	for _, r := range rows {
		run.rows = append(run.rows, lookRow{r.s.index, r.since})
	}
	for _, ws := range cols {
		run.cols = append(run.cols, ws.ID)
	}
	return run
}

// apply makes a decided pair: a question settled, or a stone linked by its look (fact identified).
func (l *trayLedger) apply(tr *openTray, best *lookBest, tw *trayWorld, ref TrayPoint, out *[]TrayFact) {
	s, id := tr.stones[best.row], best.wid
	look := tr.store[id]
	if s.status == TrayStoneOn && s.linked && s.world == id {
		s.question = nil
		setLatest(s, look)
		return
	}
	wasLost := s.status == TrayStoneLost
	y := tr.link[id]
	if y != nil {
		delete(tr.link, id)
		lose(tr, y, ref)
		y.lostByLook, y.lookLost = true, true
	}
	var from *int64
	if s.status == TrayStoneOn && s.linked {
		w0 := s.world
		from = Int64(w0)
		delete(tr.link, w0)
		if ws0 := tw.byID[w0]; ws0 != nil && !inHand(ws0) && insideBoard(tr.board, placeOf(ws0)) {
			tr.unid[w0] = &trayUnid{born: ref, place: placeOf(ws0), size: sizeOf(ws0), freed: true}
		}
	}
	u := tr.unid[id]
	delete(tr.unid, id)
	ws := tw.byID[id]
	s.sheetLoss = false
	s.linked, s.world, s.status = true, id, TrayStoneOn
	s.touch(ref.G)
	if u != nil {
		s.worldT = u.born.T
	} else { // a column is unidentified or linked
		s.worldT = y.worldT
	}
	s.carriedAway, s.dropped, s.emptied = false, false, false
	s.place = placeOf(ws)
	if ws.Detected {
		s.seenT = ref.T
	}
	tr.link[id] = s
	s.question = nil
	s.byLook = true
	s.atK, s.at = tr.k, s.place // its place in the last snapshot: where the look found it
	tr.relinked = true
	setLatest(s, look)
	why, code := "recognised by its look", "look_moved"
	if wasLost {
		why, code = "found again by its look", "look_found"
	}
	l.fact(TrayIdentified, tr, s, ref, TrayFact{Why: why, Code: code, FromWorld: from, Score: Int64(best.score),
		Margin: best.margin, Bar: Int64(best.bar)}, out)
}

// ---- the open account (section 4.6.8) --------------------------------------------------------------------------

func (l *trayLedger) openAccount(tr *openTray, tw *trayWorld) []TrayOpen {
	run := tr.lastRun // the leaving's look work ran
	rows := map[int]bool{}
	for _, r := range run.rows {
		rows[r.index] = true
	}
	sureWorld := map[int64]bool{}
	stoneOn := map[int64]string{}
	for _, s := range tr.stones {
		if s.status == TrayStoneOn {
			stoneOn[s.world] = s.id
			if s.sure {
				sureWorld[s.world] = true
			}
		}
	}
	var free []*StoneView
	for _, ws := range tw.stones {
		if !ws.UnderSheet && !inHand(ws) && insideBoard(tr.board, placeOf(ws)) && !sureWorld[ws.ID] {
			free = append(free, ws)
		}
	}
	entries := []TrayOpen{}
	for _, s := range tr.stones {
		var o TrayOpen
		switch {
		case s.addedNow:
			o = openAdded(tr, s, run)
		case s.leftNow:
			o = openStone(tr, s, TrayRemoved, run, rows, free, stoneOn, tw)
		case s.status == TrayStoneOn && !s.sure:
			o = openStone(tr, s, TrayUnsure, run, rows, free, stoneOn, tw)
		default:
			continue
		}
		entry := o
		s.open = &entry
		entries = append(entries, o)
	}
	return entries
}

func scoreOf(p lookPair, ok bool) *int64 {
	if !ok || !p.ok {
		return nil
	}
	return Int64(p.sc)
}

// tieOr is why the looks could not settle an entry from its pairs (keys[i] is pair i's row and world stone): "tie"
// when a pair reached its bar, with the best such score, else other, with the best score (nil when none).
func tieOr(run *lookRun, keys []rowWorld, ps []lookPair, other string) (string, *int64) {
	var ties, scored []int64
	for i, p := range ps {
		if p.ok {
			scored = append(scored, p.sc)
		}
		if p.ok && p.n >= nID && p.sc >= run.info[keys[i]].bar {
			ties = append(ties, p.sc)
		}
	}
	if len(ties) > 0 {
		return "tie", Int64(slices.Max(ties))
	}
	if len(scored) > 0 {
		return other, Int64(slices.Max(scored))
	}
	return other, nil
}

func openStone(tr *openTray, s *trayStone, kind string, run *lookRun, rows map[int]bool, free []*StoneView,
	stoneOn map[int64]string, tw *trayWorld) TrayOpen {
	i := s.index
	covered := false
	for _, ws := range tw.stones {
		covered = covered || (!ws.UnderSheet && !inHand(ws) && covers(ws, s.place, emptyMargin))
	}
	var cause string
	switch {
	case kind == TrayUnsure: // every unsure fact has its code
		cause = s.unsureCode
	case s.lookLost:
		cause = "look_lost"
	case s.byLook:
		cause = "by_look"
	case tr.twins[s.id]:
		cause = "twins"
	case tr.relinked:
		cause = "relinked"
	case s.lostK == tr.k:
		cause = "lost_late"
	case covered:
		cause = "covered"
	case rivals(tr, s):
		cause = "rivals"
	case !handTook(s):
		cause = "no_hand"
	default:
		cause = "unchecked"
	}
	ps := run.pairs[i]
	var look string
	var score *int64
	switch {
	case len(s.chain) == 0 && s.late:
		look = "late"
	case len(s.chain) == 0:
		look = "none"
	case s.box:
		look = "box"
	case !rows[i] || len(ps) == 0:
		look = "stale"
	default:
		keys := make([]rowWorld, len(ps))
		for j, p := range ps {
			keys[j] = rowWorld{i, p.key}
		}
		look, score = tieOr(run, keys, ps, "low")
	}
	scores := map[int64]lookPair{}
	for _, p := range ps {
		scores[p.key] = p
	}
	var cands []TrayCandidate
	if s.status == TrayStoneOn && !s.linked {
		cands = append(cands, TrayCandidate{Kind: "unseen"})
	} else if s.status == TrayStoneOn {
		p, ok := scores[s.world]
		cands = append(cands, TrayCandidate{Kind: "world", WorldID: Int64(s.world), Stone: s.id, Score: scoreOf(p, ok)})
	}
	type ranked struct {
		k  float64
		id int64
	}
	var withScore, without []ranked
	for _, ws := range free {
		if (s.linked && ws.ID == s.world) || !compatible(s.size, sizeOf(ws)) || dist(s.place, placeOf(ws)) > leaveD {
			continue
		}
		if p, ok := scores[ws.ID]; ok && p.ok {
			withScore = append(withScore, ranked{-float64(p.sc), ws.ID})
		} else {
			without = append(without, ranked{dist(s.place, placeOf(ws)), ws.ID})
		}
	}
	byRank := func(a, b ranked) int { return cmp.Or(cmp.Compare(a.k, b.k), cmp.Compare(a.id, b.id)) }
	slices.SortFunc(withScore, byRank)
	slices.SortFunc(without, byRank)
	for _, r := range append(withScore, without...) {
		p, ok := scores[r.id]
		cands = append(cands, TrayCandidate{Kind: "world", WorldID: Int64(r.id), Stone: stoneOn[r.id],
			Score: scoreOf(p, ok)})
	}
	why := ""
	switch {
	case kind == TrayRemoved && s.carriedAway:
		why = "carried_away"
	case kind == TrayRemoved && s.emptied:
		why = "emptied"
	case kind == TrayRemoved && s.dropped:
		why = "dropped"
	default:
		since := s.unsureFromT
		if kind == TrayRemoved {
			since = s.lostT
		}
		for _, m := range tr.marks {
			if m.t >= since && dist(m.p, s.place) <= handR {
				why = "hand"
				break
			}
		}
	}
	cands = append(cands, TrayCandidate{Kind: "gone", Why: why})
	if kind == TrayRemoved {
		cands = append(cands, TrayCandidate{Kind: "unseen"})
	}
	o := TrayOpen{Stone: s.id, Kind: kind, Cause: cause, Look: look, Score: score, Candidates: cands}
	if s.linked {
		o.WorldID = Int64(s.world)
	}
	return o
}

func openAdded(tr *openTray, s *trayStone, run *lookRun) TrayOpen {
	u, id := s.unidRec, s.world // an unclear addition is linked, with its unidentified record
	doubtful := false
	for _, z := range tr.stones {
		doubtful = doubtful || (z != s && !z.addedNow && compatible(z.size, s.size) &&
			(z.leftNow || (z.status == TrayStoneOn && !trusted(z))))
	}
	var cause string
	switch {
	case u.freed:
		cause = "freed"
	case u.resized:
		cause = "size_changed"
	case tr.relinked:
		cause = "relinked"
	case doubtful:
		cause = "doubtful"
	case u.seen < 2:
		cause = "seen_once"
	default:
		cause = "unchecked"
	}
	var keys []rowWorld // the rows' pairs with this world stone, in tray order
	var mine []lookPair
	for _, i := range slices.Sorted(maps.Keys(run.pairs)) {
		for _, p := range run.pairs[i] {
			if p.key == id {
				keys, mine = append(keys, rowWorld{i, id}), append(mine, p)
			}
		}
	}
	look, score := "none", (*int64)(nil)
	if slices.Contains(run.cols, id) {
		look, score = tieOr(run, keys, mine, "new")
	}
	handNear := false
	for _, m := range tr.marks {
		handNear = handNear || (u.born.T-renameT <= m.t && m.t <= u.born.T && dist(m.p, u.place) <= handR)
	}
	scores := map[int]lookPair{}
	for j, p := range mine {
		scores[keys[j].row] = p
	}
	type ranked struct {
		k float64
		i int
	}
	var withScore, without []ranked
	for _, z := range tr.stones {
		if z == s || z.addedNow || !(z.leftNow || (z.status == TrayStoneOn && !z.sure)) ||
			!compatible(z.size, s.size) || dist(z.place, u.place) > leaveD {
			continue
		}
		if p, ok := scores[z.index]; ok && p.ok {
			withScore = append(withScore, ranked{-float64(p.sc), z.index})
		} else {
			without = append(without, ranked{dist(z.place, u.place), z.index})
		}
	}
	byRank := func(a, b ranked) int { return cmp.Or(cmp.Compare(a.k, b.k), cmp.Compare(a.i, b.i)) }
	slices.SortFunc(withScore, byRank)
	slices.SortFunc(without, byRank)
	var cands []TrayCandidate
	for _, r := range append(withScore, without...) {
		p, ok := scores[r.i]
		cands = append(cands, TrayCandidate{Kind: "stone", Stone: tr.stones[r.i].id, Score: scoreOf(p, ok)})
	}
	cands = append(cands, TrayCandidate{Kind: "new"})
	_, atArrival := tr.idsAtArrival[id]
	return TrayOpen{Stone: s.id, WorldID: Int64(id), Kind: TrayAdded, Cause: cause, Look: look, Score: score,
		HandNear: Bool(handNear), SeenAtArrival: Bool(atArrival), Candidates: cands}
}

// closeTray makes the open tray leave: its final account (section 4.6.7), then it goes to onClose.
func (l *trayLedger) closeTray(out *[]TrayFact) { l.closeAt(out, nil, nil) }

func (l *trayLedger) closeAt(out *[]TrayFact, boundary *TrayPoint, lifted []*trayStone) {
	tr := l.tray
	if tr == nil {
		return
	}
	l.tray = nil
	if tr.arrived == nil || tr.last == nil { // it never arrived
		l.onClose(l.view(tr))
		return
	}
	tr.finishBlinks()
	tw := tr.last
	leave := tw.ref
	leaveWorld := tw
	var fallback *visitPoint
	if !tr.worked && tr.visit.olderCalm(tr.calmRef) {
		fallback = tr.visit.fallback
	}
	if fallback != nil {
		leaveWorld, leave = fallback.world, fallback.world.ref
	} else if tr.calmRef != nil && !tr.worked {
		leave = *tr.calmRef
		leaveWorld = tr.calmWorld
	}
	tr.leaving = &leave
	if boundary != nil {
		tr.leaving = boundary
	}
	before := make(map[*trayStone]int64, len(tr.link))
	for id, s := range tr.link {
		before[s] = id
	}
	for _, s := range tr.stones { // its place at the leaving point, if it was seen there (4.7), before the last settle
		s.leftPlace = nil
		if fallback != nil {
			if p, ok := fallback.places[s]; ok && s.linked && s.world == p.id {
				s.leftPlace = &p.place
			}
		} else if tr.calmRef != nil && !tr.worked {
			s.leftPlace = s.calmPlace
		} else {
			s.leftPlace = tw.observedPlace(s)
		}
	}
	// what is unidentified is in the last world (each frame drops the rest): a guard for the pairing; and a stone first
	// seen after the leaving point is not on the tray that leaves (the next tray's, as it comes in)
	maps.DeleteFunc(tr.unid, func(id int64, u *trayUnid) bool { return tw.byID[id] == nil || u.born.G > leave.G })
	tr.leaveWorld = leaveWorld
	l.settle(tr, tw, leave, true, out)
	var on, sure, removed, under int64
	var covered []*trayStone
	for _, s := range tr.stones {
		if s.status == TrayStoneOn {
			// The final settle can add a stone or change its world ID. Read that ID at the
			// leaving point, which can precede the last world used for matching.
			if id, ok := before[s]; !ok || id != s.world {
				s.leftPlace = leaveWorld.observedPlace(s)
			}
			on++
			if s.underSheet {
				under++
				covered = append(covered, s)
			}
			if s.sure {
				sure++
			}
		}
		if s.status == TrayStoneRemoved && s.certain != nil && *s.certain {
			removed++
		}
		s.evidence = stoneEvidence(tr, s) // what the leaving's speckle answer found of it (4.6.13)
	}
	var tid *int64
	if tr.took {
		tid = Int64(tidOf(tr))
	}
	l.c.UnderSheet += under
	l.sheetLaid(tr, covered, *tr.leaving, out)
	l.sheetFact(tr, lifted, "sheet_lifted", *tr.leaving, out)
	l.fact(TrayLeaving, tr, nil, *tr.leaving, TrayFact{UnderSheet: under, Stones: on, Sure: sure, Added: tr.added, Removed: removed,
		Unclear: tr.unclear, Open: append([]TrayOpen{}, tr.open...), TID: tid}, out)
	l.lastClosed = tr
	l.onClose(l.view(tr))
}
