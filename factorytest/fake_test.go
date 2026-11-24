package factorytest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	fixtures = "../fixtures"
	// recJoined is a 22-frame recording on 20.20 (dry1, g 399..420) with a clock line and a worker line.
	recJoined  = fixtures + "/recordings/joined-mid-move.ndjson.gz"
	repliesDir = fixtures + "/api"
)

func mustNew(t *testing.T, opts ...Option) *Fake {
	t.Helper()
	f, err := New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// do sends one request through the in-process transport and returns the status, the headers and the body.
func do(t *testing.T, f *Fake, method, path, body string, header ...string) (int, http.Header, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, f.URL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := f.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, b
}

// code is the error code of an error body ("" for other bodies).
func code(b []byte) string {
	var e struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(b, &e)
	return e.Code
}

// expect checks a request's status and error code and returns the body.
func expect(t *testing.T, f *Fake, method, path, body string, status int, errCode string, header ...string) []byte {
	t.Helper()
	got, _, b := do(t, f, method, path, body, header...)
	if got != status || code(b) != errCode {
		t.Fatalf("%s %s %s: %d %q, want %d %q: %.300s", method, path, body, got, code(b), status, errCode, b)
	}
	return b
}

// ctl sends a control with a JSON body and returns the decoded reply.
func ctl(t *testing.T, f *Fake, method, path, body string, status int, errCode string) map[string]any {
	t.Helper()
	b := expect(t, f, method, path, body, status, errCode, "Content-Type", "application/json")
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func stateOf(m map[string]any) map[string]any {
	s, _ := m["state"].(map[string]any)
	return s
}

func decode[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%v: %.200s", err, b)
	}
	return v
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func mustPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: no panic", name)
		}
	}()
	fn()
}

func TestRoute(t *testing.T) {
	for _, c := range []struct{ method, path, op string }{
		{"GET", "/api/v2", "getApiRoot"}, {"GET", "/api/v2/", "getApiRoot"}, {"GET", "/api/v2/openapi.yaml", "getOpenApi"},
		{"GET", "/api/v2/model", "getModel"}, {"GET", "/api/v2/topology", "getTopology"}, {"GET", "/api/v2/health", "getHealth"},
		{"GET", "/api/v2/ready", "getReady"}, {"GET", "/api/v2/status", "getStatus"}, {"GET", "/metrics", "getMetrics"},
		{"GET", "/api/v2/videos", "listVideos"}, {"GET", "/api/v2/videos/dry1", "getVideo"},
		{"GET", "/api/v2/videos/dry1/media", "getVideoMedia"}, {"HEAD", "/api/v2/videos/dry1/media", "getVideoMedia"},
		{"GET", "/api/v2/session", "getSession"}, {"POST", "/api/v2/session/select", "sessionSelect"},
		{"POST", "/api/v2/session/seek", "sessionSeek"}, {"POST", "/api/v2/session/play", "sessionPlay"},
		{"POST", "/api/v2/session/pause", "sessionPause"}, {"GET", "/api/v2/session/focus", "getFocus"},
		{"PUT", "/api/v2/session/focus", "putFocus"}, {"DELETE", "/api/v2/session/focus", "deleteFocus"},
		{"GET", "/api/v2/session/world", "getWorld"}, {"GET", "/api/v2/session/world/events", "getWorldEvents"},
		{"GET", "/api/v2/session/world/stones/72", "getStone"}, {"GET", "/api/v2/session/frames/stream", "streamFrames"},
		{"GET", "/api/v2/session/frames", "pollFrames"}, {"GET", "/api/v2/session/frames/12", "getFrame"},
		{"GET", "/api/v2/session/frames/12/regions.png", "getFrameRaster"},
		{"GET", "/api/v2/session/render/overlay2d", "renderOverlay2d"},
		{"GET", "/api/v2/session/render/scene3d", "renderScene3d"}, {"GET", "/api/v2/raw/stream", "rawStream"},
		{"GET", "/api/v2/raw/packets/latest", "rawPacket"}, {"GET", "/api/v2/raw/tensors/12", "rawTensors"},
	} {
		if op, _, known := route(c.method, c.path); op != c.op || !known {
			t.Errorf("%s %s: %q", c.method, c.path, op)
		}
	}
	for _, c := range []struct {
		method, path string
		known        bool
	}{
		{"POST", "/api/v2/health", true}, {"GET", "/api/v2/session/select", true}, {"GET", "/api/v1/health", false},
		{"GET", "/api/v2/nope", false}, {"GET", "/api/v2/session/frames/1/x.jpg", false}, {"GET", "/", false},
	} {
		if op, _, known := route(c.method, c.path); op != "" || known != c.known {
			t.Errorf("%s %s: %q %v", c.method, c.path, op, known)
		}
	}
	f := mustNew(t)
	expect(t, f, "GET", "/nope", "", 404, "not_found")
	expect(t, f, "DELETE", "/api/v2/session/select", "", 405, "method_not_allowed")
	expect(t, f, "GET", "/api/v2/health", "x", 400, "body_not_allowed")
	// a canned reply without a status is a 200, and its Content-Length is the body's
	f.FailNext("getHealth", 1, Reply{Body: []byte(`{"ok":false}`), Headers: map[string]string{"Content-Length": "1", "X-A": "b"}})
	if st, h, b := do(t, f, "GET", "/api/v2/health", ""); st != 200 || h.Get("X-A") != "b" || string(b) != `{"ok":false}` {
		t.Fatal(st, h, string(b))
	}
	if err := f.ReplyNext("getHealth", 1, "nope"); err == nil {
		t.Fatal("ReplyNext of an unknown reply")
	}
}

func TestSynthesisedMeta(t *testing.T) {
	f := mustNew(t, WithRecording(recJoined), WithBoot("boot42"))
	root := decode[map[string]any](t, expect(t, f, "GET", "/api/v2", "", 200, ""))
	if root["boot"] != "boot42" || root["api"] != "2.2.0" || root["build"] != "claude-20261122-hr4x1" ||
		root["backend"] != "trt" || f.Boot() != "boot42" {
		t.Fatal(root)
	}
	model := decode[map[string]any](t, expect(t, f, "GET", "/api/v2/model", "", 200, ""))
	if model["schema"] != "mdl1" || model["checkpoint"] != "8d09531c915f" || model["backend"] != "trt" {
		t.Fatal(model)
	}
	if b := expect(t, f, "GET", "/api/v2/topology", "", 200, ""); !strings.Contains(string(b), `"top1"`) {
		t.Fatal(string(b))
	}
	expect(t, f, "GET", "/api/v2/health", "", 200, "")
	expect(t, f, "GET", "/api/v2/ready", "", 200, "")
	status := decode[map[string]any](t, expect(t, f, "GET", "/api/v2/status", "", 200, ""))
	if ret, _ := status["retained"].(map[string]any); ret["g0"] != 399.0 || ret["g1"] != 420.0 || status["boot"] != "boot42" {
		t.Fatal(status)
	}
	expect(t, f, "GET", "/api/v2/openapi.yaml", "", 404, "not_found")
	expect(t, f, "GET", "/api/v2/raw/tensors/abc", "", 400, "invalid_param")
	expect(t, f, "GET", "/api/v2/raw/tensors/3", "", 404, "debug_disabled")
	for _, q := range []string{"", "?g=1&t=2", "?g=1&mode=x", "?g=1&draw=x"} {
		expect(t, f, "GET", "/api/v2/session/render/overlay2d"+q, "", 400, "invalid_param")
	}
	expect(t, f, "GET", "/api/v2/session/render/scene3d?t=1&surface=mesh", "", 400, "invalid_param")
	expect(t, f, "GET", "/api/v2/session/render/overlay2d?t=1&mode=raw&draw=shapes", "", 503, "not_ready")
	expect(t, f, "GET", "/api/v2/session/render/scene3d?g=1&surface=compact", "", 503, "not_ready")
	if b := expect(t, f, "GET", "/metrics", "", 200, ""); !strings.Contains(string(b), `interp_frames_total{video="dry1"} 22`) {
		t.Fatal(string(b))
	}
	// a header without build or backend: the backend is "replay"; no frames: an empty retained window, as the unit's
	g := mustNew(t, WithLines([]byte(`{"recording":"pfr1"}`)))
	root = decode[map[string]any](t, expect(t, g, "GET", "/api/v2", "", 200, ""))
	model = decode[map[string]any](t, expect(t, g, "GET", "/api/v2/model", "", 200, ""))
	status = decode[map[string]any](t, expect(t, g, "GET", "/api/v2/status", "", 200, ""))
	if w, _ := status["retained"].(map[string]any); root["backend"] != "replay" || model["backend"] != "replay" ||
		w == nil || w["n"] != 0.0 || w["g0"] != nil || w["t1"] != nil {
		t.Fatal(root, model, status)
	}
	latest := decode[map[string]any](t, expect(t, g, "GET", "/api/v2/session/frames/latest", "", 404, "not_retained"))
	if w, _ := latest["retained"].(map[string]any); latest["error"] != "no frame is retained yet" || w == nil || w["n"] != 0.0 {
		t.Fatal(latest)
	}
	expect(t, g, "GET", "/api/v2/session/frames/12345678901", "", 400, "invalid_param")
	expect(t, g, "GET", "/api/v2/session/world", "", 503, "world_unavailable")
}

