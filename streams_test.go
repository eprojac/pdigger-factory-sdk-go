package pdiggerfactory

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// fline is a recording frame line, with every field a frame must have (2.3 rule 9), a world of revision rev (rev < 0:
// no world) and extra JSON members (a member named again, such as "world", is checked by its last copy, but the
// model decodes both copies into one: a struct merges them).
func fline(cursor, session, g, rev int64, extra string) string {
	world := ""
	if rev >= 0 {
		world = `,"world":` + worldJSON(session, g, fmt.Sprintf(`,"rev":%d`, rev), "", "")
	}
	return fmt.Sprintf(`{"cursor":%d,"session":%d,"video":"v","g":%d,"i":%d,"t":%g,"past_valid":true,`+
		`"dt_past":0.6%s%s}`, cursor, session, g, 2*g, float64(g)/5, world, extra)
}

// worldJSON is a world layer of grid frame g with every field a world must have: its stones and events are the
// JSON objects given, and extra holds more members. Its motion is "moving", as a test frame's was when it had none,
// so no tray arrives.
func worldJSON(session, g int64, extra, stones, events string) string {
	return fmt.Sprintf(`{"schema":"stw1","session":%d,"video":"v","g":%d,"t":%g,"motion":"moving","logF":0%s,`+
		`"stones":[%s],"hands":{},"events":[%s],"stats":{}}`, session, g, float64(g)/5, extra, stones, events)
}

// rawJSON is a sip1 packet of grid frame g with every field a packet must have, and this cursor.
func rawJSON(g, cursor int64) string {
	return fmt.Sprintf(`{"schema":"sip1","maps":{},"depth":{},"boxes":{},"person":{},"stones":{},"tray":{},`+
		`"hands":{},"both":0,"session":1,"video":"v","g":%d,"i":%d,"t":%g,"past_valid":true,"dt_past":0.6,"ms":{},`+
		`"cursor":%d}`, g, 2*g, float64(g)/5, cursor)
}

const clockLine = `{"sse":"clock","data":{"session":1,"revision":3,"video":"v","playing":true,"rate":0.8,` +
	`"anchor_media":0,"anchor_wall":100,"hold_until":100,"duration":null,"changed_by":"start","focus":null,` +
	`"media_time":0,"server_now":100}}`

const workerLine = `{"sse":"worker","data":{"state":"running","reason":"","session":1,"video":"v"}}`

func linesFake(t *testing.T, lines ...string) *factorytest.Fake {
	t.Helper()
	var bs [][]byte
	for _, l := range lines {
		bs = append(bs, []byte(l))
	}
	return newFake(t, factorytest.WithLines(bs...))
}

// kinds describes stream items: f<g>[r][b] for frames (revision, backfill), and the other kinds by name.
func kinds(items []StreamItem) string {
	var s []string
	for _, it := range items {
		switch v := it.(type) {
		case *FrameItem:
			k := fmt.Sprintf("f%d", v.Frame.G)
			if v.IsRevision {
				k += "r"
			}
			if v.IsBackfill {
				k += "b"
			}
			s = append(s, k)
		case *PacketItem:
			s = append(s, fmt.Sprintf("p%d", v.Cursor))
		default:
			s = append(s, it.Kind())
		}
	}
	return strings.Join(s, ",")
}

func TestFramesStream(t *testing.T) {
	f := linesFake(t, clockLine, workerLine, fline(1, 1, 1, 0, ""), fline(2, 1, 2, 0, ""), fline(3, 1, 1, 1, ""),
		fline(4, 1, 1, 1, ""), fline(5, 1, 2, 0, ""), fline(6, 1, 3, -1, ""))
	c, _ := fakeClient(t, f, WithObserve(true))
	ctx := testCtx(t)
	items := collect(t, c.Streams().Frames(ctx, Precision("full"), After(0)), 6)
	if got := kinds(items); got != "clock,worker,f1,f2,f1r,f3" {
		t.Fatal(got)
	}
	fi := items[2].(*FrameItem)
	if fi.ID != "fakeboot01-1" || fi.Cursor != 1 || fi.Frame.World == nil {
		t.Fatalf("%+v", fi)
	}
	ck := items[0].(*ClockItem)
	if ck.State.Revision != 3 || ck.ServerNow != 100 || ck.clockEvent().Rate != 0.8 {
		t.Fatalf("%+v", ck)
	}
	if items[1].(*WorkerItem).Worker.State != "running" {
		t.Fatal("worker")
	}
	q := f.Requests()[0].Query
	if q.Get("observe") != "1" || q.Get("precision") != "full" || q.Get("after") != "0" || q.Get("format") != "sse" {
		t.Fatal(q)
	}
	if (&PacketItem{}).Kind() != StreamEventFrame || (&GapItem{}).Kind() != StreamEventGap || (&ResetItem{}).Kind() != StreamEventReset {
		t.Fatal("kinds")
	}
}

