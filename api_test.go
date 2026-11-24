package pdiggerfactory

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// TestAPIRecordedReplies calls every operation against the replies recorded on 20.20 (fixtures/api).
func TestAPIRecordedReplies(t *testing.T) {
	f := newFake(t, factorytest.WithReplies(fixturesAPI))
	c, _ := fakeClient(t, f)
	ctx := testCtx(t)
	api := c.API()

	root, err := api.GetAPIRoot(ctx)
	if err != nil || root.API != "2.2.0" || root.Boot != "a2011b037b" || root.DocsPolicy == nil ||
		!root.DocsPolicy.ForceDryRun || root.Layers["world"] != "stw1" {
		t.Fatalf("root %+v %v", root, err)
	}
	spec, err := api.GetOpenAPI(ctx)
	if err != nil || !bytes.HasPrefix(spec, []byte("openapi: 3.1.0")) {
		t.Fatalf("openapi %v", err)
	}
	model, err := api.GetModel(ctx)
	if err != nil || model.Checkpoint != "8d09531c915f" || len(model.Engines) == 0 {
		t.Fatalf("model %+v %v", model, err)
	}
	top, err := api.GetTopology(ctx)
	if err != nil || top.Schema != "top1" || *top.Wrist.Left != 62 {
		t.Fatalf("topology %+v %v", top, err)
	}
	health, err := api.GetHealth(ctx)
	if err != nil || !health.OK || health.Worker != "idle" {
		t.Fatalf("health %+v %v", health, err)
	}
	ready, err := api.GetReady(ctx)
	if err != nil || !ready.Ready {
		t.Fatalf("ready %+v %v", ready, err)
	}
	status, err := api.GetStatus(ctx)
	if err != nil || status.Boot != "a2011b037b" || status.Retained == nil || status.Retained.N != 145 {
		t.Fatalf("status %+v %v", status, err)
	}
	metrics, err := api.GetMetrics(ctx)
	if err != nil || len(metrics.Find("interp_frames_total", map[string]string{"video": "dry1"})) != 1 {
		t.Fatalf("metrics %v", err)
	}
	list, err := api.ListVideos(ctx, &ListVideosParams{IfNoneMatch: `"other"`})
	if err != nil || len(list.Videos) != 10 || list.DefaultVideo != "aug28" || list.ETag == "" || list.NotModified {
		t.Fatalf("videos %+v %v", list, err)
	}
	for i, v := range list.Videos { // the four recordings, then the six plant cameras
		if i < 4 && (v.Kind != SourceKindRecording || v.Live || !v.Seekable || v.IsLive()) {
			t.Errorf("source defaults %+v", v)
		}
		if i >= 4 && (v.Kind != SourceKindCamera || !v.Live || v.Seekable || !v.IsLive()) {
			t.Errorf("camera %+v", v)
		}
	}
	video, err := api.GetVideo(ctx, "dry1")
	if err != nil || video.ID != "dry1" || video.Duration == nil {
		t.Fatalf("video %+v %v", video, err)
	}
	media, err := api.GetVideoMedia(ctx, "dry1", &MediaParams{Range: Bytes(0, 1023)})
	if err != nil || media.Status != 206 || len(media.Body) != 1024 || media.ContentRange == nil ||
		media.ContentRange.Size != 184866573 || media.ETag == "" || media.AcceptRanges != "bytes" {
		t.Fatalf("media %+v %v", media, err)
	}
	sess, err := api.GetSession(ctx)
	if err != nil || sess.State.Session != 1795314885 || sess.State.Video != "dry2" || sess.Viewers != 0 {
		t.Fatalf("session %+v %v", sess, err)
	}
	focus, err := api.GetFocus(ctx)
	if err != nil || focus.Focus != nil || focus.Resolved != nil {
		t.Fatalf("focus %+v %v", focus, err)
	}
	world, err := api.GetWorld(ctx, &WorldParams{Precision: "rounded"})
	if err != nil || world.Schema != "stw1" || len(world.Stones) == 0 {
		t.Fatalf("world %v", err)
	}
	log, err := api.GetWorldEvents(ctx, &WorldEventsParams{After: Int64(2934), Limit: 64, Types: []string{"new", "freeze"}})
	if err != nil || log.NextAfter != 2935 || len(log.Events) != 45 || log.OldestG == nil {
		t.Fatalf("events %+v %v", log, err)
	}
	stone, err := api.GetStone(ctx, 43)
	if err != nil || stone.ID != 43 || stone.Stone == nil || stone.Stone.Solid == nil {
		t.Fatalf("stone %+v %v", stone, err)
	}
	poll, err := api.PollFrames(ctx, &PollParams{Cursor: 10389, Rev: 429, CID: "go-test-0001", Layers: []string{"world"},
		Limit: 2, WaitMS: Int64(0), Precision: "rounded"})
	if err != nil || poll.Cursor != 10391 || len(poll.Frames) != 2 || poll.Frames[0].World == nil {
		t.Fatalf("poll %v", err)
	}
	frame, err := api.GetFrame(ctx, LatestFrame(), &FrameParams{Layers: []string{"world", "cards"}, Session: Int64(1795314885),
		Precision: "full"})
	if err != nil || frame.World == nil || !frame.HasLayer(LayerWorld) {
		t.Fatalf("frame %v", err)
	}
	png, err := api.GetFrameRaster(ctx, frame.G, RasterRegions, &RasterParams{Scale: 2})
	if err != nil || !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Fatalf("raster %v", err)
	}
	ov, err := api.RenderOverlay2D(ctx, &RenderParams{G: Int64(frame.G), Mode: "stable", Draw: "primitives",
		Session: Int64(1795314885)})
	if err != nil || ov.Primitives == nil {
		t.Fatalf("overlay2d %v", err)
	}
	ov, err = api.RenderOverlay2D(ctx, &RenderParams{T: Float64(580.5)})
	if err != nil || ov.Schema == "" || ov.Primitives != nil {
		t.Fatalf("overlay2d shapes %v", err)
	}
	sc, err := api.RenderScene3D(ctx, &Scene3DParams{RenderParams: RenderParams{G: Int64(frame.G)}, Surface: "compact"})
	if err != nil || sc.Surface == nil {
		t.Fatalf("scene3d %v", err)
	}
	sc, err = api.RenderScene3D(ctx, &Scene3DParams{RenderParams: RenderParams{G: Int64(frame.G)}, Surface: "none"})
	if err != nil || sc.Surface != nil {
		t.Fatalf("scene3d none %v", err)
	}
	pkt, err := api.RawPacket(ctx, LatestFrame(), &RawPacketParams{Session: Int64(1795314885)})
	if err != nil || pkt.Schema != "sip1" || pkt.Cursor == nil {
		t.Fatalf("packet %v", err)
	}
	_, err = api.RawTensors(ctx, 5)
	mustCode(t, err, CodeDebugDisabled)

	// the recorded streams: SSE and NDJSON frames, NDJSON and SSE packets
	frames := collect(t, api.StreamFrames(ctx, &StreamFramesParams{Layers: []string{"world", "cards"}, Observe: true,
		After: Int64(10764), Limit: 3, MaxS: 5, Precision: "rounded", LastEventID: "a2011b037b-10764"}), 3)
	if frames[0].Event != StreamEventFrame || !strings.HasPrefix(frames[0].ID, "a2011b037b-") {
		t.Fatalf("sse frames %+v", frames[0])
	}
	var fr Frame
	if err := frames[0].Decode(&fr); err != nil || fr.Cursor == 0 {
		t.Fatalf("decode %v", err)
	}
	nd := collect(t, api.StreamFrames(ctx, &StreamFramesParams{Format: FormatNDJSON}), 2)
	if nd[0].Event != StreamEventFrame || nd[0].ID != "" {
		t.Fatalf("ndjson frames %+v", nd[0])
	}
	raw := collect(t, api.RawStream(ctx, nil), 2)
	if raw[0].Event != StreamEventFrame {
		t.Fatalf("raw %+v", raw[0])
	}
	rawSSE := collect(t, api.RawStream(ctx, &RawStreamParams{Format: FormatSSE, After: Int64(1), Limit: 2, MaxS: 9,
		Observe: true, LastEventID: "x-1"}), 1)
	if rawSSE[0].ID == "" {
		t.Fatalf("raw sse %+v", rawSSE[0])
	}
	if got := len(collect(t, api.StreamFrames(ctx, nil), 1)); got != 1 {
		t.Fatal("default stream")
	}
}