func TestCatalogAndMedia(t *testing.T) {
	f := mustNew(t, WithRecording(recJoined), WithLiveCamera())
	_, h, b := do(t, f, "GET", "/api/v2/videos", "")
	// the camera comes after the recording, sized like it; the default stays the recording
	cat := decode[struct {
		Default string           `json:"default_video"`
		Videos  []map[string]any `json:"videos"`
	}](t, b)
	if cat.Default != "dry1" || len(cat.Videos) != 2 || cat.Videos[0]["id"] != "dry1" || cat.Videos[1]["id"] != LiveCamera {
		t.Fatal(cat)
	}
	if c := cat.Videos[1]; c["kind"] != "camera" || c["live"] != true || c["seekable"] != false || c["fps"] != 5.0 ||
		c["grid_step"] != 1.0 || c["hold"] != 3.0 || c["proxy"] != "" || c["duration"] != nil ||
		c["station"] != "FakeFactory" || c["proxy_wh"].([]any)[0] != 960.0 || c["native_wh"].([]any)[1] != 1080.0 {
		t.Fatal(c)
	}
	if st, _, _ := do(t, f, "GET", "/api/v2/videos", "", "If-None-Match", h.Get("ETag")); st != 304 {
		t.Fatal(st)
	}
	expect(t, f, "GET", "/api/v2/videos/nope", "", 404, "unknown_video")
	expect(t, f, "GET", "/api/v2/videos/nope/media", "", 404, "unknown_video")
	// a camera has no media: its pictures come with its frames
	if b := expect(t, f, "GET", "/api/v2/videos/cam-live/media", "", 404, "not_found"); !strings.Contains(string(b),
		"cam-live is a live camera: it has no proxy video") {
		t.Fatal(string(b))
	}
	st, h, b := do(t, f, "GET", "/api/v2/videos/dry1/media", "")
	if st != 200 || len(b) != 4096 || h.Get("Accept-Ranges") != "bytes" || string(b[4:8]) != "ftyp" {
		t.Fatal(st, len(b))
	}
	tag := h.Get("ETag")
	if st, _, _ := do(t, f, "GET", "/api/v2/videos/dry1/media", "", "If-None-Match", tag); st != 304 {
		t.Fatal(st)
	}
	if st, h, _ := do(t, f, "GET", "/api/v2/videos/dry1/media", "", "Range", "bytes=9000-"); st != 416 || h.Get("Content-Range") != "bytes */4096" {
		t.Fatal(st)
	}
	if st, _, b := do(t, f, "GET", "/api/v2/videos/dry1/media", "", "Range", "bytes=0-9", "If-Range", `"old"`); st != 200 || len(b) != 4096 {
		t.Fatal(st)
	}
	if st, h, b := do(t, f, "GET", "/api/v2/videos/dry1/media", "", "Range", "bytes=10-19", "If-Range", tag); st != 206 ||
		len(b) != 10 || h.Get("Content-Range") != "bytes 10-19/4096" {
		t.Fatal(st)
	}
	if st, h, b := do(t, f, "HEAD", "/api/v2/videos/dry1/media", "", "Range", "bytes=-100"); st != 206 || len(b) != 0 ||
		h.Get("Content-Length") != "100" {
		t.Fatal(st)
	}
	// with recorded replies, the proxy bytes are the recorded ones
	g := mustNew(t, WithRecording(recJoined), WithReplies(repliesDir))
	if _, _, b := do(t, g, "GET", "/api/v2/videos/dry1/media", ""); len(b) != 1024 {
		t.Fatal(len(b))
	}
	for v, want := range map[string][3]int64{
		"bytes=0-9": {0, 9, 1}, "bytes=5-": {5, 99, 1}, "bytes=-10": {90, 99, 1}, "bytes=-500": {0, 99, 1},
		"bytes=90-200": {90, 99, 1}, "bytes=0-9,20-29": {0, 0, 0}, "items=0-9": {0, 0, 0}, "bytes=9": {0, 0, 0},
		"bytes=-0": {0, 0, 0}, "bytes=-x": {0, 0, 0}, "bytes=x-": {0, 0, 0}, "bytes=100-": {0, 0, 0},
		"bytes=5-x": {0, 0, 0}, "bytes=5-4": {0, 0, 0},
	} {
		a, b, ok := parseRange(v, 100)
		if ok != (want[2] == 1) || (ok && (a != want[0] || b != want[1])) {
			t.Errorf("%s: %d-%d %v", v, a, b, ok)
		}
	}
}

func TestRaster(t *testing.T) {
	f := mustNew(t, WithRecording(recJoined))
	expect(t, f, "GET", "/api/v2/session/frames/420/regions.png?scale=9", "", 400, "invalid_param")
	expect(t, f, "GET", "/api/v2/session/frames/420/regions.png?scale=x", "", 400, "invalid_param")
	expect(t, f, "GET", "/api/v2/session/frames/420/heat.png", "", 404, "not_found")
	expect(t, f, "GET", "/api/v2/session/frames/1/mask.png", "", 404, "not_retained")
	if st, h, b := do(t, f, "GET", "/api/v2/session/frames/420/depth.png?scale=2", ""); st != 200 ||
		h.Get("Content-Type") != "image/png" || !strings.HasPrefix(string(b), "\x89PNG") {
		t.Fatal(st)
	}
	g := mustNew(t, WithRecording(recJoined), WithReplies(repliesDir))
	if st, _, b := do(t, g, "GET", "/api/v2/session/frames/420/regions.png", ""); st != 200 || len(b) != 1106 {
		t.Fatal(st, len(b))
	}
	// without a recording: the recorded reply, whatever the frame
	h := mustNew(t, WithReplies(repliesDir))
	if st, _, b := do(t, h, "GET", "/api/v2/session/frames/1/regions.png", ""); st != 200 || len(b) != 1106 {
		t.Fatal(st)
	}
}

// frameLines is a synthetic recording of session 3: g 1 with a raw packet and stone 7, g 2 whose raw and world
// layers are not objects, g 3 without a world, g 4 with a stone that is not an object and stone 8 without a solid,
// g 5 with stone 7 turned 90 degrees; and a line that is not JSON.
func frameLines() [][]byte {
	return [][]byte{
		[]byte(`{"recording":"pfr1","source":{"id":"v","title":"v","duration":60}}`),
		[]byte(`not json`),
		[]byte(`{"cursor":1,"session":3,"video":"v","g":1,"t":0.2,"raw":{"schema":"sip1","g":1},"world":{"stones":[{"id":7,"outline":[[1,2],[3]],"solid":{"pos":[1,2,3],"q":[0,0,0,1],"dims":[0.2,0.1,0.05]}}],"events":[{"type":"new","id":7},{"type":"confirm","id":7}]}}`),
		[]byte(`{"cursor":2,"session":3,"video":"v","g":2,"t":0.4,"raw":5,"world":5}`),
		[]byte(`{"cursor":3,"session":3,"video":"v","g":3,"t":0.6}`),
		[]byte(`{"cursor":4,"session":3,"video":"v","g":4,"t":0.8,"world":{"stones":[5,{"id":8}],"events":[{"type":"freeze","id":8}]}}`),
		[]byte(`{"cursor":5,"session":3,"video":"v","g":5,"t":1.0,"world":{"stones":[{"id":7,"solid":{"pos":[1,2,3],"q":[0,0,0.7071068,0.7071068],"dims":[0.2,0.1,0.05]}}],"events":[{"type":"place","id":7,"side":"left"}]}}`),
	}
}

type eventLog struct {
	Events    []map[string]any `json:"events"`
	NextAfter int64            `json:"next_after"`
	OldestG   int64            `json:"oldest_g"`
}

