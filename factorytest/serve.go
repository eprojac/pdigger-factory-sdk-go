package factorytest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eprojac/pdigger-factory-sdk-go/codec"
)

var layerNames = []string{"raw", "overlay", "scene", "cards", "world"}

const defaultLayers = "overlay,scene,cards,world"

var baseKeys = []string{"cursor", "session", "video", "g", "i", "t", "past_valid", "dt_past"}

var cidPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{8,40}$`)

// route resolves a request to an operationId and its path variables; known reports a path the API has.
func route(method, path string) (op string, vars []string, known bool) {
	p := strings.TrimSuffix(path, "/")
	if p == "/metrics" {
		return pick(method, map[string]string{"GET": "getMetrics"}), nil, true
	}
	rest, ok := strings.CutPrefix(p, "/api/v2")
	if !ok {
		return "", nil, false
	}
	seg := strings.Split(strings.TrimPrefix(rest, "/"), "/")
	if rest == "" {
		seg = nil
	}
	get := func(name string) map[string]string { return map[string]string{"GET": name} }
	switch {
	case len(seg) == 0:
		return pick(method, get("getApiRoot")), nil, true
	case len(seg) == 1:
		switch seg[0] {
		case "openapi.yaml":
			return pick(method, get("getOpenApi")), nil, true
		case "model":
			return pick(method, get("getModel")), nil, true
		case "topology":
			return pick(method, get("getTopology")), nil, true
		case "health":
			return pick(method, get("getHealth")), nil, true
		case "ready":
			return pick(method, get("getReady")), nil, true
		case "status":
			return pick(method, get("getStatus")), nil, true
		case "videos":
			return pick(method, get("listVideos")), nil, true
		case "session":
			return pick(method, get("getSession")), nil, true
		}
	case seg[0] == "videos" && len(seg) == 2:
		return pick(method, get("getVideo")), seg[1:], true
	case seg[0] == "videos" && len(seg) == 3 && seg[2] == "media":
		return pick(method, map[string]string{"GET": "getVideoMedia", "HEAD": "getVideoMedia"}), seg[1:2], true
	case seg[0] == "session" && len(seg) == 2:
		switch seg[1] {
		case "select", "seek", "play", "pause":
			name := "session" + strings.ToUpper(seg[1][:1]) + seg[1][1:]
			return pick(method, map[string]string{"POST": name}), nil, true
		case "focus":
			return pick(method, map[string]string{"GET": "getFocus", "PUT": "putFocus", "DELETE": "deleteFocus"}),
				nil, true
		case "world":
			return pick(method, get("getWorld")), nil, true
		case "frames":
			return pick(method, get("pollFrames")), nil, true
		}
	case seg[0] == "session" && len(seg) == 3 && seg[1] == "world" && seg[2] == "events":
		return pick(method, get("getWorldEvents")), nil, true
	case seg[0] == "session" && len(seg) == 4 && seg[1] == "world" && seg[2] == "stones":
		return pick(method, get("getStone")), seg[3:], true
	case seg[0] == "session" && len(seg) == 3 && seg[1] == "frames" && seg[2] == "stream":
		return pick(method, get("streamFrames")), nil, true
	case seg[0] == "session" && len(seg) == 3 && seg[1] == "frames":
		return pick(method, get("getFrame")), seg[2:], true
	case seg[0] == "session" && len(seg) == 4 && seg[1] == "frames" && seg[3] == "image.jpg":
		return pick(method, get("getFramePicture")), seg[2:3], true
	case seg[0] == "session" && len(seg) == 4 && seg[1] == "frames" && strings.HasSuffix(seg[3], ".png"):
		return pick(method, get("getFrameRaster")), []string{seg[2], strings.TrimSuffix(seg[3], ".png")}, true
	case seg[0] == "session" && len(seg) == 3 && seg[1] == "render" && seg[2] == "overlay2d":
		return pick(method, get("renderOverlay2d")), nil, true
	case seg[0] == "session" && len(seg) == 3 && seg[1] == "render" && seg[2] == "scene3d":
		return pick(method, get("renderScene3d")), nil, true
	case seg[0] == "raw" && len(seg) == 2 && seg[1] == "stream":
		return pick(method, get("rawStream")), nil, true
	case seg[0] == "raw" && len(seg) == 3 && seg[1] == "packets":
		return pick(method, get("rawPacket")), seg[2:], true
	case seg[0] == "raw" && len(seg) == 3 && seg[1] == "tensors":
		return pick(method, get("rawTensors")), seg[2:], true
	}
	return "", nil, false
}

func pick(method string, ops map[string]string) string { return ops[method] }

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, _ := json.Marshal(v)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Length", strconv.Itoa(len(b)))
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

// apiError is an error in the server's shape.
type apiError struct {
	status int
	body   map[string]any
}

func errorOf(status int, code, message, field string) *apiError {
	b := map[string]any{"error": message, "code": code}
	if field != "" {
		b["field"] = field
	}
	return &apiError{status: status, body: b}
}

func (e *apiError) write(w http.ResponseWriter) { writeJSON(w, e.status, e.body) }

func writeReply(w http.ResponseWriter, r Reply) {
	for k, v := range r.Headers {
		if !strings.EqualFold(k, "Content-Length") {
			w.Header().Set(k, v)
		}
	}
	if r.ContentType != "" {
		w.Header().Set("Content-Type", r.ContentType)
	}
	status := r.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(r.Body)))
	w.WriteHeader(status)
	_, _ = w.Write(r.Body)
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	op, vars, known := route(r.Method, r.URL.Path)
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	f.mu.Lock()
	if r.Context().Err() != nil {
		// the client gave up before the fake got to its request: nothing is recorded, consumed or answered, so a
		// test's later hooks and faults never see a request of a stream it already stopped
		f.mu.Unlock()
		return
	}
	f.reqs = append(f.reqs, Request{Op: op, Method: r.Method, Path: r.URL.Path, Query: r.URL.Query(),
		Header: r.Header.Clone(), Body: body})
	f.advance() // a camera session takes the phases whose time has come
	hook := f.delay
	var fault *Reply
	if q := f.faults[op]; op != "" && len(q) > 0 {
		fault = &q[0]
		f.faults[op] = q[1:]
	}
	f.mu.Unlock()
	w.Header().Set("X-Interp-Build", "factorytest")
	if op == "" {
		if known {
			errorOf(405, "method_not_allowed", "method not allowed", "").write(w)
			return
		}
		errorOf(404, "not_found", "not found", "").write(w)
		return
	}
	if hook != nil {
		hook(op, r)
	}
	if fault != nil {
		writeReply(w, *fault)
		return
	}
	if (r.Method == http.MethodGet || r.Method == http.MethodHead) && len(body) > 0 {
		errorOf(400, "body_not_allowed", "a GET request cannot carry a body", "").write(w)
		return
	}
	switch op {
	case "sessionSelect", "sessionSeek", "sessionPlay", "sessionPause", "putFocus", "deleteFocus":
		f.serveControl(w, r, op, body)
	case "streamFrames", "rawStream":
		f.serveStream(w, r, op)
	case "pollFrames":
		f.servePoll(w, r)
	default:
		f.serveGet(w, r, op, vars)
	}
}

// recorded answers a GET from the recorded replies; false when there is none.
func (f *Fake) recorded(w http.ResponseWriter, r *http.Request, op string) bool {
	f.mu.Lock()
	rep, ok := f.reply(op, r)
	f.mu.Unlock()
	if ok {
		writeReply(w, rep)
	}
	return ok
}

// answerGiven answers a GET from a reply given with WithReplyMap; false when there is none.
func (f *Fake) answerGiven(w http.ResponseWriter, r *http.Request, op string) bool {
	f.mu.Lock()
	rep, ok := f.givenReply(op, r)
	f.mu.Unlock()
	if ok {
		writeReply(w, rep)
	}
	return ok
}

func (f *Fake) serveGet(w http.ResponseWriter, r *http.Request, op string, vars []string) {
	q := r.URL.Query()
	f.mu.Lock()
	hasRec := f.hasRec
	f.mu.Unlock()
	fromRecording := map[string]bool{"listVideos": true, "getVideo": true, "getVideoMedia": true,
		"getSession": true, "getFrame": true, "getFramePicture": true, "getFrameRaster": true, "rawPacket": true,
		"getWorld": true, "getWorldEvents": true, "getStone": true, "getFocus": true}
	answer := f.recorded
	if hasRec && fromRecording[op] {
		answer = f.answerGiven // a reply given by key wins over the recording
	}
	if answer(w, r, op) {
		return
	}
	var v any
	var err *apiError
	f.mu.Lock()
	switch op {
	case "getApiRoot":
		v = f.apiRoot()
	case "getModel":
		v = f.model()
	case "getTopology":
		v = json.RawMessage(defaultTopology)
	case "getHealth":
		v = map[string]any{"ok": true, "worker": "running", "viewers": f.viewers}
	case "getReady":
		v = map[string]any{"ready": true, "waiting": []string{}}
	case "getStatus":
		v = f.status()
	case "getMetrics":
		f.mu.Unlock()
		f.serveMetrics(w)
		return
	case "listVideos":
		f.mu.Unlock()
		f.serveCatalog(w, r)
		return
	case "getVideo":
		if src := f.source(vars[0]); src != nil {
			v = src
		} else {
			err = errorOf(404, "unknown_video", fmt.Sprintf("Unknown video '%s'", vars[0]), "id")
		}
	case "getVideoMedia":
		f.mu.Unlock()
		f.serveMedia(w, r, vars[0])
		return
	case "getSession":
		v = f.sessionReply()
	case "getFrame":
		v, err = f.frame(q, vars[0])
	case "rawPacket":
		v, err = f.packet(q, vars[0])
	case "getWorld":
		v, err = f.world(q)
	case "getWorldEvents":
		v, err = f.events(q)
	case "getStone":
		v, err = f.stone(vars[0])
	case "getFocus":
		v = f.focusReply()
	case "getFrameRaster":
		f.mu.Unlock()
		f.serveRaster(w, r, vars[0], vars[1])
		return
	case "getFramePicture":
		f.mu.Unlock()
		f.servePicture(w, r, vars[0])
		return
	case "rawTensors":
		if _, perr := strconv.ParseUint(vars[0], 10, 63); perr != nil {
			err = errorOf(400, "invalid_param", "g must be an integer", "g")
		} else {
			err = errorOf(404, "debug_disabled", "tensor capture is not enabled", "")
		}
	case "renderOverlay2d", "renderScene3d":
		err = renderParams(q, op == "renderScene3d")
		if err == nil {
			err = errorOf(503, "not_ready", "the fake has no recorded render reply", "")
		}
	default: // getOpenApi
		err = errorOf(404, "not_found", "the fake has no recorded "+op+" reply", "")
	}
	f.mu.Unlock()
	if err != nil {
		err.write(w)
		return
	}
	writeJSON(w, 200, v)
}

func renderParams(q map[string][]string, scene bool) *apiError {
	get := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	if (get("g") == "") == (get("t") == "") {
		return errorOf(400, "invalid_param", "give exactly one of g and t", "t")
	}
	if m := get("mode"); m != "" && m != "stable" && m != "raw" {
		return errorOf(400, "invalid_param", "mode must be stable or raw", "mode")
	}
	if d := get("draw"); d != "" && d != "shapes" && d != "primitives" {
		return errorOf(400, "invalid_param", "draw must be shapes or primitives", "draw")
	}
	if s := get("surface"); scene && s != "" && s != "vertices" && s != "compact" && s != "none" {
		return errorOf(400, "invalid_param", "surface must be vertices, compact or none", "surface")
	}
	return nil
}

func (f *Fake) apiRoot() map[string]any {
	build, _ := f.header["build"].(string)
	backend, _ := f.header["backend"].(string)
	if backend == "" {
		backend = "replay"
	}
	return map[string]any{"api": "2.2.0", "build": build, "boot": f.boot, "backend": backend,
		"spec": "/api/v2/openapi.yaml", "docs": "/api/docs/",
		"layers": map[string]string{"raw": "sip1", "overlay": "ovl1", "scene": "scn1", "cards": "crd1", "world": "stw1"},
		"render": map[string]string{"overlay2d": "r2d1", "scene3d": "r3d1"},
		"limits": map[string]any{"retained_frames": 512, "reply_frames": 8, "max_reply_frames": 16}}
}

func (f *Fake) model() map[string]any {
	checkpoint, _ := f.header["model"].(string)
	backend, _ := f.header["backend"].(string)
	if backend == "" {
		backend = "replay"
	}
	return map[string]any{"schema": "mdl1", "checkpoint": checkpoint, "backend": backend, "profile": nil,
		"tensorrt": nil, "engines": []any{}}
}

func (f *Fake) status() map[string]any {
	build, _ := f.header["build"].(string)
	s := map[string]any{"build": build, "boot": f.boot, "uptime_s": 0.0, "page": "next", "worker": f.workerJSON(),
		"stable": f.stableJSON(), "hosts": map[string]any{}, "viewers": f.viewers, "long_polls": 0, "streams": f.streams}
	if ret := f.retained(); ret != nil {
		s["retained"] = ret
	}
	return s
}

func (f *Fake) serveMetrics(w http.ResponseWriter) {
	f.mu.Lock()
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP interp_viewers Viewers now.\n# TYPE interp_viewers gauge\ninterp_viewers %d\n", f.viewers)
	counts := map[string]int{}
	for _, e := range f.tl {
		if e.side == "" {
			counts[e.video]++
		}
	}
	videos := make([]string, 0, len(counts))
	for v := range counts {
		videos = append(videos, v)
	}
	sort.Strings(videos)
	for _, v := range videos {
		fmt.Fprintf(&b, "interp_frames_total{video=%q} %d\n", v, counts[v])
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = io.WriteString(w, b.String())
}

func (f *Fake) catalogJSON() []byte {
	def := f.recSrc // the recording; without one, the first source (the camera)
	if def == "" && len(f.sources) > 0 {
		def, _ = f.sources[0]["id"].(string)
	}
	b, _ := json.Marshal(map[string]any{"schema": "stone-interpreter-catalog-v1", "title": "Stone-work interpreter",
		"default_video": def, "videos": f.sources})
	return b
}

func etag(b []byte) string {
	h := fnv.New64a()
	_, _ = h.Write(b)
	return fmt.Sprintf("\"%016x\"", h.Sum64())
}

func (f *Fake) serveCatalog(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	b := f.catalogJSON()
	f.mu.Unlock()
	tag := etag(b)
	w.Header().Set("ETag", tag)
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}

// mediaBytes is the fake's proxy video: the recorded media reply's bytes, else a small synthetic file.
func (f *Fake) mediaBytes() []byte {
	if rep, ok := f.replies["getVideoMedia-range"]; ok {
		return rep.Body
	}
	b := make([]byte, 4096)
	copy(b, "\x00\x00\x00\x18ftypisom\x00\x00\x02\x00isomiso2")
	for i := 24; i < len(b); i++ {
		b[i] = byte(i * 31)
	}
	return b
}

func (f *Fake) serveMedia(w http.ResponseWriter, r *http.Request, id string) {
	f.mu.Lock()
	src := f.source(id)
	data := f.mediaBytes()
	f.mu.Unlock()
	if src == nil {
		errorOf(404, "unknown_video", fmt.Sprintf("Unknown video '%s'", id), "id").write(w)
		return
	}
	if src["live"] == true {
		errorOf(404, "not_found", id+" is a live camera: it has no proxy video; each frame's picture is "+
			"/api/v2/session/frames/{g}/image.jpg", "id").write(w)
		return
	}
	size := int64(len(data))
	rng := r.Header.Get("Range")
	tag := etag(data)
	w.Header().Set("ETag", tag)
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Type", "video/mp4")
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	first, last, status := int64(0), size-1, http.StatusOK
	if rng != "" && (r.Header.Get("If-Range") == "" || r.Header.Get("If-Range") == tag) {
		a, b, ok := parseRange(rng, size)
		if !ok {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			w.Header().Set("Content-Length", "0")
			w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
			return
		}
		first, last, status = a, b, http.StatusPartialContent
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", a, b, size))
	}
	w.Header().Set("Content-Length", strconv.FormatInt(last-first+1, 10))
	w.WriteHeader(status)
	if r.Method != http.MethodHead {
		_, _ = w.Write(data[first : last+1])
	}
}

// parseRange parses one byte range (bytes=a-b, a-, -n) of a file of size bytes.
func parseRange(v string, size int64) (int64, int64, bool) {
	spec, ok := strings.CutPrefix(v, "bytes=")
	if !ok || strings.Contains(spec, ",") {
		return 0, 0, false
	}
	a, b, ok := strings.Cut(spec, "-")
	if !ok {
		return 0, 0, false
	}
	if a == "" {
		n, err := strconv.ParseInt(b, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	}
	first, err := strconv.ParseInt(a, 10, 64)
	if err != nil || first >= size {
		return 0, 0, false
	}
	last := size - 1
	if b != "" {
		l, err := strconv.ParseInt(b, 10, 64)
		if err != nil || l < first {
			return 0, 0, false
		}
		if l < last {
			last = l
		}
	}
	return first, last, true
}

func (f *Fake) serveRaster(w http.ResponseWriter, r *http.Request, g, raster string) {
	scale := 1
	if v := r.URL.Query().Get("scale"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 8 {
			errorOf(400, "invalid_param", "scale must be within 1..8", "scale").write(w)
			return
		}
		scale = n
	}
	if raster != "regions" && raster != "mask" && raster != "depth" {
		errorOf(404, "not_found", fmt.Sprintf("unknown raster '%s'", raster), "raster").write(w)
		return
	}
	f.mu.Lock()
	hasRec := f.hasRec
	var err *apiError
	if hasRec {
		_, err = f.frameEntry(g)
	}
	f.mu.Unlock()
	if err != nil {
		err.write(w)
		return
	}
	if f.recorded(w, r, "getFrameRaster") {
		return
	}
	img := image.NewGray(image.Rect(0, 0, 64*scale, 40*scale))
	for i := range img.Pix {
		img.Pix[i] = 0x40
	}
	img.Set(0, 0, color.Gray{Y: 0xff})
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(buf.Bytes())
}

// ---- frames of the current session (the caller holds f.mu) --------------------------------------------------------

// current returns the newest revision of every grid frame of the current session, by g: the frames of the current
// program (a camera shown at the start has the session id of the recording's own frames).
func (f *Fake) current() (map[int64]*entry, []int64) {
	by := map[int64]*entry{}
	for _, e := range f.tl[f.start:] {
		if e.side != "" || e.session != f.st.Session {
			continue
		}
		if prev, ok := by[e.g]; !ok || e.rev >= prev.rev {
			by[e.g] = e
		}
	}
	gs := make([]int64, 0, len(by))
	for g := range by {
		gs = append(gs, g)
	}
	sort.Slice(gs, func(i, j int) bool { return gs[i] < gs[j] })
	return by, gs
}

func (f *Fake) retained() map[string]any {
	by, gs := f.current()
	if len(gs) == 0 {
		return map[string]any{"session": f.st.Session, "g0": nil, "g1": nil, "t0": nil, "t1": nil, "n": 0}
	}
	return map[string]any{"session": f.st.Session, "g0": gs[0], "g1": gs[len(gs)-1], "t0": by[gs[0]].t,
		"t1": by[gs[len(gs)-1]].t, "n": len(gs)}
}

func notRetained(g string, retained map[string]any) *apiError {
	e := errorOf(404, "not_retained", "frame "+g+" is not retained", "g")
	if retained != nil {
		e.body["retained"] = retained
	}
	return e
}

func (f *Fake) frameEntry(g string) (*entry, *apiError) {
	if !gPattern.MatchString(g) {
		return nil, errorOf(400, "invalid_param", "g must be an integer or latest", "g")
	}
	by, gs := f.current()
	if g == "latest" {
		if len(gs) == 0 {
			e := notRetained(g, f.retained())
			e.body["error"] = "no frame is retained yet"
			return nil, e
		}
		return by[gs[len(gs)-1]], nil
	}
	n, _ := strconv.ParseInt(g, 10, 64)
	e, ok := by[n]
	if !ok {
		return nil, notRetained(g, f.retained())
	}
	return e, nil
}

func parseLayers(q map[string][]string) (map[string]bool, *apiError) {
	v := defaultLayers
	if l := q["layers"]; len(l) > 0 && l[0] != "" {
		v = l[0]
	}
	out := map[string]bool{}
	for _, name := range strings.Split(v, ",") {
		name = strings.TrimSpace(name)
		ok := false
		for _, known := range layerNames {
			ok = ok || name == known
		}
		if !ok {
			return nil, errorOf(400, "unknown_layer", fmt.Sprintf("unknown layer '%s'", name), "layers")
		}
		out[name] = true
	}
	return out, nil
}

func checkPrecision(q map[string][]string) *apiError {
	if p := q["precision"]; len(p) > 0 && p[0] != "rounded" && p[0] != "full" {
		return errorOf(400, "invalid_param", "precision must be rounded or full", "precision")
	}
	return nil
}

// checkSession answers 410 for a session that is not the current one. The caller holds f.mu.
func (f *Fake) checkSession(q map[string][]string) *apiError {
	s := q["session"]
	if len(s) == 0 || s[0] == "" {
		return nil
	}
	n, err := strconv.ParseInt(s[0], 10, 64)
	if err != nil {
		return errorOf(400, "invalid_param", "session must be an integer", "session")
	}
	if n != f.st.Session {
		return errorOf(410, "session_gone", fmt.Sprintf("session %d has ended", n), "session")
	}
	return nil
}

// frameJSON is a frame with the layers asked for, in the server's key order; missing lists only layers asked
// for. It is built from the recorded JSON without re-encoding it.
func frameJSON(e *entry, layers map[string]bool) json.RawMessage {
	var b bytes.Buffer
	b.WriteByte('{')
	add := func(k string, v []byte) {
		if b.Len() > 1 {
			b.WriteByte(',')
		}
		b.WriteString(`"` + k + `":`)
		b.Write(v)
	}
	for _, k := range baseKeys {
		if v, ok := e.obj[k]; ok {
			add(k, v)
		}
	}
	for _, name := range layerNames {
		if v, ok := e.obj[name]; ok && layers[name] {
			add(name, v)
		}
	}
	if raw, ok := e.obj["missing"]; ok {
		var missing, kept []string
		_ = json.Unmarshal(raw, &missing)
		for _, m := range missing {
			if layers[m] {
				kept = append(kept, m)
			}
		}
		if len(kept) > 0 {
			v, _ := json.Marshal(kept)
			add("missing", v)
		}
	}
	b.WriteByte('}')
	return b.Bytes()
}

func (f *Fake) frame(q map[string][]string, g string) (any, *apiError) {
	layers, err := parseLayers(q)
	if err == nil {
		err = checkPrecision(q)
	}
	if err == nil {
		err = f.checkSession(q)
	}
	if err != nil {
		return nil, err
	}
	e, err := f.frameEntry(g)
	if err != nil {
		return nil, err
	}
	return frameJSON(e, layers), nil
}

func (f *Fake) packet(q map[string][]string, g string) (any, *apiError) {
	if err := f.checkSession(q); err != nil {
		return nil, err
	}
	e, err := f.frameEntry(g)
	if err != nil {
		return nil, err
	}
	p, ok := packetJSON(e)
	if !ok {
		return nil, notRetained(g, f.retained())
	}
	return p, nil
}

// packetJSON is a frame's raw packet with the frame's cursor, session, video and time (false: no raw layer).
func packetJSON(e *entry) (map[string]json.RawMessage, bool) {
	raw, ok := e.obj["raw"]
	if !ok {
		return nil, false
	}
	p, err := decodeObject(raw)
	if err != nil {
		return nil, false
	}
	for _, k := range []string{"cursor", "session", "video", "g", "i", "t"} {
		if v, ok := e.obj[k]; ok {
			p[k] = v
		}
	}
	return p, true
}

// worldStone is a stone of a world layer, with its JSON.
type worldStone struct {
	ID      int64       `json:"id"`
	Outline [][]float64 `json:"outline"`
	Solid   *struct {
		Pos  []float64 `json:"pos"`
		Q    []float64 `json:"q"`
		Dims []float64 `json:"dims"`
	} `json:"solid"`
	raw json.RawMessage
}

// worldView is the part of a world layer the fake reads.
type worldView struct {
	Stones []worldStone
	Events []map[string]json.RawMessage
}

func (w *worldView) find(id int64) *worldStone {
	for i := range w.Stones {
		if w.Stones[i].ID == id {
			return &w.Stones[i]
		}
	}
	return nil
}

// worldOf parses a frame's world layer once. The caller holds f.mu.
func worldOf(e *entry) (*worldView, bool) {
	if e.wv != nil {
		return e.wv, true
	}
	raw, ok := e.obj["world"]
	if !ok {
		return nil, false
	}
	var w struct {
		Stones []json.RawMessage            `json:"stones"`
		Events []map[string]json.RawMessage `json:"events"`
	}
	if json.Unmarshal(raw, &w) != nil {
		return nil, false
	}
	wv := &worldView{Events: w.Events}
	for _, s := range w.Stones {
		var ws worldStone
		if json.Unmarshal(s, &ws) == nil {
			ws.raw = s
			wv.Stones = append(wv.Stones, ws)
		}
	}
	e.wv = wv
	return wv, true
}

func (f *Fake) world(q map[string][]string) (any, *apiError) {
	if err := checkPrecision(q); err != nil {
		return nil, err
	}
	by, gs := f.current()
	for i := len(gs) - 1; i >= 0; i-- {
		if w, ok := by[gs[i]].obj["world"]; ok {
			return w, nil
		}
	}
	return nil, &apiError{status: 503, body: map[string]any{"error": "the Stable sim is not running",
		"code": "world_unavailable", "retry_ms": 1000}}
}

var eventTypes = map[string]bool{"new": true, "confirm": true, "freeze": true, "push": true, "lift": true,
	"hinge": true, "carry": true, "place": true, "drop": true, "gone": true, "delete": true, "split": true, "sheet": true}

func intParam(q map[string][]string, key string, def, lo, hi int64, msg string) (int64, *apiError) {
	v := q[key]
	if len(v) == 0 || v[0] == "" {
		return def, nil
	}
	n, err := strconv.ParseInt(v[0], 10, 64)
	if err != nil {
		return 0, errorOf(400, "invalid_param", key+" must be an integer", key)
	}
	if n < lo || n > hi {
		return 0, errorOf(400, "invalid_param", msg, key)
	}
	return n, nil
}

func (f *Fake) events(q map[string][]string) (any, *apiError) {
	after, err := intParam(q, "after", -1, -1, 1<<62, "after must be an integer")
	if err != nil {
		return nil, err
	}
	limit, err := intParam(q, "limit", 256, 1, 1024, "limit must be within 1..1024")
	if err != nil {
		return nil, err
	}
	types := map[string]bool{}
	if t := q["type"]; len(t) > 0 && t[0] != "" {
		for _, name := range strings.Split(t[0], ",") {
			if !eventTypes[name] {
				return nil, errorOf(400, "invalid_param", fmt.Sprintf("unknown event type '%s'", name), "type")
			}
			types[name] = true
		}
	}
	by, gs := f.current()
	events := []map[string]json.RawMessage{}
	next := after
	for _, g := range gs {
		if g <= after {
			continue
		}
		w, ok := worldOf(by[g])
		if !ok {
			continue
		}
		var these []map[string]json.RawMessage
		for _, ev := range w.Events {
			var typ string
			_ = json.Unmarshal(ev["type"], &typ)
			if len(types) > 0 && !types[typ] {
				continue
			}
			out := map[string]json.RawMessage{}
			for k, v := range ev {
				out[k] = v
			}
			out["g"], _ = json.Marshal(g)
			out["t"], _ = json.Marshal(by[g].t)
			these = append(these, out)
		}
		if len(events) > 0 && int64(len(events)+len(these)) > limit {
			break
		}
		events = append(events, these...)
		next = g
	}
	reply := map[string]any{"session": f.st.Session, "video": f.st.Video, "events": events, "next_after": next}
	if len(gs) > 0 {
		reply["oldest_g"] = gs[0]
	}
	return reply, nil
}

// track is a stone's path in the current session. The caller holds f.mu.
func (f *Fake) track(id int64) (map[string]any, bool) {
	by, gs := f.current()
	var stone json.RawMessage
	var first, last int64 = -1, -1
	var t float64
	path2d, path3d := [][]float64{}, [][]float64{}
	for _, g := range gs {
		w, ok := worldOf(by[g])
		if !ok {
			continue
		}
		s := w.find(id)
		if s == nil {
			continue
		}
		if first < 0 {
			first = g
		}
		last, t, stone = g, by[g].t, s.raw
		if len(s.Outline) > 0 {
			var cx, cy float64
			for _, p := range s.Outline {
				p = append(p, 0, 0)
				cx += p[0]
				cy += p[1]
			}
			n := float64(len(s.Outline))
			path2d = append(path2d, []float64{round4(cx / n), round4(cy / n)})
		}
		if s.Solid != nil {
			p := codec.Vec3(s.Solid.Pos)
			path3d = append(path3d, p[:])
		}
	}
	if first < 0 {
		return nil, false
	}
	return map[string]any{"schema": "sts1", "session": f.st.Session, "video": f.st.Video, "id": id, "g": last,
		"t": t, "stone": stone, "first_g": first, "last_g": last, "path2d": path2d, "path3d": path3d}, true
}

func (f *Fake) stone(idText string) (any, *apiError) {
	id, err := strconv.ParseInt(idText, 10, 64)
	if err != nil || id < 0 {
		return nil, errorOf(400, "invalid_param", "id must be an integer", "id")
	}
	tr, ok := f.track(id)
	if !ok {
		return nil, errorOf(404, "unknown_stone", fmt.Sprintf("no stone %d in the world", id), "id")
	}
	return tr, nil
}

func (f *Fake) focusReply() map[string]any {
	if f.st.Focus == nil {
		return map[string]any{"focus": nil, "resolved": nil}
	}
	resolved := map[string]any{"id": f.st.Focus.ID, "path2d": [][]float64{}, "path3d": [][]float64{}}
	if tr, ok := f.track(f.st.Focus.ID); ok {
		resolved["path2d"], resolved["path3d"] = tr["path2d"], tr["path3d"]
	}
	return map[string]any{"focus": f.st.Focus, "resolved": resolved}
}

// ---- streams --------------------------------------------------------------------------------------------------------

// positionAfter is the timeline index after the last frame with an event cursor at or below c.
func (f *Fake) positionAfter(c int64) int {
	pos := 0
	for i, e := range f.tl {
		if e.side == "" && e.cursor <= c {
			pos = i + 1
		}
	}
	return pos
}

func parseEventID(id string) (string, int64, bool) {
	i := strings.LastIndexByte(id, '-')
	if i < 0 {
		return "", 0, false
	}
	n, err := strconv.ParseInt(id[i+1:], 10, 64)
	if err != nil {
		return "", 0, false
	}
	return id[:i], n, true
}

func streamFormat(r *http.Request, raw bool) (string, *apiError) {
	switch format := r.URL.Query().Get("format"); format {
	case "sse", "ndjson":
		return format, nil
	case "":
		accept := r.Header.Get("Accept")
		switch {
		case strings.Contains(accept, "text/event-stream"):
			return "sse", nil
		case strings.Contains(accept, "ndjson") || raw:
			return "ndjson", nil
		}
		return "sse", nil
	}
	return "", errorOf(400, "invalid_param", "format must be ndjson or sse", "format")
}

// phaseLook is how often a stream waiting on a linked clock (WithClock) looks whether the camera's next phase has
// come, in real time.
const phaseLook = 10 * time.Millisecond

// errAbort is panicked by a handler to drop its connection (http.ErrAbortHandler on a real server).
var errAbort = http.ErrAbortHandler

func (f *Fake) serveStream(w http.ResponseWriter, r *http.Request, op string) {
	raw := op == "rawStream"
	q := r.URL.Query()
	format, err := streamFormat(r, raw)
	var layers map[string]bool
	if err == nil && !raw {
		layers, err = parseLayers(q)
	}
	if err == nil && !raw {
		err = checkPrecision(q)
	}
	var limit, maxS, after int64
	if err == nil {
		limit, err = intParam(q, "limit", 0, 1, 4096, "limit must be within 1..4096")
	}
	if err == nil {
		maxS, err = intParam(q, "max_s", 0, 1, 3600, "max_s must be within 1..3600")
	}
	if err == nil {
		after, err = intParam(q, "after", -1, 0, 1<<62, "after must be an integer")
	}
	if err != nil {
		err.write(w)
		return
	}
	if maxS > 0 && (limit == 0 || maxS*5 < limit) {
		limit = maxS * 5
	}
	f.mu.Lock()
	if !f.hasRec {
		f.mu.Unlock()
		if !f.recorded(w, r, op) {
			errorOf(503, "not_ready", "the fake has no recording", "").write(w)
		}
		return
	}
	pos := f.start
	pre := f.injects
	f.injects = nil
	if id := r.Header.Get("Last-Event-ID"); id != "" {
		boot, cur, ok := parseEventID(id)
		if !ok || boot != f.boot {
			b, _ := json.Marshal(map[string]any{"boot": f.boot, "cursor": f.cursor})
			pre = append(pre, &entry{side: "reset", data: b})
		} else {
			pos = f.positionAfter(cur)
		}
	} else if after >= 0 {
		pos = f.positionAfter(after)
	}
	if pos < f.start {
		pos = f.start
	}
	f.play(pos) // before the headers: a client that reads the session on connect gets the state where it starts
	drop := -1
	if k, ok := f.drops[op]; ok {
		drop = k
		delete(f.drops, op)
	}
	viewer := 0
	if q.Get("observe") != "1" {
		viewer = 1
	}
	f.viewers += viewer
	f.streams++
	boot := f.boot
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.viewers -= viewer
		f.streams--
		f.mu.Unlock()
	}()
	if format == "sse" {
		w.Header().Set("Content-Type", "text/event-stream")
	} else {
		w.Header().Set("Content-Type", "application/x-ndjson")
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flush(w)
	events, frames := 0, int64(0)
	write := func(e *entry) {
		var b []byte
		switch {
		case e.side != "" && format == "sse":
			b = []byte("event: " + e.side + "\ndata: " + string(e.data) + "\n\n")
		case e.side == "gap":
			b = append(append([]byte(nil), e.data...), '\n')
		case e.side != "":
			return
		default:
			var data []byte
			if raw {
				p, ok := packetJSON(e)
				if !ok {
					return
				}
				data, _ = json.Marshal(p)
			} else {
				data = frameJSON(e, layers)
			}
			if format == "sse" {
				b = []byte(fmt.Sprintf("id: %s-%d\nevent: frame\ndata: %s\n\n", boot, e.cursor, data))
			} else {
				b = append(data, '\n')
			}
			frames++
		}
		if _, err := w.Write(b); err != nil {
			panic(errAbort)
		}
		flush(w)
		events++
		if drop >= 0 && events >= drop {
			panic(errAbort)
		}
	}
	for _, e := range pre {
		write(e)
	}
	for limit == 0 || frames < limit {
		f.mu.Lock()
		if pos < f.start {
			pos = f.start
		}
		if pos < len(f.tl) {
			e := f.tl[pos]
			pos++
			f.play(pos) // a later clock line of the recording is the session state once a stream passes it
			f.mu.Unlock()
			write(e)
			continue
		}
		if f.skipToPhase(math.Inf(1)) { // the stream waits for the camera's next phase (a still clock moves on to it)
			f.mu.Unlock()
			continue
		}
		ch, pending := f.changed, f.phasePending()
		f.mu.Unlock()
		var look <-chan time.Time
		var tick *time.Timer
		if pending { // a linked clock: look again soon whether the phase has come
			tick = time.NewTimer(phaseLook)
			look = tick.C
		}
		select {
		case <-ch:
		case <-look:
			f.mu.Lock()
			f.advance()
			f.mu.Unlock()
		case <-r.Context().Done():
		}
		if tick != nil {
			tick.Stop()
		}
		if r.Context().Err() != nil {
			return
		}
	}
	f.mu.Lock()
	hold := f.hold
	f.mu.Unlock()
	if hold {
		<-r.Context().Done() // the unit's late close: nothing more is sent, and the client has to end it
	}
}

func flush(w http.ResponseWriter) {
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
}

func (f *Fake) servePoll(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	layers, err := parseLayers(q)
	var cursor, limit int64
	if err == nil {
		cursor, err = intParam(q, "cursor", 0, 0, 1<<62, "cursor must be an integer")
	}
	if err == nil {
		_, err = intParam(q, "rev", 0, 0, 1<<62, "rev must be an integer")
	}
	if err == nil {
		limit, err = intParam(q, "limit", 8, 1, 16, "limit must be within 1..16")
	}
	var wait int64
	if err == nil {
		wait, err = intParam(q, "wait", 1000, 0, 1000, "wait must be within 0..1000 ms")
	}
	if err == nil {
		err = checkPrecision(q)
	}
	if cid := q.Get("cid"); err == nil && cid != "" && !cidPattern.MatchString(cid) {
		err = errorOf(400, "invalid_param", "cid must match ^[A-Za-z0-9_-]{8,40}$", "cid")
	}
	if err != nil {
		err.write(w)
		return
	}
	if !f.hasRecording() {
		if !f.recorded(w, r, "pollFrames") {
			errorOf(503, "not_ready", "the fake has no recording", "").write(w)
		}
		return
	}
	// Time is not simulated: the reply comes at once with the next frames after cursor (none when up to date), up to
	// a clock line of the recording that playback has not passed (the next poll passes it), so its state is the one in
	// force for its frames. A camera's phases due within the poll's wait are taken first (a still clock moves on to
	// them).
	f.mu.Lock()
	for until := f.wall() + float64(wait)/1000; f.skipToPhase(until); {
	}
	f.advance()
	pos := f.start
	if p := f.positionAfter(cursor); cursor > 0 && p > pos {
		pos = p
	}
	f.play(pos)
	frames := []json.RawMessage{}
	last := min(cursor, f.cursor)
	for i := pos; i < len(f.tl) && int64(len(frames)) < limit; i++ {
		e := f.tl[i]
		if e.later && i >= f.played && len(frames) > 0 {
			break // a clock line playback has not passed: the next poll passes it, and its reply has that state
		}
		f.play(i + 1)
		if e.side == "" {
			frames = append(frames, frameJSON(e, layers))
			last = e.cursor
		}
	}
	reply := f.sessionReply()
	reply["cursor"], reply["frames"] = last, frames
	f.mu.Unlock()
	writeJSON(w, 200, reply)
}

func (f *Fake) hasRecording() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hasRec
}