// TestAPIControlsDryRun runs every control as a dry run against a fake without a recording: the state comes from
// the recorded getSession reply, the catalog from the recorded listVideos reply.
func TestAPIControlsDryRun(t *testing.T) {
	f := newFake(t, factorytest.WithReplies(fixturesAPI))
	c, _ := fakeClient(t, f)
	ctx := testCtx(t)
	api := c.API()
	dry := &ControlParams{DryRun: true}
	r, err := api.SessionSelect(ctx, SelectRequest{Video: "aug28", Time: Float64(1277.8)}, dry)
	if err != nil || !r.DryRun || r.State.Video != "aug28" || r.State.AnchorMedia != 1277.8 || r.State.Revision != 430 {
		t.Fatalf("select %+v %v", r, err)
	}
	r, err = api.SessionSeek(ctx, SeekRequest{Time: 600}, dry)
	if err != nil || r.State.Video != "dry2" || r.State.AnchorMedia != 600 {
		t.Fatalf("seek %+v %v", r, err)
	}
	r, err = api.SessionPause(ctx, dry)
	if err != nil || r.State.Playing {
		t.Fatalf("pause %+v %v", r, err)
	}
	r, err = api.SessionPlay(ctx, dry)
	if err != nil || !r.State.Playing || r.State.Revision != 429 {
		t.Fatalf("play %+v %v", r, err)
	}
	r, err = api.DeleteFocus(ctx, dry)
	if err != nil || r.State.Focus != nil {
		t.Fatalf("delete focus %+v %v", r, err)
	}
	_, err = api.PutFocus(ctx, PutFocusParams{StoneID: 43, DryRun: true})
	e := mustCode(t, err, CodeWorldUnavailable)
	if d, ok := e.RetryDelay(); !ok || d.Milliseconds() != 1000 {
		t.Fatalf("retry hint %v", e)
	}
	if s, _, _ := f.SessionState(); s != 1795314885 {
		t.Fatalf("a dry run moved the session: %d", s)
	}
	_, err = api.SessionSeek(ctx, SeekRequest{Time: math.NaN()}, nil)
	mustCode(t, err, CodeInvalidParam)
	_, err = api.SessionSelect(ctx, SelectRequest{Video: "nope"}, nil)
	if e := mustCode(t, err, CodeUnknownVideo); e.Field != "video" || e.Status != 400 {
		t.Fatalf("unknown video %+v", e)
	}
}

