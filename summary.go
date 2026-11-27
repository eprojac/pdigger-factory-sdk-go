package pdiggerfactory

import (
	"strconv"
	"strings"
)

// EventCounts counts events by type (unknown types in Other).
type EventCounts struct {
	New     int64 `json:"new"`
	Confirm int64 `json:"confirm"`
	Freeze  int64 `json:"freeze"`
	Push    int64 `json:"push"`
	Lift    int64 `json:"lift"`
	Hinge   int64 `json:"hinge"`
	Carry   int64 `json:"carry"`
	Place   int64 `json:"place"`
	Drop    int64 `json:"drop"`
	Gone    int64 `json:"gone"`
	Delete  int64 `json:"delete"`
	Split   int64 `json:"split"`
	Sheet   int64 `json:"sheet,omitempty"`
	Other   int64 `json:"other"`
}

func (c *EventCounts) ptr(typ string) *int64 {
	switch typ {
	case EventNew:
		return &c.New
	case EventConfirm:
		return &c.Confirm
	case EventFreeze:
		return &c.Freeze
	case EventPush:
		return &c.Push
	case EventLift:
		return &c.Lift
	case EventHinge:
		return &c.Hinge
	case EventCarry:
		return &c.Carry
	case EventPlace:
		return &c.Place
	case EventDrop:
		return &c.Drop
	case EventGone:
		return &c.Gone
	case EventDelete:
		return &c.Delete
	case EventSheet:
		return &c.Sheet
	case EventSplit:
		return &c.Split
	}
	return &c.Other
}

// Count is the count of one event type (EventOther for the unknown ones).
func (c EventCounts) Count(typ string) int64 { return *c.ptr(typ) }

// MoveCounts counts closed moves. MeanDuration is over moves that are neither partial nor interrupted,
// MeanDistance over moves with a distance; nil when there are none.
type MoveCounts struct {
	Total        int64    `json:"total"`
	Placed       int64    `json:"placed"`
	Dropped      int64    `json:"dropped"`
	CarriedAway  int64    `json:"carried_away"`
	Removed      int64    `json:"removed"`
	Interrupted  int64    `json:"interrupted"`
	Partial      int64    `json:"partial"`
	Carried      int64    `json:"carried"`
	MeanDuration *float64 `json:"mean_duration"`
	MeanDistance *float64 `json:"mean_distance"`
}

// PushCounts counts closed push bursts and their pushes.
type PushCounts struct {
	Total  int64 `json:"total"`
	Pushes int64 `json:"pushes"`
	Left   int64 `json:"left"`
	Right  int64 `json:"right"`
	Both   int64 `json:"both"`
}

// StoneCounts are the stones of the last world, the most of any world, and the last stats.confirmed.
type StoneCounts struct {
	Last          *int64 `json:"last"`
	Max           int64  `json:"max"`
	ConfirmedLast *int64 `json:"confirmed_last"`
}

// HandStateTimes are media seconds a hand spent in each state: each frame with a world adds its step (section 4.5 of
// the SDK design: the media time since the session's previous frame when that frame is the grid step before it, else
// the previous step; 0.2 s on a recorded video, about 0.3 s on a plant camera).
type HandStateTimes struct {
	Free  float64 `json:"free"`
	Touch float64 `json:"touch"`
	Grasp float64 `json:"grasp"`
	Hinge float64 `json:"hinge"`
	Carry float64 `json:"carry"`
}

// HandTimes are the state times of both hands.
type HandTimes struct {
	Left  HandStateTimes `json:"left"`
	Right HandStateTimes `json:"right"`
}

// Summary is the summary of a replay (or of a live interpreter so far): its JSON form has exactly the keys of
// section 4.4 of the SDK design, numbers rounded to 3 decimals, times in seconds.
type Summary struct {
	Frames          int64       `json:"frames"`
	FramesWithWorld int64       `json:"frames_with_world"`
	MissingWorld    int64       `json:"missing_world"`
	Revisions       int64       `json:"revisions"`
	Sessions        int64       `json:"sessions"`
	FirstG          *int64      `json:"first_g"`
	LastG           *int64      `json:"last_g"`
	FirstT          *float64    `json:"first_t"`
	LastT           *float64    `json:"last_t"`
	MediaSeconds    float64     `json:"media_seconds"`
	Gaps            int64       `json:"gaps"`
	Events          EventCounts `json:"events"`
	Moves           MoveCounts  `json:"moves"`
	PushBursts      PushCounts  `json:"push_bursts"`
	Stones          StoneCounts `json:"stones"`
	Trays           TrayCounts  `json:"trays"`
	Hands           HandTimes   `json:"hands"`
}

