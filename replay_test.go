package pdiggerfactory

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// writeRecording writes lines to a temporary .ndjson file.
func writeRecording(t *testing.T, lines ...string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "rec.ndjson")
	if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const testHeader = `{"recording":"pfr1","name":"t","video":"v","frames":3,"g_first":1,"g_last":3}`

// testFrame is the frame line of grid frame g (its cursor) with extra JSON members.
func testFrame(session, g int64, extra string) string {
	return fline(g, session, g, -1, extra)
}

// testClock is the data of a clock event: playing at rate 0.8 from wall time 100 at media time 0.
const testClock = `{"session":1,"revision":1,"video":"v","playing":true,"rate":0.8,"anchor_media":0,` +
	`"anchor_wall":100,"hold_until":100,"changed_by":"start","media_time":0,"server_now":100}`

func TestReplayItems(t *testing.T) {
	ctx := testCtx(t)
	p := writeRecording(t, testHeader,
		`{"sse":"clock","data":`+testClock+`,"recv":100}`,
		`{"sse":"worker","data":{"state":"running","reason":""}}`,
		`{"sse":"gap","data":{"gap":3}}`,
		`{"sse":"reset","data":{"boot":"b2","cursor":9}}`,
		`{"sse":"other","data":{}}`,
		`{"sse":"clock","data":5}`, `{"sse":"worker","data":5}`, `{"sse":"gap","data":"x"}`, `{"sse":"reset","data":[]}`,
		`not json`,
		`{"cursor":1,"g":1,"session":"x"}`,
		`{"recording":"pfr1","name":"a second header is a plain line"}`,
		testFrame(1, 1, `,"world":`+worldJSON(1, 1, "", "", `{"type":"new","id":4}`)),
		testFrame(1, 2, ""),
		`{"note":"ignored"}`)
	r, err := OpenReplay(ctx, p, &ReplayOptions{Speed: 2})
	if err != nil {
		t.Fatal(err)
	}
	if r.Header == nil || r.Header.Name != "t" || r.Header.Frames != 3 || !bytes.Contains(r.Header.Raw, []byte("pfr1")) {
		t.Fatalf("header %+v", r.Header)
	}
	var kinds []string
	for item, err := range r.Items(ctx) {
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, item.Kind())
		switch it := item.(type) {
		case *GapItem:
			if it.Count != 3 {
				t.Fatal("gap")
			}
		case *ResetItem:
			if it.Boot != "b2" || it.Cursor != 9 {
				t.Fatal("reset")
			}
		}
	}
	if got := strings.Join(kinds, ","); got != "clock,worker,gap,reset,frame,frame" {
		t.Fatal(got)
	}
	// the four side lines whose data is not of their event's shape, the line that is not JSON and the frame whose
	// session is a string; not the side line of an event the SDK does not know, the second header or the note
	if r.Skipped() != 6 {
		t.Fatalf("skipped %d", r.Skipped())
	}
	// every iteration reads from the start again
	evs := collect(t, r.Events(ctx), 0)
	if len(evs) != 1 || evs[0].Describe() != "0:00.2 stone 4 appears" || evs[0].Wall == nil || *evs[0].Wall != 100+0.2/0.8 {
		t.Fatalf("%+v", evs)
	}
	if fr := collect(t, r.Frames(ctx), 1); fr[0].G != 1 {
		t.Fatal("frames")
	}
	if us := collect(t, r.Updates(ctx), 1); us[0].Frame.G != 1 {
		t.Fatal("updates")
	}
	if ev := collect(t, r.Events(ctx), 1); len(ev) != 1 {
		t.Fatal("events break")
	}
	sum, err := r.Summary(ctx)
	if err != nil || sum.Frames != 2 || sum.Events.New != 1 {
		t.Fatalf("%+v %v", sum, err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestReplayWithoutHeader(t *testing.T) {
	ctx := testCtx(t)
	// the first line is a frame (or not JSON): it is read again by the first iteration; a header after it is skipped
	for first, want := range map[string]int{testFrame(1, 1, ""): 2, `not json`: 1} {
		p := writeRecording(t, first, testHeader, testFrame(1, 2, ""))
		r, err := OpenReplay(ctx, p, nil)
		if err != nil || r.Header != nil {
			t.Fatalf("%v %v", r.Header, err)
		}
		if n := len(collect(t, r.Frames(ctx), 0)); n != want {
			t.Fatalf("%s: %d frames", first, n)
		}
	}
	// an empty file
	r, err := OpenReplay(ctx, writeRecording(t, ""), nil)
	if err != nil || r.Header != nil || len(collect(t, r.Frames(ctx), 0)) != 0 {
		t.Fatal(err)
	}
	// Close before any iteration closes the file opened by OpenReplay; a second Close does nothing
	r, _ = OpenReplay(ctx, writeRecording(t, testHeader), nil)
	if first, second := r.Close(), r.Close(); first != nil || second != nil {
		t.Fatal("close", first, second)
	}
}

// truncatedGzip is a gzip of a header and many frames, cut in the middle.
func truncatedGzip(t *testing.T) string {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	fmt.Fprintln(zw, testHeader)
	for g := int64(1); g < 4000; g++ {
		fmt.Fprintln(zw, testFrame(1, g, fmt.Sprintf(`,"pad":"%x"`, g*7919)))
	}
	zw.Close()
	p := filepath.Join(t.TempDir(), "cut.ndjson.gz")
	if err := os.WriteFile(p, buf.Bytes()[:buf.Len()/2], 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReplayErrors(t *testing.T) {
	ctx := testCtx(t)
	if _, err := OpenReplay(ctx, filepath.Join(t.TempDir(), "none.ndjson"), nil); !HasCode(err, CodeTransport) {
		t.Fatal(err)
	}
	// a recording that breaks after its header
	r, err := OpenReplay(ctx, truncatedGzip(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	var last error
	for _, err := range r.Frames(ctx) {
		last = err
	}
	mustCode(t, last, CodeDecode)
	// a recording that breaks in its first line: a gzip header, then a corrupt deflate stream
	p := filepath.Join(t.TempDir(), "bad.ndjson.gz")
	if err := os.WriteFile(p, []byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff}, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenReplay(ctx, p, nil); !HasCode(err, CodeDecode) {
		t.Fatal(err)
	}
	// a gzip header that is cut short, or that names an unknown compression method: decode, not transport
	for name, b := range map[string][]byte{"cut": {0x1f, 0x8b, 0, 0}, "method": {0x1f, 0x8b, 7, 0, 0, 0, 0, 0, 0, 0xff}} {
		p := filepath.Join(t.TempDir(), name+".ndjson.gz")
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		_, err := OpenReplay(ctx, p, nil)
		if e := mustCode(t, err, CodeDecode); e.IsRetryable() || !strings.Contains(e.Error(), "not a gzip stream") {
			t.Fatalf("%s: %v", name, err)
		}
		if name == "method" && !errors.Is(err, gzip.ErrHeader) {
			t.Fatalf("the gzip error is kept: %v", err)
		}
	}
	// a recording removed after OpenReplay: the second iteration cannot open it
	p = writeRecording(t, testHeader, testFrame(1, 1, ""))
	r, _ = OpenReplay(ctx, p, nil)
	collect(t, r.Frames(ctx), 0)
	os.Remove(p)
	for name, seq := range map[string]func(func(any, error) bool){
		"items":      func(y func(any, error) bool) { r.Items(ctx)(func(v StreamItem, e error) bool { return y(v, e) }) },
		"frames":     func(y func(any, error) bool) { r.Frames(ctx)(func(v *Frame, e error) bool { return y(v, e) }) },
		"updates":    func(y func(any, error) bool) { r.Updates(ctx)(func(v *Update, e error) bool { return y(v, e) }) },
		"events":     func(y func(any, error) bool) { r.Events(ctx)(func(v Event, e error) bool { return y(v, e) }) },
		"activities": func(y func(any, error) bool) { r.Activities(ctx)(func(v Activity, e error) bool { return y(v, e) }) },
	} {
		var got error
		seq(func(_ any, err error) bool { got = err; return true })
		if !HasCode(got, CodeTransport) {
			t.Errorf("%s: %v", name, got)
		}
	}
	if _, err := r.Run(ctx); !HasCode(err, CodeTransport) {
		t.Fatal(err)
	}
	if _, err := r.Summary(ctx); !HasCode(err, CodeTransport) {
		t.Fatal(err)
	}
	// a cancelled context
	p = writeRecording(t, testHeader, testFrame(1, 1, ""))
	r, _ = OpenReplay(ctx, p, nil)
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	for _, err := range r.Frames(cctx) {
		if e := mustCode(t, err, CodeTransport); e.IsRetryable() {
			t.Fatal("a cancelled replay is not retryable")
		}
	}
}

func TestReplayPacing(t *testing.T) {
	ctx := testCtx(t)
	p := writeRecording(t, testHeader, testFrame(1, 1, ""), testFrame(1, 2, ""), testFrame(1, 4, ""),
		testFrame(1, 0, ""), testFrame(2, 9, ""))
	clk := factorytest.NewClock(time.Time{})
	r, err := OpenReplay(ctx, p, &ReplayOptions{Pace: PaceRealtime, Speed: 2, Clock: clk})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(collect(t, r.Frames(ctx), 0)); n != 5 {
		t.Fatal(n)
	}
	// g 2 is 0.2 s after g 1, g 4 0.4 s after it, at speed 2; g 0 goes back and g 9 is a new session: re-anchored
	got := clk.Sleeps()
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond}
	if len(got) != 2 || got[0].Round(time.Millisecond) != want[0] || got[1].Round(time.Millisecond) != want[1] {
		t.Fatalf("sleeps %v", got)
	}
	r, _ = OpenReplay(ctx, p, &ReplayOptions{Pace: PaceRealtime, Clock: &failClock{}})
	var last error
	for _, err := range r.Frames(ctx) {
		last = err
	}
	if !errors.Is(last, errBoom) {
		t.Fatal(last)
	}
}

func TestReplayOverHTTP(t *testing.T) {
	body, err := os.ReadFile(recording("joined-mid-move"))
	if err != nil {
		t.Fatal(err)
	}
	agents := make(chan string, 8)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agents <- r.UserAgent()
		if r.URL.Path != "/joined-mid-move.ndjson.gz" {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	defer srv.Close()
	ctx := testCtx(t)
	r, err := OpenReplay(ctx, srv.URL+"/joined-mid-move.ndjson.gz", &ReplayOptions{HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	if ua := <-agents; ua != UserAgent {
		t.Fatalf("User-Agent %q", ua)
	}
	res, err := r.Run(ctx)
	if err != nil || res.Summary.Frames == 0 {
		t.Fatal(err)
	}
	if _, err := OpenReplay(ctx, srv.URL+"/missing.ndjson.gz", nil); !HasCode(err, CodeNotFound) ||
		AsError(err).Status != 404 {
		t.Fatal(err)
	}
	// Activities, stopped after the first
	if acts := collect(t, r.Activities(ctx), 1); len(acts) != 1 {
		t.Fatal("activities")
	}
}

// TestReplayFinalUpdate: the last update of a replay has no frame, is flagged Final and has what was still open.
func TestReplayFinalUpdate(t *testing.T) {
	ctx := testCtx(t)
	r, err := OpenReplay(ctx, writeRecording(t, testHeader,
		testFrame(1, 1, `,"world":`+worldJSON(1, 1, "", "", `{"type":"lift","id":4,"side":"left"}`)),
		testFrame(1, 2, `,"world":`+worldJSON(1, 2, "", "", ""))), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	us := collect(t, r.Updates(ctx), 0)
	last := us[len(us)-1]
	if len(us) != 3 || !last.Final || last.Frame != nil || len(last.Events) != 0 ||
		len(last.Activities) != 1 || last.Activities[0].Outcome != OutcomeInterrupted || last.State.G != 2 {
		t.Fatalf("%d updates, the last %+v", len(us), last)
	}
	// a recording without frames: the final update has no frame
	e, err := OpenReplay(ctx, writeRecording(t, testHeader), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if us := collect(t, e.Updates(ctx), 0); len(us) != 1 || !us[0].Final || us[0].Frame != nil {
		t.Fatalf("%+v", us)
	}
}

// urlBody is the body of a recording URL in the tests: it fails a read once its request's context has ended (as
// net/http does), serves data, then ends with end, or (end nil) stalls until the request's context ends, closing
// stalled when it does.
type urlBody struct {
	ctx     context.Context
	data    *bytes.Reader
	end     error
	once    sync.Once
	stalled chan struct{}
}

func (b *urlBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if b.data.Len() > 0 {
		return b.data.Read(p)
	}
	if b.end != nil {
		return 0, b.end
	}
	b.once.Do(func() { close(b.stalled) })
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *urlBody) Close() error { return nil }

// urlClient serves every request with data, then end (nil: a stall that closes stalled); it records the requests.
func urlClient(data string, end error, stalled chan struct{}) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp := reply(200, "", "Content-Type", "application/x-ndjson")
		resp.Body = &urlBody{ctx: r.Context(), data: bytes.NewReader([]byte(data)), end: end, stalled: stalled}
		return resp, nil
	})}
}

// TestReplayURLContexts: OpenReplay's context bounds the open and the header only; an iteration's context ends a
// read that waits on a stalled connection at once, with a transport error that is not retryable.
func TestReplayURLContexts(t *testing.T) {
	ctx := testCtx(t)
	lines := strings.Join([]string{testHeader, testFrame(1, 1, ""), testFrame(1, 2, "")}, "\n") + "\n"
	// the context of OpenReplay ends after it returned: the iterations do not notice
	octx, ocancel := context.WithCancel(ctx)
	r, err := OpenReplay(octx, "http://data.test/r.ndjson", &ReplayOptions{HTTPClient: urlClient(lines, io.EOF, nil)})
	if err != nil {
		t.Fatal(err)
	}
	ocancel()
	res, err := r.Run(ctx)
	if err != nil || res.Summary.Frames != 2 {
		t.Fatal(err)
	}
	// a stalled connection: cancelling the iteration's context ends the read and the request
	stalled := make(chan struct{})
	r, err = OpenReplay(ctx, "http://data.test/r.ndjson", &ReplayOptions{HTTPClient: urlClient(lines, nil, stalled)})
	if err != nil {
		t.Fatal(err)
	}
	ictx, icancel := context.WithCancel(ctx)
	go func() {
		<-stalled
		icancel()
	}()
	n := 0
	var last error
	for _, err := range r.Frames(ictx) {
		if err != nil {
			last = err
			continue
		}
		n++
	}
	if e := mustCode(t, last, CodeTransport); n != 2 || e.IsRetryable() || !errors.Is(last, context.Canceled) {
		t.Fatal(n, last)
	}
	// the connection breaks (not the context): transport, retryable, with the cause
	r, err = OpenReplay(ctx, "http://data.test/r.ndjson", &ReplayOptions{HTTPClient: urlClient(lines, errBoom, nil)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(ctx); !HasCode(err, CodeTransport) || !IsRetryable(err) || !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	// the context of OpenReplay ends while the header is read
	stalled = make(chan struct{})
	octx, ocancel = context.WithCancel(ctx)
	go func() {
		<-stalled
		ocancel()
	}()
	_, err = OpenReplay(octx, "http://data.test/r.ndjson", &ReplayOptions{HTTPClient: urlClient(`{"recording":"pfr1"`, nil, stalled)})
	if e := mustCode(t, err, CodeTransport); e.IsRetryable() {
		t.Fatal(e)
	}
	// ... or while the recording is opened: the request ends with it, or the reply that came is closed
	for _, wait := range []bool{true, false} {
		octx, ocancel = context.WithCancel(ctx)
		hc := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			ocancel()
			if wait {
				<-r.Context().Done()
				return nil, r.Context().Err()
			}
			return reply(200, testHeader+"\n"), nil
		})}
		if _, err := OpenReplay(octx, "http://data.test/r.ndjson", &ReplayOptions{HTTPClient: hc}); !HasCode(err, CodeTransport) ||
			IsRetryable(err) {
			t.Fatal(wait, err)
		}
	}
}

// TestReplayOverHTTPContexts: over a real connection, the published-recording case: OpenReplay's context (a timeout
// in a helper, say) ends before the body has been read; the replay goes on to the end.
func TestReplayOverHTTPContexts(t *testing.T) {
	body, err := os.ReadFile(recording("joined-mid-move"))
	if err != nil {
		t.Fatal(err)
	}
	rest := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(body[:len(body)-1024]) // the header and most frames: the last KiB comes after OpenReplay's context ended
		w.(http.Flusher).Flush()
		select {
		case <-rest:
		case <-r.Context().Done():
			return
		}
		w.Write(body[len(body)-1024:])
	}))
	defer srv.Close()
	ctx := testCtx(t)
	octx, ocancel := context.WithCancel(ctx)
	r, err := OpenReplay(octx, srv.URL+"/joined-mid-move.ndjson.gz", &ReplayOptions{HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ocancel()
	close(rest)
	res, err := r.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want, err := OpenReplay(ctx, recording("joined-mid-move"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer want.Close()
	ws, err := want.Summary(ctx)
	if err != nil || res.Summary.Describe() != ws.Describe() {
		t.Fatal(err, res.Summary.Describe())
	}
}

// TestReplayStatusErrors: a recording URL that answers an error status: not_found for a 404, the API's code when the
// reply has an API error body, else decode, each with the status.
func TestReplayStatusErrors(t *testing.T) {
	ctx := testCtx(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/busy":
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(503)
			w.Write([]byte(`{"error":"busy now","code":"busy"}`))
		case "/teapot":
			w.WriteHeader(418)
			w.Write([]byte("short and stout"))
		case "/partial":
			w.WriteHeader(206)
			w.Write([]byte(`{"recording":"pfr1","video":"v"}` + "\n" + testFrame(1, 1, "") + "\n"))
		case "/empty":
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	_, err := OpenReplay(ctx, srv.URL+"/none.ndjson.gz", &ReplayOptions{HTTPClient: srv.Client()})
	if e := mustCode(t, err, CodeNotFound); e.Status != 404 || !strings.Contains(e.Message, "cannot open "+srv.URL+"/none") {
		t.Fatal(e)
	}
	_, err = OpenReplay(ctx, srv.URL+"/busy", &ReplayOptions{HTTPClient: srv.Client()})
	if e := mustCode(t, err, CodeBusy); e.Status != 503 || !e.IsRetryable() || !strings.Contains(e.Message, "busy now") {
		t.Fatal(e)
	} else if d, ok := e.RetryDelay(); !ok || d != 3*time.Second {
		t.Fatal(d, ok)
	}
	_, err = OpenReplay(ctx, srv.URL+"/teapot", &ReplayOptions{HTTPClient: srv.Client()})
	if e := mustCode(t, err, CodeDecode); e.Status != 418 || !strings.Contains(e.Message, "short and stout") {
		t.Fatal(e)
	}
	// any success is read: a 206 like a 200, a 204 as an empty recording
	for path, want := range map[string]int{"/partial": 1, "/empty": 0} {
		r, err := OpenReplay(ctx, srv.URL+path, &ReplayOptions{HTTPClient: srv.Client()})
		if err != nil {
			t.Fatal(path, err)
		}
		frames := 0
		for _, err := range r.Frames(ctx) {
			if err != nil {
				t.Fatal(path, err)
			}
			frames++
		}
		if frames != want {
			t.Fatal(path, frames)
		}
	}
}