// TestAPIRecordedErrors answers recorded error replies and checks the error fields.
func TestAPIRecordedErrors(t *testing.T) {
	f := newFake(t, factorytest.WithReplies(fixturesAPI))
	c, _ := fakeClient(t, f)
	ctx := testCtx(t)
	api := c.API()
	for _, k := range []string{"getFrame-404", "getFrame-410", "getFrame-400"} {
		if err := f.ReplyNext("getFrame", 1, k); err != nil {
			t.Fatal(err)
		}
	}
	_, err := api.GetFrame(ctx, FrameAt(0), nil)
	e := mustCode(t, err, CodeNotRetained)
	if e.Status != 404 || e.Retained == nil || e.Retained.N != 145 || !IsNotRetained(err) {
		t.Fatalf("404 %+v", e)
	}
	_, err = api.GetFrame(ctx, FrameAt(1), nil)
	if !IsSessionGone(err) {
		t.Fatalf("410 %v", err)
	}
	_, err = api.GetFrame(ctx, FrameAt(2), nil)
	mustCode(t, err, CodeUnknownLayer)
	if err := f.ReplyNext("getFrame", 1, "nope"); err == nil {
		t.Fatal("unknown reply key")
	}
	_, err = api.RenderOverlay2D(ctx, &RenderParams{})
	mustCode(t, err, CodeInvalidParam)
	_, err = api.RenderOverlay2D(ctx, nil)
	mustCode(t, err, CodeInvalidParam)
	_, err = api.RenderScene3D(ctx, nil)
	mustCode(t, err, CodeInvalidParam)
	_, err = api.RenderScene3D(ctx, &Scene3DParams{RenderParams: RenderParams{G: Int64(1), T: Float64(2)}})
	mustCode(t, err, CodeInvalidParam)
	// a 503 is retried, and the retry gets the recorded reply
	f.FailNext("getWorld", 1, factorytest.ErrorReply(503, "world_unavailable", "the Stable sim is not running"))
	if _, err = api.GetWorld(ctx, nil); err != nil {
		t.Fatalf("world after a 503: %v", err)
	}
	f.FailNext("getStone", 1, factorytest.ErrorReply(404, "unknown_stone", "no stone 9 in the world"))
	_, err = api.GetStone(ctx, 9)
	mustCode(t, err, CodeUnknownStone)
	// every operation's error path: one 404 each
	calls := map[string]func() error{
		"getApiRoot":      func() error { _, err := api.GetAPIRoot(ctx); return err },
		"getOpenApi":      func() error { _, err := api.GetOpenAPI(ctx); return err },
		"getModel":        func() error { _, err := api.GetModel(ctx); return err },
		"getTopology":     func() error { _, err := api.GetTopology(ctx); return err },
		"getHealth":       func() error { _, err := api.GetHealth(ctx); return err },
		"getReady":        func() error { _, err := api.GetReady(ctx); return err },
		"getStatus":       func() error { _, err := api.GetStatus(ctx); return err },
		"getMetrics":      func() error { _, err := api.GetMetrics(ctx); return err },
		"listVideos":      func() error { _, err := api.ListVideos(ctx, nil); return err },
		"getVideo":        func() error { _, err := api.GetVideo(ctx, "x"); return err },
		"getVideoMedia":   func() error { _, err := api.GetVideoMedia(ctx, "x", nil); return err },
		"getSession":      func() error { _, err := api.GetSession(ctx); return err },
		"getFocus":        func() error { _, err := api.GetFocus(ctx); return err },
		"getWorld":        func() error { _, err := api.GetWorld(ctx, nil); return err },
		"getWorldEvents":  func() error { _, err := api.GetWorldEvents(ctx, nil); return err },
		"pollFrames":      func() error { _, err := api.PollFrames(ctx, nil); return err },
		"getFrameRaster":  func() error { _, err := api.GetFrameRaster(ctx, 1, RasterMask, nil); return err },
		"renderOverlay2d": func() error { _, err := api.RenderOverlay2D(ctx, &RenderParams{G: Int64(1)}); return err },
		"renderScene3d": func() error {
			_, err := api.RenderScene3D(ctx, &Scene3DParams{RenderParams: RenderParams{G: Int64(1)}})
			return err
		},
		"rawPacket":     func() error { _, err := api.RawPacket(ctx, FrameAt(1), nil); return err },
		"sessionSelect": func() error { _, err := api.SessionSelect(ctx, SelectRequest{Video: "dry1"}, nil); return err },
		"sessionSeek":   func() error { _, err := api.SessionSeek(ctx, SeekRequest{Time: 1}, nil); return err },
		"sessionPlay":   func() error { _, err := api.SessionPlay(ctx, nil); return err },
		"sessionPause":  func() error { _, err := api.SessionPause(ctx, nil); return err },
		"putFocus":      func() error { _, err := api.PutFocus(ctx, PutFocusParams{StoneID: 1}); return err },
		"deleteFocus":   func() error { _, err := api.DeleteFocus(ctx, nil); return err },
		"getStone":      func() error { _, err := api.GetStone(ctx, 1); return err },
		"streamFrames": func() error {
			for _, err := range api.StreamFrames(ctx, nil) {
				return err
			}
			return nil
		},
		"rawStream": func() error {
			for _, err := range api.RawStream(ctx, nil) {
				return err
			}
			return nil
		},
	}
	for op, call := range calls {
		f.FailNext(op, 1, factorytest.ErrorReply(404, "not_found", "gone"))
		if err := call(); !HasCode(err, CodeNotFound) {
			t.Errorf("%s: %v", op, err)
		}
	}
	if _, err := api.HeadVideoMedia(ctx, "x", &MediaParams{IfNoneMatch: "a", IfRange: "b"}); err != nil {
		t.Errorf("head: %v", err)
	}
	f.FailNext("getVideoMedia", 2, factorytest.ErrorReply(404, "unknown_video", "Unknown video 'x'"))
	// a HEAD error has no body: the status is all there is
	if _, err := api.HeadVideoMedia(ctx, "x", nil); !HasCode(err, CodeNotFound) || AsError(err).Status != 404 {
		t.Errorf("head: %v", err)
	}
	if _, err := api.OpenVideoMedia(ctx, "x", nil); !HasCode(err, CodeUnknownVideo) {
		t.Errorf("open: %v", err)
	}
}