func TestFramesResume(t *testing.T) {
	lines := []string{fline(1, 1, 1, 0, ""), fline(2, 1, 2, 0, ""), fline(3, 1, 3, 0, ""), fline(4, 1, 4, 0, ""),
		fline(5, 1, 5, 0, ""), fline(6, 1, 6, 0, "")}
	ctx := testCtx(t)
	// a dropped connection: reconnect with Last-Event-ID, after a backoff, and tell OnReconnect
	f := linesFake(t, lines...)
	f.DropAfter("streamFrames", 2)
	c, clk := fakeClient(t, f)
	var calls []string
	items := collect(t, c.Streams().Frames(ctx, Layers(LayerWorld), OnReconnect(func(n int, err error) {
		calls = append(calls, fmt.Sprintf("%d:%v", n, AsError(err).Code))
	})), 6)
	if got := kinds(items); got != "f1,f2,f3,f4,f5,f6" {
		t.Fatal(got)
	}
	reqs := f.Requests()
	if len(reqs) != 2 || reqs[1].Header.Get("Last-Event-ID") != "fakeboot01-2" || reqs[1].Query.Get("layers") != "world" {
		t.Fatalf("%d requests %v", len(reqs), reqs[1].Header)
	}
	if strings.Join(calls, " ") != "1:transport" || len(clk.Sleeps()) != 1 {
		t.Fatal(calls, clk.Sleeps())
	}
	// the server ends each connection after 2 frames: reconnect at once
	f = linesFake(t, lines...)
	c, clk = fakeClient(t, f)
	if got := kinds(collect(t, c.Streams().Frames(ctx, ConnectionLimit(2), LastEventID("fakeboot01-1")), 5)); got != "f2,f3,f4,f5,f6" {
		t.Fatal(got)
	}
	if reqs := f.Requests(); len(reqs) != 3 || reqs[0].Query.Get("limit") != "2" || reqs[0].Header.Get("Last-Event-ID") != "fakeboot01-1" {
		t.Fatal(len(reqs))
	}
	if len(clk.Sleeps()) != 0 {
		t.Fatal(clk.Sleeps())
	}
	// max_s: the fake counts five frames a second
	f = linesFake(t, lines...)
	c, _ = fakeClient(t, f)
	collect(t, c.Streams().Frames(ctx, ConnectionMaxSeconds(1)), 6)
	if reqs := f.Requests(); len(reqs) != 2 || reqs[0].Query.Get("max_s") != "1" {
		t.Fatal(len(reqs))
	}
	// without resume, the stream ends with the connection
	f = linesFake(t, lines...)
	c, _ = fakeClient(t, f)
	if got := kinds(collect(t, c.Streams().Frames(ctx, ConnectionLimit(2), Resume(false)), 0)); got != "f1,f2" {
		t.Fatal(got)
	}
	f.DropAfter("streamFrames", 1)
	var errs []error
	for _, err := range c.Streams().Frames(ctx, Resume(false)) {
		errs = append(errs, err)
	}
	if len(errs) != 2 || errs[0] != nil || !HasCode(errs[1], CodeTransport) {
		t.Fatal(errs)
	}
}

func TestFramesErrors(t *testing.T) {
	ctx := testCtx(t)
	f := linesFake(t, fline(1, 1, 1, 0, ""), fline(2, 1, 2, 0, `,"cards":5`), fline(3, 1, 3, 0, ""))
	c, _ := fakeClient(t, f)
	// a non-retryable error ends the stream
	var errs []error
	for _, err := range c.Streams().Frames(ctx, Layers("depth")) {
		errs = append(errs, err)
	}
	if len(errs) != 1 || !HasCode(errs[0], CodeUnknownLayer) {
		t.Fatal(errs)
	}
	// an event that does not decode is an error; the stream goes on
	var got []string
	for it, err := range c.Streams().Frames(ctx, Layers(LayerWorld, LayerCards)) {
		if err != nil {
			got = append(got, string(AsError(err).Code))
		} else {
			got = append(got, kinds([]StreamItem{it}))
		}
		if len(got) == 3 {
			break
		}
	}
	if strings.Join(got, ",") != "f1,decode,f3" {
		t.Fatal(got)
	}
	// the reconnect budget: three failures in a row with MaxReconnects(2)
	f.FailNext("streamFrames", 5, factorytest.JSONReply(503, map[string]any{"error": "busy", "code": "busy", "retry_ms": 40}))
	var fails []int
	errs = nil
	for _, err := range c.Streams().Frames(ctx, MaxReconnects(2), OnReconnect(func(n int, _ error) { fails = append(fails, n) })) {
		errs = append(errs, err)
	}
	if len(errs) != 1 || !HasCode(errs[0], CodeStreamClosed) || len(fails) != 2 || !errors.Is(errs[0], AsError(errs[0]).Err) {
		t.Fatal(errs, fails)
	}
	// a server that answers with nothing: back off between empty connections
	n := 0
	ec, clk := funcClient(t, func(r *http.Request) (*http.Response, error) {
		n++
		body := ""
		switch n {
		case 1, 2:
		case 3:
			body = "event: hello\ndata: {}\n\nevent: frame\ndata: " + fline(9, 1, 9, -1, "") + "\n\n"
		default: // an unnamed event ("message") is no frame, as in the other languages
			body = "data: " + fline(11, 1, 11, -1, "") + "\n\nevent: frame\ndata: " + fline(10, 1, 10, -1, "") + "\n\n"
		}
		return reply(200, body, "Content-Type", "text/event-stream"), nil
	})
	if got := kinds(collect(t, ec.Streams().Frames(ctx), 2)); got != "f9,f10" || len(clk.Sleeps()) != 2 {
		t.Fatal(got, clk.Sleeps())
	}
	// a failing backoff sleep ends the stream quietly
	n = 0
	ec, _ = funcClient(t, func(r *http.Request) (*http.Response, error) {
		return reply(200, "", "Content-Type", "text/event-stream"), nil
	}, WithClock(&failClock{}))
	if items := collect(t, ec.Streams().Frames(ctx), 0); len(items) != 0 {
		t.Fatal(items)
	}
	ec, _ = funcClient(t, func(r *http.Request) (*http.Response, error) { return nil, errBoom }, WithClock(&failClock{}))
	if items := collect(t, ec.Streams().Frames(ctx), 0); len(items) != 0 {
		t.Fatal(items)
	}
}

