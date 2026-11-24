package pdiggerfactory

// Presence questions (section 4.6.1 of the SDK design) on edited copies of the fixture recordings. Each case changes a
// few frames of one tray, so that one rule decides how the tray ends where no fixture tray needs that rule alone, and
// the copy's tray facts must be the ones the Python SDK (version 3 of these rules, which every SDK shares) gives for
// the same copy: the recording's golden trays.txt with a few lines in a row changed, every other tray as recorded. The
// rules: the probation of a stone whose question a sight settled before a calm moment that misses it; the leaving's
// count of a last frame that is not its last snapshot's own; which stones make a crowd; which looks of its world stone
// show another open stone (a lost one's, one read after its last sight, a fresh one, one on N_ID paired cells, one
// against a sure stone); a hand event with both hands free, and a hand not flagged untrusted; a hand mark near the
// stone's place now, and a lift of the stone with its hand mark away from its places; where its first and its last
// sight are; a freed push next to it at its doubt.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// presEdit is a case: the fixture recording it copies, the change to the world of each frame (at the frame's g), and
// the lines of the recording's golden trays.txt (in a row; none: the file as it is) that the copy's trays.txt has in
// their place (now). With states, the stone of tray n first seen under wid goes through these states in the copy.
type presEdit struct {
	name     string
	rec      string
	edit     func(g int64, w map[string]any)
	was, now []string
	n, wid   int64
	states   []presState
}

// presEditCheck replays the case's copy: every tray keeps its self-check, its facts are the golden ones with the
// case's lines changed, and its stone goes through the case's states.
func presEditCheck(t *testing.T, c presEdit) {
	t.Helper()
	path := presFrames(t, c.rec, c.edit)
	var facts []TrayFact
	for _, tr := range replayTrays(t, path) {
		if bad := tr.Check(); len(bad) != 0 {
			t.Errorf("tray %d breaks %v", tr.N, bad)
		}
		facts = append(facts, tr.Facts...)
	}
	if got, want := factsText(facts), presChanged(t, c.rec, c.was, c.now); got != want {
		t.Fatal(firstLineDiff(got, want))
	}
	if c.states != nil {
		if got := presStatesAt(t, path, c.n, c.wid); !slices.Equal(got, c.states) {
			t.Fatal(got)
		}
	}
}

// presEditRun runs the cases, each in a subtest of its own (in parallel: each replays its own copy).
func presEditRun(t *testing.T, cases []presEdit) {
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			presEditCheck(t, c)
		})
	}
}

// presFrames is a copy of a fixture recording with change applied to the world of each frame (numbers kept as
// written, except those change sets).
func presFrames(t *testing.T, name string, change func(g int64, w map[string]any)) string {
	t.Helper()
	return presEdited(t, name, func(x map[string]any) map[string]any {
		w, _ := x["world"].(map[string]any)
		g, _ := x["g"].(json.Number)
		if w != nil && g != "" && x["cursor"] != nil {
			n, err := g.Int64()
			if err != nil {
				t.Fatal(err)
			}
			change(n, w)
		}
		return x
	})
}

// presChanged is the golden trays.txt of a recording with the lines was, which it has once and in a row, replaced by
// now.
func presChanged(t *testing.T, rec string, was, now []string) string {
	t.Helper()
	text := "\n" + readGoldenFile(t, rec+".trays.txt")
	if len(was) == 0 {
		return text[1:]
	}
	block := "\n" + strings.Join(was, "\n") + "\n"
	if n := strings.Count(text, block); n != 1 {
		t.Fatalf("%s.trays.txt has these lines %d times: %q", rec, n, was)
	}
	with := "\n"
	if len(now) > 0 {
		with += strings.Join(now, "\n") + "\n"
	}
	return strings.Replace(text, block, with, 1)[1:]
}

// edStone is the world's stone with that id (nil: none).
func edStone(w map[string]any, id string) map[string]any {
	for _, s := range objects(w, "stones") {
		if numberIs(s["id"], id) {
			return s
		}
	}
	return nil
}

// edSolid is a member of a stone's solid ("pos", "dims"): setting its numbers moves or sizes the stone.
func edSolid(s map[string]any, key string) []any {
	solid, _ := s["solid"].(map[string]any)
	v, _ := solid[key].([]any)
	return v
}

// edMoveX sets x (pos[0]) of the stone with that id, when the world has it.
func edMoveX(w map[string]any, id string, x float64) {
	if s := edStone(w, id); s != nil {
		edSolid(s, "pos")[0] = x
	}
}

