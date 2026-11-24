package pdiggerfactory

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func TestNumberFormats(t *testing.T) {
	for _, c := range []struct {
		x    float64
		d    int
		want string
	}{
		{0.05, 1, "0.1"}, {0.25, 1, "0.3"}, {1.005, 2, "1.00"}, {123, 2, "123.00"}, {0.5, 1, "0.5"},
		{-1.25, 1, "-1.3"}, {2.5, 0, "3"}, {math.NaN(), 1, "-"}, {math.Inf(1), 2, "-"}, {0.004, 2, "0.00"},
	} {
		if got := FormatFixed(c.x, c.d); got != c.want {
			t.Errorf("FormatFixed(%v, %d) = %q, want %q", c.x, c.d, got, c.want)
		}
	}
	for _, c := range []struct {
		t    float64
		want string
	}{
		{5.2, "0:05.2"}, {1277.8, "21:17.8"}, {3723.4, "1:02:03.4"}, {59.96, "1:00.0"}, {0, "0:00.0"},
		{-1.2, "-1:59:58.8"}, {math.NaN(), "-"}, {36000, "10:00:00.0"},
	} {
		if got := FormatClock(c.t); got != c.want {
			t.Errorf("FormatClock(%v) = %q, want %q", c.t, got, c.want)
		}
	}
	if Round3(0.0005) != 0.001 || Round3(1.23449) != 1.234 {
		t.Fatal("Round3")
	}
	if floorDiv(-7, 2) != -4 || floorMod(-7, 2) != 1 || floorDiv(7, 2) != 3 || floorDiv(-6, 2) != -3 {
		t.Fatal("floor division")
	}
	if HandWord("x") != "A hand" || HandWord(HandBoth) != "Both hands" || verb(HandBoth, "is") != "are" ||
		verb(HandLeft, "is") != "is" {
		t.Fatal("hand words")
	}
	if idText(nil) != "?" || joinIDs([]int64{3}) != "3" || joinIDs([]int64{3, 5, 9}) != "3, 5 and 9" {
		t.Fatal("ids")
	}
}

func TestEventSentences(t *testing.T) {
	id := Int64(72)
	for _, c := range []struct {
		e    Event
		want string
	}{
		{Event{Type: EventNew, StoneID: id}, "stone 72 appears"},
		{Event{Type: EventNew, StoneID: id, Source: "image"}, "stone 72 appears (found in the image)"},
		{Event{Type: EventConfirm, StoneID: id}, "stone 72 is confirmed"},
		{Event{Type: EventFreeze, StoneID: id}, "stone 72 gets its final shape"},
		{Event{Type: EventPush, StoneID: id, Why: "course"}, "stone 72 is pushed along its row"},
		{Event{Type: EventPush, StoneID: id, Why: "reseat"}, "stone 72 is reseated"},
		{Event{Type: EventPush, StoneID: id, Hand: HandBoth}, "Both hands push stone 72"},
		{Event{Type: EventPush, StoneID: id, Why: "nudge"}, "stone 72 is pushed (nudge)"},
		{Event{Type: EventPush, StoneID: id}, "stone 72 is pushed"},
		{Event{Type: EventLift, StoneID: id, Hand: HandRight}, "Right hand lifts stone 72"},
		{Event{Type: EventHinge, StoneID: id, Hand: HandLeft}, "Left hand tips stone 72 up on its edge"},
		{Event{Type: EventCarry, StoneID: id, Hand: HandBoth}, "Both hands carry stone 72"},
		{Event{Type: EventPlace, StoneID: id}, "A hand places stone 72"},
		{Event{Type: EventDrop, StoneID: id, Hand: HandLeft}, "Left hand drops stone 72"},
		{Event{Type: EventGone, StoneID: id, Hand: HandBoth}, "Both hands take stone 72 out of view"},
		{Event{Type: EventDelete, StoneID: id}, "stone 72 is removed"},
		{Event{Type: EventDelete, StoneID: id, Why: "absent"}, "stone 72 is removed (no longer seen)"},
		{Event{Type: EventDelete, StoneID: id, Why: "carried-away"}, "stone 72 is removed (carried away)"},
		{Event{Type: EventDelete, StoneID: id, Why: "weird"}, "stone 72 is removed (weird)"},
		{Event{Type: EventSplit, StoneID: id}, "stone 72 splits"},
		{Event{Type: EventSplit, StoneID: id, Into: []int64{72}}, "stone 72 is trimmed"},
		{Event{Type: EventSplit, StoneID: id, Into: []int64{72, 90}}, "stone 72 splits into 72 and 90"},
		{Event{Type: EventSplit, StoneID: id, Into: []int64{72, 90, 91}}, "stone 72 splits into 72, 90 and 91"},
		{Event{Type: "spin", StoneID: id}, "stone 72: spin"},
		{Event{Type: EventLift}, "A hand lifts stone ?"},
	} {
		if got := c.e.Sentence(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.e, got, c.want)
		}
	}
	e := Event{Type: EventLift, T: 1277.8, StoneID: id, Hand: HandRight}
	if e.Describe() != "21:17.8 Right hand lifts stone 72" || e.String() != e.Describe() || e.Clock() != "21:17.8" {
		t.Fatal(e.Describe())
	}
	if !OfType(EventDrop, EventLift)(e) || OfType(EventDrop)(e) || !AboutStone(72)(e) || AboutStone(1)(e) ||
		AboutStone(72)(Event{}) {
		t.Fatal("matchers")
	}
	if k := keyOf(&WorldEvent{Type: EventSplit, ID: id, Into: []int64{1, 2}}); k.into != "[1,2]" || !k.hasID {
		t.Fatalf("%+v", k)
	}
}