func TestFramesSideEventsAndCancel(t *testing.T) {
	ctx := testCtx(t)
	f := linesFake(t, fline(1, 1, 1, 0, ""), fline(2, 1, 2, 0, ""))
	f.InjectGap(5)
	f.InjectReset()
	c, _ := fakeClient(t, f)
	items := collect(t, c.Streams().Frames(ctx), 4)
	if got := kinds(items); got != "gap,reset,f1,f2" || items[0].(*GapItem).Count != 5 || items[1].(*ResetItem).Boot != "fakeboot01" {
		t.Fatal(got)
	}
	// a Last-Event-ID of another boot gets a reset, then the program from its start
	if got := kinds(collect(t, c.Streams().Frames(ctx, LastEventID("otherboot-7")), 3)); got != "reset,f1,f2" {
		t.Fatal(got)
	}
	// side events that do not decode
	for _, body := range []string{"event: clock\ndata: 5\n\n", "event: worker\ndata: 5\n\n", "event: gap\ndata: \"x\"\n\n",
		"event: reset\ndata: []\n\n"} {
		ec, _ := funcClient(t, func(r *http.Request) (*http.Response, error) {
			return reply(200, body, "Content-Type", "text/event-stream"), nil
		})
		for _, err := range ec.Streams().Frames(ctx) {
			mustCode(t, err, CodeDecode)
			break
		}
	}
	// cancelling the context ends the stream with a cancelled transport error
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var last error
	n := 0
	for _, err := range c.Streams().Frames(cctx) {
		n++
		if n == 2 {
			cancel()
		}
		if err != nil {
			last = err
		}
	}
	if e := mustCode(t, last, CodeTransport); e.IsRetryable() || !errors.Is(last, context.Canceled) {
		t.Fatal(last)
	}
}

func TestFramesBackfill(t *testing.T) {
	ctx := testCtx(t)
	// g 2 is released without its world; the world comes in a later revision that getFrame returns
	f := linesFake(t, fline(1, 1, 1, 0, ""), fline(2, 1, 2, -1, `,"missing":["world","depth"]`),
		fline(3, 1, 3, 0, ""), fline(4, 1, 2, 1, ""), fline(5, 1, 4, 0, `,"missing":["world"]`), fline(6, 1, 5, 0, ""))
	c, clk := fakeClient(t, f)
	items := collect(t, c.Streams().Frames(ctx, Backfill(true)), 5)
	// (the server lists only layers asked for as missing: depth is not one)
	if got := kinds(items); got != "f1,f2b,f3,f4b,f5" {
		t.Fatal(got)
	}
	f2 := items[1].(*FrameItem).Frame
	if f2.World == nil || f2.World.Rev != 1 || len(f2.Missing) != 0 {
		t.Fatalf("%+v", f2)
	}
	if len(clk.Sleeps()) == 0 {
		t.Fatal("the backfill waits")
	}
	// a frame that is gone (404) is released as it is; other errors are tried three times
	f = linesFake(t, fline(1, 1, 1, -1, `,"missing":["world"]`), fline(2, 1, 2, -1, `,"missing":["world"]`),
		fline(3, 1, 3, 0, ""))
	f.FailNext("getFrame", 1, factorytest.ErrorReply(404, "not_retained", "gone"))
	f.FailNext("getFrame", 3, factorytest.ErrorReply(500, "internal", "x"))
	c, _ = fakeClient(t, f)
	if got := kinds(collect(t, c.Streams().Frames(ctx, Backfill(true)), 3)); got != "f1,f2,f3" {
		t.Fatal(got)
	}
	// a stream stopped while frames wait for their backfill
	f = linesFake(t, fline(1, 1, 1, -1, `,"missing":["world"]`), fline(2, 1, 2, 0, ""))
	asked := make(chan struct{}, 1)
	f.SetDelay(func(op string, r *http.Request) {
		if op == "getFrame" {
			asked <- struct{}{}
			<-r.Context().Done()
		}
	})
	c, _ = fakeClient(t, f)
	cctx, cancel := context.WithCancel(ctx)
	go func() {
		<-asked
		cancel()
	}()
	for _, err := range c.Streams().Frames(cctx, Backfill(true)) {
		if err == nil {
			t.Fatal("a held frame was released")
		}
	}
}

func TestMergeLayers(t *testing.T) {
	dst := &Frame{Missing: []string{LayerRaw, LayerOverlay, LayerScene, LayerCards, LayerWorld, "depth"}}
	src := &Frame{Raw: &Sip1Packet{}, Overlay: &OverlayLayer{}, Scene: &SceneLayer{}, Cards: &CardsLayer{}, World: &WorldLayer{}}
	if !mergeLayers(dst, src) || len(dst.Missing) != 1 || dst.Raw == nil || dst.Overlay == nil || dst.Scene == nil ||
		dst.Cards == nil || dst.World == nil {
		t.Fatalf("%+v", dst)
	}
	dst = &Frame{Missing: []string{LayerRaw, LayerOverlay, LayerScene, LayerCards, LayerWorld}}
	if mergeLayers(dst, &Frame{}) || len(dst.Missing) != 5 {
		t.Fatal("nothing to merge")
	}
}

func TestStreamPipelinePrunes(t *testing.T) {
	old := keepFrames
	keepFrames = 2
	defer func() { keepFrames = old }()
	var lines []string
	for g := int64(1); g <= 12; g++ {
		lines = append(lines, fline(g, 1, g, 0, ""))
	}
	f := linesFake(t, lines...)
	c, _ := fakeClient(t, f)
	if n := len(collect(t, c.Streams().Frames(testCtx(t)), 12)); n != 12 {
		t.Fatal(n)
	}
	// the dedupe stays bounded over sessions; a revision inside its window is flagged, and its repeat dropped
	d := &dedupe{delivered: map[sessG]int64{}}
	frame := func(s, g, rev int64) *FrameItem {
		return &FrameItem{Frame: &Frame{Session: s, G: g, World: &WorldLayer{Rev: rev}}}
	}
	for s := int64(0); s < 3; s++ {
		for g := int64(1); g <= 12; g++ {
			if !d.admit(frame(s, g, 0)) || len(d.delivered) > 2*keepFrames {
				t.Fatal(s, g, len(d.delivered))
			}
		}
	}
	if rev := frame(2, 12, 1); !d.admit(rev) || !rev.IsRevision || d.admit(frame(2, 12, 1)) || d.admit(frame(2, 11, 0)) {
		t.Fatal("a revision, its repeat, a frame again")
	}
}

