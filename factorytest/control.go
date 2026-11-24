package factorytest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/eprojac/pdigger-factory-sdk-go/codec"
)

// maxControlBody is the server's control body limit.
const maxControlBody = 512

// checkControl refuses what the server refuses before reading a control: a foreign Origin, a body over 512 bytes,
// a body that is not JSON, and JSON that is not an object.
func checkControl(r *http.Request, body []byte, op string) *apiError {
	if o := r.Header.Get("Origin"); o != "" {
		if u, err := url.Parse(o); err != nil || u.Host != r.Host {
			return errorOf(403, "cross_origin", "cross-origin control refused", "")
		}
	}
	if op == "deleteFocus" {
		return nil
	}
	if len(body) > maxControlBody {
		return errorOf(413, "body_too_large", "body must be 1..512 bytes", "")
	}
	if len(body) == 0 {
		if op == "sessionPlay" || op == "sessionPause" {
			return nil
		}
		return errorOf(400, "invalid_json", "invalid JSON", "")
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		return errorOf(415, "json_only", "JSON only", "")
	}
	if !json.Valid(body) {
		return errorOf(400, "invalid_json", "invalid JSON", "")
	}
	if b := bytes.TrimSpace(body); b[0] != '{' {
		return errorOf(400, "not_an_object", "the body must be a JSON object", "")
	}
	return nil
}

func (f *Fake) serveControl(w http.ResponseWriter, r *http.Request, op string, body []byte) {
	if err := checkControl(r, body, op); err != nil {
		err.write(w)
		return
	}
	dry := false
	switch r.URL.Query().Get("dry_run") {
	case "", "0":
	case "1":
		dry = true
	default:
		errorOf(400, "invalid_param", "dry_run must be 0 or 1", "dry_run").write(w)
		return
	}
	req := map[string]json.RawMessage{}
	if op != "deleteFocus" && len(body) > 0 {
		req, _ = decodeObject(body)
	}
	f.mu.Lock()
	reply, err := f.control(op, req, dry)
	f.mu.Unlock()
	if err != nil {
		err.write(w)
		return
	}
	writeJSON(w, 200, reply)
}

// control validates a control and, unless dry, applies it. The caller holds f.mu.
func (f *Fake) control(op string, req map[string]json.RawMessage, dry bool) (map[string]any, *apiError) {
	now := f.wall()
	next := f.st
	program := false
	var err *apiError
	switch op {
	case "sessionSelect":
		next, err = f.selectState(req)
		program = err == nil
	case "sessionSeek":
		next, err = f.seekState(req)
		program = err == nil
	case "sessionPlay":
		if !next.Playing {
			if l := next.Live; l != nil {
				if l.Phase == "live" {
					next.AnchorMedia = math.Max(0, now-l.Since-l.Lag) // a live camera plays on at its live edge
				}
				c := *l
				c.Phase = "connecting" // and connects again: its next frames start a new warm-up
				next.Live = &c
			}
			next.Playing, next.AnchorWall = true, now
			f.bump(&next)
		}
	case "sessionPause":
		if next.Playing {
			next.AnchorMedia, next.AnchorWall, next.Playing = next.mediaTime(now), now, false
			f.bump(&next)
		}
	case "putFocus":
		next, err = f.focusState(req)
	default: // deleteFocus
		if next.Focus != nil {
			next.Focus = nil
			f.bump(&next)
		}
	}
	if err != nil {
		return nil, err
	}
	if !dry && next.Revision != f.st.Revision {
		f.played = len(f.tl) // the state is the fake's own from now on: the recording's later clock lines are history
		f.st = next
		switch {
		case program:
			f.session = next.Session
			f.startProgram()
		case op == "sessionPlay" && next.Live != nil:
			f.restartCamera()
			f.appendTimeline(f.clockEntry())
		default:
			f.appendTimeline(f.clockEntry())
		}
	}
	return map[string]any{"state": next.json(now), "server_now": now, "server_recv": now, "dry_run": dry}, nil
}

// bump marks a state as changed by a viewer.
func (f *Fake) bump(s *state) {
	s.Revision++
	s.ChangedBy = "viewer"
}

// isLiveVideo reports whether id is a live camera: one the catalog lists as live, or the design's old id "live".
func (f *Fake) isLiveVideo(id string) bool {
	return id == "live" || f.source(id)["live"] == true
}

