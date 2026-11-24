package pdiggerfactory

import (
	"strconv"
	"strings"
)

// Event types of the world (WorldEvent.type).
const (
	EventNew     = "new"
	EventConfirm = "confirm"
	EventFreeze  = "freeze"
	EventPush    = "push"
	EventLift    = "lift"
	EventHinge   = "hinge"
	EventCarry   = "carry"
	EventPlace   = "place"
	EventDrop    = "drop"
	EventGone    = "gone"
	EventDelete  = "delete"
	EventSplit   = "split"
	// EventOther is the summary bucket of event types this SDK version does not know.
	EventOther = "other"
)

// eventTypes are the known event types, in the order of the summary.
var eventTypes = [...]string{EventNew, EventConfirm, EventFreeze, EventPush, EventLift, EventHinge, EventCarry,
	EventPlace, EventDrop, EventGone, EventDelete, EventSplit}

// EventTypes returns the event types this SDK version knows, in the order of the summary (a new slice on every
// call). Other types are counted as EventOther.
func EventTypes() []string { return append([]string(nil), eventTypes[:]...) }

var deleteReasons = map[string]string{
	"absent": "no longer seen", "overlap": "overlapped another stone", "outside": "outside the board",
	"merged": "merged with a neighbour", "empty": "its place is empty", "hand": "a hand emptied its place",
	"carried-away": "carried away",
}

// Event is what the world says happened to a stone in a frame, typed.
type Event struct {
	// Type is the wire name: new, confirm, freeze, push, lift, hinge, carry, place, drop, gone, delete, split, or a
	// newer one.
	Type string
	// G and T are the frame's grid index and media time (seconds).
	G int64
	T float64
	// Wall is the frame's wall time (epoch seconds) from the newest clock state, max(anchor_wall, hold_until) +
	// (T - anchor_media) / rate: when the shared playhead showed it; nil until a clock state is known.
	Wall    *float64
	Session int64
	Video   string
	// StoneID is the stone's id (nil when the event names none).
	StoneID *int64
	// Hand is the side: left, right, both, or "" when the event has none.
	Hand string
	// Why is the reason of a push (course, reseat, ...) or a delete (absent, overlap, ...); "" when none.
	Why string
	// Into are the ids of a split: the stone's own id, then the new part's.
	Into []int64
	// Source is "image" for a stone the model never boxed.
	Source string
	// Stone is the stone's snapshot in this frame's world, nil when it is not there.
	Stone *StoneView
	// TrayStone is the id of the tray stone this world id stood for on the open tray before this frame (section 4.6
	// of the SDK design), "" when none. StoneID is the world's id, which the engine's tracker renames and reuses.
	TrayStone string
}

// Clock is the event's media time as M:SS.d.
func (e Event) Clock() string { return FormatClock(e.T) }

// Sentence is the event in words, without the time: "Right hand lifts stone 2660".
func (e Event) Sentence() string {
	id := idText(e.StoneID)
	side := e.Hand
	hand := HandWord(side)
	switch e.Type {
	case EventNew:
		if e.Source == "image" {
			return "stone " + id + " appears (found in the image)"
		}
		return "stone " + id + " appears"
	case EventConfirm:
		return "stone " + id + " is confirmed"
	case EventFreeze:
		return "stone " + id + " gets its final shape"
	case EventPush:
		switch {
		case e.Why == "course":
			return "stone " + id + " is pushed along its row"
		case e.Why == "reseat":
			return "stone " + id + " is reseated"
		case side != "":
			return hand + " " + verb(side, "pushes") + " stone " + id
		case e.Why != "":
			return "stone " + id + " is pushed (" + e.Why + ")"
		}
		return "stone " + id + " is pushed"
	case EventLift:
		return hand + " " + verb(side, "lifts") + " stone " + id
	case EventHinge:
		return hand + " " + verb(side, "tips") + " stone " + id + " up on its edge"
	case EventCarry:
		return hand + " " + verb(side, "carries") + " stone " + id
	case EventPlace:
		return hand + " " + verb(side, "places") + " stone " + id
	case EventDrop:
		return hand + " " + verb(side, "drops") + " stone " + id
	case EventGone:
		return hand + " " + verb(side, "takes") + " stone " + id + " out of view"
	case EventDelete:
		if e.Why == "" {
			return "stone " + id + " is removed"
		}
		reason, ok := deleteReasons[e.Why]
		if !ok {
			reason = e.Why
		}
		return "stone " + id + " is removed (" + reason + ")"
	case EventSplit:
		switch {
		case len(e.Into) == 0:
			return "stone " + id + " splits"
		case len(e.Into) == 1:
			return "stone " + id + " is trimmed"
		}
		return "stone " + id + " splits into " + joinIDs(e.Into)
	}
	return "stone " + id + ": " + e.Type
}

// Describe is "{clock} {sentence}", such as "21:17.8 Right hand lifts stone 2660".
func (e Event) Describe() string { return e.Clock() + " " + e.Sentence() }

// String is Describe.
func (e Event) String() string { return e.Describe() }

// eventKey identifies an event within a frame: (type, id, side, into).
type eventKey struct {
	typ   string
	hasID bool
	id    int64
	side  string
	into  string
}

func keyOf(e *WorldEvent) eventKey {
	k := eventKey{typ: e.Type, side: e.Side}
	if e.ID != nil {
		k.hasID, k.id = true, *e.ID
	}
	if e.Into != nil {
		var b strings.Builder
		b.WriteByte('[')
		for i, v := range e.Into {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.FormatInt(v, 10))
		}
		b.WriteByte(']')
		k.into = b.String()
	}
	return k
}

// EventMatch selects events, for WaitFor.
type EventMatch func(Event) bool

// OfType matches events of any of the given types.
func OfType(types ...string) EventMatch {
	return func(e Event) bool {
		for _, t := range types {
			if e.Type == t {
				return true
			}
		}
		return false
	}
}

// AboutStone matches events of one stone.
func AboutStone(id int64) EventMatch {
	return func(e Event) bool { return e.StoneID != nil && *e.StoneID == id }
}