func TestActivityText(t *testing.T) {
	d := Float64(0.381)
	id := Int64(76)
	start, end := Moment{G: 545, T: 109.3}, Moment{G: 547, T: 109.7}
	mv := func(a Activity) Activity {
		a.Kind, a.StoneID, a.Start, a.End = KindMove, id, start, end
		a.Duration = end.T - start.T
		return a
	}
	for _, c := range []struct {
		a    Activity
		want string
	}{
		{mv(Activity{Hand: HandLeft, Outcome: OutcomeDropped}), "1:49.3 Left hand drops stone 76 after 0.4 s"},
		{mv(Activity{Hand: HandRight, Outcome: OutcomePlaced, Carried: true, Distance: d}),
			"1:49.3 Right hand moves stone 76 0.38 units in 0.4 s"},
		{mv(Activity{Hand: HandBoth, Outcome: OutcomePlaced, Carried: true}), "1:49.3 Both hands move stone 76 in 0.4 s"},
		{mv(Activity{Hand: HandLeft, Outcome: OutcomePlaced, Tipped: true}),
			"1:49.3 Left hand tips stone 76 and sets it down (0.4 s)"},
		{mv(Activity{Hand: HandLeft, Outcome: OutcomePlaced}), "1:49.3 Left hand lifts stone 76 and sets it down (0.4 s)"},
		{mv(Activity{Hand: HandBoth, Outcome: OutcomeCarriedAway}), "1:49.3 Both hands take stone 76 away (0.4 s)"},
		{mv(Activity{Hand: HandRight, Outcome: OutcomeRemoved}),
			"1:49.3 Right hand lifts stone 76; it is removed after 0.4 s"},
		{mv(Activity{Hand: HandBoth, Outcome: OutcomeInterrupted}),
			"1:49.3 Both hands are still holding stone 76 (0.4 s so far)"},
		{mv(Activity{Outcome: OutcomeDropped, Partial: true}), "1:49.7 A hand drops stone 76 after 0.4 s (joined mid-move)"},
		{Activity{Kind: KindPush, Hand: HandBoth, Start: Moment{T: 20.5}, Stones: []int64{49, 53}, Pushes: 5, Duration: 0.6},
			"0:20.5 Both hands push stones 49 and 53 (5 pushes in 0.6 s)"},
		{Activity{Kind: KindPush, Hand: HandLeft, Stones: []int64{3}, Pushes: 1}, "0:00.0 Left hand pushes stone 3 (1 push)"},
		{Activity{Kind: KindPush, Hand: HandLeft, Pushes: 2}, "0:00.0 Left hand pushes stone ? (2 pushes in 0.0 s)"},
		{Activity{Kind: KindPush, Hand: HandRight, Stones: []int64{3, 5, 9, 11, 12, 14, 20, 21, 22, 23}, Pushes: 12},
			"0:00.0 Right hand pushes stones 3, 5, 9, 11, 12, 14 and 4 more (12 pushes in 0.0 s)"},
	} {
		if got := c.a.Describe(); got != c.want {
			t.Errorf("%q, want %q", got, c.want)
		}
		if c.a.String() != c.a.Describe() {
			t.Error("String")
		}
	}
	acts := []Activity{
		{Kind: KindPush, Hand: HandRight, End: Moment{G: 5}, Start: Moment{G: 2}, Stones: []int64{4}},
		{Kind: KindPush, Hand: HandLeft, End: Moment{G: 5}, Start: Moment{G: 2}, Stones: []int64{4}},
		{Kind: KindPush, End: Moment{G: 5}, Start: Moment{G: 2}},
		{Kind: KindMove, End: Moment{G: 5}, Start: Moment{G: 2}, StoneID: Int64(9)},
		{Kind: KindMove, End: Moment{G: 5}, Start: Moment{G: 2}},
		{Kind: KindMove, End: Moment{G: 5}, Start: Moment{G: 1}, StoneID: Int64(9)},
		{Kind: KindMove, End: Moment{G: 4}, Start: Moment{G: 3}, StoneID: Int64(9)},
	}
	sortActivities(acts)
	var order []string
	for _, a := range acts {
		order = append(order, a.Kind+":"+idText(a.StoneID)+":"+a.Hand+":"+itoa(a.Start.G)+":"+itoa(a.End.G))
	}
	want := "move:9::3:4 move:9::1:5 move:?::2:5 move:9::2:5 push:?::2:5 push:?:left:2:5 push:?:right:2:5"
	if got := strings.Join(order, " "); got != want {
		t.Fatalf("order %s", got)
	}
}

