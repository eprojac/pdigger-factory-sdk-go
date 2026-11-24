package factorytest

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
)

// LiveCamera is the id of the fake's live camera (WithLiveCamera).
const LiveCamera = "cam-live"

// ConnectSeconds is how long the fake's camera takes to send its first frame after a select: the session is
// connecting meanwhile.
const ConnectSeconds = 0.5

// LiveLagSeconds is how far the fake's camera session is behind the camera once it is live (the unit's interpreter
// time).
const LiveLagSeconds = 0.25

// KeepPictures is how many camera pictures the fake keeps, as the unit's worker does: those of the newest released
// frames of the camera session; an older frame that is still retained has none (404 not_retained).
const KeepPictures = 64

// liveHold is how far ahead of a camera select hold_until stays until the session goes live (as on the unit).
const liveHold = 3600.0

// liveGap is how many grid frame numbers a camera session skips at a break, as the unit's worker does: the first
// frame after a pause is the last one's g + 1 + liveGap.
const liveGap = 4

// fakeJPEG is a 16 x 9 grey JPEG: the picture of every frame of the fake's camera (with the frame in a comment
// segment).
const fakeJPEG = "/9j/4AAQSkZJRgABAQAAAQABAAD/2wBDABQODxIPDRQSEBIXFRQYHjIhHhwcHj0sLiQySUBMS0dARkVQWnNiUFVtVkVGZIhlbXd7gYKBTmCNl4x9" +
	"lnN+gXz/2wBDARUXFx4aHjshITt8U0ZTfHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHx8fHz/wAARCAAJABAD" +
	"ASIAAhEBAxEB/8QAFQABAQAAAAAAAAAAAAAAAAAAAAP/xAAUEAEAAAAAAAAAAAAAAAAAAAAA/8QAFQEBAQAAAAAAAAAAAAAAAAAAAAH/xAAUEQEA" +
	"AAAAAAAAAAAAAAAAAAAA/9oADAMBAAIRAxEAPwCYCD//2Q=="

// phase is a phase of a camera session still to come: the state it takes at wall time at.
type phase struct {
	at float64
	st state
}

// number reads a catalog value as a number (JSON numbers decode as float64; the fake's own entries use ints).
func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

// cameraSource is the catalog entry of the fake's live camera, sized like like (the first catalog entry, nil for
// none) and shaped as a plant camera's.
func cameraSource(like map[string]any) map[string]any {
	gridFPS := 5.0
	if fps, ok := number(like["fps"]); ok && fps > 0 {
		step, _ := number(like["grid_step"])
		gridFPS = fps / math.Max(step, 1)
	}
	size := func(key string, w, h int) any {
		switch wh := like[key].(type) {
		case []any:
			return wh
		case []int:
			return wh
		}
		return []int{w, h}
	}
	return map[string]any{"id": LiveCamera, "title": "Live camera", "subtitle": "The recording, replayed as a live camera",
		"duration": nil, "fps": math.Round(gridFPS*10) / 10, "frames": nil, "grid_step": 1, "default_start": nil,
		"hold": 3.0, "proxy": "", "proxy_wh": size("proxy_wh", 960, 540), "native_wh": size("native_wh", 1920, 1080),
		"held_out": []any{}, "kind": "camera", "live": true, "seekable": false, "station": "FakeFactory"}
}

// fakePicture is the fake camera's JPEG of frame g: fakeJPEG with "FakeFactory <video> g <g>" in a comment segment.
func fakePicture(video string, g int64) []byte {
	jpeg, _ := base64.StdEncoding.DecodeString(fakeJPEG)
	note := "FakeFactory " + video + " g " + strconv.FormatInt(g, 10)
	segment := binary.BigEndian.AppendUint16([]byte{0xff, 0xfe}, uint16(len(note)+2))
	out := append(append(append([]byte{}, jpeg[:2]...), segment...), note...)
	return append(out, jpeg[2:]...)
}

// warmup is the warm-up of camera video: its hold, 3 s by default. The caller holds f.mu.
func (f *Fake) warmup(video string) float64 {
	if h, ok := number(f.source(video)["hold"]); ok && h > 0 {
		return h
	}
	return 3
}

// cameraFrame copies a recorded frame into session session of camera video as its frame g at t, numbered as a
// camera's frames are (g = i, the raw packet's g, i and t the frame's, the overlay's prev_g moved with g): the frame
// and its world and raw layers move to that session. It has no cursor yet (stamp gives it one when it is released).
func cameraFrame(e *entry, session int64, video string, g int64, t float64) *entry {
	n := *e
	n.obj = make(map[string]json.RawMessage, len(e.obj))
	for k, v := range e.obj {
		n.obj[k] = v
	}
	n.session, n.video, n.g, n.t, n.wv = session, video, g, t, nil
	set := func(m map[string]json.RawMessage, k string, v any) { m[k], _ = json.Marshal(v) }
	set(n.obj, "session", session)
	set(n.obj, "video", video)
	set(n.obj, "g", g)
	set(n.obj, "i", g)
	set(n.obj, "t", t)
	for _, layer := range []string{"world", "raw", "overlay"} {
		var m map[string]json.RawMessage
		if json.Unmarshal(n.obj[layer], &m) != nil || m == nil {
			continue
		}
		switch layer {
		case "world":
			set(m, "session", session)
			set(m, "video", video)
			set(m, "g", g)
			set(m, "t", t)
		case "raw":
			set(m, "session", session)
			set(m, "video", video)
			set(m, "g", g)
			set(m, "i", g)
			set(m, "t", t)
		default: // overlay: prev_g is the frame before, numbered as the camera's
			var prev int64
			if json.Unmarshal(m["prev_g"], &prev) == nil {
				set(m, "prev_g", prev+g-e.g)
			}
		}
		n.obj[layer], _ = json.Marshal(m)
	}
	return &n
}

