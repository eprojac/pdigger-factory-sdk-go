package pdiggerfactory

// Presence questions (section 4.6.1 of the SDK design) beyond the golden files: every tray of the four presence
// recordings ends as its generator says; a stone held in question reads as one and is sure again once it is seen; a
// settle at once holds only if the next calm moment shows the stone; a stone with a look chain is never held in
// question; held stones, hand deletes, revisions, a crowd of stones of its size and another stone's look keep a
// question from settling; the leaving counts its own frame again when something could have changed the stone; and the
// frames the tray rules can not read (the core's Gap, a replay's gap and reset lines, a live stream's GapItem and
// ResetItem) end the questions open then. The rules themselves are golden-tested over trays-presence-synthetic (35
// trays), trays-presence-looks-synthetic (8 trays, with made-up looks), trays-presence-attacks-synthetic (28 trays) and
// trays-presence-attacks-looks-synthetic (7 trays, with made-up looks). The rules that no fixture tray decides alone
// are tested on edited copies of these recordings in trays_presence_edits_test.go.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

const (
	presPlain       = "trays-presence-synthetic"
	presLooks       = "trays-presence-looks-synthetic"
	presAttacks     = "trays-presence-attacks-synthetic"
	presAttackLooks = "trays-presence-attacks-looks-synthetic"
)

// presEnd is a tray as its generator writes it: its name there, the world ids of the stones that end sure (the
// leaving counts exactly these), of those that end not sure, and of those among them whose unsure fact ends a question
// ("open since", Opened set); every other stone that ends not sure gets its fact at its doubt.
type presEnd struct {
	name                string
	sure, notSure, held []int64
}

func wids(ids ...int64) []int64 { return ids }

var presEnds = map[string]map[int64]presEnd{
	presPlain: {
		1:  {"CTRL", wids(101, 102, 103), nil, nil},
		2:  {"MOT1", wids(202, 203), wids(201), wids(201)},
		3:  {"MOT2", wids(302, 303), wids(301), wids(301)},
		4:  {"MOT3", wids(402, 403), wids(401), nil},
		5:  {"NOW1", wids(502, 503), wids(501), wids(501)},
		6:  {"GAP1", wids(602, 603), wids(601), wids(601)},
		7:  {"REV1", wids(702, 703), wids(701), wids(701)},
		8:  {"DROP1", wids(802, 803), wids(801, 804), nil},
		9:  {"PSAME", wids(902), wids(901, 903), wids(901)},
		10: {"DROP2", wids(1002, 1003), wids(1001, 1004), nil},
		11: {"SIDE1", wids(1102, 1103), wids(1101), wids(1101)},
		12: {"T15", wids(1122, 1123), wids(1121), wids(1121)},
		13: {"T16", wids(1132), wids(1131), wids(1131)},
		14: {"T17", wids(1143), wids(1141, 1142), wids(1141)},
		15: {"Q1", wids(1501, 1502, 1503), nil, nil},
		16: {"Q2", wids(1602, 1603), wids(1601), nil},
		17: {"Q3", wids(1702, 1703, 1704), wids(1701), nil},
		18: {"QS", wids(1802, 1803), wids(1801, 1804), nil},
		19: {"T13", wids(1903), wids(1901, 1902), nil},
		20: {"T14", wids(2002), wids(2001), nil},
		21: {"QP", wids(2102, 2103), wids(2101), nil},
		22: {"PNEIGH", wids(2203), wids(2201, 2202), wids(2201)},
		23: {"LEAVE1", wids(2301, 2302, 2303), nil, nil},
		24: {"NOARR", wids(2401, 2402, 2403), nil, nil},
		25: {"PUSHX", wids(2502, 2503), wids(2501), wids(2501)},
		26: {"ORDER", wids(2603), wids(2601, 2602), wids(2602)},
		27: {"MINT", wids(2701, 2702), wids(2707), nil},
		28: {"REVPUSH", wids(2802, 2803), wids(2801), wids(2801)},
		29: {"REVSAME", wids(2902, 2903), wids(2901), wids(2901)},
		30: {"CROWD", wids(3002, 3003), wids(3001), wids(3001)},
		31: {"ONCELOST", wids(3102, 3103), wids(3101), nil},
		32: {"ONCETOUCH", wids(3202, 3203), wids(3201), nil},
		33: {"CROWD1", wids(3302, 3303), wids(3301), wids(3301)},
		34: {"LVHAND", wids(3402, 3403), wids(3401), nil},
		35: {"SETTLE3", wids(3501, 3502, 3503), nil, nil},
	},
	presLooks: {
		1: {"LK1", wids(102, 103), wids(101), wids(101)},
		2: {"LK2", wids(202, 203), wids(201), wids(201)},
		3: {"LK0", wids(302, 303), wids(301), wids(301)},
		4: {"QE", wids(402, 403), wids(401), wids(401)},
		5: {"QL1", wids(501, 502, 503), nil, nil},
		6: {"QL2", wids(602, 603), wids(601), wids(601)},
		7: {"LKNOCH", wids(702, 703), wids(701, 704), wids(701, 704)},
		8: {"LKNOCH0", wids(801, 802, 803), wids(804), wids(804)},
	},
	presAttacks: {
		1:  {"CTRL", wids(101, 102, 103), nil, nil},
		2:  {"NHELD", wids(202, 203, 204), wids(201), wids(201)},
		3:  {"NHELDF", wids(302, 303), wids(301), wids(301)},
		4:  {"XHELDD", wids(402, 403), wids(401), wids(401)},
		5:  {"DELHF", wids(503), wids(501), wids(501)},
		6:  {"DELCA", wids(602, 603), wids(601), wids(601)},
		7:  {"DELHN", wids(702, 703), wids(701), wids(701)},
		8:  {"REVHAND", wids(802, 803), wids(801), wids(801)},
		9:  {"REVUNTR", wids(902, 903), wids(901), wids(901)},
		10: {"REVHELD", wids(1002, 1003), wids(1001), wids(1001)},
		11: {"REVLATE", wids(1101, 1102, 1103), nil, nil},
		12: {"REVMID", wids(1202, 1203), wids(1201), wids(1201)},
		13: {"MOTFS", wids(1302, 1303), wids(1301), wids(1301)},
		14: {"MOTS1", wids(1402, 1403), wids(1401), wids(1401)},
		15: {"ONCEMOT", wids(1502, 1503), wids(1501), nil},
		16: {"ONCEBUSY", wids(1602, 1603), wids(1601), nil},
		17: {"ONCEGAP", wids(1702, 1703), wids(1701), nil},
		18: {"GAPC2", wids(1802, 1803), wids(1801), nil},
		19: {"RENSWAP", wids(1902, 1903), wids(1901, 1904), wids(1901, 1904)},
		20: {"SMALLSW", wids(2002, 2003), wids(2001, 2004), wids(2001, 2004)},
		21: {"SMALLSWD", wids(2101, 2102, 2103, 2104), nil, nil},
		22: {"PUSHX1", wids(2202, 2203), wids(2201), wids(2201)},
		23: {"TWOQ", wids(2302, 2303), wids(2301, 2304), wids(2301)},
		24: {"NEWNEXT", wids(2401, 2402, 2403), nil, nil},
		25: {"LVBUSY", wids(2502, 2503), wids(2501), wids(2501)},
		26: {"LVHOLE", wids(2602, 2603), wids(2601), wids(2601)},
		27: {"LVUNTR", wids(2702, 2703), wids(2701), wids(2701)},
		28: {"LVPUSH", wids(2802), wids(2801, 2803), wids(2801)},
	},
	presAttackLooks: {
		1: {"LKSET", wids(102, 103), wids(101), wids(101)},
		2: {"LKONCE", wids(202, 203), wids(201), wids(201)},
		3: {"LKLIFT", wids(302, 303), wids(301), wids(301)},
		4: {"LKSWAPZ", wids(402, 403, 404), wids(401), wids(401)},
		5: {"LKSWAPZ0", wids(502, 503, 504), wids(501), wids(501)},
		6: {"LKGREY", wids(602, 603), wids(601), wids(601)},
		7: {"LKSWAPZG", wids(702, 703, 704), wids(701), wids(701)},
	},
}