func TestFramesWorldAndEvents(t *testing.T) {
	f := mustNew(t, WithLines(frameLines()...))
	if s, v, _ := f.SessionState(); s != 3 || v != "v" {
		t.Fatal(s, v)
	}
	if s := decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session", "", 200, "")); stateOf(s)["duration"] != 60.0 {
		t.Fatal(s)
	}
	expect(t, f, "GET", "/api/v2/session/frames/abc", "", 400, "invalid_param")
	expect(t, f, "GET", "/api/v2/session/frames/1?precision=exact", "", 400, "invalid_param")
	expect(t, f, "GET", "/api/v2/session/frames/1?session=x", "", 400, "invalid_param")
	expect(t, f, "GET", "/api/v2/session/frames/1?session=2", "", 410, "session_gone")
	expect(t, f, "GET", "/api/v2/session/frames/1?layers=depth", "", 400, "unknown_layer")
	if b := expect(t, f, "GET", "/api/v2/session/frames/1?layers=raw,world&session=3", "", 200, ""); !strings.Contains(string(b), `"raw"`) {
		t.Fatal(string(b))
	}
	expect(t, f, "GET", "/api/v2/raw/packets/1?session=2", "", 410, "session_gone")
	expect(t, f, "GET", "/api/v2/raw/packets/9", "", 404, "not_retained")
	expect(t, f, "GET", "/api/v2/raw/packets/2", "", 404, "not_retained") // not an object
	expect(t, f, "GET", "/api/v2/raw/packets/3", "", 404, "not_retained") // no raw layer
	expect(t, f, "GET", "/api/v2/raw/packets/1", "", 200, "")
	expect(t, f, "GET", "/api/v2/session/world?precision=x", "", 400, "invalid_param")
	if b := expect(t, f, "GET", "/api/v2/session/world", "", 200, ""); !strings.Contains(string(b), `"place"`) {
		t.Fatal(string(b))
	}
	for _, q := range []string{"after=x", "after=-2", "limit=0", "limit=x", "type=spin"} {
		expect(t, f, "GET", "/api/v2/session/world/events?"+q, "", 400, "invalid_param")
	}
	log := decode[eventLog](t, expect(t, f, "GET", "/api/v2/session/world/events?type=new,place&after=0", "", 200, ""))
	if len(log.Events) != 2 || log.Events[1]["g"] != 5.0 || log.NextAfter != 5 || log.OldestG != 1 {
		t.Fatalf("%+v", log)
	}
	// a limit is kept by whole frames
	log = decode[eventLog](t, expect(t, f, "GET", "/api/v2/session/world/events?limit=1", "", 200, ""))
	if len(log.Events) != 2 || log.NextAfter != 1 {
		t.Fatalf("%+v", log)
	}
	log = decode[eventLog](t, expect(t, f, "GET", "/api/v2/session/world/events?after=1", "", 200, ""))
	if len(log.Events) != 2 || log.NextAfter != 5 {
		t.Fatalf("%+v", log)
	}
	type track struct {
		Path2D [][]float64 `json:"path2d"`
		Path3D [][]float64 `json:"path3d"`
		FirstG int64       `json:"first_g"`
		LastG  int64       `json:"last_g"`
	}
	tr := decode[track](t, expect(t, f, "GET", "/api/v2/session/world/stones/7", "", 200, ""))
	if tr.FirstG != 1 || tr.LastG != 5 || len(tr.Path2D) != 1 || tr.Path2D[0][0] != 2 || tr.Path2D[0][1] != 1 || len(tr.Path3D) != 2 {
		t.Fatalf("%+v", tr)
	}
	expect(t, f, "GET", "/api/v2/session/world/stones/x", "", 400, "invalid_param")
	expect(t, f, "GET", "/api/v2/session/world/stones/99", "", 404, "unknown_stone")
	if b := expect(t, f, "GET", "/api/v2/session/focus", "", 200, ""); string(b) != `{"focus":null,"resolved":null}` {
		t.Fatal(string(b))
	}
	// the focus: validated, applied, resolved
	for _, c := range []struct {
		body   string
		status int
		code   string
	}{
		{`{}`, 400, "missing_field"}, {`{"stone_id":7.5}`, 400, "invalid_type"}, {`{"stone_id":-1}`, 400, "invalid_type"},
		{`{"stone_id":"7"}`, 400, "invalid_type"}, {`{"stone_id":7,"g":"x"}`, 400, "invalid_type"},
		{`{"stone_id":7,"session":"x"}`, 400, "invalid_type"}, {`{"stone_id":7,"session":2}`, 409, "stale_session"},
		{`{"stone_id":99}`, 404, "unknown_stone"}, {`{"stone_id":7,"g":4}`, 404, "unknown_stone"},
	} {
		ctl(t, f, "PUT", "/api/v2/session/focus", c.body, c.status, c.code)
	}
	r := ctl(t, f, "PUT", "/api/v2/session/focus", `{"stone_id":7,"g":1,"session":3,"stone":null}`, 200, "")
	if fc, _ := stateOf(r)["focus"].(map[string]any); fc["g"] != 1.0 || fc["x"] != 1.0 || fc["yaw"] != 0.0 || fc["L"] != 0.2 {
		t.Fatal(r)
	}
	r = ctl(t, f, "PUT", "/api/v2/session/focus", `{"stone_id":8}`, 200, "")
	if fc, _ := stateOf(r)["focus"].(map[string]any); fc["g"] != 4.0 || fc["x"] != 0.0 {
		t.Fatal(r)
	}
	r = ctl(t, f, "PUT", "/api/v2/session/focus", `{"stone_id":7}`, 200, "")
	if fc, _ := stateOf(r)["focus"].(map[string]any); fc["g"] != 5.0 || fc["yaw"] != 1.5708 {
		t.Fatal(r)
	}
	got := decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session/focus", "", 200, ""))
	if res, _ := got["resolved"].(map[string]any); res["id"] != 7.0 {
		t.Fatal(got)
	}
	ctl(t, f, "DELETE", "/api/v2/session/focus", "", 200, "")
}

func TestControls(t *testing.T) {
	f := mustNew(t, WithRecording(recJoined), WithLiveCamera())
	expect(t, f, "POST", "/api/v2/session/pause", "{}", 403, "cross_origin", "Origin", "http://evil.example")
	expect(t, f, "POST", "/api/v2/session/pause", "{}", 403, "cross_origin", "Origin", "::bad")
	expect(t, f, "DELETE", "/api/v2/session/focus", "", 403, "cross_origin", "Origin", "http://evil.example")
	expect(t, f, "POST", "/api/v2/session/pause?dry_run=1", "{}", 200, "", "Origin", "http://factory.test",
		"Content-Type", "application/json")
	expect(t, f, "POST", "/api/v2/session/select", `{"video":"`+strings.Repeat("x", 600)+`"}`, 413, "body_too_large",
		"Content-Type", "application/json")
	expect(t, f, "POST", "/api/v2/session/play", "", 200, "")
	expect(t, f, "POST", "/api/v2/session/select", "", 400, "invalid_json")
	expect(t, f, "POST", "/api/v2/session/select", `{"video":"dry1"}`, 415, "json_only", "Content-Type", "text/plain")
	ctl(t, f, "POST", "/api/v2/session/select", `{"video":`, 400, "invalid_json")
	ctl(t, f, "POST", "/api/v2/session/select", `[1]`, 400, "not_an_object")
	ctl(t, f, "POST", "/api/v2/session/pause?dry_run=2", `{}`, 400, "invalid_param")
	for _, c := range []struct{ body, code string }{
		{`{}`, "missing_field"}, {`{"video":null}`, "invalid_type"}, {`{"video":5}`, "invalid_type"},
		{`{"video":"nope"}`, "unknown_video"}, {`{"video":"cam-live","time":3}`, "not_seekable"},
		{`{"video":"cam-live","time":null}`, "not_seekable"}, {`{"video":"dry1","time":"x"}`, "invalid_type"},
		{`{"video":"dry1","time":null}`, "invalid_type"}, {`{"video":"dry1","time":99999}`, "time_out_of_range"},
	} {
		ctl(t, f, "POST", "/api/v2/session/select", c.body, 400, c.code)
	}
	for _, c := range []struct{ body, code, msg string }{
		{`{"video":null}`, "invalid_type", "Unknown video None"}, {`{"video":5}`, "invalid_type", "Unknown video 5"},
		{`{"video":true}`, "invalid_type", "Unknown video True"}, {`{"video":false}`, "invalid_type", "Unknown video False"},
		{`{"video":"nope"}`, "unknown_video", "Unknown video 'nope'"},
	} {
		if r := ctl(t, f, "POST", "/api/v2/session/select", c.body, 400, c.code); r["error"] != c.msg || r["field"] != "video" {
			t.Fatal(c.body, r)
		}
	}
	if r := ctl(t, f, "POST", "/api/v2/session/select", `{"video":"dry1","time":-1}`, 400, "time_out_of_range"); r["error"] != "time must be within 0..1807.158 s" {
		t.Fatal(r)
	}
	// the seek's checks in the unit's order: the video, a camera, the time; the session shows cam-live here
	for _, c := range []struct {
		body, code string
		status     int
	}{
		{`{"video":"nope"}`, "unknown_video", 400}, {`{"video":"nope","time":1}`, "unknown_video", 400},
		{`{"video":null,"time":1}`, "invalid_type", 400}, {`{}`, "not_seekable", 409},
		{`{"time":true}`, "not_seekable", 409}, {`{"video":"cam-live","time":1}`, "not_seekable", 409},
		{`{"video":"dry1"}`, "missing_field", 400}, {`{"video":"dry1","time":true}`, "invalid_type", 400},
		{`{"video":"dry1","time":null}`, "invalid_type", 400}, {`{"video":"dry1","time":-1}`, "time_out_of_range", 400},
	} {
		if r := ctl(t, f, "POST", "/api/v2/session/seek?dry_run=1", c.body, c.status, c.code); c.status == 409 &&
			(r["field"] != "video" || r["error"] != "cam-live is a live camera and cannot seek") {
			t.Fatal(c.body, r)
		}
	}
	// a dry run answers the new state and moves nothing
	before, _, _ := f.SessionState()
	r := ctl(t, f, "POST", "/api/v2/session/select?dry_run=1", `{"video":"dry1"}`, 200, "")
	if s := stateOf(r); s["anchor_media"] != 139.0 || r["dry_run"] != true ||
		!approx(s["hold_until"].(float64)-s["anchor_wall"].(float64), 2.2) || s["session"] != float64(before+1) {
		t.Fatal(r)
	}
	if after, _, _ := f.SessionState(); after != before {
		t.Fatal("a dry run moved the session")
	}
	// a camera: rate 1, no duration, no seek; connecting, warming and live on the fake's clock
	r = ctl(t, f, "POST", "/api/v2/session/select", `{"video":"cam-live"}`, 200, "")
	if s := stateOf(r); s["video"] != LiveCamera || s["rate"] != 1.0 || s["duration"] != nil || s["media_time"] != 0.0 ||
		s["live"].(map[string]any)["phase"] != "connecting" {
		t.Fatal(s)
	}
	if r := ctl(t, f, "POST", "/api/v2/session/seek", `{"time":3}`, 409, "not_seekable"); r["field"] != "video" ||
		r["error"] != "cam-live is a live camera and cannot seek" {
		t.Fatal(r)
	}
	f.Advance(ConnectSeconds)
	if p := f.LivePhase(); p != "warming" {
		t.Fatal(p)
	}
	// a select's session has no lag before it is live
	if l := stateOf(decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session", "", 200, "")))["live"]; l.(map[string]any)["lag"] != 0.0 {
		t.Fatal(l)
	}
	f.Advance(4)
	if p := f.LivePhase(); p != "live" {
		t.Fatal(p)
	}
	// pause and play continue at the live edge: now - since - lag
	if s := stateOf(ctl(t, f, "POST", "/api/v2/session/pause", `{}`, 200, "")); !approx(s["anchor_media"].(float64), 4.25) {
		t.Fatal(s)
	}
	f.Advance(10)
	if s := stateOf(ctl(t, f, "POST", "/api/v2/session/play", `{"video":"dry1","time":5}`, 200, "")); s["video"] != LiveCamera ||
		!approx(s["media_time"].(float64), 14.25) || s["live"].(map[string]any)["phase"] != "connecting" {
		t.Fatal(s)
	}
	// a seek without a video is refused while the session shows a camera; one naming a recording leaves the camera,
	// as a select does
	ctl(t, f, "POST", "/api/v2/session/seek", `{"time":82}`, 409, "not_seekable")
	r = ctl(t, f, "POST", "/api/v2/session/seek", `{"video":"dry1","time":82}`, 200, "")
	if s := stateOf(r); s["video"] != "dry1" || s["anchor_media"] != 82.0 || s["rate"] != 0.8 || s["duration"] != 1807.157567 {
		t.Fatal(s)
	}
	if _, v, playing := f.SessionState(); v != "dry1" || !playing {
		t.Fatal(v)
	}
	// repeats change nothing
	r1 := ctl(t, f, "POST", "/api/v2/session/play", "{}", 200, "")
	r2 := ctl(t, f, "DELETE", "/api/v2/session/focus", "", 200, "")
	if stateOf(r1)["revision"] != stateOf(r2)["revision"] {
		t.Fatal("a repeat bumped the revision")
	}
	ctl(t, f, "POST", "/api/v2/session/pause", "{}", 200, "")
	p1 := ctl(t, f, "POST", "/api/v2/session/pause", "{}", 200, "")
	p2 := ctl(t, f, "POST", "/api/v2/session/play", "{}", 200, "")
	if stateOf(p2)["revision"].(float64) != stateOf(p1)["revision"].(float64)+1 {
		t.Fatal(p1, p2)
	}
	// the playhead stops at the end of the video
	f.Advance(1e6)
	if s := decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session", "", 200, "")); stateOf(s)["media_time"] != 1807.157567 {
		t.Fatal(s)
	}
}