// TestFramesResumeAfterAReset: the resume id becomes the reset's (2.3), so a connection that ends before the next
// frame resumes after the reset's cursor and gets no second reset.
func TestFramesResumeAfterAReset(t *testing.T) {
	ctx, cancel := context.WithCancel(testCtx(t))
	defer cancel()
	f := linesFake(t, fline(1, 1, 1, 0, ""), fline(2, 1, 2, 0, ""))
	f.DropAfter("streamFrames", 1) // the first connection ends right after its reset
	var mu sync.Mutex
	streams := 0
	f.SetDelay(func(op string, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if op == "streamFrames" {
			if streams++; streams == 2 {
				cancel() // the resumed connection: enough
			}
		}
	})
	c, _ := fakeClient(t, f)
	resets := 0
	for item, err := range c.Streams().Frames(ctx, LastEventID("otherboot-7")) {
		if err != nil {
			break
		}
		if _, ok := item.(*ResetItem); ok {
			resets++
		}
	}
	reqs := f.Requests()
	if resets != 1 || len(reqs) < 2 || reqs[0].Header.Get("Last-Event-ID") != "otherboot-7" ||
		reqs[1].Header.Get("Last-Event-ID") != f.Boot()+"-2" {
		t.Fatal(resets, len(reqs))
	}
}

// TestRawPacketsResumeAfterAReset: over SSE the raw stream does as the frames do (2.3): the resume id becomes the
// reset's, so a connection that ends right after its reset resumes after the reset's cursor and gets no second reset.
func TestRawPacketsResumeAfterAReset(t *testing.T) {
	ctx, cancel := context.WithCancel(testCtx(t))
	defer cancel()
	raw := func(c int64) string { return `,"raw":` + rawJSON(c, c) }
	f := linesFake(t, fline(1, 1, 1, 0, raw(1)), fline(2, 1, 2, 0, raw(2)))
	f.DropAfter("rawStream", 1) // the first connection ends right after its reset
	var mu sync.Mutex
	streams := 0
	f.SetDelay(func(op string, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if op == "rawStream" {
			if streams++; streams == 2 {
				cancel() // the resumed connection: enough
			}
		}
	})
	c, _ := fakeClient(t, f)
	resets := 0
	for item, err := range c.Streams().RawPackets(ctx, RawFormat(FormatSSE), LastEventID("otherboot-7")) {
		if err != nil {
			break
		}
		if _, ok := item.(*ResetItem); ok {
			resets++
		}
	}
	reqs := f.Requests()
	if resets != 1 || len(reqs) < 2 || reqs[0].Header.Get("Last-Event-ID") != "otherboot-7" ||
		reqs[1].Header.Get("Last-Event-ID") != f.Boot()+"-2" || reqs[1].Query.Has("after") {
		t.Fatal(resets, len(reqs))
	}
}

func TestPollStream(t *testing.T) {
	ctx := testCtx(t)
	f := linesFake(t, clockLine, workerLine, fline(1, 1, 1, 0, ""), fline(2, 1, 2, 0, ""), fline(3, 1, 3, 0, ""))
	c, clk := fakeClient(t, f)
	items := collect(t, c.Streams().Poll(ctx, PollLimit(2), PollWait(0), Layers(LayerWorld), Precision("rounded")), 5)
	if got := kinds(items); got != "clock,worker,f1,f2,f3" {
		t.Fatal(got)
	}
	q := f.Requests()[0].Query
	if !strings.HasPrefix(q.Get("cid"), "go-") || len(q.Get("cid")) != 19 || q.Get("limit") != "2" || q.Get("wait") != "0" {
		t.Fatal(q)
	}
	// the next poll has nothing new (a back-off), and before the third one a viewer pauses: a clock item
	n, polls := len(clk.Sleeps()), 0
	var mu sync.Mutex
	f.SetDelay(func(op string, r *http.Request) {
		if op != "pollFrames" {
			return
		}
		mu.Lock()
		polls++
		third := polls == 3
		mu.Unlock()
		if third {
			if _, err := c.Session().Pause(context.Background()); err != nil {
				t.Error(err)
			}
		}
	})
	start := len(f.Requests())
	items = collect(t, c.Streams().Poll(ctx, After(3), Observe(true)), 3)
	f.SetDelay(nil)
	if got := kinds(items); got != "clock,worker,clock" || items[2].(*ClockItem).State.Playing || len(clk.Sleeps()) == n {
		t.Fatal(got, clk.Sleeps())
	}
	if r := f.Requests()[start]; r.Query.Get("cid") != "" || r.Query.Get("cursor") != "3" {
		t.Fatal("an observing poll sends no cid", r.Query)
	}
	// errors: a retryable one is retried, a final one ends the stream
	f.FailNext("pollFrames", 1, factorytest.ErrorReply(503, "busy", "x"))
	f.FailNext("pollFrames", 1, factorytest.ErrorReply(400, "invalid_param", "x"))
	var errs []error
	for _, err := range c.Streams().Poll(ctx, After(3), Resume(true)) {
		errs = append(errs, err)
	}
	if len(errs) != 1 || !HasCode(errs[0], CodeInvalidParam) {
		t.Fatal(errs)
	}
	// a cancelled context ends a failing poll quietly
	cctx, cancel := context.WithCancel(ctx)
	ec, _ := funcClient(t, func(r *http.Request) (*http.Response, error) {
		cancel()
		return nil, errBoom
	})
	for _, err := range ec.Streams().Poll(cctx) {
		if !HasCode(err, CodeTransport) {
			t.Fatal(err)
		}
	}
	// a failing backoff sleep ends a poll that gets nothing new
	ec, _ = funcClient(t, func(r *http.Request) (*http.Response, error) {
		return reply(200, `{"state":{"revision":1},"worker":{"state":"running"},"cursor":0,"frames":[]}`), nil
	}, WithClock(&failClock{}))
	if got := kinds(collect(t, ec.Streams().Poll(ctx), 0)); got != "clock,worker" {
		t.Fatal(got)
	}
}

