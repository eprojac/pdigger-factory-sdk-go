package pdiggerfactory

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

func TestSourcesAndLive(t *testing.T) {
	f := newFake(t, factorytest.WithRecording(recording("joined-mid-move")), factorytest.WithLiveCamera())
	c, _ := fakeClient(t, f)
	ctx := testCtx(t)
	// the recordings, then the cameras
	srcs, err := c.Sources(ctx)
	if err != nil || len(srcs) != 2 || srcs[0].ID != "dry1" || srcs[0].IsLive() || srcs[0].Name() != "Dry side · 1" ||
		srcs[1].ID != factorytest.LiveCamera || !srcs[1].IsLive() || srcs[1].Seekable || srcs[1].Kind != SourceKindCamera ||
		srcs[1].Station != "FakeFactory" || srcs[1].Proxy != "" {
		t.Fatalf("%+v %v", srcs, err)
	}
	cameras, err := c.Cameras(ctx)
	if err != nil || len(cameras) != 1 || cameras[0].ID != factorytest.LiveCamera {
		t.Fatalf("%+v %v", cameras, err)
	}
	live, err := c.Live(ctx)
	if err != nil || live.ID != factorytest.LiveCamera || live.Duration != nil || live.Name() != "Live camera" {
		t.Fatalf("%+v %v", live, err)
	}
	dry1, err := c.Source(ctx, "dry1")
	if err != nil || dry1.Duration == nil || *dry1.Duration != 1807.157567 {
		t.Fatalf("%+v %v", dry1, err)
	}
	if _, err := c.Source(ctx, "nope"); !HasCode(err, CodeUnknownVideo) {
		t.Fatal(err)
	}
	if (&Source{ID: "x"}).Name() != "x" {
		t.Fatal("a source without a title is named by its id")
	}
	f.FailNext("listVideos", 3, factorytest.ErrorReply(500, "internal", "x"))
	if _, err := c.Sources(ctx); !HasCode(err, CodeInternal) {
		t.Fatal(err)
	}
	if _, err := c.Cameras(ctx); !HasCode(err, CodeInternal) {
		t.Fatal(err)
	}
	if _, err := c.Live(ctx); !HasCode(err, CodeInternal) {
		t.Fatal(err)
	}
	// several cameras: Live is the first, Cameras keeps the catalog's order (entries without kind, live or seekable
	// take them from the others)
	cat := map[string]any{"default_video": "rec", "videos": []map[string]any{{"id": "rec", "duration": 10.0},
		{"id": "camB", "kind": "camera", "live": true, "seekable": false, "duration": nil},
		{"id": "camA", "kind": "camera", "duration": nil}, {"id": "camC", "live": true}}}
	m, _ := fakeClient(t, newFake(t, factorytest.WithReplyMap(map[string]factorytest.Reply{
		"listVideos": factorytest.JSONReply(200, cat)})))
	cams, err := m.Cameras(ctx)
	if err != nil || len(cams) != 3 || cams[0].ID != "camB" || cams[1].ID != "camA" || cams[2].ID != "camC" ||
		cams[1].Seekable || cams[2].Kind != SourceKindCamera {
		t.Fatalf("%+v %v", cams, err)
	}
	if l, err := m.Live(ctx); err != nil || l.ID != "camB" {
		t.Fatal(l, err)
	}
	// a unit that plays recordings only
	r, _ := fakeClient(t, newFake(t, factorytest.WithRecording(recording("joined-mid-move"))))
	if cams, err := r.Cameras(ctx); err != nil || cams == nil || len(cams) != 0 {
		t.Fatal(cams, err)
	}
	if _, err := r.Live(ctx); !HasCode(err, CodeLiveUnavailable) {
		t.Fatal(err)
	}
}