// cameraRun is a run of camera video's frames in session session: the recording's frames, its sessions end to end,
// as the camera sends them once connected at session time t0 (seconds since the session started), numbered from g0
// and arriving from ConnectSeconds after t0 at the recording's pace. It returns the frames released after the run's
// warm-up (those of its first hold seconds are numbered but never released), the t the run goes live at (its first
// released frame's; the warm-up's end when there is none), and the g and t after its last frame. The caller holds
// f.mu.
func (f *Fake) cameraRun(video string, session, g0 int64, t0 float64) (released []*entry, liveT float64, nextG int64,
	nextT float64) {
	warmEnd := t0 + ConnectSeconds + f.warmup(video)
	var segment, baseG, recG0 int64
	baseT, recT0 := 0.0, 0.0
	nextG, nextT = g0, t0+ConnectSeconds
	for i, e := range f.rec {
		if i == 0 || e.session != segment {
			segment, baseG, baseT, recG0, recT0 = e.session, nextG, nextT, e.g, e.t
		}
		g := baseG + e.g - recG0
		t := math.Round((baseT+e.t-recT0)*1e6) / 1e6
		nextG, nextT = max(nextG, g+1), max(nextT, t+0.2)
		if t >= warmEnd-1e-9 {
			released = append(released, cameraFrame(e, session, video, g, t))
		}
	}
	liveT = warmEnd
	if len(released) > 0 {
		liveT = released[0].t
	}
	return released, liveT, nextG, nextT
}

// connecting is the state of a new session of camera video that starts at wall time since (start gives its session,
// revision, playing state and changed_by): connecting, from media time 0 at rate 1.0, held until it goes live.
func (f *Fake) connecting(video string, start state, since float64) state {
	return state{Session: start.Session, Revision: start.Revision, Video: video, Playing: start.Playing, Rate: 1,
		AnchorWall: since, HoldUntil: since + liveHold, ChangedBy: start.ChangedBy,
		Live: &live{Phase: "connecting", Warmup: f.warmup(video), Since: since}}
}

// cameraPhases are the phases a camera session takes once it connects at wall time at, from base (its connecting
// state): warming ConnectSeconds later, while the interpreter runs on the run's first hold seconds, then live when
// the frame of time liveT is released, LiveLagSeconds behind it (the playhead starts there). Each bumps the revision,
// as the unit's clock does. Warming changes only the phase, as the unit's: its lag is base's (0 after a select, the
// live lag after a play).
func cameraPhases(base state, at, liveT float64) []phase {
	warming := base
	warming.Revision++
	warming.Live = &live{Phase: "warming", Warmup: base.Live.Warmup, Since: base.Live.Since, Lag: base.Live.Lag}
	wentLive := base.Live.Since + liveT + LiveLagSeconds
	onAir := base
	onAir.Revision += 2
	onAir.AnchorMedia, onAir.AnchorWall, onAir.HoldUntil = liveT, wentLive, wentLive
	onAir.Live = &live{Phase: "live", Warmup: base.Live.Warmup, Since: base.Live.Since, Lag: LiveLagSeconds}
	return []phase{{at + ConnectSeconds, warming}, {wentLive, onAir}}
}

// offerCamera shows the live camera from the start, as if it had been selected just long enough ago to be live now:
// the current program is its session, with the frames after its warm-up. The caller holds f.mu (or is New).
func (f *Fake) offerCamera(first *entry) {
	session := int64(1)
	if first != nil && first.session != 0 {
		session = first.session
	}
	frames, liveT, nextG, nextT := f.cameraRun(LiveCamera, session, 0, 0)
	since := f.wall() - liveT - LiveLagSeconds
	start := f.connecting(LiveCamera, state{Session: session, Revision: 1, Playing: true, ChangedBy: "start"}, since)
	f.st = cameraPhases(start, since, liveT)[1].st
	f.camG, f.camT = nextG, nextT
	f.start = len(f.tl)
	f.appendTimeline(append([]*entry{f.clockEntry()}, f.stamp(frames)...)...)
}

// startCamera plans the phases of the camera session the state has just started (connecting) and holds its first
// run's frames until it goes live. The caller holds f.mu.
func (f *Fake) startCamera() {
	frames, liveT, nextG, nextT := f.cameraRun(f.st.Video, f.st.Session, 0, 0)
	f.phases, f.held = cameraPhases(f.st, f.st.AnchorWall, liveT), frames
	f.camG, f.camT = nextG, nextT
}