// timeRange is the server's message for a time outside a video.
func timeRange(src map[string]any) string {
	if d, ok := src["duration"].(float64); ok {
		return "time must be within 0.." + strconv.FormatFloat(d, 'f', 3, 64) + " s"
	}
	return "time must be at least 0 s"
}

// mediaTimeOf reads the time field: present reports whether it was given (null is a time of the wrong type, as on
// the unit).
func mediaTimeOf(req map[string]json.RawMessage, src map[string]any) (t float64, present bool, err *apiError) {
	raw, ok := req["time"]
	if !ok {
		return 0, false, nil
	}
	var v float64
	if string(raw) == "null" || json.Unmarshal(raw, &v) != nil {
		return 0, true, errorOf(400, "invalid_type", timeRange(src), "time")
	}
	d, hasDuration := src["duration"].(float64)
	if v < 0 || (hasDuration && v > d) {
		return 0, true, errorOf(400, "time_out_of_range", timeRange(src), "time")
	}
	return v, true, nil
}

// videoOf reads the video field: the source, or an error in the server's words (missing: missing_field; not a
// string, null too: invalid_type; an id the catalog does not list: unknown_video).
func (f *Fake) videoOf(req map[string]json.RawMessage) (map[string]any, *apiError) {
	raw, ok := req["video"]
	if !ok {
		return nil, errorOf(400, "missing_field", "Unknown video None", "video")
	}
	var id string
	if json.Unmarshal(raw, &id) != nil || string(raw) == "null" {
		return nil, errorOf(400, "invalid_type", "Unknown video "+pyRepr(raw), "video")
	}
	src := f.source(id)
	if src == nil {
		return nil, errorOf(400, "unknown_video", fmt.Sprintf("Unknown video '%s'", id), "video")
	}
	return src, nil
}

func (f *Fake) selectState(req map[string]json.RawMessage) (state, *apiError) {
	src, err := f.videoOf(req)
	if err != nil {
		return state{}, err
	}
	id := src["id"].(string)
	if _, ok := req["time"]; ok && f.isLiveVideo(id) { // any time, null too
		return state{}, errorOf(400, "not_seekable", id+" is a live camera: leave time out", "time")
	}
	t, present, err := mediaTimeOf(req, src)
	if err != nil {
		return state{}, err
	}
	if !present {
		t, _ = src["default_start"].(float64)
	}
	return f.newSession(src, t), nil
}

// seekState checks a seek in the unit's order: its video (the one it names, else the one shown), a camera (409: a
// seek in a recording is applied even while the session shows a camera), then the time (missing, its type, the range).
func (f *Fake) seekState(req map[string]json.RawMessage) (state, *apiError) {
	src := f.source(f.st.Video)
	if _, named := req["video"]; named {
		var err *apiError
		if src, err = f.videoOf(req); err != nil {
			return state{}, err
		}
	}
	if src == nil {
		return state{}, errorOf(409, "not_seekable", "the session shows no video", "video")
	}
	if id := src["id"].(string); f.isLiveVideo(id) {
		return state{}, errorOf(409, "not_seekable", id+" is a live camera and cannot seek", "video")
	}
	t, present, err := mediaTimeOf(req, src)
	switch {
	case err != nil:
		return state{}, err
	case !present:
		return state{}, errorOf(400, "missing_field", timeRange(src), "time")
	}
	return f.newSession(src, t), nil
}

// newSession is the state of a new session on a source from media time t (the playing state is kept). On a camera
// it is connecting (from media time 0, at rate 1.0). The caller holds f.mu.
func (f *Fake) newSession(src map[string]any, t float64) state {
	now := f.wall()
	id := src["id"].(string)
	if f.isLiveVideo(id) {
		next := f.connecting(id, state{Session: f.session + 1, Revision: f.st.Revision, Playing: f.st.Playing}, now)
		f.bump(&next)
		return next
	}
	next := f.st
	next.Session, next.Video, next.AnchorMedia, next.AnchorWall = f.session+1, id, t, now
	hold, ok := src["hold"].(float64)
	if !ok {
		hold = 1.4
	}
	next.HoldUntil = now + hold
	next.Duration = nil
	if d, ok := src["duration"].(float64); ok {
		next.Duration = &d
	}
	next.Rate, next.Focus, next.Live = f.rate, nil, nil
	f.bump(&next)
	return next
}