// edUnseen marks the stone with that id not detected, when the world has it.
func edUnseen(w map[string]any, id string) {
	if s := edStone(w, id); s != nil {
		s["detected"] = false
	}
}

// edHand gives a hand of the world a state, holding no stone.
func edHand(w map[string]any, side, state string) {
	w["hands"].(map[string]any)[side] = map[string]any{"state": state, "stone": nil}
}

// edEvent adds an event to the world's.
func edEvent(w map[string]any, e map[string]any) {
	events, _ := w["events"].([]any)
	w["events"] = append(events, e)
}

// edWithout leaves the stone with that id out of the world.
func edWithout(w map[string]any, id string) {
	stones, _ := w["stones"].([]any)
	w["stones"] = slices.DeleteFunc(stones, func(v any) bool {
		s, _ := v.(map[string]any)
		return s != nil && numberIs(s["id"], id)
	})
}

// edCopy is a deep copy of a JSON object, its numbers kept as written.
func edCopy(t *testing.T, x map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(x)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := d.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// edFrameStone is a copy of the stone with that id in the world of the recording's frame g.
func edFrameStone(t *testing.T, rec string, g, id string) map[string]any {
	t.Helper()
	for _, x := range presObjects(t, rec) {
		if w, _ := x["world"].(map[string]any); w != nil && numberIs(x["g"], g) {
			if s := edStone(w, id); s != nil {
				return edCopy(t, s)
			}
		}
	}
	t.Fatalf("%s: no stone %s at g %s", rec, id, g)
	return nil
}

// edCut is a look's cells (its d) with only those in columns u0 to u1-1 of rows v0 to v1-1 read: every other cell is
// marked not read (-128).
func edCut(t *testing.T, look map[string]any, u0, u1, v0, v1 int) string {
	t.Helper()
	d, _ := look["d"].(string)
	raw, err := base64.StdEncoding.DecodeString(d)
	nu, _ := look["nu"].(json.Number).Int64()
	nv, _ := look["nv"].(json.Number).Int64()
	if err != nil || nu < 1 || int64(len(raw)) != nu*nv {
		t.Fatal(err, nu, nv, len(raw))
	}
	for k := range raw {
		if i, j := k%int(nu), k/int(nu); i < u0 || i >= u1 || j < v0 || j >= v1 {
			raw[k] = 0x80
		}
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// TestPresenceProbationOnEditedTrays: a stone settled on a calm moment that does not show it starts its next unseen
// run there on probation: the next calm moment that shows it must settle that run, nothing having changed the stone
// since the run's last sight (a plain fact there, else); a run on probation that reaches a second unseen calm moment is
// the stone's next doubt, as any run's, and the probation is over.
func TestPresenceProbationOnEditedTrays(t *testing.T) {
	presEditRun(t, []presEdit{
		// tray 35: stone 3501's question is settled by a sight before the calm moment at g 1456, which does not show
		// it; a hand touches the tray at g 1457 while it is out of sight: its run on probation is spoilt, and the calm
		// moment that shows it again makes it not sure
		{name: "settled-by-a-sight", rec: presPlain, edit: func(g int64, w map[string]any) {
			if g == 1457 {
				edHand(w, "left", "touch")
			}
		}, was: []string{"4:53.8 tray 35 leaving: 3 stones (3 sure), 0 added, 0 removed, 0 unclear"},
			now: []string{
				"4:52.4 tray 35: stone 20261118T000446.0Z-35-3501 is not sure any more (not seen at two calm moments)",
				"4:53.8 tray 35 leaving: 3 stones (2 sure), 0 added, 0 removed, 0 unclear"}},
		// tray 15: stone 1501, settled at once at g 650, is seen again at g 652 on its place, then a hand touches the
		// tray at g 653 while it is out of sight: a clean first sight does not do, nothing may have changed it since
		// its run's last sight
		{name: "spoilt-after-its-first-sight", rec: presPlain, edit: func(g int64, w map[string]any) {
			if g == 653 {
				edUnseen(w, "1501")
				edHand(w, "left", "touch")
			}
		}, was: []string{"2:13.0 tray 15 leaving: 3 stones (3 sure), 0 added, 0 removed, 0 unclear"},
			now: []string{
				"2:11.6 tray 15: stone 20261118T000206.4Z-15-1501 is not sure any more (not seen at two calm moments)",
				"2:13.0 tray 15 leaving: 3 stones (2 sure), 0 added, 0 removed, 0 unclear"}},
		// tray 15: stone 1501 is not detected from g 651 to g 659: its run on probation reaches a second unseen calm
		// moment (g 658), where it gets a presence question, which the sight from g 660 settles: the tray as recorded
		{name: "second-unseen-calm-moment", rec: presPlain, edit: func(g int64, w map[string]any) {
			if g >= 651 && g <= 659 {
				edUnseen(w, "1501")
			}
		}, n: 15, wid: 1501, states: []presState{presSure, presAsked, presSure}},
	})
}

// TestPresenceLeavingCountsALaterLastFrame: tray 15's stone 1501 is not detected from g 662 to g 665; the last snapshot
// is g 664's, the tray's last frame g 665: the leaving counts that frame (it is not the snapshot's own), so the
// presence question opens there and ends at the leaving, "open since" the leaving's own clock.
func TestPresenceLeavingCountsALaterLastFrame(t *testing.T) {
	presEditRun(t, []presEdit{{name: "last-frame-after-the-snapshot", rec: presPlain, edit: func(g int64, w map[string]any) {
		if g >= 662 && g <= 665 {
			edUnseen(w, "1501")
		}
	}, was: []string{"2:13.0 tray 15 leaving: 3 stones (3 sure), 0 added, 0 removed, 0 unclear"},
		now: []string{"2:13.0 tray 15: stone 20261118T000206.4Z-15-1501 is not sure any more (not seen at two calm " +
			"moments; open since 2:13.0)", "2:13.0 tray 15 leaving: 3 stones (2 sure), 0 added, 0 removed, 0 unclear"}}})
}

// TestPresenceCrowdIsOfItsSizeAndNotHeld: tray 30's stone 3001 is seen again on its place while stone 3004 stands
// within nearD of it (the recording: a crowd, its question ends when the tray leaves). Only a world stone of a
// compatible size that is not held makes a crowd: 3004 made 0.04 x 0.03 (and set 0.11 from 3001's place), or reading
// carry in every frame, leaves 3001's question to be settled.
func TestPresenceCrowdIsOfItsSizeAndNotHeld(t *testing.T) {
	crowd := []string{
		"4:13.0 tray 30: stone 20261118T000406.2Z-30-3001 is not sure any more (not seen at two calm moments; open " +
			"since 4:09.8)",
		"4:13.0 tray 30: stone 20261118T000410.4Z-30-3004 not recognised (unclear)",
		"4:13.0 tray 30 leaving: 4 stones (2 sure), 0 added, 0 removed, 1 unclear"}
	presEditRun(t, []presEdit{
		{name: "other-size", rec: presPlain, edit: func(g int64, w map[string]any) {
			if s := edStone(w, "3004"); s != nil {
				pos, dims := edSolid(s, "pos"), edSolid(s, "dims")
				pos[0], pos[1], dims[0], dims[1] = -0.3, 0.11, 0.04, 0.03
			}
		}, was: crowd, now: []string{"4:12.8 tray 30: stone 20261118T000410.4Z-30-3004 was set down",
			"4:13.0 tray 30 leaving: 4 stones (4 sure), 1 added, 0 removed, 0 unclear"}},
		{name: "held", rec: presPlain, edit: func(g int64, w map[string]any) {
			if s := edStone(w, "3004"); s != nil {
				s["mode"] = "carry"
			}
		}, was: crowd, now: []string{"4:13.0 tray 30 leaving: 3 stones (3 sure), 0 added, 0 removed, 0 unclear"}},
	})
}

// TestPresenceLookAgainstOnEditedTrays: looks tray 7's stone 701 (no chain) is in question; its world stone is seen
// again on its place at g 310 with the face of stone 704, which has a chain and is in question too: in the recording
// that look ends 701's question at the settle at g 317. The look counts against a stone that is lost (704's world stone
// gone from g 299: a stone gone silently is a blink for two frames, which are not calm, so gone from g 300 it would
// take away the first calm moment that misses 701), and against a stone that is sure only (704 not sure by an overlap
// at g 302, then lost: 701 settled); and only a look read after 701's last sight (read at g 299, its last sight:
// settled), still fresh (701 standing 0.15 from where it was read: settled) and on at least N_ID paired cells (the face
// cut to 99 read cells: settled; to 100: the recording).
func TestPresenceLookAgainstOnEditedTrays(t *testing.T) {
	look := edFrameStone(t, presLooks, "310", "701")["look"].(map[string]any)
	at299 := edCopy(t, look)
	at299["g"] = json.Number("299")
	cut99, cut100 := edCut(t, look, 0, 9, 0, 11), edCut(t, look, 4, 14, 1, 11)
	over := edFrameStone(t, presLooks, "302", "704")
	over["id"], over["detected"] = json.Number("7090"), true
	delete(over, "look")
	settled := []string{
		"1:03.4 tray 7: stone 20261118T000058.0Z-7-701 is not sure any more (not seen at two calm moments; open " +
			"since 1:01.6)",
		"1:04.8 tray 7: stone 20261118T000058.0Z-7-704 is not sure any more (not seen at two calm moments; open " +
			"since 1:01.6)",
		"1:04.8 tray 7 leaving: 4 stones (2 sure), 0 added, 0 removed, 0 unclear"}
	presEditRun(t, []presEdit{
		{name: "lost", rec: presLooks, edit: func(g int64, w map[string]any) {
			if g >= 299 && g <= 325 {
				edWithout(w, "704")
			}
		}, was: settled[1:], now: []string{"1:04.8 tray 7: stone 20261118T000058.0Z-7-704 not found (unclear)",
			"1:04.8 tray 7 leaving: 3 stones (2 sure), 0 added, 0 removed, 1 unclear"}},
		{name: "lost-not-sure", rec: presLooks, edit: func(g int64, w map[string]any) {
			if g == 302 {
				w["stones"] = append(w["stones"].([]any), over)
			}
			if g >= 303 && g <= 325 {
				edWithout(w, "704")
			}
		}, was: settled, now: []string{
			"1:00.4 tray 7: stone 20261118T000058.0Z-7-704 is not sure any more (its box overlaps another stone)",
			"1:04.8 tray 7: stone 20261118T000058.0Z-7-704 not found (unclear)",
			"1:04.8 tray 7 leaving: 3 stones (3 sure), 0 added, 0 removed, 1 unclear"}},
		{name: "read-at-its-last-sight", rec: presLooks, edit: func(g int64, w map[string]any) {
			if g == 310 {
				delete(edStone(w, "701"), "look")
			}
			if g == 299 {
				edStone(w, "701")["look"] = at299
			}
		}, was: settled, now: []string{settled[1], "1:04.8 tray 7 leaving: 4 stones (3 sure), 0 added, 0 removed, 0 unclear"}},
		{name: "not-fresh", rec: presLooks, edit: func(g int64, w map[string]any) {
			if g == 310 {
				edMoveX(w, "701", -0.2)
			}
			if g >= 311 && g <= 325 {
				edMoveX(w, "701", -0.35)
			}
		}, was: settled, now: []string{settled[1], "1:04.8 tray 7 leaving: 4 stones (3 sure), 0 added, 0 removed, 0 unclear"}},
		{name: "99-cells", rec: presLooks, edit: func(g int64, w map[string]any) {
			if g == 310 {
				edStone(w, "701")["look"].(map[string]any)["d"] = cut99
			}
		}, was: settled, now: []string{settled[1], "1:04.8 tray 7 leaving: 4 stones (3 sure), 0 added, 0 removed, 0 unclear"}},
		{name: "100-cells", rec: presLooks, edit: func(g int64, w map[string]any) {
			if g == 310 {
				edStone(w, "701")["look"].(map[string]any)["d"] = cut100
			}
		}},
	})
}

// TestPresenceBusyFramesOnEditedTrays: tray 1's stone 101 is in question and out of sight until g 24. A hand event is a
// busy frame also when both hands read free (a drop of a stone that is not on the tray at g 23, as the complete
// recording of jul30 has one at g 1111): 101's question is not settled and ends at the next settle. A hand reading
// untrusted false is no busy frame: both hands so in every frame leave the tray as recorded.
func TestPresenceBusyFramesOnEditedTrays(t *testing.T) {
	for _, x := range presObjects(t, presPlain) {
		w, _ := x["world"].(map[string]any)
		if w == nil || !numberIs(x["g"], "23") {
			continue
		}
		for _, side := range []string{"left", "right"} {
			if h, _ := w["hands"].(map[string]any)[side].(map[string]any); h["state"] != "free" {
				t.Fatalf("g 23: the %s hand is %v, not free", side, h["state"])
			}
		}
	}
	presEditRun(t, []presEdit{
		{name: "hand-event-with-both-hands-free", rec: presPlain, edit: func(g int64, w map[string]any) {
			if g == 23 {
				edEvent(w, map[string]any{"type": "drop", "id": json.Number("9002"), "side": "left", "why": "lost"})
			}
		}, was: []string{"0:07.4 tray 1 leaving: 3 stones (3 sure), 0 added, 0 removed, 0 unclear"},
			now: []string{"0:06.0 tray 1: stone 20261118T000000.8Z-1-101 is not sure any more (not seen at two calm " +
				"moments; open since 0:04.4)", "0:07.4 tray 1 leaving: 3 stones (2 sure), 0 added, 0 removed, 0 unclear"}},
		{name: "untrusted-false", rec: presPlain, edit: func(g int64, w map[string]any) {
			if g <= 37 {
				for _, side := range []string{"left", "right"} {
					if h, _ := w["hands"].(map[string]any)[side].(map[string]any); h != nil {
						h["untrusted"] = false
					}
				}
			}
		}},
	})
}

// TestPresenceSightsOnEditedTrays: tray 1's stone 101 is in question and seen again from g 24. Nothing may have
// changed it: a hand mark near its place now counts, not only one near its last place (102 at (0.18, 0) lifted and set
// down at g 25 and 26 while 101 stands 0.10 from its last place: the marks are 0.38 from its place now, 0.48 from its
// last place); and a lift of 101 itself counts with no hand mark near either place (lifted at g 25 while its world
// stone reads 0.45 from its place, where the lift's hand mark is). And a sight settles it only when its first sight
// (g 24) and its last sight are within nearD of its last place: either one 0.2 away, and its question ends when the
// tray leaves.
func TestPresenceSightsOnEditedTrays(t *testing.T) {
	ends := func(at string) []string {
		return []string{"0:" + at + " tray 1: stone 20261118T000000.8Z-1-101 is not sure any more (not seen at two calm " +
			"moments; open since 0:04.4)", "0:07.4 tray 1 leaving: 3 stones (2 sure), 0 added, 0 removed, 0 unclear"}
	}
	was := []string{"0:07.4 tray 1 leaving: 3 stones (3 sure), 0 added, 0 removed, 0 unclear"}
	presEditRun(t, []presEdit{
		{name: "hand-mark-near-its-place-now", rec: presPlain, edit: func(g int64, w map[string]any) {
			if g > 38 {
				return
			}
			edMoveX(w, "102", 0.18)
			if g >= 24 {
				edMoveX(w, "101", -0.2)
			}
			if g == 25 || g == 26 {
				typ := "lift"
				if g == 26 {
					typ = "place"
				}
				edHand(w, "left", "grasp")
				edEvent(w, map[string]any{"type": typ, "id": json.Number("102"), "side": "left"})
			}
		}, was: was, now: ends("06.0")},
		{name: "lifted-away-from-its-places", rec: presPlain, edit: func(g int64, w map[string]any) {
			if g == 25 {
				edMoveX(w, "101", -0.75)
				edHand(w, "left", "grasp")
				edEvent(w, map[string]any{"type": "lift", "id": json.Number("101"), "side": "left"})
			}
		}, was: was, now: ends("06.0")},
		{name: "first-sight-away", rec: presPlain, edit: func(g int64, w map[string]any) {
			if g == 24 {
				edMoveX(w, "101", -0.1)
			}
		}, was: was, now: ends("07.4")},
		{name: "last-sight-away", rec: presPlain, edit: func(g int64, w map[string]any) {
			if g >= 25 && g <= 38 {
				edMoveX(w, "101", -0.1)
			}
		}, was: was, now: ends("07.4")},
	})
}

// TestPresenceFreedPushAtTheDoubt: trays-looks-synthetic tray 14: world stone 410, freed at g 563 by the look decision
// that links stone 410 to world stone 430, is pushed at g 567, with no side here. Stone 432, given no look (so no
// chain), is not detected at the calm moments g 563 and g 570 and is detected at g 567 only, so that push frame does
// not spoil its unseen run: at its doubt (g 570) the freed push next to it is a later reason that holds, so the doubt
// is no presence question: a plain fact there.
func TestPresenceFreedPushAtTheDoubt(t *testing.T) {
	presEditRun(t, []presEdit{{name: "seen-on-the-push-frame", rec: "trays-looks-synthetic",
		edit: func(g int64, w map[string]any) {
			if g < 526 || g > 600 {
				return
			}
			if s := edStone(w, "432"); s != nil {
				delete(s, "look")
				if g >= 563 && g <= 570 && g != 567 {
					s["detected"] = false
				}
			}
			for _, e := range objects(w, "events") {
				if g == 567 && e["type"] == "push" && numberIs(e["id"], "410") {
					delete(e, "side")
				}
			}
		},
		was: []string{"2:00.0 tray 14: stone 20261118T000146.0Z-14-432 is not sure any more (a stone next to it was " +
			"pushed; open since 1:54.0)"},
		now: []string{"1:54.0 tray 14: stone 20261118T000146.0Z-14-432 is not sure any more (not seen at two calm moments)"}}})
}