func TestRawPackets(t *testing.T) {
	ctx := testCtx(t)
	raw := func(c int64) string { return `,"raw":` + rawJSON(c, c) }
	lines := []string{fline(1, 1, 1, 0, raw(1)), fline(2, 1, 2, 0, ""), fline(3, 1, 3, 0, raw(3)), fline(4, 1, 4, 0, raw(4)),
		fline(5, 1, 5, 0, `,"raw":{"schema":5}`)}
	f := linesFake(t, lines...)
	c, _ := fakeClient(t, f, WithObserve(true))
	items := collect(t, c.Streams().RawPackets(ctx, ConnectionLimit(2), After(0)), 3)
	if got := kinds(items); got != "p1,p3,p4" || items[0].(*PacketItem).Packet.Schema != "sip1" {
		t.Fatal(got)
	}
	reqs := f.Requests()
	if reqs[0].Query.Get("format") != "ndjson" || reqs[0].Query.Get("observe") != "1" || reqs[1].Query.Get("after") != "3" ||
		reqs[0].Query.Get("limit") != "2" {
		t.Fatal(reqs[1].Query)
	}
	// SSE packets carry their event id and resume with it; a packet that does not decode is an error
	f = linesFake(t, lines...)
	f.DropAfter("rawStream", 2)
	c, _ = fakeClient(t, f)
	var got []string
	for it, err := range c.Streams().RawPackets(ctx, RawFormat(FormatSSE), ConnectionMaxSeconds(9)) {
		if err != nil {
			got = append(got, string(AsError(err).Code))
		} else {
			got = append(got, kinds([]StreamItem{it}))
		}
		if len(got) == 4 {
			break
		}
	}
	if strings.Join(got, ",") != "p1,p3,p4,decode" {
		t.Fatal(got)
	}
	if reqs := f.Requests(); reqs[1].Header.Get("Last-Event-ID") != "fakeboot01-3" {
		t.Fatal(reqs[1].Header)
	}
	// a Last-Event-ID of another boot gets a reset (its id and cursor become the resume point), then the program
	f = linesFake(t, lines...)
	c, _ = fakeClient(t, f)
	if got := kinds(collect(t, c.Streams().RawPackets(ctx, RawFormat(FormatSSE), LastEventID("otherboot-7")), 2)); got != "reset,p1" {
		t.Fatal(got)
	}
	// failures and empty connections, as for frames
	f.FailNext("rawStream", 1, factorytest.ErrorReply(400, "invalid_param", "x"))
	for _, err := range c.Streams().RawPackets(ctx) {
		mustCode(t, err, CodeInvalidParam)
	}
	f.FailNext("rawStream", 1, factorytest.ErrorReply(503, "busy", "x"))
	for _, err := range c.Streams().RawPackets(ctx, ConnectionLimit(1), Resume(false)) {
		mustCode(t, err, CodeBusy)
	}
	if got := kinds(collect(t, c.Streams().RawPackets(ctx, ConnectionLimit(1), Resume(false)), 0)); got != "p1" {
		t.Fatal(got)
	}
	ec, _ := funcClient(t, func(r *http.Request) (*http.Response, error) {
		return reply(200, "", "Content-Type", "application/x-ndjson"), nil
	}, WithClock(&failClock{}))
	if n := len(collect(t, ec.Streams().RawPackets(ctx), 0)); n != 0 {
		t.Fatal(n)
	}
	// cancelled while packets flow
	cctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, err := range c.Streams().RawPackets(cctx) {
		cancel()
		if err != nil {
			break
		}
	}
}

func TestChan(t *testing.T) {
	ctx := testCtx(t)
	f := linesFake(t, fline(1, 1, 1, 0, ""), fline(2, 1, 2, 0, ""))
	c, _ := fakeClient(t, f)
	cctx, cancel := context.WithCancel(ctx)
	ch := Chan(cctx, c.Streams().Frames(cctx))
	first := <-ch
	if first.Err != nil || first.Value.(*FrameItem).Frame.G != 1 {
		t.Fatal(first)
	}
	cancel()
	for range ch {
	}
	// a finished iterator closes the channel
	done := Chan(ctx, func(yield func(int, error) bool) { yield(1, nil) })
	if v := <-done; v.Value != 1 {
		t.Fatal(v)
	}
	if _, ok := <-done; ok {
		t.Fatal("open")
	}
	// a value that comes after the context ended is not handed over, and the channel closes
	cctx, cancel = context.WithCancel(ctx)
	finished := make(chan bool)
	stopped := Chan(cctx, func(yield func(int, error) bool) {
		cancel()
		finished <- yield(1, nil)
	})
	if <-finished {
		t.Fatal("a value was handed over after the context ended")
	}
	if _, ok := <-stopped; ok {
		t.Fatal("open")
	}
}

// TestSourcesStopAfterTheConsumer: an event that arrives after the consumer stopped is read but not handed over,
// and the source ends (frames over SSE, raw packets over NDJSON).
func TestSourcesStopAfterTheConsumer(t *testing.T) {
	ctx := testCtx(t)
	for _, c := range []struct {
		name, ctype, first, late string
		raw                      bool
	}{
		{"frames", "text/event-stream", "id: b-1\nevent: frame\ndata: " + fline(1, 1, 1, 0, "") + "\n\n",
			"id: b-2\nevent: frame\ndata: " + fline(2, 1, 2, 0, "") + "\n\n", false},
		{"packets", "application/x-ndjson", rawJSON(1, 1) + "\n", `{"schema":5}` + "\n", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			late := make(chan error, 1)
			cl, _ := funcClient(t, func(r *http.Request) (*http.Response, error) {
				pr, pw := io.Pipe()
				go func() {
					_, _ = io.WriteString(pw, c.first)
					<-r.Context().Done()
					_, err := io.WriteString(pw, c.late)
					late <- err
					_ = pw.Close()
				}()
				resp := reply(200, "", "Content-Type", c.ctype)
				resp.Body = pr
				return resp, nil
			})
			s := cl.Streams().Frames(ctx)
			if c.raw {
				s = cl.Streams().RawPackets(ctx)
			}
			for it, err := range s {
				if err != nil || it == nil {
					t.Fatal(it, err)
				}
				break
			}
			if err := <-late; err != nil {
				t.Fatal("the late event was not read:", err)
			}
		})
	}
}