// TestAPISynthesised checks the operations the fake synthesises from a recording with every layer.
func TestAPISynthesised(t *testing.T) {
	f := newFake(t, factorytest.WithRecording(recording("dry1-0296-0306-full")))
	c, _ := fakeClient(t, f)
	ctx := testCtx(t)
	api := c.API()
	list, err := api.ListVideos(ctx, nil)
	if err != nil || len(list.Videos) != 1 || list.Videos[0].ID != "dry1" || list.ETag == "" {
		t.Fatalf("videos %+v %v", list, err)
	}
	again, err := api.ListVideos(ctx, &ListVideosParams{IfNoneMatch: list.ETag})
	if err != nil || !again.NotModified || len(again.Videos) != 0 {
		t.Fatalf("not modified %+v %v", again, err)
	}
	if _, err := c.Live(ctx); !HasCode(err, CodeLiveUnavailable) {
		t.Fatalf("live: %v", err)
	}
	sess, err := api.GetSession(ctx)
	if err != nil || sess.State.Video != "dry1" || sess.State.Session == 0 {
		t.Fatalf("session %+v %v", sess, err)
	}
	latest, err := api.GetFrame(ctx, LatestFrame(), &FrameParams{Layers: []string{"raw", "overlay", "scene", "cards", "world"}})
	if err != nil || latest.Raw == nil || latest.Overlay == nil || latest.Scene == nil || latest.Cards == nil ||
		latest.World == nil {
		t.Fatalf("latest %v", err)
	}
	pkt, err := api.RawPacket(ctx, FrameAt(latest.G), &RawPacketParams{Session: Int64(latest.Session)})
	if err != nil || pkt.G != latest.G || pkt.Cursor == nil || *pkt.Cursor != latest.Cursor {
		t.Fatalf("packet %+v %v", pkt, err)
	}
	_, err = api.RawPacket(ctx, FrameAt(latest.G), &RawPacketParams{Session: Int64(latest.Session - 1)})
	if !IsSessionGone(err) {
		t.Fatalf("old session: %v", err)
	}
	_, err = api.GetFrame(ctx, FrameAt(1), nil)
	if e := mustCode(t, err, CodeNotRetained); e.Retained == nil || e.Retained.G1 == nil || *e.Retained.G1 != latest.G {
		t.Fatalf("retained %+v", e)
	}
	world, err := api.GetWorld(ctx, nil)
	if err != nil || world.G != latest.G {
		t.Fatalf("world %v", err)
	}
	log, err := api.GetWorldEvents(ctx, &WorldEventsParams{Limit: 1})
	if err != nil || log.Video != "dry1" {
		t.Fatalf("events %v", err)
	}
	all, err := api.GetWorldEvents(ctx, nil)
	if err != nil || len(all.Events) < len(log.Events) {
		t.Fatalf("events %v", err)
	}
	id := world.Stones[0].ID
	track, err := api.GetStone(ctx, id)
	if err != nil || track.ID != id || track.LastG != latest.G || len(track.Path3D) == 0 || len(track.Path2D) == 0 {
		t.Fatalf("track %+v %v", track, err)
	}
	_, err = api.GetStone(ctx, 999999)
	mustCode(t, err, CodeUnknownStone)
	r, err := api.PutFocus(ctx, PutFocusParams{StoneID: id})
	if err != nil || r.State.Focus == nil || r.State.Focus.ID != id || r.State.Focus.G != latest.G {
		t.Fatalf("focus %+v %v", r, err)
	}
	fr, err := api.GetFocus(ctx)
	if err != nil || fr.Focus == nil || fr.Resolved == nil || fr.Resolved.ID != id {
		t.Fatalf("get focus %+v %v", fr, err)
	}
	top, err := api.GetTopology(ctx)
	if err != nil || top.Joints != 70 {
		t.Fatalf("topology %v", err)
	}
	m, err := api.GetMetrics(ctx)
	if err != nil || len(m.Find("interp_frames_total", nil)) != 1 {
		t.Fatalf("metrics %v", err)
	}
	png, err := api.GetFrameRaster(ctx, latest.G, RasterDepth, &RasterParams{Scale: 1})
	if err != nil || !bytes.HasPrefix(png, []byte("\x89PNG")) {
		t.Fatalf("raster %v", err)
	}
	media, err := api.GetVideoMedia(ctx, "dry1", &MediaParams{Range: &ByteRange{Suffix: 16}})
	if err != nil || media.Status != 206 || len(media.Body) != 16 || media.ContentRange.Last != media.ContentRange.Size-1 {
		t.Fatalf("media %+v %v", media, err)
	}
	mr, err := api.OpenVideoMedia(ctx, "dry1", &MediaParams{Range: &ByteRange{Start: 8}})
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(mr)
	if err != nil || int64(len(b)) != mr.ContentLength || mr.Status != http.StatusPartialContent {
		t.Fatalf("open media %d %v", len(b), err)
	}
	if err := mr.Close(); err != nil {
		t.Fatal(err)
	}
	head, err := api.HeadVideoMedia(ctx, "dry1", nil)
	if err != nil || head.ContentLength <= 0 || len(head.Body) != 0 {
		t.Fatalf("head %+v %v", head, err)
	}
	cached, err := api.GetVideoMedia(ctx, "dry1", &MediaParams{IfNoneMatch: head.ETag})
	if err != nil || cached.Status != http.StatusNotModified {
		t.Fatalf("cached %+v %v", cached, err)
	}
}

