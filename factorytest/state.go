package factorytest

import (
	"encoding/json"
	"math"
)

// live is the live part of a camera session's state (schema Live).
type live struct {
	Phase  string  `json:"phase"`
	Warmup float64 `json:"warmup"`
	Since  float64 `json:"since"`
	Lag    float64 `json:"lag"`
}

// focus is the shared stone focus.
type focus struct {
	N       int64   `json:"n"`
	Session int64   `json:"session"`
	Video   string  `json:"video"`
	ID      int64   `json:"id"`
	G       int64   `json:"g"`
	T       float64 `json:"t"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Z       float64 `json:"z"`
	L       float64 `json:"L"`
	W       float64 `json:"W"`
	Yaw     float64 `json:"yaw"`
}

// state is the shared session state (schema State, without media_time, which is computed).
type state struct {
	Session     int64    `json:"session"`
	Revision    int64    `json:"revision"`
	Video       string   `json:"video"`
	Playing     bool     `json:"playing"`
	Rate        float64  `json:"rate"`
	AnchorMedia float64  `json:"anchor_media"`
	AnchorWall  float64  `json:"anchor_wall"`
	HoldUntil   float64  `json:"hold_until"`
	Duration    *float64 `json:"duration"`
	ChangedBy   string   `json:"changed_by"`
	Focus       *focus   `json:"focus"`
	Live        *live    `json:"live,omitempty"`
}

// mediaTime is the media time at wall time now: the playhead holds until hold_until after a select or seek.
func (s *state) mediaTime(now float64) float64 {
	t := s.AnchorMedia
	if s.Playing {
		start := math.Max(s.AnchorWall, s.HoldUntil)
		if now > start {
			t += float64((now - start) * s.Rate)
		}
	}
	if s.Duration != nil && t > *s.Duration {
		t = *s.Duration
	}
	return t
}

// json is the state as the API sends it, with media_time at now.
func (s *state) json(now float64) map[string]any {
	b, _ := json.Marshal(s)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	m["media_time"] = s.mediaTime(now)
	return m
}

func (f *Fake) initialState(clock json.RawMessage, first *entry) state {
	var st state
	if clock != nil && json.Unmarshal(clock, &st) == nil && st.Session != 0 {
		return st
	}
	st = state{Playing: true, Rate: 0.8, AnchorWall: fakeEpoch, HoldUntil: fakeEpoch, ChangedBy: "start"}
	if first != nil {
		st.Session, st.Video, st.AnchorMedia = first.session, first.video, first.t
	}
	if src := f.source(st.Video); src != nil {
		if d, ok := src["duration"].(float64); ok {
			st.Duration = &d
		}
	}
	if st.Session == 0 {
		st.Session = 1
	}
	if st.Video == "" {
		st.Video = f.recSrc
	}
	return st
}

// play moves the playhead on to timeline index to. The streams and long polls play the recording as fast as they read
// it, and each later clock line of the recording they pass (the first is the state at the start) is the session state
// from then on, as the unit's state changes where its clock events say (its own rate, a pause, a seek), and a select
// or seek plays on at the rate in force; the fake's wall clock does not move with them (as in the other SDKs). A control
// makes the state the fake's own (it moves the playhead past every line), and the camera's program has no line of the
// recording (its lines lie before where it starts). The caller holds f.mu.
func (f *Fake) play(to int) {
	for ; f.played < to; f.played++ {
		e := f.tl[f.played]
		var st state
		if !e.later || f.played < f.start || json.Unmarshal(e.data, &st) != nil || st.Session == 0 {
			continue
		}
		f.st = st
		if !f.isLiveVideo(st.Video) {
			f.rate = st.Rate
		}
	}
}

func (f *Fake) workerJSON() json.RawMessage {
	if f.worker != nil {
		return f.worker
	}
	b, _ := json.Marshal(map[string]any{"state": "running", "reason": "", "session": f.st.Session, "video": f.st.Video})
	return b
}

func (f *Fake) stableJSON() map[string]any {
	return map[string]any{"state": "running", "fits": 4, "evidence": true, "fixer": "thin-v1"}
}

// sessionReply is getSession's answer. The caller holds f.mu.
func (f *Fake) sessionReply() map[string]any {
	now := f.wall()
	return map[string]any{"server_now": now, "server_recv": now, "state": f.st.json(now),
		"worker": f.workerJSON(), "viewers": f.viewers, "stable": f.stableJSON()}
}

// clockEntry is a clock side event of the current state, now. The caller holds f.mu.
func (f *Fake) clockEntry() *entry { return f.clockEntryAt(f.wall()) }

// clockEntryAt is a clock side event of the current state sent at server wall time at. The caller holds f.mu.
func (f *Fake) clockEntryAt(at float64) *entry {
	m := f.st.json(at)
	m["server_now"] = at
	b, _ := json.Marshal(m)
	return &entry{side: "clock", data: b}
}