// TestLiveCamera: the fake's camera behaves as the unit's plant cameras do: shown live at the start, a select
// connects and warms up on the fake's clock (a stream or long poll that passes the phases waits for them), and the
// frames released after the warm-up have pictures.
func TestLiveCamera(t *testing.T) {
	f := mustNew(t, WithRecording(recJoined), WithLiveCamera())
	// at the start the session shows the camera, already live: g from 0, t the arrival since the session started
	sess := decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session", "", 200, ""))
	st := stateOf(sess)
	l := st["live"].(map[string]any)
	if st["video"] != LiveCamera || st["revision"] != 3.0 || st["changed_by"] != "start" || l["phase"] != "live" ||
		l["lag"] != LiveLagSeconds || l["warmup"] != 3.0 || st["duration"] != nil ||
		!approx(st["media_time"].(float64), sess["server_now"].(float64)-l["since"].(float64)-LiveLagSeconds) {
		t.Fatal(sess)
	}
	session := int64(st["session"].(float64))
	latest := decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session/frames/latest?layers=world", "", 200, ""))
	world := latest["world"].(map[string]any)
	if latest["g"] != 21.0 || latest["video"] != LiveCamera || latest["session"] != float64(session) ||
		world["g"] != 21.0 || world["t"] != latest["t"] || world["video"] != LiveCamera || !approx(latest["t"].(float64), 4.688311) {
		t.Fatal(latest["g"], latest["t"], world["g"], world["t"])
	}
	// the warm-up's frames (the camera's first 3 s) are never released
	if first := decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session/frames/16", "", 200, "")); !approx(first["t"].(float64), 3.6911) {
		t.Fatal(first)
	}
	expect(t, f, "GET", "/api/v2/session/frames/15", "", 404, "not_retained")
	// pictures, checked in the unit's order: g, a session that has ended, a recording, the frame
	code, h, b := do(t, f, "GET", "/api/v2/session/frames/latest/image.jpg", "")
	if code != 200 || h.Get("Content-Type") != "image/jpeg" || h.Get("X-Interp-G") != "21" || h.Get("Cache-Control") != "no-store" ||
		!bytes.HasPrefix(b, []byte{0xff, 0xd8, 0xff, 0xfe}) || !bytes.Contains(b, []byte("FakeFactory cam-live g 21")) {
		t.Fatal(code, h, b)
	}
	if _, h, _ := do(t, f, "GET", "/api/v2/session/frames/16/image.jpg?session="+fmt.Sprint(session), ""); h.Get("X-Interp-G") != "16" {
		t.Fatal(h)
	}
	expect(t, f, "GET", "/api/v2/session/frames/x/image.jpg?session=1", "", 400, "invalid_param")
	expect(t, f, "GET", "/api/v2/session/frames/15/image.jpg?session=1", "", 410, "session_gone")
	if b := expect(t, f, "GET", "/api/v2/session/frames/15/image.jpg", "", 404, "not_retained"); !strings.Contains(string(b), `"retained"`) {
		t.Fatal(string(b))
	}
	expect(t, f, "POST", "/api/v2/session/frames/latest/image.jpg", "", 405, "method_not_allowed")

	// a dry run plans nothing
	if r := ctl(t, f, "POST", "/api/v2/session/select?dry_run=1", `{"video":"cam-live"}`, 200, ""); stateOf(r)["live"].(map[string]any)["phase"] != "connecting" ||
		f.LivePhase() != "live" {
		t.Fatal(r)
	}
	// a select connects (nothing released), then a stream waits for the phases: the fake's clock moves on to each
	r := ctl(t, f, "POST", "/api/v2/session/select", `{"video":"cam-live"}`, 200, "")
	since := stateOf(r)["live"].(map[string]any)["since"].(float64)
	expect(t, f, "GET", "/api/v2/session/frames/latest/image.jpg", "", 404, "not_retained")
	sse := readStream(t, f, "/api/v2/session/frames/stream?layers=world", 9, "Accept", "text/event-stream")
	var phases []string
	for i := 1; i < 6; i += 2 {
		var c struct {
			Revision int64 `json:"revision"`
			Live     live  `json:"live"`
		}
		_ = json.Unmarshal([]byte(strings.TrimPrefix(sse[i], "data: ")), &c)
		phases = append(phases, c.Live.Phase)
	}
	frame := decode[map[string]any](t, []byte(strings.TrimPrefix(sse[8], "data: ")))
	if strings.Join(phases, ",") != "connecting,warming,live" || frame["g"] != 16.0 || frame["session"] != float64(session+1) ||
		!approx(f.Now(), since+3.6911+LiveLagSeconds) {
		t.Fatal(phases, frame["g"], f.Now()-since)
	}
	// a long poll takes the phases due within its wait (the still clock moves on to them): none with wait=0, the
	// warm-up within the default second, the frames once the clock has passed the warm-up; a refused poll none
	ctl(t, f, "POST", "/api/v2/session/select", `{"video":"cam-live"}`, 200, "")
	expect(t, f, "GET", "/api/v2/session/frames?wait=5000", "", 400, "invalid_param")
	phaseOf := func(m map[string]any) any { return stateOf(m)["live"].(map[string]any)["phase"] }
	poll := decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session/frames?cursor=0&layers=world&wait=0", "", 200, ""))
	if phaseOf(poll) != "connecting" {
		t.Fatal(poll)
	}
	poll = decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session/frames?cursor=0&layers=world", "", 200, ""))
	if phaseOf(poll) != "warming" || len(poll["frames"].([]any)) != 0 {
		t.Fatal(poll)
	}
	f.Advance(3)
	poll = decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session/frames?cursor=0&layers=world", "", 200, ""))
	if frames := poll["frames"].([]any); phaseOf(poll) != "live" || len(frames) != 6 || frames[0].(map[string]any)["g"] != 16.0 {
		t.Fatal(poll)
	}
	// a camera frame is numbered as the unit numbers it: g = i, its raw packet's g, i, t and cursor the frame's, the
	// overlay's prev_g the frame before (a recording with every layer)
	full := mustNew(t, WithRecording(fixtures+"/recordings/dry1-0296-0306-full.ndjson.gz"), WithLiveCamera())
	cf := decode[map[string]any](t, expect(t, full, "GET", "/api/v2/session/frames/latest?layers=raw,overlay", "", 200, ""))
	raw, ov := cf["raw"].(map[string]any), cf["overlay"].(map[string]any)
	pk := decode[map[string]any](t, expect(t, full, "GET", "/api/v2/raw/packets/latest", "", 200, ""))
	if cf["i"] != cf["g"] || raw["g"] != cf["g"] || raw["i"] != cf["g"] || raw["t"] != cf["t"] || raw["cursor"] != cf["cursor"] ||
		raw["video"] != LiveCamera || ov["prev_g"] != cf["g"].(float64)-1 || pk["g"] != cf["g"] || pk["i"] != cf["g"] ||
		pk["t"] != cf["t"] || pk["cursor"] != cf["cursor"] || pk["video"] != LiveCamera {
		t.Fatal(cf["g"], cf["i"], cf["t"], raw["g"], raw["i"], raw["t"], raw["cursor"], ov["prev_g"], pk)
	}
	fr := decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session/frames/latest", "", 200, ""))
	// pause and play of a live camera: the play's reply is connecting, then a new warm-up on the camera's next
	// frames, numbered after a break (G jumps)
	lastG := fr["g"].(float64)
	ctl(t, f, "POST", "/api/v2/session/pause", `{}`, 200, "")
	f.Advance(2)
	if p := f.LivePhase(); p != "live" {
		t.Fatal("a pause keeps the phase:", p)
	}
	if s := stateOf(ctl(t, f, "POST", "/api/v2/session/play", `{}`, 200, "")); s["live"].(map[string]any)["phase"] != "connecting" ||
		s["live"].(map[string]any)["lag"] != LiveLagSeconds {
		t.Fatal(s)
	}
	f.Advance(ConnectSeconds)
	if p := f.LivePhase(); p != "warming" {
		t.Fatal(p)
	}
	// the unit's warming changes only the phase: after a play it keeps the live lag
	if l := stateOf(decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session", "", 200, "")))["live"]; l.(map[string]any)["lag"] != LiveLagSeconds {
		t.Fatal(l)
	}
	f.Advance(30)
	again := decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session/frames/latest", "", 200, ""))
	// the run after the break starts at lastG + 1 + liveGap; its 16 warm-up frames are never released
	g0 := int(lastG) + 1 + liveGap
	first := decode[map[string]any](t, expect(t, f, "GET", fmt.Sprintf("/api/v2/session/frames/%d", g0+16), "", 200, ""))
	if f.LivePhase() != "live" || again["g"].(float64) <= float64(g0+16) || first["t"].(float64) <= fr["t"].(float64) {
		t.Fatal(lastG, again["g"], first["t"], fr["t"])
	}
	for _, g := range []int{int(lastG) + 1, g0, g0 + 15} {
		expect(t, f, "GET", fmt.Sprintf("/api/v2/session/frames/%d", g), "", 404, "not_retained")
	}
	// while paused nothing runs; the play connects again and the phases come after it, with later times
	ctl(t, f, "POST", "/api/v2/session/select", `{"video":"cam-live"}`, 200, "")
	ctl(t, f, "POST", "/api/v2/session/pause", `{}`, 200, "")
	f.Advance(10)
	readStream(t, f, "/api/v2/session/frames/stream?limit=1", 0)
	if p := f.LivePhase(); p != "connecting" {
		t.Fatal(p)
	}
	played := stateOf(ctl(t, f, "POST", "/api/v2/session/play", `{}`, 200, ""))
	if phaseOf(map[string]any{"state": played}) != "connecting" || played["media_time"] != 0.0 || played["anchor_media"] != 0.0 {
		t.Fatal(played)
	}
	f.Advance(ConnectSeconds)
	if p := f.LivePhase(); p != "warming" {
		t.Fatal(p)
	}
	f.Advance(3.5)
	after := stateOf(decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session", "", 200, "")))
	if after["live"].(map[string]any)["phase"] != "live" || after["playing"] != true ||
		after["revision"].(float64) != played["revision"].(float64)+2 ||
		after["anchor_wall"].(float64) <= played["anchor_wall"].(float64)+3 {
		t.Fatal(played, after)
	}
	// a camera selected while paused, then played: the phases keep the playing state
	ctl(t, f, "POST", "/api/v2/session/pause", `{}`, 200, "")
	ctl(t, f, "POST", "/api/v2/session/select", `{"video":"cam-live"}`, 200, "")
	ctl(t, f, "POST", "/api/v2/session/play", `{}`, 200, "")
	f.Advance(5)
	if _, _, playing := f.SessionState(); f.LivePhase() != "live" || !playing {
		t.Fatal(f.LivePhase(), playing)
	}
	// a recording after a camera: no live state, its rate 0.8, no pictures
	r = ctl(t, f, "POST", "/api/v2/session/select", `{"video":"dry1"}`, 200, "")
	if s := stateOf(r); s["live"] != nil || s["rate"] != 0.8 || f.LivePhase() != "" {
		t.Fatal(s)
	}
	if b := expect(t, f, "GET", "/api/v2/session/frames/latest/image.jpg", "", 404, "not_found"); !strings.Contains(string(b),
		"dry1 is a recording: its picture is the proxy video") {
		t.Fatal(string(b))
	}

	// WithClock: the fake's wall clock follows a clock (here a manual one), so the phases follow it too
	clk := NewClock(time.Time{})
	k := mustNew(t, WithRecording(recJoined), WithLiveCamera(), WithClock(clk))
	start := k.Now()
	ctl(t, k, "POST", "/api/v2/session/select", `{"video":"cam-live"}`, 200, "")
	// a linked clock is never moved: a long poll takes only what is due
	if poll := decode[map[string]any](t, expect(t, k, "GET", "/api/v2/session/frames", "", 200, "")); phaseOf(poll) != "connecting" ||
		k.Now() != start {
		t.Fatal(poll, k.Now()-start)
	}
	clk.Advance(time.Second)
	if !approx(k.Now(), start+1) || k.LivePhase() != "warming" {
		t.Fatal(k.Now()-start, k.LivePhase())
	}
	clk.Advance(3 * time.Second)
	if p := k.LivePhase(); p != "live" {
		t.Fatal(p)
	}

	// a camera sized like a recording the frames name (the fake's own entry), and a recording made on a camera
	// whose entry has no warm-up (3 s then): its sessions end to end, g and t go on
	v := mustNew(t, WithLiveCamera(), WithFrames(map[string]any{"cursor": 1, "session": 2, "video": "v1", "g": 0, "t": 0.0}))
	cat := decode[struct {
		Videos []map[string]any `json:"videos"`
	}](t, expect(t, v, "GET", "/api/v2/videos", "", 200, ""))
	if len(cat.Videos) != 2 || cat.Videos[1]["fps"] != 5.0 || cat.Videos[1]["proxy_wh"].([]any)[0] != 960.0 {
		t.Fatal(cat)
	}
	c := mustNew(t, WithLines([]byte(`{"recording":"pfr1","source":{"id":"cam9","kind":"camera","live":true,"seekable":false,"duration":null}}`),
		[]byte(`{"cursor":1,"session":5,"video":"cam9","g":7,"t":0.0}`), []byte(`{"cursor":2,"session":5,"video":"cam9","g":8,"t":3.2}`),
		[]byte(`{"cursor":3,"session":6,"video":"cam9","g":0,"t":0.4}`)))
	ctl(t, c, "POST", "/api/v2/session/select", `{"video":"cam9"}`, 200, "")
	got := readStream(t, c, "/api/v2/session/frames/stream?limit=2", 2, "Accept", "application/x-ndjson")
	one, two := decode[map[string]any](t, []byte(got[0])), decode[map[string]any](t, []byte(got[1]))
	if one["g"] != 1.0 || one["t"] != 3.7 || two["g"] != 2.0 || two["t"] != 3.9 || one["video"] != "cam9" {
		t.Fatal(got)
	}
}