// countClock lets the first ok sleeps pass (advancing it) and fails the others.
type countClock struct {
	mu sync.Mutex
	ok int
	factorytest.Clock
}

func (c *countClock) Sleep(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ok <= 0 {
		return errBoom
	}
	c.ok--
	return nil
}

func TestBackfillSleepsFail(t *testing.T) {
	ctx := testCtx(t)
	lines := []string{fline(1, 1, 1, -1, `,"missing":["world"]`), fline(2, 1, 2, 0, "")}
	// the wait for the release deadline fails: the frame is released as it is
	f := linesFake(t, lines...)
	c, _ := fakeClient(t, f, WithClock(&failClock{}))
	if got := kinds(collect(t, c.Streams().Frames(ctx, Backfill(true)), 2)); got != "f1,f2" {
		t.Fatal(got)
	}
	// the wait between tries fails
	f = linesFake(t, lines...)
	f.FailNext("getFrame", 1, factorytest.ErrorReply(500, "internal", "x"))
	c, _ = fakeClient(t, f, WithClock(&countClock{ok: 1}))
	if got := kinds(collect(t, c.Streams().Frames(ctx, Backfill(true)), 2)); got != "f1,f2" {
		t.Fatal(got)
	}
}

// pollReplies serves long-poll replies in turn and closes reached when the last one is asked for.
func pollReplies(t *testing.T, reached chan struct{}, replies ...string) *Client {
	t.Helper()
	var mu sync.Mutex
	n := 0
	c, _ := funcClient(t, func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		i := n
		if n < len(replies)-1 {
			n++
		} else if i == len(replies)-1 {
			select {
			case <-reached:
			default:
				close(reached)
			}
		}
		return reply(200, replies[i]), nil
	})
	return c
}

func pollReply(rev int64, worker string, frames int) string {
	var fs []string
	for g := 1; g <= frames; g++ {
		fs = append(fs, fmt.Sprintf(`{"cursor":%d,"session":1,"g":%d}`, g, g))
	}
	return fmt.Sprintf(`{"state":{"revision":%d},"worker":{"state":%q},"cursor":%d,"frames":[%s]}`, rev, worker, frames,
		strings.Join(fs, ","))
}

// TestPollStopsWhileBlocked stops a poll stream while its source waits to hand over a clock, worker or frame item.
func TestPollStopsWhileBlocked(t *testing.T) {
	ctx := testCtx(t)
	for name, second := range map[string]string{
		"clock":  pollReply(2, "running", 0),
		"worker": pollReply(1, "stopped", 0),
		"frame":  pollReply(1, "running", 1),
	} {
		t.Run(name, func(t *testing.T) {
			reached := make(chan struct{})
			c := pollReplies(t, reached, pollReply(1, "running", 16), second)
			n := 0
			for _, err := range c.Streams().Poll(ctx) {
				if err != nil {
					t.Fatal(err)
				}
				if n++; n == 2 {
					<-reached
					break
				}
			}
		})
	}
	// a retryable poll failure is retried
	f := linesFake(t, fline(1, 1, 1, 0, ""))
	f.FailNext("pollFrames", 1, factorytest.ErrorReply(503, "busy", "x"))
	c, _ := fakeClient(t, f, WithRetries(0))
	if got := kinds(collect(t, c.Streams().Poll(ctx), 3)); got != "clock,worker,f1" {
		t.Fatal(got)
	}
}

func TestRawStopsWhileBlocked(t *testing.T) {
	var lines []string
	for g := int64(1); g <= 20; g++ {
		lines = append(lines, fline(g, 1, g, 0, `,"raw":`+rawJSON(g, g)))
	}
	f := linesFake(t, lines...)
	c, _ := fakeClient(t, f)
	for range c.Streams().RawPackets(testCtx(t)) {
		// let the source fill the queue and block on its next packet, then stop
		time.Sleep(5 * time.Millisecond)
		break
	}
}

// gateClock's Sleep waits for its context, then for release.
type gateClock struct {
	factorytest.Clock
	once     sync.Once
	sleeping chan struct{}
	release  chan struct{}
}

func (c *gateClock) Sleep(ctx context.Context, d time.Duration) error {
	c.once.Do(func() { close(c.sleeping) })
	<-ctx.Done()
	<-c.release
	return ctx.Err()
}

// TestBackfillAfterTheStreamStopped stops a stream while a frame waits for its backfill: the backfill gives up
// without handing the frame over, and the frame is not released.
func TestBackfillAfterTheStreamStopped(t *testing.T) {
	f := linesFake(t, fline(1, 1, 1, -1, `,"missing":["world"]`), fline(2, 1, 2, 0, ""))
	clk := &gateClock{sleeping: make(chan struct{}), release: make(chan struct{})}
	c, _ := fakeClient(t, f, WithClock(clk))
	ctx, cancel := context.WithCancel(testCtx(t))
	defer cancel()
	go func() {
		<-clk.sleeping
		cancel()
	}()
	for it, err := range c.Streams().Frames(ctx, Backfill(true)) {
		if err == nil {
			t.Fatal("a held frame was released", it)
		}
		close(clk.release)
	}
}

// streamResumes are the Last-Event-ID headers (or the after cursors) of a fake's stream requests of op, in order.
func streamResumes(f *factorytest.Fake, op string) []string {
	var out []string
	for _, r := range f.Requests() {
		if r.Op == op {
			out = append(out, r.Header.Get("Last-Event-ID")+r.Query.Get("after"))
		}
	}
	return out
}