func dash(v *int64) string {
	if v == nil {
		return "-"
	}
	return strconv.FormatInt(*v, 10)
}

func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// Describe is the summary as text, a "\n" after every line:
//
//	frames: 632 (with world 632, without 0, revisions 0)
//	media: 2:09.6, sessions 1, gaps 1
//	stones: last 40, max 49, confirmed 40
//	moves: 9 (placed 5, dropped 2, carried away 2, removed 0, interrupted 0, partial 0, carried 6), mean 1.4 s, 0.18 units
//	push bursts: 51 (left 25, right 16, both 10), pushes 456
//	events: new 276, confirm 119, ...
//	trays: 2 (arrived 2), stones at arrival 83, added 0 (+14 unclear), removed 6 (+9 unclear), not sure 37
//	left hand: free 0:53.7, touch 0:52.1, grasp 0:17.1, hinge 0:00.4, carry 0:02.4
//	right hand: ...
func (s *Summary) Describe() string {
	var b strings.Builder
	line := func(parts ...string) {
		for _, p := range parts {
			b.WriteString(p)
		}
		b.WriteByte('\n')
	}
	m, p, e := s.Moves, s.PushBursts, s.Events
	meanD, meanU := "-", "-"
	if m.MeanDuration != nil {
		meanD = FormatFixed(*m.MeanDuration, 1)
	}
	if m.MeanDistance != nil {
		meanU = FormatFixed(*m.MeanDistance, 2)
	}
	line("frames: ", itoa(s.Frames), " (with world ", itoa(s.FramesWithWorld), ", without ", itoa(s.MissingWorld),
		", revisions ", itoa(s.Revisions), ")")
	line("media: ", FormatClock(s.MediaSeconds), ", sessions ", itoa(s.Sessions), ", gaps ", itoa(s.Gaps))
	line("stones: last ", dash(s.Stones.Last), ", max ", itoa(s.Stones.Max), ", confirmed ", dash(s.Stones.ConfirmedLast))
	line("moves: ", itoa(m.Total), " (placed ", itoa(m.Placed), ", dropped ", itoa(m.Dropped), ", carried away ",
		itoa(m.CarriedAway), ", removed ", itoa(m.Removed), ", interrupted ", itoa(m.Interrupted), ", partial ",
		itoa(m.Partial), ", carried ", itoa(m.Carried), "), mean ", meanD, " s, ", meanU, " units")
	line("push bursts: ", itoa(p.Total), " (left ", itoa(p.Left), ", right ", itoa(p.Right), ", both ", itoa(p.Both),
		"), pushes ", itoa(p.Pushes))
	parts := make([]string, 0, len(eventTypes)+1)
	for _, t := range eventTypes {
		if t == EventSheet && e.Sheet == 0 {
			continue
		}
		parts = append(parts, t+" "+itoa(e.Count(t)))
	}
	parts = append(parts, EventOther+" "+itoa(e.Count(EventOther)))
	line("events: ", strings.Join(parts, ", "))
	tr := s.Trays
	sheets := ""
	if tr.Sheets != 0 || tr.UnderSheet != 0 {
		sheets = ", sheets laid " + itoa(tr.Sheets) + ", under a sheet " + itoa(tr.UnderSheet)
	}
	line("trays: ", itoa(tr.Total), " (arrived ", itoa(tr.Arrived), "), stones at arrival ", itoa(tr.Stones), ", added ",
		itoa(tr.Added), " (+", itoa(tr.AddedUnclear), " unclear), removed ", itoa(tr.Removed), " (+",
		itoa(tr.RemovedUnclear), " unclear), not sure ", itoa(tr.Unsure), sheets)
	for _, h := range []struct {
		name string
		t    HandStateTimes
	}{{"left", s.Hands.Left}, {"right", s.Hands.Right}} {
		line(h.name, " hand: free ", FormatClock(h.t.Free), ", touch ", FormatClock(h.t.Touch), ", grasp ",
			FormatClock(h.t.Grasp), ", hinge ", FormatClock(h.t.Hinge), ", carry ", FormatClock(h.t.Carry))
	}
	return b.String()
}

// String is Describe.
func (s *Summary) String() string { return s.Describe() }