// TestLiveCameraPicturesAndStreams: only the newest KeepPictures camera frames have a picture, as on the unit; a
// stream opened before a select follows the camera's phases, on a still clock (moved on to each) and on a linked one
// (waiting for it); the catalog's default video is never empty.
func TestLiveCameraPicturesAndStreams(t *testing.T) {
	f := mustNew(t, WithSampleRecording(), WithLiveCamera())
	f.mu.Lock()
	_, gs := f.current()
	f.mu.Unlock()
	if len(gs) <= KeepPictures {
		t.Fatal(len(gs))
	}
	kept, gone := gs[len(gs)-KeepPictures], gs[len(gs)-KeepPictures-1]
	if _, h, _ := do(t, f, "GET", fmt.Sprintf("/api/v2/session/frames/%d/image.jpg", kept), ""); h.Get("X-Interp-G") != fmt.Sprint(kept) {
		t.Fatal(h)
	}
	r := decode[map[string]any](t, expect(t, f, "GET", fmt.Sprintf("/api/v2/session/frames/%d/image.jpg", gone), "", 404, "not_retained"))
	if r["error"] != fmt.Sprintf("the picture of frame %d is no longer kept: only the newest 64 pictures are", gone) ||
		r["field"] != "g" || r["retained"] != nil {
		t.Fatal(r)
	}
	expect(t, f, "GET", fmt.Sprintf("/api/v2/session/frames/%d", gone), "", 200, "") // the frame itself is still retained
	expect(t, f, "GET", "/api/v2/session/frames/12345678901/image.jpg", "", 400, "invalid_param")
	// a stale session comes first, whatever the session shows (a recording here)
	ctl(t, f, "POST", "/api/v2/session/select", `{"video":"aug28"}`, 200, "")
	expect(t, f, "GET", "/api/v2/session/frames/latest/image.jpg?session=1", "", 410, "session_gone")

	// a stream open before the select follows the camera: the still clock moves on to each phase
	o := mustNew(t, WithRecording(recJoined), WithLiveCamera())
	lines, stop := openStream(t, o, "/api/v2/session/frames/stream?layers=world")
	defer stop()
	for i := 0; i < 6; i++ { // the camera shown at the start: g 16..21
		<-lines
	}
	since := stateOf(ctl(t, o, "POST", "/api/v2/session/select", `{"video":"cam-live"}`, 200, ""))["live"].(map[string]any)["since"].(float64)
	next := decode[map[string]any](t, []byte(<-lines))
	if next["g"] != 16.0 || next["t"].(float64) < 3.6 || !approx(o.Now(), since+3.6911+LiveLagSeconds) {
		t.Fatal(next["g"], next["t"], o.Now()-since)
	}

	// on a linked clock the stream waits until the clock has passed the phases, and never moves it
	clk := NewClock(time.Time{})
	k := mustNew(t, WithRecording(recJoined), WithLiveCamera(), WithClock(clk))
	ctl(t, k, "POST", "/api/v2/session/select", `{"video":"cam-live"}`, 200, "")
	start := k.Now()
	kl, kstop := openStream(t, k, "/api/v2/session/frames/stream?layers=world")
	defer kstop()
	select {
	case l := <-kl:
		t.Fatal("a frame before the clock moved:", l)
	case <-time.After(50 * time.Millisecond):
	}
	clk.Advance(5 * time.Second)
	if first := decode[map[string]any](t, []byte(<-kl)); first["g"] != 16.0 || !approx(k.Now(), start+5) {
		t.Fatal(first["g"], k.Now()-start)
	}

	// the catalog's default video: the recording, else the first source (the camera of a fake without one)
	cat := decode[map[string]any](t, expect(t, mustNew(t, WithLiveCamera()), "GET", "/api/v2/videos", "", 200, ""))
	if cat["default_video"] != LiveCamera {
		t.Fatal(cat)
	}
}