func TestFrameRefAndRanges(t *testing.T) {
	if FrameAt(12).String() != "12" || LatestFrame().String() != "latest" {
		t.Fatal("FrameRef")
	}
	for _, c := range []struct {
		r    ByteRange
		want string
	}{{*Bytes(0, 99), "bytes=0-99"}, {ByteRange{Start: 5}, "bytes=5-"}, {ByteRange{}, "bytes=0-"},
		{*Bytes(0, 0), "bytes=0-0"}, {ByteRange{Suffix: 10, Start: 3}, "bytes=-10"}} {
		if c.r.String() != c.want {
			t.Errorf("%+v: %s", c.r, c.r.String())
		}
	}
	for v, want := range map[string]*ContentRange{
		"bytes 0-9/100":   {0, 9, 100},
		"bytes 0-9/*":     {0, 9, -1},
		"bytes */100":     {-1, -1, 100},
		"":                nil,
		"bytes 0-9":       nil,
		"bytes 0-9/x":     nil,
		"bytes 09/10":     nil,
		"bytes a-9/10":    nil,
		"items 0-9/10":    nil,
		" bytes 1-2/3 ":   {1, 2, 3},
		"bytes 1-x/3":     nil,
		"bytes */*":       {-1, -1, -1},
		"bytes 5-4/10":    {5, 4, 10},
		"bytes 0-0/1":     {0, 0, 1},
		"bytes 10-20/100": {10, 20, 100},
	} {
		got := ParseContentRange(v)
		if (got == nil) != (want == nil) || (got != nil && *got != *want) {
			t.Errorf("%q: %+v", v, got)
		}
	}
}