func TestSummaryText(t *testing.T) {
	var s Summary
	s.Events.Other = 2
	text := s.Describe()
	for _, want := range []string{
		"frames: 0 (with world 0, without 0, revisions 0)\n",
		"stones: last -, max 0, confirmed -\n",
		"), mean - s, - units\n",
		", split 0, other 2\n",
		"right hand: free 0:00.0, touch 0:00.0, grasp 0:00.0, hinge 0:00.0, carry 0:00.0\n",
		"trays: 0 (arrived 0), stones at arrival 0, added 0 (+0 unclear), removed 0 (+0 unclear), not sure 0\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in\n%s", want, text)
		}
	}
	if s.String() != text || strings.Count(text, "\n") != 9 {
		t.Fatal("String")
	}
	b, err := json.Marshal(&s)
	if err != nil || !strings.Contains(string(b), `"first_g":null`) || !strings.Contains(string(b), `"other":2`) ||
		!strings.Contains(string(b), `"trays":{"total":0,"arrived":0,"stones":0,"added":0,"added_unclear":0,`+
			`"removed":0,"removed_unclear":0,"unsure":0},"hands":`) {
		t.Fatalf("%s %v", b, err)
	}
	for _, typ := range append(EventTypes(), EventOther, "brand-new") {
		*s.Events.ptr(typ)++
	}
	if s.Events.Count("brand-new") != 4 || s.Events.Count(EventSplit) != 1 {
		t.Fatalf("%+v", s.Events)
	}
}

// TestNoFusedMultiplyAdd: the numbers round every product on its own, as the other languages' IEEE arithmetic does; a
// fused multiply-add (which Go may use on arm64 unless a product is converted) rounds once and differs at the edge.
func TestNoFusedMultiplyAdd(t *testing.T) {
	// x*100 rounds to 0.49999999999999994 and + 0.5 ties to 1.0; fused, x*100 + 0.5 rounds down below 1.0
	if got := FormatFixed(0.004999999999999999, 2); got != "0.01" {
		t.Fatalf("FormatFixed = %s, want 0.01 (the reference's fmt)", got)
	}
	if got := math.Floor(math.FMA(0.004999999999999999, 100, 0.5)); got != 0 {
		t.Fatalf("the fused value is %v: the case does not tell the two apart", got)
	}
	// the distance of a move: a sum of two rounded squares
	differ := 0
	for i := 1; i < 5000 && differ < 25; i++ {
		dx, dy := float64(i)*0.0123456789, float64(i)*0.0141096331
		plain := math.Sqrt(float64(dx*dx) + float64(dy*dy))
		if fused := math.Sqrt(math.FMA(dx, dx, float64(dy*dy))); fused == plain {
			continue
		}
		differ++
		if got := planarDistance([]float64{0, 0, 0}, []float64{dx, dy, 9}); got != plain {
			t.Fatalf("planarDistance(%v, %v) = %v, want %v", dx, dy, got, plain)
		}
	}
	if differ == 0 {
		t.Fatal("no distance that a fused multiply-add changes")
	}
}