// openStream opens an NDJSON frame stream and returns its frame lines as they come, and a stop function.
func openStream(t *testing.T, f *Fake, path string) (<-chan string, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	req, _ := http.NewRequestWithContext(ctx, "GET", f.URL()+path, nil)
	req.Header.Set("Accept", "application/x-ndjson")
	resp, err := f.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	lines := make(chan string, 1024)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 1<<24)
		for sc.Scan() {
			if l := sc.Text(); l != "" {
				lines <- l
			}
		}
	}()
	return lines, func() {
		cancel()
		_ = resp.Body.Close()
	}
}

func TestSourcesWithoutDurationOrHold(t *testing.T) {
	f := mustNew(t, WithLines([]byte(`{"recording":"pfr1","source":{"id":"v"}}`), []byte(`{"cursor":1,"g":1,"t":0.2}`)))
	if r := ctl(t, f, "POST", "/api/v2/session/seek", `{"time":-1}`, 400, "time_out_of_range"); r["error"] != "time must be at least 0 s" {
		t.Fatal(r)
	}
	r := ctl(t, f, "POST", "/api/v2/session/select", `{"video":"v"}`, 200, "")
	if s := stateOf(r); !approx(s["hold_until"].(float64)-s["anchor_wall"].(float64), 1.4) || s["session"] != 2.0 {
		t.Fatal(s)
	}
	if s, v, _ := f.SessionState(); s != 2 || v != "v" {
		t.Fatal(s, v)
	}
	// frames without a session or video: session 1 of the header's source
	g := mustNew(t, WithLines([]byte(`{"recording":"pfr1","source":{"id":"v","duration":5}}`), []byte(`{"cursor":1,"g":1}`)))
	if s, v, _ := g.SessionState(); s != 1 || v != "v" {
		t.Fatal(s, v)
	}
	// a fake with only the live camera starts on it, live without frames; recordings it selects later play at 0.8
	h := mustNew(t, WithLiveCamera(), WithLines([]byte(`{"recording":"pfr1"}`)))
	if _, v, _ := h.SessionState(); v != LiveCamera || h.rate != 0.8 || h.LivePhase() != "live" {
		t.Fatal(v, h.rate)
	}
	expect(t, h, "GET", "/api/v2/session/frames/latest/image.jpg", "", 404, "not_retained")
	// no catalog at all
	e := mustNew(t)
	if r := ctl(t, e, "POST", "/api/v2/session/seek", `{"time":1}`, 409, "not_seekable"); r["error"] != "the session shows no video" {
		t.Fatal(r)
	}
	ctl(t, e, "POST", "/api/v2/session/select", `{"video":"v"}`, 400, "unknown_video")
}