func TestMediaOfBadContentLength(t *testing.T) {
	resp := reply(200, "abc", "Content-Length", "x")
	resp.ContentLength = 3
	if m := mediaOf(resp); m.ContentLength != 3 {
		t.Fatalf("%+v", m)
	}
}

// TestMediaThatNeverEnds: a camera has no media (404 from the unit, sent like any other request); a reply with neither
// a length nor byte ranges is refused unread by GetVideoMedia, and read as it arrives by OpenVideoMedia, whose Close
// closes the connection.
func TestMediaThatNeverEnds(t *testing.T) {
	f := newFake(t, factorytest.WithRecording(recording("joined-mid-move")), factorytest.WithLiveCamera())
	c, _ := fakeClient(t, f)
	ctx := testCtx(t)
	_, err := c.API().GetVideoMedia(ctx, factorytest.LiveCamera, nil)
	if e := mustCode(t, err, CodeNotFound); e.Field != "id" || e.Status != 404 || !strings.Contains(e.Message, "image.jpg") {
		t.Fatalf("%+v", e)
	}
	if _, err := c.API().GetVideoMedia(ctx, LiveSourceID, nil); !HasCode(err, CodeUnknownVideo) {
		t.Fatal(err)
	}

	var closed bool
	endless, _ := funcClient(t, func(r *http.Request) (*http.Response, error) {
		body := &endlessBody{closed: &closed}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"video/mp4"}}, Body: body,
			ContentLength: -1, Request: r}, nil
	})
	_, err = endless.API().GetVideoMedia(ctx, "cam2", nil)
	if e := mustCode(t, err, CodeNotSeekable); e.Status != 200 || e.Field != "id" || !closed {
		t.Fatalf("%+v closed=%v", e, closed)
	}
	closed = false
	mr, err := endless.API().OpenVideoMedia(ctx, "cam2", nil)
	if err != nil || mr.Status != http.StatusOK || mr.ContentLength >= 0 {
		t.Fatalf("%+v %v", mr, err)
	}
	b := make([]byte, 64<<10)
	if _, err := io.ReadFull(mr, b); err != nil || closed {
		t.Fatalf("64 KiB of media that never ends: %v", err)
	}
	if err := mr.Close(); err != nil || !closed {
		t.Fatal(err, closed)
	}
}