// TestStreamsCloseFinishedConnections: like the 20.20 unit, the fake holds a stream that reached its limit open and
// sends nothing more; the SDK counts the frames and the seconds itself, closes the connection and goes on at once.
func TestStreamsCloseFinishedConnections(t *testing.T) {
	ctx := testCtx(t)
	var lines []string
	for g := int64(1); g <= 7; g++ {
		lines = append(lines, fline(g, 1, g, 0, `,"raw":`+rawJSON(g, g)))
	}
	held := func() *factorytest.Fake {
		f := linesFake(t, lines...)
		f.HoldFinishedStreams(true)
		return f
	}
	// limit: three connections of two frames, one after the other, with no wait
	f := held()
	c, clk := fakeClient(t, f)
	if got := kinds(collect(t, c.Streams().Frames(ctx, ConnectionLimit(2)), 6)); got != "f1,f2,f3,f4,f5,f6" {
		t.Fatal(got)
	}
	// (the stream may have opened the fourth connection before the loop stopped)
	if got := strings.Join(streamResumes(f, "streamFrames")[:3], ","); got != ",fakeboot01-2,fakeboot01-4" ||
		len(clk.Sleeps()) != 0 {
		t.Fatal(got, clk.Sleeps())
	}
	// max_s: the fake ends a max_s=1 stream after five frames and holds it; when the client's clock passes the second
	// the SDK closes the connection and goes on after the fifth frame
	f = held()
	c, clk = fakeClient(t, f)
	n := 0
	for it, err := range c.Streams().Frames(ctx, ConnectionMaxSeconds(1)) {
		if err != nil {
			t.Fatal(err)
		}
		if n++; n == 5 {
			clk.Advance(time.Second)
		}
		if n == 6 {
			if it.(*FrameItem).Frame.G != 6 {
				t.Fatal(kinds([]StreamItem{it}))
			}
			break
		}
	}
	if got := strings.Join(streamResumes(f, "streamFrames")[:2], ","); got != ",fakeboot01-5" {
		t.Fatal(got)
	}
	// raw packets: the same, resumed with after (NDJSON has no event ids)
	f = held()
	c, clk = fakeClient(t, f)
	if got := kinds(collect(t, c.Streams().RawPackets(ctx, ConnectionLimit(3)), 5)); got != "p1,p2,p3,p4,p5" {
		t.Fatal(got)
	}
	if got := strings.Join(streamResumes(f, "rawStream")[:2], ","); got != ",3" {
		t.Fatal(got)
	}
	n = 0
	for _, err := range c.Streams().RawPackets(ctx, ConnectionMaxSeconds(1), After(0)) {
		if err != nil {
			t.Fatal(err)
		}
		if n++; n == 5 {
			clk.Advance(time.Second)
		}
		if n == 6 {
			break
		}
	}
	// the low level: one connection, and the loop ends at the limit or after max_s without an error
	f = held()
	c, clk = fakeClient(t, f)
	var evs []string
	for ev, err := range c.API().StreamFrames(ctx, &StreamFramesParams{Limit: 2}) {
		if err != nil {
			t.Fatal(err)
		}
		evs = append(evs, ev.ID)
	}
	if strings.Join(evs, ",") != "fakeboot01-1,fakeboot01-2" {
		t.Fatal(evs)
	}
	n = 0
	for _, err := range c.API().RawStream(ctx, &RawStreamParams{MaxS: 1}) {
		if err != nil {
			t.Fatal(err)
		}
		if n++; n == 5 {
			clk.Advance(time.Second)
		}
	}
	if n != 5 || clk.Timers() != 0 {
		t.Fatal(n, clk.Timers())
	}
	// a clock without AfterFunc times max_s with a Sleep: here one that ends when the test opens its gate
	f = held()
	gate := make(chan struct{})
	c, _ = fakeClient(t, f, WithClock(&plainClock{gate: gate}))
	n = 0
	for _, err := range c.API().StreamFrames(ctx, &StreamFramesParams{MaxS: 1}) {
		if err != nil {
			t.Fatal(err)
		}
		if n++; n == 5 {
			close(gate)
		}
	}
	if n != 5 {
		t.Fatal(n)
	}
}

// plainClock is a Clock without AfterFunc. Its Sleep returns when gate (if any) is closed or its context ends, and
// then closes slept (if any).
type plainClock struct {
	gate  chan struct{}
	slept chan struct{}
}

func (*plainClock) Now() time.Time { return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) }

func (c *plainClock) Sleep(ctx context.Context, d time.Duration) error {
	if c.slept != nil {
		defer close(c.slept)
	}
	if c.gate != nil {
		select {
		case <-c.gate:
		case <-ctx.Done():
		}
	}
	return ctx.Err()
}

func TestAfterFunc(t *testing.T) {
	fired := make(chan struct{})
	afterFunc(&plainClock{}, time.Second, func() { close(fired) })
	<-fired
	// stopped before the Sleep ends: not called
	clk := &plainClock{gate: make(chan struct{}), slept: make(chan struct{})}
	stop := afterFunc(clk, time.Second, func() { t.Error("called after stop") })
	stop()
	<-clk.slept
	// the system clock's timers
	fired = make(chan struct{})
	SystemClock{}.AfterFunc(time.Millisecond, func() { close(fired) })
	<-fired
	if !(SystemClock{}).AfterFunc(time.Hour, func() {})() {
		t.Fatal("stop")
	}
	// the manual clock's: due when the clock passes them, stopped, or at once
	mc := factorytest.NewClock(time.Time{})
	calls := make(chan string, 3)
	mc.AfterFunc(time.Second, func() { calls <- "1s" })
	stopLater := mc.AfterFunc(2*time.Second, func() { calls <- "2s" })
	mc.AfterFunc(0, func() { calls <- "now" })
	if got := <-calls; got != "now" {
		t.Fatal(got)
	}
	_ = mc.Sleep(context.Background(), time.Second)
	if got := <-calls; got != "1s" || mc.Timers() != 1 || !stopLater() || stopLater() || mc.Timers() != 0 {
		t.Fatal(got, mc.Timers())
	}
}