func TestSessionControls(t *testing.T) {
	f := newFake(t, factorytest.WithRecording(recording("joined-mid-move")), factorytest.WithLiveCamera())
	c, _ := fakeClient(t, f)
	ctx := testCtx(t)
	s := c.Session()
	if s.Known() != nil {
		t.Fatal("known before any read")
	}
	// the fake shows its camera at the start, already live
	st, err := s.State(ctx)
	if err != nil || st.Video != factorytest.LiveCamera || st.Session != 1795314878 || !st.IsLive() ||
		st.LivePhase() != LivePhaseLive || s.Known().Revision != st.Revision {
		t.Fatalf("%+v %v", st, err)
	}
	// a time for a camera is refused before the select: the catalog says it is one (LiveSourceID always is)
	n := len(f.Requests())
	_, err = s.Watch(ctx, factorytest.LiveCamera, Float64(3))
	if e := mustCode(t, err, CodeNotSeekable); e.Field != "time" || e.Status != 0 ||
		e.Message != "cam-live is a live camera: it has no time; leave `at` out" || strings.Join(f.Ops()[n:], ",") != "listVideos" {
		t.Fatal(e, f.Ops()[n:])
	}
	n = len(f.Requests())
	if _, err := s.Watch(ctx, LiveSourceID, Float64(3)); !HasCode(err, CodeNotSeekable) || len(f.Requests()) != n {
		t.Fatal(err)
	}
	// a time for an id the catalog does not list as a camera goes to the unit; the catalog read may fail
	if _, err := s.Watch(ctx, "nope", Float64(3)); !HasCode(err, CodeUnknownVideo) {
		t.Fatal(err)
	}
	f.FailNext("listVideos", 1, factorytest.ErrorReply(400, "invalid_param", "x"))
	if _, err := s.Watch(ctx, "dry1", Float64(3)); !HasCode(err, CodeInvalidParam) {
		t.Fatal(err)
	}
	// a paused session plays first, then selects: a camera is connecting
	if _, err := s.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	n = len(f.Requests())
	st, err = s.Watch(ctx, factorytest.LiveCamera, nil)
	if err != nil || st.Video != factorytest.LiveCamera || st.Rate != 1 || st.Duration != nil || !st.Playing ||
		st.LivePhase() != LivePhaseConnecting {
		t.Fatalf("%+v %v", st, err)
	}
	if ops := strings.Join(f.Ops()[n:], ","); ops != "getSession,sessionPlay,sessionSelect" {
		t.Fatal(ops)
	}
	for _, r := range f.Requests()[n:] {
		if r.Op == "sessionSelect" && strings.Contains(string(r.Body), "time") {
			t.Fatalf("a time was sent for the camera: %s", r.Body)
		}
	}
	// a live session is not seeked: Seek reads the session and refuses locally, without sending the seek
	n = len(f.Requests())
	_, err = s.Seek(ctx, 10)
	if e := mustCode(t, err, CodeNotSeekable); e.Field != "video" || strings.Join(f.Ops()[n:], ",") != "getSession" {
		t.Fatal(e, f.Ops()[n:])
	}
	// another viewer switches the shared session to a recording: this client's next seek is sent, though the last
	// state it saw was live
	other, _ := fakeClient(t, f)
	if _, err := other.Session().Watch(ctx, "dry1", Float64(80)); err != nil {
		t.Fatal(err)
	}
	if !s.Known().IsLive() {
		t.Fatal("the client saw the switch")
	}
	n = len(f.Requests())
	if st, err := s.Seek(ctx, 81); err != nil || st.AnchorMedia != 81 ||
		strings.Join(f.Ops()[n:], ",") != "getSession,sessionSeek" {
		t.Fatal(err, f.Ops()[n:])
	}
	// and the other way round: a session switched to a camera by another viewer is not seeked
	if _, err := other.Session().Watch(ctx, factorytest.LiveCamera, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Seek(ctx, 82); !HasCode(err, CodeNotSeekable) || s.Known().Video != factorytest.LiveCamera {
		t.Fatal(err)
	}
	// back to dry1 at a time, then seek; a dry run leaves Known alone
	st, err = s.Watch(ctx, "dry1", Float64(80))
	if err != nil || st.Video != "dry1" || st.AnchorMedia != 80 {
		t.Fatalf("%+v %v", st, err)
	}
	known := s.Known()
	if _, err := s.Seek(ctx, 81, DryRun()); err != nil || s.Known().Revision != known.Revision {
		t.Fatal(err)
	}
	st, err = s.Seek(ctx, 82)
	if err != nil || st.AnchorMedia != 82 || s.Known().AnchorMedia != 82 {
		t.Fatalf("%+v %v", st, err)
	}
	if st, err = s.Play(ctx); err != nil || !st.Playing {
		t.Fatal(err)
	}
	if st, err = s.Pause(ctx, DryRun()); err != nil || st.Playing || !s.Known().Playing {
		t.Fatal(err)
	}
	frame, err := c.API().GetFrame(ctx, LatestFrame(), &FrameParams{Layers: []string{LayerWorld}})
	if err != nil || len(frame.World.Stones) == 0 {
		t.Fatal(err)
	}
	id := frame.World.Stones[0].ID
	if st, err = s.Focus(ctx, id); err != nil || st.Focus == nil || st.Focus.ID != id {
		t.Fatalf("%+v %v", st, err)
	}
	if st, err = s.ClearFocus(ctx); err != nil || st.Focus != nil {
		t.Fatal(err)
	}
	// a fresh client: Seek reads the state, then seeks
	c2, _ := fakeClient(t, f)
	n = len(f.Requests())
	if _, err := c2.Session().Seek(ctx, 83); err != nil || strings.Join(f.Ops()[n:], ",") != "getSession,sessionSeek" {
		t.Fatal(err, f.Ops()[n:])
	}
}

func TestSessionControlErrors(t *testing.T) {
	f := newFake(t, factorytest.WithRecording(recording("joined-mid-move")))
	c, _ := fakeClient(t, f)
	ctx := testCtx(t)
	s := c.Session()
	fail := func(op string) { f.FailNext(op, 1, factorytest.ErrorReply(400, "invalid_param", op)) }
	for op, call := range map[string]func() error{
		"getSession": func() error { _, err := s.State(ctx); return err },
		"sessionPause": func() error {
			_, err := s.Pause(ctx)
			return err
		},
		"sessionPlay":   func() error { _, err := s.Play(ctx); return err },
		"putFocus":      func() error { _, err := s.Focus(ctx, 1); return err },
		"deleteFocus":   func() error { _, err := s.ClearFocus(ctx); return err },
		"sessionSelect": func() error { _, err := s.Watch(ctx, "dry1", nil); return err },
		"sessionSeek":   func() error { _, err := s.Seek(ctx, 1); return err },
	} {
		fail(op)
		if err := call(); !HasCode(err, CodeInvalidParam) {
			t.Errorf("%s: %v", op, err)
		}
	}
	// Watch: the session read fails; the play of a paused session fails
	fail("getSession")
	if _, err := s.Watch(ctx, "dry1", nil); !HasCode(err, CodeInvalidParam) {
		t.Fatal(err)
	}
	if _, err := s.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	fail("sessionPlay")
	if _, err := s.Watch(ctx, "dry1", nil); !HasCode(err, CodeInvalidParam) {
		t.Fatal(err)
	}
	// Seek: the read of the state fails
	c2, _ := fakeClient(t, f)
	fail("getSession")
	if _, err := c2.Session().Seek(ctx, 1); !HasCode(err, CodeInvalidParam) {
		t.Fatal(err)
	}
	fail("getSession")
	if _, err := c2.Session().Clock(ctx); !HasCode(err, CodeInvalidParam) {
		t.Fatal(err)
	}
}

func TestSessionClock(t *testing.T) {
	f := newFake(t, factorytest.WithReplies(fixturesAPI))
	c, clk := fakeClient(t, f)
	ctx := testCtx(t)
	// put the local clock 10 s behind the server's
	sess, _ := c.API().GetSession(ctx)
	start := clk.Now()
	sc, err := c.Session().Clock(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sc.RTT != 0 || sc.IsLive() || sc.State.Session != sess.State.Session {
		t.Fatalf("%+v", sc)
	}
	if want := sess.ServerRecv - epoch(start); sc.Offset != want {
		t.Fatalf("offset %v, want %v", sc.Offset, want)
	}
	if st := c.Session().Known(); st == nil || st.Session != sess.State.Session {
		t.Fatal("Clock remembers the state")
	}
	// the fixture: playing at 0.75 from media 586 after the hold (hold_until 2.2 s after anchor_wall)
	st := sc.State
	if got := sc.MediaTimeAt(st.HoldUntil + 10); !near(got, 593.5) {
		t.Fatalf("MediaTimeAt %v", got)
	}
	if got := sc.MediaTimeAt(st.AnchorWall + 1); got != 586 {
		t.Fatalf("during the hold %v", got)
	}
	if got := sc.MediaTimeAt(st.HoldUntil + 1e6); got != *st.Duration {
		t.Fatalf("clamped %v", got)
	}
	if got := sc.MediaTimeNow(); got != sc.MediaTimeAt(sc.ServerTime()) {
		t.Fatal("MediaTimeNow")
	}
	if w := sc.WallOf(593.5); w == nil || !near(*w, st.HoldUntil+10) {
		t.Fatalf("WallOf %v", w)
	}
	if w := sc.WallOf(585); w == nil || !near(*w, st.HoldUntil-1/0.75) {
		t.Fatalf("WallOf before the anchor %v", w)
	}
	if lw := sc.LocalWallOf(593.5); lw == nil || !near(*lw, st.HoldUntil+10-sc.Offset) {
		t.Fatal("LocalWallOf")
	}
	if p := sc.Progress(); p == nil || *p != sc.MediaTimeNow()/(*st.Duration) {
		t.Fatal("Progress")
	}
	// paused, no rate, no duration, a hold already over
	paused := NewSessionClock(&SessionReply{State: State{Playing: false, AnchorMedia: 5, Video: LiveSourceID}},
		start, start.Add(time.Second), nil)
	if paused.WallOf(5) != nil || paused.LocalWallOf(5) != nil || paused.Progress() != nil || !paused.IsLive() ||
		paused.MediaTimeAt(1e12) != 5 || paused.RTT != time.Second {
		t.Fatal("paused clock")
	}
	still := NewSessionClock(&SessionReply{State: State{Playing: true}}, start, start, clk)
	if still.WallOf(1) != nil {
		t.Fatal("no rate")
	}
	zero := NewSessionClock(&SessionReply{State: State{Playing: true, Rate: 1, AnchorWall: 100, HoldUntil: 50,
		Duration: Float64(0)}}, start, start, clk)
	if zero.Progress() != nil || zero.MediaTimeAt(110) != 0 {
		t.Fatal("zero duration")
	}
	if w := zero.WallOf(3); w == nil || *w != 103 {
		t.Fatal("hold over")
	}
}

// TestSessionClockSample: a clock sample is one request timed on its own; a failed one is measured again, so the
// round trip never includes the wait before the retry.
func TestSessionClockSample(t *testing.T) {
	f := newFake(t, factorytest.WithReplies(fixturesAPI))
	c, clk := fakeClient(t, f)
	ctx := testCtx(t)
	busy := factorytest.Reply{Status: 503, ContentType: "application/json", Headers: map[string]string{"Retry-After": "2"},
		Body: []byte(`{"error":"busy","code":"busy"}`)}
	f.FailNext("getSession", 1, busy)
	sc, err := c.Session().Clock(ctx)
	if err != nil || sc.RTT != 0 || len(clk.Sleeps()) != 1 || clk.Sleeps()[0] != 2*time.Second {
		t.Fatal(err, clk.Sleeps())
	}
	// retry_ms, else the backoff; the retries used up
	f.FailNext("getSession", 1, factorytest.JSONReply(503, map[string]any{"error": "busy", "code": "busy", "retry_ms": 30}))
	f.FailNext("getSession", 3, factorytest.ErrorReply(503, "busy", "busy"))
	if _, err := c.Session().Clock(ctx); !HasCode(err, CodeBusy) || clk.Sleeps()[1] != 30*time.Millisecond ||
		len(clk.Sleeps()) != 4 {
		t.Fatal(err, clk.Sleeps())
	}
	// a wait that ends with the context
	fc, _ := fakeClient(t, f, WithClock(&failClock{}))
	f.FailNext("getSession", 1, busy)
	if _, err := fc.Session().Clock(ctx); !HasCode(err, CodeTransport) {
		t.Fatal(err)
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// TestSessionControlOptions: InVideo sends a seek for a named video without reading the state first; PickedAt and
// InSession pick a stone as drawn in a frame and guard it with the session it belongs to.
func TestSessionControlOptions(t *testing.T) {
	f := newFake(t, factorytest.WithRecording(recording("joined-mid-move")), factorytest.WithLiveCamera())
	c, _ := fakeClient(t, f)
	ctx := testCtx(t)
	s := c.Session()
	if _, err := s.Watch(ctx, "dry1", Float64(80)); err != nil {
		t.Fatal(err)
	}
	// a seek in a named video: one request, the video in its body
	n := len(f.Requests())
	st, err := s.Seek(ctx, 81, InVideo("dry1"))
	if err != nil || st.Video != "dry1" || st.AnchorMedia != 81 || strings.Join(f.Ops()[n:], ",") != "sessionSeek" ||
		!strings.Contains(string(f.Requests()[n].Body), `"video":"dry1"`) {
		t.Fatal(st, err, f.Ops()[n:])
	}
	// InVideo("") is no video: the state is read first
	n = len(f.Requests())
	if _, err := s.Seek(ctx, 82, InVideo("")); err != nil || strings.Join(f.Ops()[n:], ",") != "getSession,sessionSeek" {
		t.Fatal(err, f.Ops()[n:])
	}
	// the live camera named: refused locally, nothing sent
	n = len(f.Requests())
	if _, err := s.Seek(ctx, 1, InVideo(LiveSourceID)); !HasCode(err, CodeNotSeekable) || len(f.Requests()) != n {
		t.Fatal(err)
	}
	// a camera named: sent, and the unit refuses it
	_, err = s.Seek(ctx, 1, InVideo(factorytest.LiveCamera))
	if e := mustCode(t, err, CodeNotSeekable); e.Status != 409 || e.Field != "video" {
		t.Fatal(e)
	}
	// another viewer switched to the camera: a seek naming a recording is applied, as on the unit (it moves every
	// viewer off the camera), while one without InVideo reads the state and is refused locally
	other, _ := fakeClient(t, f)
	if _, err := other.Session().Watch(ctx, factorytest.LiveCamera, nil); err != nil {
		t.Fatal(err)
	}
	n = len(f.Requests())
	if _, err := s.Seek(ctx, 83); !HasCode(err, CodeNotSeekable) || strings.Join(f.Ops()[n:], ",") != "getSession" {
		t.Fatal(err, f.Ops()[n:])
	}
	if st, err := s.Seek(ctx, 83, InVideo("dry1")); err != nil || st.Video != "dry1" || st.Live != nil || st.AnchorMedia != 83 {
		t.Fatalf("%+v %v", st, err)
	}
	// a stone picked in a frame, guarded by its session; a later select makes the guard refuse
	if _, err := s.Watch(ctx, "dry1", Float64(80)); err != nil {
		t.Fatal(err)
	}
	frame, err := c.API().GetFrame(ctx, LatestFrame(), &FrameParams{Layers: []string{LayerWorld}})
	if err != nil || len(frame.World.Stones) == 0 {
		t.Fatal(err)
	}
	id := frame.World.Stones[0].ID
	n = len(f.Requests())
	st, err = s.Focus(ctx, id, PickedAt(frame.G), InSession(frame.Session), DryRun())
	if err != nil || st.Focus == nil || st.Focus.ID != id || st.Focus.G != frame.G {
		t.Fatalf("%+v %v", st, err)
	}
	if body := string(f.Requests()[n].Body); !strings.Contains(body, fmt.Sprintf(`"g":%d`, frame.G)) ||
		!strings.Contains(body, fmt.Sprintf(`"session":%d`, frame.Session)) {
		t.Fatal(body)
	}
	if _, err := other.Session().Seek(ctx, 85); err != nil {
		t.Fatal(err)
	}
	_, err = s.Focus(ctx, id, PickedAt(frame.G), InSession(frame.Session))
	if e := mustCode(t, err, CodeStaleSession); e.Status != 409 || e.Field != "session" {
		t.Fatal(e)
	}
	// the options of other controls are ignored
	if _, err := s.Pause(ctx, InVideo("dry1"), PickedAt(1), InSession(1), DryRun()); err != nil {
		t.Fatal(err)
	}
}

// failingSleep is a manual clock whose Sleep fails.
type failingSleep struct{ *factorytest.Clock }

func (failingSleep) Sleep(context.Context, time.Duration) error { return context.Canceled }

// TestUntilLiveAndPicture: UntilLive waits out a camera's connection and warm-up on the client's clock (the fake
// follows it), returns at once on a recording and gives up after its timeout; Picture is the JPEG of a live frame.
func TestUntilLiveAndPicture(t *testing.T) {
	clk := factorytest.NewClock(time.Time{})
	f := newFake(t, factorytest.WithRecording(recording("joined-mid-move")), factorytest.WithLiveCamera(),
		factorytest.WithClock(clk))
	c, err := New(f.URL(), WithTransport(f.Transport()), WithClock(clk))
	if err != nil {
		t.Fatal(err)
	}
	ctx := testCtx(t)
	s := c.Session()
	if _, err := s.Watch(ctx, factorytest.LiveCamera, nil); err != nil {
		t.Fatal(err)
	}
	n := len(f.Requests())
	st, err := s.UntilLive(ctx, 30*time.Second)
	if err != nil || st.LivePhase() != LivePhaseLive || st.Live.Lag != factorytest.LiveLagSeconds || st.Live.Warmup != 3 {
		t.Fatalf("%+v %v", st, err)
	}
	// connecting for 0.5 s, then warming until 0.25 s after the first frame after the warm-up (t 3.69): the state
	// read every 250 ms on the client's clock, and nothing else sent
	if sleeps := clk.Sleeps(); len(sleeps) != 16 || sleeps[0] != UntilLivePoll ||
		strings.Trim(strings.ReplaceAll(strings.Join(f.Ops()[n:], ","), "getSession", ""), ",") != "" {
		t.Fatal(sleeps, f.Ops()[n:])
	}
	p, err := s.Picture(ctx, LatestFrame())
	if err != nil || p.G != 21 || !strings.Contains(string(p.JPEG), "FakeFactory cam-live g 21") {
		t.Fatalf("%+v %v", p, err)
	}
	if p, err := s.Picture(ctx, FrameAt(16), InSession(st.Session)); err != nil || p.G != 16 {
		t.Fatal(p, err)
	}
	if _, err := s.Picture(ctx, LatestFrame(), InSession(st.Session-1)); !IsSessionGone(err) {
		t.Fatal(err)
	}
	// a recording: the state at once, no picture
	if _, err := s.Watch(ctx, "dry1", nil); err != nil {
		t.Fatal(err)
	}
	n = len(clk.Sleeps())
	if st, err := s.UntilLive(ctx, time.Second); err != nil || st.Live != nil || st.LivePhase() != "" || len(clk.Sleeps()) != n {
		t.Fatal(st, err)
	}
	if _, err := s.Picture(ctx, LatestFrame()); !HasCode(err, CodeNotFound) {
		t.Fatal(err)
	}
	// a paused camera does not go live: live_timeout, with the phase it saw last
	if _, err := s.Watch(ctx, factorytest.LiveCamera, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = s.UntilLive(ctx, 2*time.Second)
	if e := mustCode(t, err, CodeLiveTimeout); e.Message != "cam-live is still connecting after 2 s" || e.Status != 0 {
		t.Fatal(e)
	}
	_, err = s.UntilLive(ctx, 500*time.Millisecond)
	if e := mustCode(t, err, CodeLiveTimeout); e.Message != "cam-live is still connecting after 0.5 s" {
		t.Fatal(e)
	}
	// the last wait is cut short to end at the timeout, on the client's clock
	n = len(clk.Sleeps())
	_, err = s.UntilLive(ctx, 600*time.Millisecond)
	if e := mustCode(t, err, CodeLiveTimeout); e.Message != "cam-live is still connecting after 0.6 s" ||
		fmt.Sprint(clk.Sleeps()[n:]) != "[250ms 250ms 100ms]" {
		t.Fatal(e, clk.Sleeps()[n:])
	}
	// the message names the phase seen last
	if _, err := s.Watch(ctx, factorytest.LiveCamera, nil); err != nil {
		t.Fatal(err)
	}
	clk.Advance(time.Second)
	if _, err := s.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	_, err = s.UntilLive(ctx, time.Second)
	if e := mustCode(t, err, CodeLiveTimeout); e.Message != "cam-live is still warming after 1 s" {
		t.Fatal(e)
	}
	// play of a live camera: connecting in the reply, then a new warm-up, and the frames after it jump in G
	if _, err := s.Play(ctx); err != nil {
		t.Fatal(err)
	}
	if st, err := s.UntilLive(ctx, 30*time.Second); err != nil || st.LivePhase() != LivePhaseLive {
		t.Fatal(st, err)
	}
	before, err := s.Picture(ctx, LatestFrame())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	played, err := s.Play(ctx)
	if err != nil || played.LivePhase() != LivePhaseConnecting {
		t.Fatal(played, err)
	}
	if st, err := s.UntilLive(ctx, 30*time.Second); err != nil || st.LivePhase() != LivePhaseLive {
		t.Fatal(st, err)
	}
	if after, err := s.Picture(ctx, LatestFrame()); err != nil || after.G <= before.G+1 {
		t.Fatal(before.G, after, err)
	}
	// the state read fails; the sleep fails (on a paused camera that is connecting)
	if _, err := s.Watch(ctx, factorytest.LiveCamera, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	f.FailNext("getSession", 1, factorytest.ErrorReply(400, "invalid_param", "x"))
	if _, err := s.UntilLive(ctx, time.Second); !HasCode(err, CodeInvalidParam) {
		t.Fatal(err)
	}
	broken, err := New(f.URL(), WithTransport(f.Transport()), WithClock(failingSleep{clk}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = broken.Session().UntilLive(ctx, time.Second)
	if e := mustCode(t, err, CodeTransport); e.Op != "untilLive" || !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}

// TestSessionClockWallOf: the wall time of a media time maps from the end of the hold while playing and back from
// the pause while paused; nil only without a rate, as in the other languages.
func TestSessionClockWallOf(t *testing.T) {
	start := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	st := State{Playing: true, Rate: 0.8, AnchorMedia: 100, AnchorWall: 1000, HoldUntil: 1002}
	clock := func(st State) *SessionClock { return NewSessionClock(&SessionReply{State: st}, start, start, nil) }
	if w := clock(st).WallOf(108); w == nil || !near(*w, 1012) {
		t.Fatal(w)
	}
	if w := clock(st).WallOf(96); w == nil || !near(*w, 997) {
		t.Fatal("before the anchor", w)
	}
	st.Playing = false
	if w := clock(st).WallOf(96); w == nil || !near(*w, 995) {
		t.Fatal("paused", w)
	}
	for _, rate := range []float64{0, -1, math.NaN()} {
		st.Rate = rate
		if clock(st).WallOf(96) != nil || clock(st).LocalWallOf(96) != nil {
			t.Fatal("rate", rate)
		}
	}
}

// TestDryRunsMoveNothing: every control with DryRun() is sent as a dry run (Watch's play first too) and leaves the
// shared session as it was: the integration test sends exactly these to the shared unit.
func TestDryRunsMoveNothing(t *testing.T) {
	f := newFake(t, factorytest.WithRecording(recording("joined-mid-move")))
	c, _ := fakeClient(t, f)
	ctx := testCtx(t)
	s := c.Session()
	frame, err := c.API().GetFrame(ctx, LatestFrame(), &FrameParams{Layers: []string{LayerWorld}})
	if err != nil || len(frame.World.Stones) == 0 {
		t.Fatal(err)
	}
	id := frame.World.Stones[0].ID
	if _, err := s.Focus(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pause(ctx); err != nil {
		t.Fatal(err)
	}
	before, err := s.State(ctx)
	if err != nil || before.Focus == nil || before.Playing {
		t.Fatal(before, err)
	}
	n := len(f.Requests())
	for i, step := range []func() error{
		func() error { _, err := s.Watch(ctx, "dry1", nil, DryRun()); return err },
		func() error { _, err := s.Play(ctx, DryRun()); return err },
		func() error { _, err := s.Pause(ctx, DryRun()); return err },
		func() error { _, err := s.Focus(ctx, id, DryRun()); return err },
		func() error { _, err := s.ClearFocus(ctx, DryRun()); return err },
		func() error { _, err := s.Seek(ctx, 5, DryRun()); return err },
	} {
		if err := step(); err != nil {
			t.Fatal(i, err)
		}
	}
	controls := 0
	for _, r := range f.Requests()[n:] {
		if r.Method == "GET" {
			continue
		}
		controls++
		if r.Query.Get("dry_run") != "1" {
			t.Fatal(r.Op, "sent without dry_run")
		}
	}
	after, err := s.State(ctx)
	if err != nil || controls != 7 || after.Session != before.Session || after.Playing || after.Revision != before.Revision ||
		after.Focus == nil || after.Focus.ID != id {
		t.Fatalf("%d controls: %+v %+v %v", controls, before, after, err)
	}
}