// TestFramePicture: the camera picture of a live frame, its frame from X-Interp-G; a recording has none; a reply
// without the header does not decode.
func TestFramePicture(t *testing.T) {
	f := newFake(t, factorytest.WithRecording(recording("joined-mid-move")), factorytest.WithLiveCamera())
	c, _ := fakeClient(t, f)
	ctx := testCtx(t)
	p, err := c.API().GetFramePicture(ctx, LatestFrame(), nil)
	if err != nil || p.G != 21 || !bytes.HasPrefix(p.JPEG, []byte{0xff, 0xd8}) ||
		!bytes.Contains(p.JPEG, []byte("FakeFactory cam-live g 21")) {
		t.Fatalf("%+v %v", p, err)
	}
	session, _, _ := f.SessionState()
	if p, err := c.API().GetFramePicture(ctx, FrameAt(16), &PictureParams{Session: Int64(session)}); err != nil || p.G != 16 ||
		f.Requests()[len(f.Requests())-1].Query.Get("session") != fmt.Sprint(session) {
		t.Fatal(p, err)
	}
	if _, err := c.API().GetFramePicture(ctx, FrameAt(15), nil); !IsNotRetained(err) {
		t.Fatal("a warm-up frame", err)
	}
	if _, err := c.API().GetFramePicture(ctx, LatestFrame(), &PictureParams{Session: Int64(1)}); !IsSessionGone(err) {
		t.Fatal(err)
	}
	rec := newFake(t, factorytest.WithRecording(recording("joined-mid-move")))
	r, _ := fakeClient(t, rec)
	if _, err := r.API().GetFramePicture(ctx, LatestFrame(), nil); !HasCode(err, CodeNotFound) {
		t.Fatal(err)
	}
	bare := newFake(t, factorytest.WithRecording(recording("joined-mid-move")), factorytest.WithReplyMap(
		map[string]factorytest.Reply{"getFramePicture": {Status: 200, ContentType: "image/jpeg", Body: []byte{0xff, 0xd8}}}))
	b, _ := fakeClient(t, bare)
	_, err = b.API().GetFramePicture(ctx, LatestFrame(), nil)
	if e := mustCode(t, err, CodeDecode); e.Status != 200 || e.Message != "the picture reply has no X-Interp-G header" ||
		e.Err == nil {
		t.Fatalf("%+v", e)
	}
}

// endlessBody never ends; Close records that it was closed.
type endlessBody struct{ closed *bool }

func (b *endlessBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	return len(p), nil
}

func (b *endlessBody) Close() error {
	*b.closed = true
	return nil
}