var (
	presMu       sync.Mutex
	presLeft     = map[string][]Tray{}
	presQuestion int // frames fed when tray 1's stone 101 of trays-presence-synthetic first held its question
)

// presTrays are the trays of a presence recording, replayed once; the tests read them only.
func presTrays(t *testing.T, name string) []Tray {
	t.Helper()
	presMu.Lock()
	defer presMu.Unlock()
	if trays, ok := presLeft[name]; ok {
		return trays
	}
	trays := replayTrays(t, recording(name))
	presLeft[name] = trays
	return trays
}

// replayTrays replays a recording (a path) and returns its trays.
func replayTrays(t *testing.T, path string) []Tray {
	t.Helper()
	ctx := testCtx(t)
	r, err := OpenReplay(ctx, path, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	trays, err := r.Trays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return trays
}

func presTray(t *testing.T, name string, n int64) Tray {
	t.Helper()
	for _, tr := range presTrays(t, name) {
		if tr.N == n {
			return tr
		}
	}
	t.Fatalf("%s: no tray %d", name, n)
	return Tray{}
}

// presStone is the tray stone first seen under that world id ("...-1-101").
func presStone(t *testing.T, tr Tray, wid int64) TrayStone {
	t.Helper()
	return tr.Stones[stoneOf(t, tr, wid)]
}

// presUnsure are the tray's unsure facts by the world id their stone was first seen under, and those ids in fact order.
func presUnsure(tr Tray) (map[int64]TrayFact, []int64) {
	facts, order := map[int64]TrayFact{}, []int64{}
	for _, f := range tr.Facts {
		if f.Kind != TrayUnsure || f.Stone == "" {
			continue
		}
		wid, _ := strconv.ParseInt(strings.Split(f.Stone, "-")[2], 10, 64)
		facts[wid] = f
		order = append(order, wid)
	}
	return facts, order
}

// presLines are the lines of a recording (a path: a fixture, gzipped, or an edited copy), without their newlines.
func presLines(t *testing.T, path string) [][]byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var in io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		z, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		in = z
	}
	var lines [][]byte
	r := bufio.NewReader(in)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			lines = append(lines, bytes.TrimSuffix(line, []byte("\n")))
		}
		if errors.Is(err, io.EOF) {
			return lines
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// presItems are the recording's lines read as a replay reads them (the header and the lines a reader ignores left out).
func presItems(t *testing.T, name string) []StreamItem {
	t.Helper()
	return presItemsAt(t, recording(name))
}

// presItemsAt are presItems of a recording at a path.
func presItemsAt(t *testing.T, path string) []StreamItem {
	t.Helper()
	var items []StreamItem
	for i, line := range presLines(t, path) {
		item, _, err := parseLine(line, i == 0)
		if err != nil {
			t.Fatal(err)
		}
		if item != nil {
			items = append(items, item)
		}
	}
	return items
}

// presFed is a core fed the recording as a replay feeds it (its clock lines, its gap lines, its frames), with Gap()
// after its gapAfter-th frame, then its end.
func presFed(t *testing.T, name string, gapAfter int) *InterpreterCore {
	t.Helper()
	core := NewInterpreterCore()
	n := 0
	for _, item := range presItems(t, name) {
		switch it := item.(type) {
		case *ClockItem:
			core.SetClock(it.clockEvent())
		case *GapItem:
			core.Gap()
		case *FrameItem:
			core.Feed(it.Frame)
			if n++; n == gapAfter {
				core.Gap()
			}
		}
	}
	core.End()
	return core
}

type presView struct {
	k     int // frames fed
	stone TrayStone
	tray  Tray
}

// presViews are frame by frame while tray n is on (a core fed the recording's frames only): how many frames were fed,
// its stone first seen under wid, the tray.
func presViews(t *testing.T, name string, n, wid int64) []presView {
	t.Helper()
	return presViewsAt(t, recording(name), n, wid)
}

// presViewsAt are presViews of a recording at a path.
func presViewsAt(t *testing.T, path string, n, wid int64) []presView {
	t.Helper()
	core := NewInterpreterCore()
	var out []presView
	k := 0
	for _, item := range presItemsAt(t, path) {
		fi, ok := item.(*FrameItem)
		if !ok {
			continue
		}
		k++
		u := core.Feed(fi.Frame)
		if u.State == nil || u.State.Tray == nil { // a duplicate changes nothing; no tray is open
			continue
		}
		if live := *u.State.Tray; live.N == n && live.Leaving == nil {
			out = append(out, presView{k, presStone(t, live, wid), live})
		}
	}
	return out
}

// presState is a stone's (sure, in question).
type presState [2]bool

var (
	presSure  = presState{true, false}
	presAsked = presState{false, true}
	presGiven = presState{false, false}
)

// presStates are the stone's (sure, in question) frame by frame while its tray is on, each change once.
func presStates(t *testing.T, name string, n, wid int64) []presState {
	t.Helper()
	return presStatesAt(t, recording(name), n, wid)
}

// presStatesAt are presStates of a recording at a path.
func presStatesAt(t *testing.T, path string, n, wid int64) []presState {
	t.Helper()
	var out []presState
	for _, v := range presViewsAt(t, path, n, wid) {
		st := presState{v.stone.Sure, v.stone.Question != nil}
		if len(out) == 0 || out[len(out)-1] != st {
			out = append(out, st)
		}
	}
	return out
}

// presInQuestion is how many frames of trays-presence-synthetic were fed when tray 1's stone 101 first held its
// question.
func presInQuestion(t *testing.T) int {
	t.Helper()
	presMu.Lock()
	k := presQuestion
	presMu.Unlock()
	if k > 0 {
		return k
	}
	for _, v := range presViews(t, presPlain, 1, 101) {
		if v.stone.Question != nil {
			presMu.Lock()
			presQuestion = v.k
			presMu.Unlock()
			return v.k
		}
	}
	t.Fatal("stone 101 of tray 1 never in question")
	return 0
}

// presAfterFirst is the facts of every tray but the first, as text.
func presAfterFirst(trays []Tray) string {
	var facts []TrayFact
	for _, tr := range trays[1:] {
		facts = append(facts, tr.Facts...)
	}
	return factsText(facts)
}

// presEndedUnseen is the stone's unsure fact, which must end its presence question of the doubt at 0:04.4 (tray 1's
// stone 101).
func presEndedUnseen(t *testing.T, tr Tray, wid int64) TrayFact {
	t.Helper()
	facts, _ := presUnsure(tr)
	f, ok := facts[wid]
	if !ok || f.Why != "not seen at two calm moments" || f.Code != "unseen" || f.Opened == nil ||
		FormatClock(f.Opened.T) != "0:04.4" || presStone(t, tr, wid).Sure {
		t.Fatalf("%+v (%v)", f, ok)
	}
	return f
}

// presObjects are the lines of a recording after its header, decoded with their numbers kept as written.
func presObjects(t *testing.T, name string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range presLines(t, recording(name))[1:] {
		var x map[string]any
		d := json.NewDecoder(bytes.NewReader(line))
		d.UseNumber()
		if err := d.Decode(&x); err != nil {
			t.Fatal(err)
		}
		out = append(out, x)
	}
	return out
}

// presEdited is a copy of the recording with every line after its header passed through change (nil drops the line).
func presEdited(t *testing.T, name string, change func(map[string]any) map[string]any) string {
	t.Helper()
	var b bytes.Buffer
	b.Write(append(presLines(t, recording(name))[0], '\n'))
	for _, x := range presObjects(t, name) {
		if x = change(x); x == nil {
			continue
		}
		out, err := json.Marshal(x)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(append(out, '\n'))
	}
	path := filepath.Join(t.TempDir(), name+".ndjson")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// presWithLine is a copy of trays-presence-synthetic with line after its after-th frame.
func presWithLine(t *testing.T, line string, after int) string {
	t.Helper()
	var b bytes.Buffer
	n := 0
	for i, l := range presLines(t, recording(presPlain)) {
		b.Write(append(l, '\n'))
		if item, _, _ := parseLine(l, i == 0); item != nil {
			if _, ok := item.(*FrameItem); ok {
				if n++; n == after {
					b.WriteString(line + "\n")
				}
			}
		}
	}
	path := filepath.Join(t.TempDir(), presPlain+".ndjson")
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// objects are the JSON objects of a list member of x (nil when it has none).
func objects(x map[string]any, keys ...string) []map[string]any {
	var v any = x
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	list, _ := v.([]any)
	var out []map[string]any
	for _, e := range list {
		if m, ok := e.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func numberIs(v any, n string) bool {
	x, ok := v.(json.Number)
	return ok && x.String() == n
}

func TestPresenceRecordingsHoldTheTraysOfTheirGenerator(t *testing.T) {
	for name, ends := range presEnds {
		var want []int64
		for n := range ends {
			want = append(want, n)
		}
		slices.Sort(want)
		trays := presTrays(t, name)
		if got := trayNumbers(trays); !slices.Equal(got, want) {
			t.Fatalf("%s: trays %v, want %v", name, got, want)
		}
		for _, tr := range trays {
			if bad := tr.Check(); len(bad) != 0 {
				t.Errorf("%s tray %d breaks %v", name, tr.N, bad)
			}
		}
	}
}

// TestPresenceEveryTrayEndsAsItsGeneratorSays: the stones that end sure are on the tray, sure, out of question and
// without an unsure fact, and the leaving counts exactly them; each of the others has one, which ends a question
// ("open since", Opened set) exactly where the stone was held in question, and else comes at its doubt.
func TestPresenceEveryTrayEndsAsItsGeneratorSays(t *testing.T) {
	for _, name := range []string{presPlain, presLooks, presAttacks, presAttackLooks} {
		for n, end := range presEnds[name] {
			t.Run(name+"-"+strconv.FormatInt(n, 10)+"-"+end.name, func(t *testing.T) {
				tr := presTray(t, name, n)
				facts, order := presUnsure(tr)
				slices.Sort(order)
				if want := slices.Sorted(slices.Values(end.notSure)); !slices.Equal(order, want) {
					t.Fatalf("unsure facts of %v, want %v", order, want)
				}
				for _, wid := range end.sure {
					if s := presStone(t, tr, wid); s.Status != TrayStoneOn || !s.Sure || s.Question != nil {
						t.Errorf("%d: %+v", wid, s)
					}
				}
				for _, wid := range end.notSure {
					if presStone(t, tr, wid).Sure {
						t.Errorf("%d is sure", wid)
					}
					if (facts[wid].Opened != nil) != slices.Contains(end.held, wid) {
						t.Errorf("%d: opened %v", wid, facts[wid].Opened)
					}
				}
				leaving := tr.Facts[len(tr.Facts)-1]
				if leaving.Kind != TrayLeaving || leaving.Sure != int64(len(end.sure)) {
					t.Fatalf("%+v", leaving)
				}
			})
		}
	}
}

// TestPresenceStoneHeldInQuestionIsNotSureUntilSeenAgain: tray 1's stone 101 is not seen at two calm moments while
// nothing could have changed it. It is in question (not sure, Question set, no fact) until the world sees it again on
// its place, and then sure again.
func TestPresenceStoneHeldInQuestionIsNotSureUntilSeenAgain(t *testing.T) {
	if got := presStates(t, presPlain, 1, 101); !slices.Equal(got, []presState{presSure, presAsked, presSure}) {
		t.Fatal(got)
	}
	i := slices.IndexFunc(presViews(t, presPlain, 1, 101), func(v presView) bool { return v.stone.Question != nil })
	v := presViews(t, presPlain, 1, 101)[i]
	q := v.stone.Question
	if q.Cause != "unseen" || q.Why != "not seen at two calm moments" || q.Since != 22 || q.Opened.G != 22 ||
		FormatClock(q.Opened.T) != "0:04.4" || v.stone.Sure {
		t.Fatalf("%+v", q)
	}
	if slices.ContainsFunc(v.tray.Facts, func(f TrayFact) bool { return f.Kind == TrayUnsure }) {
		t.Fatal(factsText(v.tray.Facts))
	}
	if bad := v.tray.Check(); !slices.Contains(bad, "C1") || !slices.Contains(bad, "C4") {
		t.Fatal(bad) // it has not left, and a question is open
	}
	one := presTrays(t, presPlain)[0]
	if facts, _ := presUnsure(one); !presStone(t, one, 101).Sure || len(facts) != 0 {
		t.Fatal(factsText(one.Facts))
	}
}

// TestPresenceSettledAtOnce: tray 15's stone 1501 is seen again on its place between the two calm moments that miss
// it, with nothing at work: the doubt settles at once, so the stone is never in question; its next unseen run, which
// starts at that calm moment, is on probation, and the calm moment that shows it again settles that run too: it stays
// sure. Tray 35: stone 3501's question is settled by a sight between two calm moments, and the second does not show
// it: sure again there, its next run starts there on probation, and the calm moment after settles that run too.
func TestPresenceSettledAtOnce(t *testing.T) {
	if got := presStates(t, presPlain, 15, 1501); !slices.Equal(got, []presState{presSure}) {
		t.Fatal(got)
	}
	if got := presStates(t, presPlain, 35, 3501); !slices.Equal(got, []presState{presSure, presAsked, presSure}) {
		t.Fatal(got)
	}
	for _, n := range []int64{15, 35} {
		if facts, _ := presUnsure(presTray(t, presPlain, n)); len(facts) != 0 {
			t.Fatal(n, facts)
		}
	}
}

// TestPresenceSettleAtOnceHoldsOnlyIfTheNextCalmMomentSettlesItsRun: the stone is settled at once (never in question)
// and is sure until the next calm moment that shows it; when its run on probation can not be settled there (the stone
// was lost meanwhile, its first sight was busy, the tray moved or hands worked while it was out of sight, frames were
// dropped), it gets its unsure fact there, a plain one, and the tray has not left yet.
func TestPresenceSettleAtOnceHoldsOnlyIfTheNextCalmMomentSettlesItsRun(t *testing.T) {
	for _, c := range []struct {
		why, name string
		n, wid    int64
	}{{"lost", presPlain, 31, 3101}, {"busy-first-sight", presPlain, 32, 3201}, {"motion", presAttacks, 15, 1501},
		{"hands", presAttacks, 16, 1601}, {"dropped-frames", presAttacks, 17, 1701}} {
		t.Run(c.why, func(t *testing.T) {
			if got := presStates(t, c.name, c.n, c.wid); !slices.Equal(got, []presState{presSure, presGiven}) {
				t.Fatal(got)
			}
			tr := presTray(t, c.name, c.n)
			facts, _ := presUnsure(tr)
			f := facts[c.wid]
			if f.Code != "unseen" || f.Why != "not seen at two calm moments" || f.Opened != nil || tr.Leaving == nil ||
				!(f.T < tr.Leaving.T) {
				t.Fatalf("%+v", f)
			}
		})
	}
}

// TestPresenceQuestionKeepsItsRun: tray 12's stone is seen at a calm moment after its question opened, then a hand works
// while it is out of sight: the question keeps the run it was opened with, which that hand spoils, so it ends at the
// next settle and not when the tray leaves. Tray 14: at the leaving's step 1 the question that ends comes first, then
// the doubt of the stone after it in tray order.
func TestPresenceQuestionKeepsItsRun(t *testing.T) {
	twelve := presTray(t, presPlain, 12)
	if facts, _ := presUnsure(twelve); twelve.Leaving == nil || !(facts[1121].T < twelve.Leaving.T) {
		t.Fatal(factsText(twelve.Facts))
	}
	if _, order := presUnsure(presTray(t, presPlain, 14)); !slices.Equal(order, []int64{1141, 1142}) {
		t.Fatal(order)
	}
}

// TestPresenceSettleGoesStoneByStone: tray 26's stone 2601, first in tray order, was pushed while stone 2602 was in
// question and out of sight; one settle gives both facts, 2601's first. Tray 25: stone 2501, in question, was pushed
// and is still unseen: its own later reason ends the question at that settle, with the question's cause and why, and
// not when the tray leaves.
func TestPresenceSettleGoesStoneByStone(t *testing.T) {
	facts, order := presUnsure(presTray(t, presPlain, 26))
	if !slices.Equal(order, []int64{2601, 2602}) || facts[2601].T != facts[2602].T || facts[2601].Code != "pushed" ||
		facts[2601].Opened != nil || facts[2602].Opened == nil {
		t.Fatal(order, facts)
	}
	tr := presTray(t, presPlain, 25)
	facts, _ = presUnsure(tr)
	if f := facts[2501]; f.Code != "unseen" || f.Why != "not seen at two calm moments" || f.Opened == nil ||
		tr.Leaving == nil || !(f.T < tr.Leaving.T) {
		t.Fatalf("%+v", f)
	}
}

// TestPresenceStoneAddedForCertain: tray 27's slab, which a hand set down, is added for certain, then not seen at two
// calm moments, seen once between them: its last sight is its placing, where the hand worked, so the doubt is not a
// presence question (a plain fact).
func TestPresenceStoneAddedForCertain(t *testing.T) {
	tr := presTray(t, presPlain, 27)
	var kinds []string
	for _, f := range tr.Facts {
		if strings.HasSuffix(f.Stone, "-27-2707") {
			kinds = append(kinds, f.Kind+" "+strconv.FormatBool(isTrue(f.Certain))+" "+strconv.FormatBool(f.Certain == nil))
		}
	}
	if !slices.Equal(kinds, []string{"added true false", "unsure false true"}) {
		t.Fatal(kinds)
	}
	if facts, _ := presUnsure(tr); facts[2707].Opened != nil {
		t.Fatalf("%+v", facts[2707])
	}
}

// TestPresenceCrowdKeepsTheQuestionOpen: trays 30 and 33: a small stone in question is seen again on its place, but
// another stone of its size stands within nearD of that place at the settle (tray 30) or at its first sight (tray
// 33): a sight can not tell the two apart, so the question is never settled and ends when the tray leaves.
func TestPresenceCrowdKeepsTheQuestionOpen(t *testing.T) {
	for _, c := range []struct{ n, wid int64 }{{30, 3001}, {33, 3301}} {
		if got := presStates(t, presPlain, c.n, c.wid); !slices.Equal(got, []presState{presSure, presAsked}) {
			t.Fatal(c.n, got)
		}
		tr := presTray(t, presPlain, c.n)
		facts, _ := presUnsure(tr)
		if f := facts[c.wid]; tr.Leaving == nil || f.Opened == nil || f.T != tr.Leaving.T {
			t.Fatalf("%d: %+v", c.n, f)
		}
	}
}

// TestPresenceLookOfAnotherOpenStone: looks tray 7's stone 701 (no chain) is in question; its world stone is seen
// again on its place with the face of stone 704, which is in question too: the look reaches the bar against 704's
// chain, so the sight does not settle 701, and its question ends at that settle. Looks tray 8: the same with a face
// that is not 804's (its chain scores low on it): 801 is settled, 804 stays in question until the tray leaves.
func TestPresenceLookOfAnotherOpenStone(t *testing.T) {
	if got := presStates(t, presLooks, 7, 701); !slices.Equal(got, []presState{presSure, presAsked, presGiven}) {
		t.Fatal(got)
	}
	tr := presTray(t, presLooks, 7)
	facts, _ := presUnsure(tr)
	if f := facts[701]; f.Opened == nil || tr.Leaving == nil || !(f.T < tr.Leaving.T) ||
		len(presStone(t, tr, 701).Looks) != 0 {
		t.Fatalf("%+v", f)
	}
	if got := presStates(t, presLooks, 8, 801); !slices.Equal(got, []presState{presSure, presAsked, presSure}) {
		t.Fatal(got)
	}
	eight := presTray(t, presLooks, 8)
	if facts, _ := presUnsure(eight); eight.Leaving == nil || facts[804].T != eight.Leaving.T {
		t.Fatal(factsText(eight.Facts))
	}
}

// TestPresenceChainStoneNeverHeld: a stone with a look chain that is not seen at two calm moments gets a question only
// a look settles: in question from that doubt on, it is not settled by being seen again on its place; its fact ends
// the question when the tray leaves. Looks tray 4: still unseen at the next calm moment, the question's Since grows to
// it.
func TestPresenceChainStoneNeverHeld(t *testing.T) {
	for _, c := range []struct {
		name   string
		n, wid int64
	}{{presLooks, 4, 401}, {presLooks, 3, 301}, {presAttackLooks, 4, 401}} {
		var seen []TrayQuestion
		for _, v := range presViews(t, c.name, c.n, c.wid) {
			if q := v.stone.Question; q != nil {
				seen = append(seen, *q)
			}
		}
		if len(seen) == 0 {
			t.Fatal(c, "never in question")
		}
		for _, q := range seen {
			if q.Cause != "unseen" || !reflect.DeepEqual(q.Opened, seen[0].Opened) {
				t.Fatal(c, q)
			}
		}
		if got := presStates(t, c.name, c.n, c.wid); !slices.Equal(got, []presState{presSure, presAsked}) {
			t.Fatal(c, got)
		}
		tr := presTray(t, c.name, c.n)
		facts, _ := presUnsure(tr)
		f := facts[c.wid]
		if len(presStone(t, tr, c.wid).Looks) == 0 || f.Opened == nil || f.Opened.G != seen[0].Opened.G ||
			tr.Leaving == nil || f.T != tr.Leaving.T {
			t.Fatalf("%v: %+v", c, f) // the views feed no clock lines: no wall there
		}
		if c.name == presLooks && c.n == 4 {
			if first, last := seen[0].Since, seen[len(seen)-1].Since; !(last > first) ||
				slices.ContainsFunc(seen, func(q TrayQuestion) bool { return q.Since != first && q.Since != last }) {
				t.Fatal(seen)
			}
		}
	}
}

// TestPresenceHeldOrTakenMakesABusyFrame: attack trays 2 and 6: while stone 201 is in question and out of sight, the
// brick next to it reads carry (tray 2), or the one next to 601 is deleted, why carried-away (tray 6), with both
// hands free and no hand event: busy frames, so neither question is settled. The same recording with that brick kept,
// and that delete why absent: both settled, every other tray the recording's.
func TestPresenceHeldOrTakenMakesABusyFrame(t *testing.T) {
	calm := func(x map[string]any) map[string]any {
		for _, s := range objects(x, "world", "stones") {
			if numberIs(s["id"], "204") {
				s["mode"] = "kept"
			}
		}
		for _, e := range objects(x, "world", "events") {
			if numberIs(e["id"], "604") && e["why"] == "carried-away" {
				e["why"] = "absent"
			}
		}
		return x
	}
	trays := replayTrays(t, presEdited(t, presAttacks, calm))
	for _, c := range []struct{ n, wid int64 }{{2, 201}, {6, 601}} {
		if presStone(t, presTray(t, presAttacks, c.n), c.wid).Sure {
			t.Fatal(c, "sure in the recording")
		}
		if facts, _ := presUnsure(trays[c.n-1]); !presStone(t, trays[c.n-1], c.wid).Sure || len(facts) != 0 {
			t.Fatal(c, factsText(trays[c.n-1].Facts))
		}
	}
	others := func(trays []Tray) string {
		var facts []TrayFact
		for _, tr := range trays {
			if tr.N != 2 && tr.N != 6 {
				facts = append(facts, tr.Facts...)
			}
		}
		return factsText(facts)
	}
	if others(trays) != others(presTrays(t, presAttacks)) {
		t.Fatal(firstLineDiff(others(trays), others(presTrays(t, presAttacks))))
	}
}

// firstLineDiff is the first line where two texts differ.
func firstLineDiff(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	for i := range min(len(g), len(w)) {
		if g[i] != w[i] {
			return "got " + g[i] + "\nwant " + w[i]
		}
	}
	return "one text is longer"
}

// TestPresenceEveryRevisionIsUnread: tray 29: a frame inside stone 2901's unseen run comes again as a revision that
// changes nothing: the next frame counts as one after frames the rules could not read, so the question ends ("open
// since"). Without that revision the stone is seen again and sure, and every other tray is the recording's.
func TestPresenceEveryRevisionIsUnread(t *testing.T) {
	if facts, _ := presUnsure(presTray(t, presPlain, 29)); facts[2901].Opened == nil {
		t.Fatalf("%+v", facts[2901])
	}
	var revision any // the frame g of the first world of rev 1 with stone 2901
	for _, x := range presObjects(t, presPlain) {
		w, _ := x["world"].(map[string]any)
		if w != nil && numberIs(w["rev"], "1") &&
			slices.ContainsFunc(objects(x, "world", "stones"), func(s map[string]any) bool { return numberIs(s["id"], "2901") }) {
			revision = x["g"]
			break
		}
	}
	if revision == nil {
		t.Fatal("no revision of a frame with stone 2901")
	}
	trays := replayTrays(t, presEdited(t, presPlain, func(x map[string]any) map[string]any {
		if w, _ := x["world"].(map[string]any); x["g"] == revision && w != nil && w["rev"] != nil && !numberIs(w["rev"], "0") {
			return nil // the revision
		}
		return x
	}))
	if facts, _ := presUnsure(trays[28]); !presStone(t, trays[28], 2901).Sure || len(facts) != 0 {
		t.Fatal(factsText(trays[28].Facts))
	}
	others := func(trays []Tray) string {
		var facts []TrayFact
		for _, tr := range trays {
			if tr.N != 29 {
				facts = append(facts, tr.Facts...)
			}
		}
		return factsText(facts)
	}
	if others(trays) != others(presTrays(t, presPlain)) {
		t.Fatal(firstLineDiff(others(trays), others(presTrays(t, presPlain))))
	}
}

// TestPresenceLeavingCountsItsOwnFrameAgain: attack trays 25-28: stone X is not detected at the last calm moment, whose
// frame is the tray's last, after hands worked, frames were dropped, a hand was flagged untrusted or a stone was pushed
// while it was out of sight: the leaving counts that frame again, so X's question opens and ends there ("open since"
// the leaving itself). Tray 23 of trays-presence-synthetic, where nothing happened: the frame is not counted twice,
// the stone stays sure. Tray 34: a hand lifted and set down stone 3401 while it was not detected, and the last calm
// moment, the tray's last frame, does not show it: the leaving counts that frame again, and the doubt there is no
// question (a hand handled it since its last sight): a plain fact at the leaving.
func TestPresenceLeavingCountsItsOwnFrameAgain(t *testing.T) {
	for _, n := range []int64{25, 26, 27, 28} { // hands, dropped frames, untrusted, push
		tr := presTray(t, presAttacks, n)
		facts, _ := presUnsure(tr)
		if f := facts[n*100+1]; tr.Leaving == nil || f.Opened == nil || !reflect.DeepEqual(*f.Opened, *tr.Leaving) ||
			f.T != tr.Leaving.T {
			t.Fatalf("%d: %+v", n, f)
		}
	}
	if facts, _ := presUnsure(presTray(t, presPlain, 23)); len(facts) != 0 {
		t.Fatal(facts)
	}
	tr := presTray(t, presPlain, 34)
	facts, _ := presUnsure(tr)
	if f := facts[3401]; tr.Leaving == nil || f.Code != "unseen" || f.Opened != nil || f.T != tr.Leaving.T {
		t.Fatalf("%+v", f)
	}
}

// TestPresenceCoreGapEndsTheQuestionsOpenThen: InterpreterCore.Gap while stone 101 of tray 1 is in question: the next
// frame comes after frames the rules could not read, so the question ends at the next settle with the fact unsure
// ("open since" its doubt). Without it the stone is seen again and sure; every other tray is the recording's.
func TestPresenceCoreGapEndsTheQuestionsOpenThen(t *testing.T) {
	trays := presFed(t, presPlain, presInQuestion(t)+2).Trays()
	presEndedUnseen(t, trays[0], 101)
	if _, order := presUnsure(trays[0]); !slices.Equal(order, []int64{101}) {
		t.Fatal(order)
	}
	if presAfterFirst(trays) != presAfterFirst(presTrays(t, presPlain)) {
		t.Fatal(firstLineDiff(presAfterFirst(trays), presAfterFirst(presTrays(t, presPlain))))
	}
	var facts []TrayFact
	for _, tr := range presFed(t, presPlain, 0).Trays() {
		facts = append(facts, tr.Facts...)
	}
	if got, want := factsText(facts), readGoldenFile(t, presPlain+".trays.txt"); got != want {
		t.Fatal(firstLineDiff(got, want))
	}
}

func readGoldenFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("fixtures", "golden", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestPresenceReplayGapOrResetLine: a replay's gap or reset line while stone 101 of tray 1 is in question ends its
// question as the core's Gap does; a gap or reset line that does not decode is a line a reader does not understand
// (section 6): ignored, the trays as recorded.
func TestPresenceReplayGapOrResetLine(t *testing.T) {
	ctx := testCtx(t)
	after := presInQuestion(t) + 2
	for _, line := range []string{`{"sse":"reset","data":{"boot":"0123456789","cursor":7},"recv":1794960004.8}`,
		`{"sse":"gap","data":{"gap":2}}`} {
		trays := replayTrays(t, presWithLine(t, line, after))
		presEndedUnseen(t, trays[0], 101)
		if presAfterFirst(trays) != presAfterFirst(presTrays(t, presPlain)) {
			t.Fatal(line, firstLineDiff(presAfterFirst(trays), presAfterFirst(presTrays(t, presPlain))))
		}
	}
	for _, line := range []string{`{"sse":"reset","data":{"boot":5,"cursor":7}}`, `{"sse":"gap","data":{"gap":1.5}}`,
		`{"sse":"gap","data":{"gap":2},"recv":"late"}`} {
		r, err := OpenReplay(ctx, presWithLine(t, line, after), nil)
		if err != nil {
			t.Fatal(err)
		}
		var facts []TrayFact
		for f, err := range r.TrayFacts(ctx) {
			if err != nil {
				t.Fatal(err)
			}
			facts = append(facts, f)
		}
		if got, want := factsText(facts), readGoldenFile(t, presPlain+".trays.txt"); got != want || r.Skipped() != 1 {
			t.Fatal(line, r.Skipped(), firstLineDiff(got, want))
		}
		r.Close()
	}
}

// TestPresenceLiveGapOrReset: the Interpreter tells its core about a GapItem or a ResetItem of its stream (Gap): a
// loop follows the fake unit's stream until stone 101 of tray 1 is in question, the fake sends a gap or a reset at
// the start of the next connection, and the next loop goes on from there: the question ends, every other tray is the
// replay's (the recording's own gap line comes in its place).
func TestPresenceLiveGapOrReset(t *testing.T) {
	ctx := testCtx(t)
	frames := 0
	for _, item := range presItems(t, presPlain) {
		if _, ok := item.(*FrameItem); ok {
			frames++
		}
	}
	after := presInQuestion(t) + 2
	for name, inject := range map[string]func(*factorytest.Fake){
		"reset": func(f *factorytest.Fake) { f.InjectReset() },
		"gap":   func(f *factorytest.Fake) { f.InjectGap(2) },
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFake(t, factorytest.WithRecording(recording(presPlain)))
			c, _ := fakeClient(t, fake)
			in := c.Interpreter(Resume(false), ConnectionLimit(frames-after))
			var got []TrayFact
			n := 0
			for u, err := range in.Updates(ctx) {
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, u.TrayFacts...)
				if n++; n == after {
					break
				}
			}
			inject(fake)
			for f, err := range in.TrayFacts(ctx) {
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, f)
			}
			trays := in.Trays()
			fact := presEndedUnseen(t, trays[0], 101)
			if !slices.ContainsFunc(got, func(f TrayFact) bool { return reflect.DeepEqual(f, fact) }) {
				t.Fatal(factsText(got))
			}
			if presAfterFirst(trays) != presAfterFirst(presTrays(t, presPlain)) {
				t.Fatal(firstLineDiff(presAfterFirst(trays), presAfterFirst(presTrays(t, presPlain))))
			}
		})
	}
}