// restartCamera plans the camera session's next run after a play (the state is connecting again): the camera
// connects now and warms up again on its next frames. Once frames were released, the run is numbered after a break
// of liveGap frames (G jumps, as on the unit) and arrives after the frames already sent: the fake's camera has sent
// its whole run by the time it goes live. The caller holds f.mu.
func (f *Fake) restartCamera() {
	now := f.wall()
	g0, t0 := int64(0), now-f.st.Live.Since
	if _, gs := f.current(); len(gs) > 0 {
		g0, t0 = f.camG+liveGap, math.Max(t0, f.camT)
	}
	frames, liveT, nextG, nextT := f.cameraRun(f.st.Video, f.st.Session, g0, t0)
	f.phases, f.held = cameraPhases(f.st, now, liveT), frames
	f.camG, f.camT = nextG, nextT
}

// stamp gives frames the next cursors (the raw layer's too), in order. The caller holds f.mu.
func (f *Fake) stamp(frames []*entry) []*entry {
	for _, e := range frames {
		f.cursor++
		e.cursor = f.cursor
		e.obj["cursor"], _ = json.Marshal(e.cursor)
		var m map[string]json.RawMessage
		if json.Unmarshal(e.obj["raw"], &m) == nil && m != nil {
			m["cursor"] = e.obj["cursor"]
			e.obj["raw"], _ = json.Marshal(m)
		}
	}
	return frames
}

// advance takes the camera phases whose wall time has come, while the session plays (nothing runs while it is
// paused: a play plans the phases again). The caller holds f.mu.
func (f *Fake) advance() {
	for f.st.Playing && len(f.phases) > 0 && f.phases[0].at <= f.wall() {
		p := f.phases[0]
		f.phases = f.phases[1:]
		f.takePhase(p)
	}
}

// skipToPhase is how a stream or long poll waits for the camera's next phase when the fake's clock stands still (no
// WithClock): it moves the clock on to the phase, if it comes by wall time until, and takes it, while the session
// plays. False when there is none to take (a linked clock is never moved: its own time takes the phases). The caller
// holds f.mu.
func (f *Fake) skipToPhase(until float64) bool {
	if f.clock != nil || !f.st.Playing || len(f.phases) == 0 || f.phases[0].at > until {
		return false
	}
	if w := f.wall(); f.phases[0].at > w {
		f.now += f.phases[0].at - w
	}
	f.advance()
	return true
}

// phasePending reports whether a stream waiting on a linked clock should look again for the camera's next phase. The
// caller holds f.mu.
func (f *Fake) phasePending() bool { return f.clock != nil && f.st.Playing && len(f.phases) > 0 }

// takePhase takes a camera phase after any control made since it was planned (a higher revision; the playing state,
// the focus and what changed it kept). Going live releases the run's frames after the warm-up. The caller holds f.mu.
func (f *Fake) takePhase(p phase) {
	next := p.st
	next.Revision = max(p.st.Revision, f.st.Revision+1)
	next.Playing, next.Focus, next.ChangedBy = f.st.Playing, f.st.Focus, f.st.ChangedBy
	f.st = next
	es := []*entry{f.clockEntryAt(p.at)}
	if next.Live.Phase == "live" {
		es = append(es, f.stamp(f.held)...)
		f.held = nil
	}
	f.appendTimeline(es...)
}

// gPattern is the unit's grid frame parameter: an integer of 1 to 10 digits, or latest.
var gPattern = regexp.MustCompile(`^(latest|[0-9]{1,10})$`)

// pictureFrame is the frame whose camera picture GET frames/{g}/image.jpg asks for, checked in the unit's order: g,
// a stale session (gone whatever it showed), a recording, the frame, its picture (only the newest KeepPictures
// released frames have one). The caller holds f.mu.
func (f *Fake) pictureFrame(q url.Values, g string) (*entry, *apiError) {
	if !gPattern.MatchString(g) {
		return nil, errorOf(400, "invalid_param", "g must be an integer or latest", "g")
	}
	if err := f.checkSession(q); err != nil {
		return nil, err
	}
	if f.st.Live == nil {
		return nil, errorOf(404, "not_found", f.st.Video+" is a recording: its picture is the proxy video", "g")
	}
	e, err := f.frameEntry(g)
	if err != nil {
		return nil, err
	}
	if _, gs := f.current(); len(gs) > KeepPictures && e.g < gs[len(gs)-KeepPictures] {
		return nil, errorOf(404, "not_retained", fmt.Sprintf(
			"the picture of frame %d is no longer kept: only the newest %d pictures are", e.g, KeepPictures), "g")
	}
	return e, nil
}

// servePicture answers the camera picture of a released frame of the camera session.
func (f *Fake) servePicture(w http.ResponseWriter, r *http.Request, g string) {
	f.mu.Lock()
	e, err := f.pictureFrame(r.URL.Query(), g)
	f.mu.Unlock()
	if err != nil {
		err.write(w)
		return
	}
	b := fakePicture(e.video, e.g)
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Interp-G", strconv.FormatInt(e.g, 10))
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(b)
}