// TestStreamReconnectWaits: a failed connection is tried again after retry_ms, else Retry-After, else the backoff.
func TestStreamReconnectWaits(t *testing.T) {
	ctx := testCtx(t)
	busy := func(header string, body string) factorytest.Reply {
		r := factorytest.Reply{Status: 503, ContentType: "application/json", Body: []byte(body)}
		if header != "" {
			r.Headers = map[string]string{"Retry-After": header}
		}
		return r
	}
	f := linesFake(t, fline(1, 1, 1, 0, ""))
	f.FailNext("streamFrames", 1, busy("2", `{"error":"busy","code":"busy"}`))
	f.FailNext("streamFrames", 1, busy("2", `{"error":"busy","code":"busy","retry_ms":40}`))
	f.FailNext("streamFrames", 1, busy("", `{"error":"busy","code":"busy"}`))
	c, clk := fakeClient(t, f, WithRand(rand.New(rand.NewPCG(1, 2))))
	if got := kinds(collect(t, c.Streams().Frames(ctx), 1)); got != "f1" {
		t.Fatal(got)
	}
	sleeps := clk.Sleeps()
	if len(sleeps) != 3 || sleeps[0] != 2*time.Second || sleeps[1] != 40*time.Millisecond || sleeps[2] <= 0 ||
		sleeps[2] >= time.Second {
		t.Fatal(sleeps)
	}
}

// TestClockOnConnect: with ClockOnConnect the stream reads the shared state after every connect and passes it on
// first; a read that fails or takes too long is left out.
func TestClockOnConnect(t *testing.T) {
	ctx := testCtx(t)
	f := linesFake(t, clockLine, fline(1, 1, 1, 0, ""), fline(2, 1, 2, 0, ""))
	f.DropAfter("streamFrames", 2)
	c, _ := fakeClient(t, f)
	items := collect(t, c.Streams().Frames(ctx, ClockOnConnect(true)), 5)
	if got := kinds(items); got != "clock,clock,f1,clock,f2" {
		t.Fatal(got)
	}
	if ck := items[0].(*ClockItem); ck.State.Video != "v" || ck.ServerNow != 100 {
		t.Fatalf("%+v", ck)
	}
	// the read fails: the stream goes on without it
	f = linesFake(t, fline(1, 1, 1, 0, ""))
	f.FailNext("getSession", 1, factorytest.ErrorReply(503, "busy", "x"))
	c, _ = fakeClient(t, f)
	if got := kinds(collect(t, c.Streams().Frames(ctx, ClockOnConnect(true)), 1)); got != "f1" {
		t.Fatal(got)
	}
	// the read takes too long: its timeout runs on the client's clock, which the hook moves past it
	f = linesFake(t, fline(1, 1, 1, 0, ""))
	c, clk := fakeClient(t, f)
	f.SetDelay(func(op string, r *http.Request) {
		if op == "getSession" {
			clk.Advance(clockReadTimeout)
			<-r.Context().Done()
		}
	})
	if got := kinds(collect(t, c.Streams().Frames(ctx, ClockOnConnect(true)), 1)); got != "f1" {
		t.Fatal(got)
	}
	if clk.Timers() != 0 {
		t.Fatal("a timer is left", clk.Timers())
	}
}

// TestBackfillTimesOut: each backfill read is bounded by a timeout on the client's clock; when every one times out
// the frame is released as it is. The fake's hook moves the clock past the timeout, so nothing depends on how long
// the test takes: the reads start 1.5 s, 2.5 s and 3.5 s after the frame arrived.
func TestBackfillTimesOut(t *testing.T) {
	f := linesFake(t, fline(1, 1, 1, -1, `,"missing":["world"]`), fline(2, 1, 2, 0, ""))
	c, clk := fakeClient(t, f)
	start := clk.Now()
	var mu sync.Mutex
	var reads []time.Duration
	f.SetDelay(func(op string, r *http.Request) {
		if op == "getFrame" {
			mu.Lock()
			reads = append(reads, clk.Now().Sub(start))
			mu.Unlock()
			clk.Advance(backfillTimeout)
			<-r.Context().Done()
		}
	})
	if got := kinds(collect(t, c.Streams().Frames(testCtx(t), Backfill(true)), 2)); got != "f1,f2" {
		t.Fatal(got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reads) != 3 || reads[0] != 1500*time.Millisecond || reads[1] != 2500*time.Millisecond ||
		reads[2] != 3500*time.Millisecond {
		t.Fatal(reads)
	}
	if sleeps := clk.Sleeps(); len(sleeps) == 0 || sleeps[0] != 1500*time.Millisecond {
		t.Fatal(sleeps)
	}
}

// TestStreamEventTooLarge: an event over the limit ends the stream with a decode error, without a reconnect (the
// unit would send the same event again).
func TestStreamEventTooLarge(t *testing.T) {
	big := `{"cursor":1,"session":1,"video":"v","g":1,"t":0.2,"world":{"schema":"stw1","g":1,"rev":0,"stones":[],` +
		`"events":[],"note":"` + strings.Repeat("x", 300) + `"}}`
	f := linesFake(t, big, fline(2, 1, 2, 0, ""))
	c, _ := fakeClient(t, f)
	old := maxLine
	maxLine = 256
	defer func() { maxLine = old }()
	var errs []error
	for _, err := range c.Streams().Frames(testCtx(t)) {
		errs = append(errs, err)
	}
	if len(errs) != 1 {
		t.Fatal(errs)
	}
	if e := mustCode(t, errs[0], CodeDecode); e.Op != "streamFrames" {
		t.Fatal(e)
	}
	if ops := strings.Join(f.Ops(), ","); ops != "streamFrames" {
		t.Fatal(ops)
	}
}