// readStream reads a stream's body until n non-empty lines were read, then closes it.
func readStream(t *testing.T, f *Fake, path string, n int, header ...string) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", f.URL()+path, nil)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	resp, err := f.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(nil, 1<<24)
	var out []string
	for len(out) < n && sc.Scan() {
		if l := sc.Text(); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestStreams(t *testing.T) {
	clock := `{"sse":"clock","data":{"session":3,"revision":1,"video":"v","playing":true,"rate":0.8,"anchor_media":0,"anchor_wall":10,"hold_until":10,"server_now":10}}`
	lines := append([][]byte{[]byte(clock)}, frameLines()[2:]...)
	lines = append(lines, []byte(`{"cursor":6,"session":3,"video":"v","g":6,"t":1.2,"raw":{"schema":"sip1","g":6}}`))
	f := mustNew(t, WithLines(lines...))
	for _, c := range []struct{ q, code string }{
		{"format=xml", "invalid_param"}, {"limit=0", "invalid_param"}, {"max_s=0", "invalid_param"},
		{"after=x", "invalid_param"}, {"layers=depth", "unknown_layer"}, {"precision=x", "invalid_param"},
	} {
		expect(t, f, "GET", "/api/v2/session/frames/stream?"+c.q, "", 400, c.code)
	}
	// the Accept header picks the format; NDJSON leaves the side events out and writes a gap as a line
	sse := readStream(t, f, "/api/v2/session/frames/stream?limit=1", 3, "Accept", "text/event-stream")
	if sse[0] != "event: clock" || sse[2] != "id: fakeboot01-1" {
		t.Fatal(sse)
	}
	f.InjectGap(4)
	nd := readStream(t, f, "/api/v2/session/frames/stream?max_s=1", 2, "Accept", "application/x-ndjson")
	if nd[0] != `{"gap":4}` || !strings.HasPrefix(nd[1], `{"cursor":1,"session":3,"video":"v","g":1,`) {
		t.Fatal(nd)
	}
	if def := readStream(t, f, "/api/v2/session/frames/stream?limit=1", 1); def[0] != "event: clock" {
		t.Fatal(def)
	}
	// raw packets: frames without one are left out
	raw := readStream(t, f, "/api/v2/raw/stream?limit=2", 2)
	if len(raw) != 2 || !strings.Contains(raw[0], `"schema":"sip1"`) || !strings.Contains(raw[1], `"g":6`) {
		t.Fatal(raw)
	}
	// Last-Event-ID: another boot or a broken id resets; this boot resumes after the cursor
	for _, id := range []string{"nodash", "fakeboot01-x", "otherboot-3"} {
		if got := readStream(t, f, "/api/v2/session/frames/stream?limit=1", 2, "Last-Event-ID", id); got[0] != "event: reset" {
			t.Fatal(id, got)
		}
	}
	if got := readStream(t, f, "/api/v2/session/frames/stream", 1, "Last-Event-ID", "fakeboot01-5"); got[0] != "id: fakeboot01-6" {
		t.Fatal(got)
	}
	f.InjectReset()
	if got := readStream(t, f, "/api/v2/session/frames/stream?format=sse", 1); got[0] != "event: reset" {
		t.Fatal(got)
	}
	// a dropped connection
	f.DropAfter("streamFrames", 2)
	if got := readStream(t, f, "/api/v2/session/frames/stream?format=ndjson", 9); len(got) != 2 {
		t.Fatal(got)
	}
	// a select while a stream is behind: the stream moves on to the new session
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", f.URL()+"/api/v2/session/frames/stream?format=ndjson&observe=1", nil)
	resp, err := f.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(nil, 1<<24)
	if !sc.Scan() || !strings.Contains(sc.Text(), `"g":1,`) {
		t.Fatal(sc.Text())
	}
	ctl(t, f, "POST", "/api/v2/session/select", `{"video":"v","time":0.6}`, 200, "")
	next := ""
	for sc.Scan() {
		if next = sc.Text(); strings.Contains(next, `"session":4`) {
			break
		}
	}
	if !strings.HasPrefix(next, `{"cursor":7,"session":4,"video":"v","g":3,"t":0.6`) {
		t.Fatal(next)
	}
	cancel()
	resp.Body.Close()
	// a stream after a cursor of an earlier session starts at the current one
	if got := readStream(t, f, "/api/v2/session/frames/stream?after=1&format=ndjson", 1); !strings.Contains(got[0], `"session":4`) {
		t.Fatal(got)
	}
	// without a recording: the recorded stream, else 503
	g := mustNew(t, WithReplies(repliesDir))
	if got := readStream(t, g, "/api/v2/session/frames/stream?format=ndjson", 1); !strings.HasPrefix(got[0], `{"cursor":10769,`) {
		t.Fatal(got)
	}
	e := mustNew(t)
	expect(t, e, "GET", "/api/v2/session/frames/stream", "", 503, "not_ready")
	expect(t, e, "GET", "/api/v2/session/frames", "", 503, "not_ready")
}

func TestPoll(t *testing.T) {
	f := mustNew(t, WithLines(frameLines()...))
	for _, c := range []struct{ q, code string }{
		{"cursor=x", "invalid_param"}, {"rev=x", "invalid_param"}, {"limit=17", "invalid_param"},
		{"wait=1001", "invalid_param"}, {"precision=x", "invalid_param"}, {"cid=short", "invalid_param"},
		{"layers=x", "unknown_layer"},
	} {
		expect(t, f, "GET", "/api/v2/session/frames?"+c.q, "", 400, c.code)
	}
	type poll struct {
		Cursor int64             `json:"cursor"`
		Frames []json.RawMessage `json:"frames"`
	}
	r := decode[poll](t, expect(t, f, "GET", "/api/v2/session/frames?limit=2&cid=go-abcdefgh", "", 200, ""))
	if r.Cursor != 2 || len(r.Frames) != 2 {
		t.Fatalf("%+v", r)
	}
	r = decode[poll](t, expect(t, f, "GET", "/api/v2/session/frames?cursor=5", "", 200, ""))
	if r.Cursor != 5 || len(r.Frames) != 0 {
		t.Fatalf("%+v", r)
	}
	r = decode[poll](t, expect(t, f, "GET", "/api/v2/session/frames?cursor=99", "", 200, ""))
	if r.Cursor != 5 || len(r.Frames) != 0 {
		t.Fatalf("%+v", r)
	}
	g := mustNew(t, WithReplies(repliesDir))
	expect(t, g, "GET", "/api/v2/session/frames?layers=world", "", 200, "")
}

// TestClockLinesInForce: the session state is the recording's clock line in force where playback is: the first at the
// start, a later one once a long poll (whose reply ends before it) or a stream (an NDJSON one too, which leaves it out)
// has passed it, while the wall clock stands still; after a control the state is the fake's own.
func TestClockLinesInForce(t *testing.T) {
	frame := func(n int) []byte {
		return []byte(fmt.Sprintf(`{"cursor":%d,"session":3,"video":"v","g":%d,"t":%g}`, n, n, float64(n)/5))
	}
	lines := [][]byte{[]byte(`{"recording":"pfr1","source":{"id":"v"}}`),
		[]byte(`{"sse":"clock","data":{"session":3,"revision":1,"video":"v","playing":true,"rate":0.8,"anchor_media":0,"anchor_wall":10,"hold_until":10,"changed_by":"viewer","server_now":10}}`),
		frame(1), frame(2),
		[]byte(`{"sse":"clock","data":{"session":3,"revision":2,"video":"v","playing":true,"rate":0.75,"anchor_media":0.4,"anchor_wall":10.5,"hold_until":10,"changed_by":"auto-rate","server_now":10.5}}`),
		frame(3), frame(4)}
	session := func(f *Fake) map[string]any {
		return decode[map[string]any](t, expect(t, f, "GET", "/api/v2/session", "", 200, ""))
	}
	f := mustNew(t, WithLines(lines...))
	if s := session(f); stateOf(s)["revision"] != 1.0 || stateOf(s)["rate"] != 0.8 || s["server_now"] != 10.0 {
		t.Fatal(s)
	}
	type poll struct {
		Cursor int64             `json:"cursor"`
		Frames []json.RawMessage `json:"frames"`
		State  map[string]any    `json:"state"`
	}
	r := decode[poll](t, expect(t, f, "GET", "/api/v2/session/frames?wait=0", "", 200, ""))
	if r.Cursor != 2 || len(r.Frames) != 2 || r.State["revision"] != 1.0 {
		t.Fatalf("%+v", r)
	}
	r = decode[poll](t, expect(t, f, "GET", "/api/v2/session/frames?cursor=2&wait=0", "", 200, ""))
	if r.Cursor != 4 || len(r.Frames) != 2 || r.State["revision"] != 2.0 || r.State["rate"] != 0.75 || f.Now() != 10 {
		t.Fatalf("%+v %v", r, f.Now())
	}
	if s := session(f); stateOf(s)["revision"] != 2.0 || s["server_now"] != 10.0 {
		t.Fatal(s)
	}
	g := mustNew(t, WithLines(lines...))
	if got := readStream(t, g, "/api/v2/session/frames/stream?format=ndjson", 3); !strings.Contains(got[2], `"g":3,`) {
		t.Fatal(got)
	}
	if s := stateOf(session(g)); s["revision"] != 2.0 || s["changed_by"] != "auto-rate" {
		t.Fatal(s)
	}
	h := mustNew(t, WithLines(lines...))
	ctl(t, h, "POST", "/api/v2/session/pause", `{}`, 200, "")
	readStream(t, h, "/api/v2/session/frames/stream?format=ndjson", 3)
	if s := stateOf(session(h)); s["revision"] != 2.0 || s["playing"] != false || s["rate"] != 0.8 {
		t.Fatal(s)
	}
}

func TestOptionsAndLoading(t *testing.T) {
	mustPanic(t, "JSONReply", func() { JSONReply(200, make(chan int)) })
	mustPanic(t, "WithFrames", func() { _, _ = New(WithFrames(make(chan int))) })
	f := mustNew(t, WithFrames(map[string]any{"cursor": 1, "session": 2, "video": "v", "g": 1, "t": 0.2}),
		WithReplyMap(map[string]Reply{"getHealth": JSONReply(200, map[string]any{"ok": true})}))
	if keys := f.ReplyKeys(); len(keys) != 1 || keys[0] != "getHealth" {
		t.Fatal(keys)
	}
	if err := f.ReplyNext("getVideo", 1, "getHealth"); err != nil {
		t.Fatal(err)
	}
	if b := expect(t, f, "GET", "/api/v2/videos/v", "", 200, ""); string(b) != `{"ok":true}` {
		t.Fatal(string(b))
	}
	if f.Handler() == nil || f.Now() != fakeEpoch {
		t.Fatal(f.Now())
	}
	f.Advance(2.5)
	if f.Now() != fakeEpoch+2.5 {
		t.Fatal(f.Now())
	}
	// a recorded getSession reply without a state gives no clock
	if g := mustNew(t, WithReplyMap(map[string]Reply{"getSession": JSONReply(200, map[string]any{})})); g.Now() != fakeEpoch {
		t.Fatal(g.Now())
	}
	for _, opts := range [][]Option{
		{WithReplies(t.TempDir())},
		{WithRecording(filepath.Join(t.TempDir(), "none.ndjson"))},
	} {
		if _, err := New(opts...); err == nil {
			t.Fatal("no error")
		}
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "INDEX.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(WithReplies(dir)); err == nil {
		t.Fatal("a bad index")
	}
	if err := os.WriteFile(filepath.Join(dir, "INDEX.json"), []byte(`{"getHealth":{"file":"none.json","status":200}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(WithReplies(dir)); err == nil {
		t.Fatal("a missing reply file")
	}
	// a recording that ends in the middle of its compressed stream
	src, err := os.ReadFile(recJoined)
	if err != nil {
		t.Fatal(err)
	}
	cut := filepath.Join(dir, "cut.ndjson.gz")
	if err := os.WriteFile(cut, src[:len(src)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(WithRecording(cut)); err == nil {
		t.Fatal("a cut recording")
	}
	if _, ok := num(map[string]json.RawMessage{"a": json.RawMessage("null")}, "a"); ok {
		t.Fatal("null")
	}
	if _, ok := num(nil, "a"); ok {
		t.Fatal("absent")
	}
	if _, err := decodeObject([]byte("[1]")); err == nil {
		t.Fatal("an array")
	}
}

func TestCancelledRequestsAreNotServed(t *testing.T) {
	f := mustNew(t, WithReplies(repliesDir))
	f.FailNext("getHealth", 1, ErrorReply(503, "busy", "x"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	f.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/api/v2/health", nil).WithContext(ctx))
	if len(f.Requests()) != 0 || rec.Body.Len() != 0 {
		t.Fatal(f.Requests(), rec.Body.String())
	}
	// the fault is still there for the next request
	expect(t, f, "GET", "/api/v2/health", "", 503, "busy")
}

func TestHTTPServer(t *testing.T) {
	f := mustNew(t, WithReplies(repliesDir))
	u := f.Start()
	if f.Start() != u {
		t.Fatal("Start twice")
	}
	resp, err := http.Get(u + "/api/v2/health")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || len(f.Ops()) != 1 || f.Ops()[0] != "getHealth" || f.Requests()[0].Path != "/api/v2/health" {
		t.Fatal(resp.Status, f.Ops())
	}
	f.Close()
	f.Close()
}

type errBody struct{}

func (errBody) Read([]byte) (int, error) { return 0, errors.New("boom") }
func (errBody) Close() error             { return nil }

func TestTransport(t *testing.T) {
	ctx := context.Background()
	u, _ := http.NewRequestWithContext(ctx, "GET", "http://factory.test/x", nil)
	// a request body that cannot be read
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://factory.test/api/v2/session/play", errBody{})
	if _, err := (&transport{h: http.NotFoundHandler()}).RoundTrip(req); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatal(err)
	}
	// a request without Host, a handler that writes nothing
	var host string
	tr := &transport{h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { host = r.Host })}
	bare := (&http.Request{Method: "GET", URL: u.URL, Header: http.Header{}}).WithContext(ctx)
	resp, err := tr.RoundTrip(bare)
	if err != nil || resp.StatusCode != 200 || host != "factory.test" || resp.ContentLength != -1 {
		t.Fatal(resp, err)
	}
	resp.Body.Close()
	// a handler that panics before its status, and one that aborts after it
	tr = &transport{h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { panic("bug") })}
	if _, err := tr.RoundTrip(u); err == nil || !strings.Contains(err.Error(), "handler panic: bug") {
		t.Fatal(err)
	}
	tr = &transport{h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("abc"))
		panic(http.ErrAbortHandler)
	})}
	resp, err = tr.RoundTrip(u)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(b) != "abc" || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(string(b), err)
	}
	// a request cancelled before the status
	cctx, cancel := context.WithCancel(ctx)
	release := make(chan struct{})
	tr = &transport{h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cancel()
		<-release
	})}
	if _, err := tr.RoundTrip(u.WithContext(cctx)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	// a body closed while the handler streams: the handler's context ends and its writes fail
	done := make(chan error, 1)
	tr = &transport{h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		_, err := w.Write([]byte("late"))
		done <- err
	})}
	resp, err = tr.RoundTrip(u)
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(err)
	}
	resp.Body.Close()
	if err := <-done; err == nil {
		t.Fatal("a write after close")
	}
	// a read of a body whose request was cancelled reports the cancellation
	cctx, cancel = context.WithCancel(ctx)
	defer cancel()
	tr = &transport{h: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		<-r.Context().Done()
	})}
	resp, err = tr.RoundTrip(u.WithContext(cctx))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := io.ReadAll(resp.Body); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestClock(t *testing.T) {
	c := NewClock(time.Unix(100, 0))
	c.Advance(time.Second)
	if err := c.Sleep(context.Background(), -time.Second); err != nil || !c.Now().Equal(time.Unix(101, 0)) {
		t.Fatal(c.Now())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Sleep(ctx, time.Second); !errors.Is(err, context.Canceled) || len(c.Sleeps()) != 1 {
		t.Fatal(err)
	}
	if NewClock(time.Time{}).Now().Year() != 2026 {
		t.Fatal("default start")
	}
}

// TestEmbeddedCopies: the files this package embeds are the fixtures' (when this fails, copy the fixtures here again
// from fixtures/).
func TestEmbeddedCopies(t *testing.T) {
	for file, embedded := range map[string][]byte{
		fixtures + "/recordings/" + SampleRecording + ".ndjson.gz": sampleRecording,
		fixtures + "/api/getTopology.json":                         defaultTopology,
	} {
		b, err := os.ReadFile(file)
		if err != nil || !bytes.Equal(b, embedded) {
			t.Errorf("the embedded copy of %s differs from it (%v)", file, err)
		}
	}
	if b := SampleRecordingFile(); !bytes.Equal(b, sampleRecording) || &b[0] == &sampleRecording[0] {
		t.Fatal("SampleRecordingFile is a copy")
	}
}

func TestSampleRecording(t *testing.T) {
	f := mustNew(t, WithRecording(recJoined), WithSampleRecording())
	type state struct {
		State struct {
			Video string `json:"video"`
		} `json:"state"`
	}
	if s := decode[state](t, expect(t, f, "GET", "/api/v2/session", "", 200, "")); s.State.Video != "aug28" {
		t.Fatalf("%+v", s)
	}
	if got := readStream(t, f, "/api/v2/session/frames/stream?format=ndjson&after=0&limit=1&layers=world", 1); !strings.Contains(got[0], `"g":6350,`) {
		t.Fatal(got)
	}
	// the last option wins
	g := mustNew(t, WithSampleRecording(), WithRecording(recJoined))
	if got := readStream(t, g, "/api/v2/session/frames/stream?format=ndjson&after=0&limit=1", 1); !strings.Contains(got[0], `"g":399,`) {
		t.Fatal(got)
	}
	// a damaged embedded copy
	old := sampleRecording
	defer func() { sampleRecording = old }()
	sampleRecording = []byte{0x1f, 0x8b, 0, 0}
	if _, err := New(WithSampleRecording()); err == nil {
		t.Fatal("a bad gzip header")
	}
	sampleRecording = old[:len(old)/2]
	if _, err := New(WithSampleRecording()); err == nil {
		t.Fatal("a cut copy")
	}
}

// TestHeldStreams: with HoldFinishedStreams a stream that reached its limit sends nothing more and stays open until
// the client closes it.
func TestHeldStreams(t *testing.T) {
	f := mustNew(t, WithRecording(recJoined))
	f.HoldFinishedStreams(true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", f.URL()+"/api/v2/session/frames/stream?format=ndjson&after=0&limit=2", nil)
	resp, err := f.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	lines := make(chan string)
	go func() {
		defer close(lines)
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(nil, 1<<24)
		for sc.Scan() {
			lines <- sc.Text()
		}
	}()
	for i := 0; i < 2; i++ {
		if l := <-lines; !strings.HasPrefix(l, `{"cursor":`) {
			t.Fatal(l)
		}
	}
	select {
	case l, ok := <-lines:
		t.Fatalf("the finished stream went on (%q, %v)", l, ok)
	case <-time.After(20 * time.Millisecond):
	}
	cancel()
	if _, ok := <-lines; ok {
		t.Fatal("a line after the close")
	}
	f.HoldFinishedStreams(false)
	if got := readStream(t, f, "/api/v2/session/frames/stream?format=ndjson&limit=1", 9); len(got) != 1 {
		t.Fatal(got)
	}
}

func TestClockTimers(t *testing.T) {
	c := NewClock(time.Time{})
	fired := make(chan time.Duration, 4)
	c.AfterFunc(2*time.Second, func() { fired <- 2 * time.Second })
	stop := c.AfterFunc(3*time.Second, func() { fired <- 3 * time.Second })
	c.AfterFunc(-time.Second, func() { fired <- 0 })
	if d := <-fired; d != 0 || c.Timers() != 2 {
		t.Fatal(d, c.Timers())
	}
	c.Advance(time.Second)
	if c.Timers() != 2 {
		t.Fatal("fired early")
	}
	if err := c.Sleep(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	}
	if d := <-fired; d != 2*time.Second || !stop() || stop() || c.Timers() != 0 {
		t.Fatal(d, c.Timers())
	}
	c.Advance(time.Hour)
	select {
	case d := <-fired:
		t.Fatal("a stopped timer fired", d)
	default:
	}
}

// TestReplyPrecedence: a reply given with WithReplyMap answers its operation even where the recording would (the key
// every request, a 2xx variant the requests that name it, an error variant only through ReplyNext); the replies of
// a WithReplies directory answer only what the recording does not.
func TestReplyPrecedence(t *testing.T) {
	session := JSONReply(200, map[string]any{"state": map[string]any{"session": 7, "video": "given"}, "server_now": 1.0})
	latest := JSONReply(200, map[string]any{"cursor": 9, "session": 7, "video": "given", "g": 99, "t": 19.8})
	f := mustNew(t, WithRecording(recJoined), WithReplies(repliesDir), WithReplyMap(map[string]Reply{
		"getSession": session, "getFrame-latest": latest, "getFrame-404": ErrorReply(404, "not_retained", "gone")}))
	if b := expect(t, f, "GET", "/api/v2/session", "", 200, ""); !strings.Contains(string(b), `"video":"given"`) {
		t.Fatal(string(b))
	}
	if b := expect(t, f, "GET", "/api/v2/session/frames/latest", "", 200, ""); !strings.Contains(string(b), `"g":99`) {
		t.Fatal(string(b))
	}
	// no given variant stands in for another request: frame 420 is the recording's
	if b := expect(t, f, "GET", "/api/v2/session/frames/420", "", 200, ""); !strings.Contains(string(b), `"g":420`) {
		t.Fatal(string(b))
	}
	if err := f.ReplyNext("getFrame", 1, "getFrame-404"); err != nil {
		t.Fatal(err)
	}
	expect(t, f, "GET", "/api/v2/session/frames/420", "", 404, "not_retained")
	// the directory's catalog and world are another run's: the recording's answer
	if b := expect(t, f, "GET", "/api/v2/videos", "", 200, ""); !strings.Contains(string(b), `"dry1"`) ||
		strings.Contains(string(b), `"aug28"`) {
		t.Fatal(string(b))
	}
	if b := expect(t, f, "GET", "/api/v2/session/world", "", 200, ""); !strings.Contains(string(b), `"g":420`) {
		t.Fatalf("%.200s", b)
	}
	// what the recording cannot answer comes from the directory
	model, err := os.ReadFile(filepath.Join(repliesDir, "getModel.json"))
	if err != nil {
		t.Fatal(err)
	}
	if b := expect(t, f, "GET", "/api/v2/model", "", 200, ""); !bytes.Equal(b, model) {
		t.Fatalf("%.200s", b)
	}
	// a given key wins over the directory's reply of the same key
	g := mustNew(t, WithReplies(repliesDir), WithReplyMap(map[string]Reply{"getModel": JSONReply(200, map[string]any{"given": true})}))
	if b := expect(t, g, "GET", "/api/v2/model", "", 200, ""); string(b) != `{"given":true}` {
		t.Fatal(string(b))
	}
}
