package pdiggerfactory

// The tray rules by look (sections 4.6.3-4.6.10 of the SDK design) beyond the golden files: the self-check finds each
// broken code on accounts made broken on purpose, the open account and a live question as values, and the permille
// text. The rules themselves are golden-tested over trays-looks-synthetic (32 trays, every rule by look) and the
// real excerpts.

import (
	"context"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var looksPath = filepath.Join("fixtures", "recordings", "trays-looks-synthetic.ndjson.gz")

var (
	looksOnce  sync.Once
	looksTrays []Tray
	looksErr   error
)

// leftTrays are the trays of trays-looks-synthetic, read once; the tests change copies only.
func leftTrays(t *testing.T) []Tray {
	t.Helper()
	looksOnce.Do(func() {
		ctx := context.Background()
		r, err := OpenReplay(ctx, looksPath, nil)
		if err != nil {
			looksErr = err
			return
		}
		defer r.Close()
		looksTrays, looksErr = r.Trays(ctx)
	})
	if looksErr != nil {
		t.Fatal(looksErr)
	}
	return looksTrays
}

// trayN is a copy of tray n whose stones, facts and open account a test may change.
func trayN(t *testing.T, n int64) Tray {
	t.Helper()
	for _, x := range leftTrays(t) {
		if x.N == n {
			x.Stones, x.Facts, x.Open = slices.Clone(x.Stones), slices.Clone(x.Facts), slices.Clone(x.Open)
			return x
		}
	}
	t.Fatalf("no tray %d", n)
	return Tray{}
}

// stoneOf is the index of the tray stone whose id ends with that world id ("...-13-301").
func stoneOf(t *testing.T, tr Tray, wid int64) int {
	t.Helper()
	suffix := "-" + strconv.FormatInt(tr.N, 10) + "-" + strconv.FormatInt(wid, 10)
	if i := slices.IndexFunc(tr.Stones, func(s TrayStone) bool { return strings.HasSuffix(s.ID, suffix) }); i >= 0 {
		return i
	}
	t.Fatalf("tray %d: no stone ...%s", tr.N, suffix)
	return -1
}

func withFacts(tr Tray, facts []TrayFact) []string {
	tr.Facts = facts
	return tr.Check()
}

func withStone(tr Tray, i int, change func(*TrayStone)) []string {
	tr.Stones = slices.Clone(tr.Stones)
	change(&tr.Stones[i])
	return tr.Check()
}

// withEntry is the check of the tray with one open entry of its leaving fact changed.
func withEntry(t *testing.T, tr Tray, stone string, change func(*TrayOpen)) []string {
	t.Helper()
	facts := slices.Clone(tr.Facts)
	leave := &facts[len(facts)-1]
	leave.Open = slices.Clone(leave.Open)
	i := slices.IndexFunc(leave.Open, func(o TrayOpen) bool { return o.Stone == stone })
	if i < 0 {
		t.Fatalf("no open entry of %s", stone)
	}
	change(&leave.Open[i])
	return withFacts(tr, facts)
}

func entryOf(t *testing.T, tr Tray, stone string) TrayOpen {
	t.Helper()
	i := slices.IndexFunc(tr.Open, func(o TrayOpen) bool { return o.Stone == stone })
	if i < 0 {
		t.Fatalf("no open entry of %s", stone)
	}
	return tr.Open[i]
}

// wantCode fails the test unless every check found code.
func wantCode(t *testing.T, code string, checks map[string][]string) {
	t.Helper()
	for name, got := range checks {
		if !slices.Contains(got, code) {
			t.Errorf("%s: %s not found in %v", name, code, got)
		}
	}
}

func TestEveryLeftTrayChecksClean(t *testing.T) {
	trays := leftTrays(t)
	want := make([]int64, 32) // the synthetic recording's trays 1 to 32
	for i := range want {
		want[i] = int64(i + 1)
	}
	if !slices.Equal(trayNumbers(trays), want) {
		t.Fatalf("trays %v", trayNumbers(trays))
	}
	for _, tr := range trays {
		if got := tr.Check(); got == nil || len(got) != 0 {
			t.Errorf("tray %d: %#v", tr.N, got)
		}
	}
}

// TestTheCertainFactsWithLooksAreTheFirstOfV1s: on each tray the same certain facts on the same frames, up to where
// the tray is relinked (4.6.7); the copy of the recording without any look is v1.
func TestTheCertainFactsWithLooksAreTheFirstOfV1s(t *testing.T) {
	ctx := context.Background()
	r, err := OpenReplay(ctx, filepath.Join("fixtures", "recordings", "trays-looks-synthetic-nolooks.ndjson.gz"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	bare, err := r.Trays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	certain := func(trays []Tray) map[int64][]string {
		m := map[int64][]string{}
		for _, tr := range trays {
			m[tr.N] = []string{}
			for _, f := range tr.Facts {
				if f.Certain != nil && *f.Certain {
					m[tr.N] = append(m[tr.N], f.Kind+" "+f.Stone+" "+strconv.FormatInt(f.G, 10)+" "+f.Code)
				}
			}
		}
		return m
	}
	looks, v1 := certain(leftTrays(t)), certain(bare)
	if len(looks) != len(v1) {
		t.Fatalf("%d trays with looks, %d without", len(looks), len(v1))
	}
	held := map[int64]int{}
	for n, mine := range looks {
		theirs, ok := v1[n]
		if !ok || len(mine) > len(theirs) || !slices.Equal(mine, theirs[:len(mine)]) {
			t.Errorf("tray %d: %v, v1 %v", n, mine, theirs)
			continue
		}
		if len(theirs) > len(mine) {
			held[n] = len(theirs) - len(mine)
		}
	}
	// tray 2 a swap, 10 a stone linked by its look, 30 twins, 31 a look that changed (no hand took 15's X: v1
	// leaves its loss unclear too)
	if want := map[int64]int{2: 1, 10: 1, 30: 3, 31: 1}; !reflect.DeepEqual(held, want) {
		t.Errorf("held back %v, want %v", held, want)
	}
}

func TestCheckC1TheFactsRunFromArrivedToLeaving(t *testing.T) {
	tr := trayN(t, 13)
	never := Tray{Session: tr.Session, Video: tr.Video, N: 99}
	if got := never.Check(); got == nil || len(got) != 0 {
		t.Errorf("a tray that never arrived: %#v", got)
	}
	n := len(tr.Facts)
	wantCode(t, "C1", map[string][]string{
		"not left yet":             withFacts(tr, tr.Facts[:n-1:n-1]),
		"no facts":                 withFacts(tr, nil),
		"a fact after the leaving": withFacts(tr, slices.Concat(tr.Facts, tr.Facts[n-1:])),
		"no arrival first":         withFacts(tr, tr.Facts[1:]),
	})
}

func TestCheckC2IDsAreUniqueAndEveryFactNamesAStoneOfTheTray(t *testing.T) {
	tr := trayN(t, 13)
	twice := tr
	twice.Stones = slices.Concat(tr.Stones, tr.Stones[:1])
	facts := slices.Clone(tr.Facts)
	facts[slices.IndexFunc(facts, func(x TrayFact) bool { return x.Stone != "" })].Stone = "nobody"
	wantCode(t, "C2", map[string][]string{"a stone twice": twice.Check(), "no such stone": withFacts(tr, facts)})
}

func TestCheckC3EachStonesFactsComeInOrder(t *testing.T) {
	tr := trayN(t, 13)
	pushed := tr.Stones[stoneOf(t, tr, 301)] // unsure (pushed) at the leaving
	i := slices.IndexFunc(tr.Facts, func(x TrayFact) bool { return x.Kind == TrayUnsure && x.Stone == pushed.ID })
	after := func(xs ...TrayFact) []TrayFact { return slices.Concat(tr.Facts[:i+1], xs, tr.Facts[i+1:]) }
	unsure := tr.Facts[i]
	again, added, found, gone := unsure, unsure, unsure, unsure
	again.Code = "pushed"
	added.Kind, added.Certain = TrayAdded, Bool(false)
	found.Kind, found.Certain, found.Score, found.Bar = TrayIdentified, nil, Int64(900), Int64(750)
	gone.Kind, gone.Certain = TrayRemoved, Bool(false)
	ten := trayN(t, 10)
	j := slices.IndexFunc(ten.Facts, func(x TrayFact) bool { return x.Kind == TrayRemoved })
	late := ten.Facts[j]
	late.Kind, late.Certain, late.Score, late.Bar = TrayIdentified, nil, Int64(900), Int64(750)
	wantCode(t, "C3", map[string][]string{
		"two unsure facts":                 withFacts(tr, after(again)),
		"an addition after its first fact": withFacts(tr, after(added)),
		"identified after unsure":          withFacts(tr, after(found)),
		"two removals, a fact after one":   withFacts(tr, after(gone, gone)),
		"identified after removed": withFacts(ten, slices.Concat(ten.Facts[:j+1], []TrayFact{late},
			ten.Facts[j+1:])),
	})
}

func TestCheckC4TheFinalStateMatchesTheFacts(t *testing.T) {
	tr := trayN(t, 13)
	i := stoneOf(t, tr, 292)
	if s := tr.Stones[i]; s.Status != TrayStoneOn || !s.Sure {
		t.Fatalf("292: %+v", s)
	}
	ten := trayN(t, 10)
	gone := slices.IndexFunc(ten.Stones, func(s TrayStone) bool { return s.Status == TrayStoneRemoved })
	question := &TrayQuestion{Cause: "pushed", Why: "it was pushed", Since: 120, Opened: TrayPoint{G: 119, T: 23.8}}
	wantCode(t, "C4", map[string][]string{
		"removed with no removal":                withStone(tr, i, func(s *TrayStone) { s.Status, s.WorldID = TrayStoneRemoved, nil }),
		"not sure with no unsure fact":           withStone(tr, i, func(s *TrayStone) { s.Sure = false }),
		"a question still open":                  withStone(tr, i, func(s *TrayStone) { s.Question = question }),
		"the removal unclear, the stone certain": withStone(ten, gone, func(s *TrayStone) { s.Certain = Bool(true) }),
		"the removed stone without certainty":    withStone(ten, gone, func(s *TrayStone) { s.Certain = nil }),
	})
}

func TestCheckC5TheCountsAndTheOpenList(t *testing.T) {
	tr := trayN(t, 13)
	n := len(tr.Facts)
	arrived, counted, unlisted := slices.Clone(tr.Facts), slices.Clone(tr.Facts), slices.Clone(tr.Facts)
	arrived[0].Stones++
	counted[n-1].Sure++
	unlisted[n-1].Open = nil
	wantCode(t, "C5", map[string][]string{
		"the arrival count": withFacts(tr, arrived),
		"the sure count":    withFacts(tr, counted),
		"no open list":      withFacts(tr, unlisted),
	})
	// one entry per open stone, in tray order
	open := tr.Facts[n-1].Open
	if len(open) < 2 || len(tr.Check()) != 0 {
		t.Fatal(len(open), tr.Check())
	}
	twice, swapped := slices.Clone(tr.Facts), slices.Clone(tr.Facts)
	twice[n-1].Open = slices.Concat(open, open[:1])
	swapped[n-1].Open = slices.Concat(open[1:2], open[:1], open[2:])
	for name, facts := range map[string][]TrayFact{"an entry twice": twice, "out of tray order": swapped} {
		if got := withFacts(tr, facts); !slices.Equal(got, []string{"C5"}) {
			t.Errorf("%s: %v", name, got)
		}
	}
	// an id on the tray twice (C2): each stone is open or settled by itself (4.6.8), so an open copy of a sure stone
	// needs its own entry
	sure := tr.Stones[stoneOf(t, tr, 291)]
	if sure.Status != TrayStoneOn || !sure.Sure {
		t.Fatalf("%+v", sure)
	}
	copied, no, wid := sure, false, int64(99999)
	copied.Sure, copied.Added, copied.Certain, copied.WorldID = false, true, &no, &wid
	twin := tr
	twin.Stones = append(slices.Clone(tr.Stones), copied)
	added := open[slices.IndexFunc(open, func(o TrayOpen) bool { return o.Kind == TrayAdded })]
	added.Stone = sure.ID
	listed, unlisted := slices.Clone(tr.Facts), slices.Clone(tr.Facts)
	listed[n-1].Stones++
	listed[n-1].Open = append(slices.Clone(open), added)
	unlisted[n-1].Stones++
	if got := withFacts(twin, listed); !slices.Equal(got, []string{"C2"}) {
		t.Errorf("an open copy, listed: %v", got)
	}
	if got := withFacts(twin, unlisted); !slices.Equal(got, []string{"C2", "C5"}) {
		t.Errorf("an open copy, not listed: %v", got)
	}
}

func TestCheckC6WorldIDsAreUniqueOnTheTray(t *testing.T) {
	tr := trayN(t, 13)
	a, b := stoneOf(t, tr, 292), stoneOf(t, tr, 294)
	wantCode(t, "C6", map[string][]string{
		"one world id twice": withStone(tr, a, func(s *TrayStone) { s.WorldID = tr.Stones[b].WorldID }),
		"no world id":        withStone(tr, a, func(s *TrayStone) { s.WorldID = nil }),
	})
}

func TestCheckC7TheOpenEntriesAreWellFormed(t *testing.T) {
	tr := trayN(t, 13)
	unsure, added := tr.Stones[stoneOf(t, tr, 301)].ID, tr.Stones[stoneOf(t, tr, 311)].ID
	cands := entryOf(t, tr, unsure).Candidates
	addedCands := entryOf(t, tr, added).Candidates
	ten := trayN(t, 10)
	removed := ten.Stones[slices.IndexFunc(ten.Stones, func(s TrayStone) bool { return s.Status == TrayStoneRemoved })].ID
	removedCands := entryOf(t, ten, removed).Candidates
	set := func(c []TrayCandidate) func(*TrayOpen) { return func(o *TrayOpen) { o.Candidates = c } }
	notGone := slices.DeleteFunc(slices.Clone(cands), func(c TrayCandidate) bool { return c.Kind == "gone" })
	wantCode(t, "C7", map[string][]string{
		"no such cause":                 withEntry(t, tr, unsure, func(o *TrayOpen) { o.Cause = "no such cause" }),
		"no such look":                  withEntry(t, tr, unsure, func(o *TrayOpen) { o.Look = "no such look" }),
		"no candidates":                 withEntry(t, tr, unsure, set(nil)),
		"a candidate twice":             withEntry(t, tr, unsure, set(slices.Concat(cands, cands[:1]))),
		"its own world stone not first": withEntry(t, tr, unsure, set(cands[1:])),
		"its world id gone":             withEntry(t, tr, unsure, func(o *TrayOpen) { o.WorldID = nil }),
		"no gone candidate":             withEntry(t, tr, unsure, set(notGone)),
		"no new last":                   withEntry(t, tr, added, set(addedCands[:len(addedCands)-1])),
		"no unseen last":                withEntry(t, ten, removed, set(removedCands[:len(removedCands)-1])),
	})
}

func TestCheckC8ACandidateIsNeverASettledStone(t *testing.T) {
	tr := trayN(t, 13)
	added := entryOf(t, tr, tr.Stones[stoneOf(t, tr, 311)].ID)
	sure := tr.Stones[stoneOf(t, tr, 292)]
	settled := TrayCandidate{Kind: "stone", Stone: sure.ID}
	taken := TrayCandidate{Kind: "world", WorldID: sure.WorldID, Stone: sure.ID}
	first := func(c TrayCandidate) func(*TrayOpen) {
		return func(o *TrayOpen) { o.Candidates = slices.Concat([]TrayCandidate{c}, added.Candidates) }
	}
	wantCode(t, "C8", map[string][]string{
		"a settled stone":         withEntry(t, tr, added.Stone, first(settled)),
		"a sure stone's world id": withEntry(t, tr, added.Stone, first(taken)),
	})
	// an entry with a bad cause breaks C7, and its candidate naming a sure stone C8 as well: each broken code
	got := withEntry(t, tr, added.Stone, func(o *TrayOpen) {
		o.Cause = "no such cause"
		o.Candidates = slices.Concat([]TrayCandidate{settled}, added.Candidates)
	})
	if !slices.Equal(got, []string{"C7", "C8"}) {
		t.Fatal(got)
	}
}

func TestCheckC9AnIdentifiedFactClearedItsBar(t *testing.T) {
	tr := trayN(t, 12)
	i := slices.IndexFunc(tr.Facts, func(x TrayFact) bool { return x.Kind == TrayIdentified })
	found := tr.Facts[i]
	if found.Score == nil || found.Bar == nil {
		t.Fatalf("identified: %+v", found)
	}
	checks := map[string][]string{}
	for name, change := range map[string]func(*TrayFact){
		"below its bar":      func(x *TrayFact) { x.Score = Int64(*found.Bar - 1) },
		"a bar below 0.750":  func(x *TrayFact) { x.Bar, x.Score = Int64(700), Int64(720) },
		"certain":            func(x *TrayFact) { x.Certain = Bool(true) },
		"a margin below 150": func(x *TrayFact) { x.Margin = Int64(149) },
		"no score":           func(x *TrayFact) { x.Score = nil },
	} {
		facts := slices.Clone(tr.Facts)
		change(&facts[i])
		checks[name] = withFacts(tr, facts)
	}
	wantCode(t, "C9", checks)
}

func TestTheOpenAccountAsValues(t *testing.T) {
	tr := trayN(t, 13)
	if tr.TID == nil || *tr.TID != 750 {
		t.Fatalf("bar %v", tr.TID)
	}
	tie := entryOf(t, tr, tr.Stones[stoneOf(t, tr, 311)].ID)
	pushed := tr.Stones[stoneOf(t, tr, 301)].ID
	if tie.Kind != TrayAdded || tie.Cause != "doubtful" || tie.Look != "tie" || tie.Score == nil ||
		!sameID(tie.WorldID, Int64(311)) || !isTrue(tie.HandNear) || tie.SeenAtArrival == nil || *tie.SeenAtArrival {
		t.Errorf("the tie: %+v", tie)
	}
	if c := tie.Candidates[0]; c.Kind != "stone" || c.Stone != pushed || c.WorldID != nil || *c.Score != *tie.Score {
		t.Errorf("its first candidate: %+v", c)
	}
	if c := tie.Candidates[len(tie.Candidates)-1]; !reflect.DeepEqual(c, TrayCandidate{Kind: "new"}) {
		t.Errorf("its last candidate: %+v", c)
	}
	if !reflect.DeepEqual(tr.Facts[len(tr.Facts)-1].Open, tr.Open) || tr.Facts[len(tr.Facts)-1].TID == nil {
		t.Error("the leaving fact's open account is not the tray's")
	}
	if s := tr.Stones[stoneOf(t, tr, 311)]; s.Open == nil || !reflect.DeepEqual(*s.Open, tie) {
		t.Errorf("the stone's open entry: %+v", s.Open)
	}
	if got := (TrayCandidate{Kind: "gone", Why: "hand"}).Describe(); got != "gone (hand)" {
		t.Error(got)
	}
}

// TestAQuestionWhileItsTrayIsOn: a stone in question is not sure while its tray is on, and holds its question; the live
// tray breaks C1 (it has not left) and C4 (a question is open) until it leaves.
func TestAQuestionWhileItsTrayIsOn(t *testing.T) {
	ctx := testCtx(t)
	r, err := OpenReplay(ctx, looksPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	core := NewInterpreterCore()
	for item, err := range r.Items(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		it, ok := item.(*FrameItem)
		if !ok {
			continue
		}
		core.Feed(it.Frame)
		trays := core.Trays()
		if len(trays) == 0 {
			continue
		}
		live := trays[len(trays)-1]
		i := slices.IndexFunc(live.Stones, func(s TrayStone) bool { return s.Question != nil })
		if i < 0 || live.Leaving != nil {
			continue
		}
		s := live.Stones[i]
		if s.Sure || !slices.Contains([]string{"pushed", "neighbour_pushed", "found_again"}, s.Question.Cause) {
			t.Errorf("in question: %+v %+v", s, *s.Question)
		}
		if got := live.Check(); !slices.Contains(got, "C1") || !slices.Contains(got, "C4") {
			t.Errorf("the live tray checks %v", got)
		}
		return
	}
	t.Fatal("no question while a tray was on")
}

func TestPermilleTextAndTheSmallHelpers(t *testing.T) {
	for p, want := range map[int64]string{853: "0.853", 1000: "1.000", -5: "-0.005", -1000: "-1.000", 0: "0.000"} {
		if got := milli(p); got != want {
			t.Errorf("milli(%d) = %q, want %q", p, got, want)
		}
	}
	if _, _, ok := LookPairing(nil, nil); ok {
		t.Error("two missing looks paired")
	}
	if !sameBool(nil, nil) || sameBool(nil, Bool(false)) || !sameID(nil, nil) || sameID(Int64(1), nil) {
		t.Error("sameBool or sameID on nil")
	}
	s := &trayStone{}
	if got := sinceOf(s, TrayPoint{G: 5}); got != 5 {
		t.Errorf("an undisturbed stone: since %d", got)
	}
	s.disturb(7)
	if got := sinceOf(s, TrayPoint{G: 9}); got != 8 {
		t.Errorf("a stone disturbed at 7: since %d", got)
	}
}
