package pdiggerfactory

import (
	"sort"
	"strconv"
	"strings"
)

// Activity kinds.
const (
	// KindMove is one stone taken and put somewhere: from a lift (or a tip up on its edge) to a put-down, drop,
	// carry-away or removal.
	KindMove = "move"
	// KindPush is a burst of pushes of one hand side: pushes at most 1 s apart.
	KindPush = "push"
)

// Move outcomes.
const (
	OutcomePlaced      = "placed"
	OutcomeDropped     = "dropped"
	OutcomeCarriedAway = "carried_away"
	OutcomeRemoved     = "removed"
	// OutcomeInterrupted closes a move still open at a session change or at the end of a replay or stream.
	OutcomeInterrupted = "interrupted"
)

// Moment is a point of an activity: grid frame, media time and wall time (nil until a clock state is known).
type Moment struct {
	G    int64
	T    float64
	Wall *float64
}

// Activity is a human-scale action the SDK joins from events: a move of one stone, or a push burst of one hand.
// Kind tells which; the fields after Video belong to one kind or the other.
type Activity struct {
	// Kind is KindMove or KindPush.
	Kind string
	// Hand is left, right, both, or "" when no event named a side.
	Hand  string
	Start Moment
	End   Moment
	// Duration is End.T - Start.T, seconds.
	Duration float64
	Session  int64
	Video    string

	// StoneID is the moved stone (moves).
	StoneID *int64
	// Outcome is placed, dropped, carried_away, removed or interrupted (moves).
	Outcome string
	// From and To are the stone's solid position (x, y, z, scene world) at the start and at a put-down or drop; nil
	// when unknown (moves).
	From []float64
	To   []float64
	// Distance is the x-y distance of From and To rounded to 3 decimals, in units of the template gauge; nil when
	// either is unknown (moves).
	Distance *float64
	// Carried is set by a carry event, Tipped by a tip up on the edge (moves).
	Carried bool
	Tipped  bool
	// Partial is a move whose start was not seen: a stream or a recording joined in the middle of it (moves).
	Partial bool

	// Stones are the pushed stones' unique ids, ascending, and Pushes the number of push events (push bursts).
	Stones []int64
	Pushes int
}

// Describe is the activity in words with its start time, such as
// "0:50.5 Left hand drops stone 128 after 0.2 s" or "0:20.5 Both hands push stones 86, 90 and 94 (6 pushes in 0.6 s)".
func (a Activity) Describe() string {
	side := a.Hand
	hand := HandWord(side)
	if a.Kind == KindPush {
		var what string
		switch n := len(a.Stones); {
		case n == 0:
			what = "stone ?"
		case n == 1:
			what = "stone " + strconv.FormatInt(a.Stones[0], 10)
		case n <= 6:
			what = "stones " + joinIDs(a.Stones)
		default:
			parts := make([]string, 6)
			for i, id := range a.Stones[:6] {
				parts[i] = strconv.FormatInt(id, 10)
			}
			what = "stones " + strings.Join(parts, ", ") + " and " + strconv.Itoa(n-6) + " more"
		}
		tail := "(1 push)"
		if a.Pushes != 1 {
			tail = "(" + strconv.Itoa(a.Pushes) + " pushes in " + FormatFixed(a.Duration, 1) + " s)"
		}
		return FormatClock(a.Start.T) + " " + hand + " " + verb(side, "pushes") + " " + what + " " + tail
	}
	id := idText(a.StoneID)
	dur := FormatFixed(a.Duration, 1)
	var s string
	switch a.Outcome {
	case OutcomePlaced:
		switch {
		case a.Carried && a.Distance == nil:
			s = hand + " " + verb(side, "moves") + " stone " + id + " in " + dur + " s"
		case a.Carried:
			s = hand + " " + verb(side, "moves") + " stone " + id + " " + FormatFixed(*a.Distance, 2) + " units in " +
				dur + " s"
		case a.Tipped:
			s = hand + " " + verb(side, "tips") + " stone " + id + " and sets it down (" + dur + " s)"
		default:
			s = hand + " " + verb(side, "lifts") + " stone " + id + " and sets it down (" + dur + " s)"
		}
	case OutcomeDropped:
		s = hand + " " + verb(side, "drops") + " stone " + id + " after " + dur + " s"
	case OutcomeCarriedAway:
		s = hand + " " + verb(side, "takes") + " stone " + id + " away (" + dur + " s)"
	case OutcomeRemoved:
		s = hand + " " + verb(side, "lifts") + " stone " + id + "; it is removed after " + dur + " s"
	default:
		s = hand + " " + verb(side, "is") + " still holding stone " + id + " (" + dur + " s so far)"
	}
	if a.Partial {
		return FormatClock(a.End.T) + " " + s + " (joined mid-move)"
	}
	return FormatClock(a.Start.T) + " " + s
}

// String is Describe.
func (a Activity) String() string { return a.Describe() }

// sortActivities orders activities closed together: by end g, start g, move before push, stone id (or first stone),
// hand. The sort is stable.
func sortActivities(acts []Activity) {
	sort.SliceStable(acts, func(i, j int) bool {
		a, b := &acts[i], &acts[j]
		if a.End.G != b.End.G {
			return a.End.G < b.End.G
		}
		if a.Start.G != b.Start.G {
			return a.Start.G < b.Start.G
		}
		ka, kb := kindRank(a.Kind), kindRank(b.Kind)
		if ka != kb {
			return ka < kb
		}
		fa, fb := firstStone(a), firstStone(b)
		if fa != fb {
			return fa < fb
		}
		return a.Hand < b.Hand
	})
}

func kindRank(kind string) int {
	if kind == KindMove {
		return 0
	}
	return 1
}

func firstStone(a *Activity) int64 {
	if a.Kind == KindMove {
		if a.StoneID == nil {
			return -1
		}
		return *a.StoneID
	}
	if len(a.Stones) == 0 {
		return -1
	}
	return a.Stones[0]
}