// startProgram appends the new session to the timeline: its clock event, then the recording's frames from its
// start, relabelled with the new session. A camera session's frames are held until it goes live (startCamera).
// Streams move on to it. The caller holds f.mu.
func (f *Fake) startProgram() {
	f.start = len(f.tl)
	f.phases, f.held = nil, nil
	es := []*entry{f.clockEntry()}
	switch {
	case f.isLiveVideo(f.st.Video):
		f.startCamera()
	case f.st.Video == f.recSrc:
		for _, e := range f.rec {
			if e.t >= f.st.AnchorMedia {
				es = append(es, f.relabel(e, e.t))
			}
		}
	}
	f.appendTimeline(es...)
}

// relabel copies a recorded frame into the current session with a new cursor and media time t.
func (f *Fake) relabel(e *entry, t float64) *entry {
	f.cursor++
	n := *e
	n.obj = make(map[string]json.RawMessage, len(e.obj))
	for k, v := range e.obj {
		n.obj[k] = v
	}
	n.cursor, n.session, n.video, n.t = f.cursor, f.st.Session, f.st.Video, math.Round(t*1e6)/1e6
	n.obj["cursor"], _ = json.Marshal(n.cursor)
	n.obj["session"], _ = json.Marshal(n.session)
	n.obj["video"], _ = json.Marshal(n.video)
	n.obj["t"], _ = json.Marshal(n.t)
	return &n
}

func intField(req map[string]json.RawMessage, key string) (int64, bool, bool) {
	raw, ok := req[key]
	if !ok || string(raw) == "null" {
		return 0, false, true
	}
	var v int64
	if json.Unmarshal(raw, &v) != nil {
		return 0, true, false
	}
	return v, true, true
}

func round4(v float64) float64 { return math.Round(v*1e4) / 1e4 }

func (f *Fake) focusState(req map[string]json.RawMessage) (state, *apiError) {
	const msg = "stone_id must be an integer within 0..1000000000"
	id, present, ok := intField(req, "stone_id")
	switch {
	case !present:
		return state{}, errorOf(400, "missing_field", msg, "stone_id")
	case !ok || id < 0 || id > 1000000000:
		return state{}, errorOf(400, "invalid_type", msg, "stone_id")
	}
	g, hasG, ok := intField(req, "g")
	if !ok {
		return state{}, errorOf(400, "invalid_type", "g must be an integer", "g")
	}
	session, hasSession, ok := intField(req, "session")
	if !ok {
		return state{}, errorOf(400, "invalid_type", "session must be an integer", "session")
	}
	if hasSession && session != f.st.Session {
		return state{}, errorOf(409, "stale_session", "that stone belongs to an earlier session", "session")
	}
	by, gs := f.current()
	var hasWorld bool
	var at *entry
	var stone *worldStone
	for i := len(gs) - 1; i >= 0; i-- {
		e := by[gs[i]]
		w, ok := worldOf(e)
		if !ok {
			continue
		}
		hasWorld = true
		if hasG && e.g != g {
			continue
		}
		if s := w.find(id); s != nil {
			at, stone = e, s
			break
		}
	}
	if !hasWorld {
		return state{}, &apiError{status: 503, body: map[string]any{"error": "the Stable sim is not running",
			"code": "world_unavailable", "retry_ms": 1000}}
	}
	if stone == nil {
		return state{}, errorOf(404, "unknown_stone", fmt.Sprintf("no stone %d in the world", id), "stone_id")
	}
	next := f.st
	f.bump(&next)
	fc := &focus{N: next.Revision, Session: next.Session, Video: next.Video, ID: id, G: at.g, T: at.t}
	if s := stone.Solid; s != nil {
		p, d := codec.Vec3(s.Pos), codec.Vec3(s.Dims)
		fc.X, fc.Y, fc.Z, fc.L, fc.W = round4(p[0]), round4(p[1]), round4(p[2]), round4(d[0]), round4(d[1])
		fc.Yaw = round4(codec.QuatToYaw(codec.Quat(s.Q)))
	}
	next.Focus = fc
	return next, nil
}

// pyRepr is a JSON value other than a string as the unit's messages show it (Python's repr): None, True, False, else
// its JSON.
func pyRepr(raw json.RawMessage) string {
	switch t := string(bytes.TrimSpace(raw)); t {
	case "null":
		return "None"
	case "true":
		return "True"
	case "false":
		return "False"
	default:
		return t
	}
}
