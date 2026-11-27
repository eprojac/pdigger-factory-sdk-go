package pdiggerfactory

import (
	"context"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

func TestNewValidatesTheBaseURL(t *testing.T) {
	for _, u := range []string{"", "ftp://factory", "http://", "factory.local:8120", "http://[::1"} {
		_, err := New(u)
		if e := mustCode(t, err, CodeInvalidParam); e.Field != "base_url" || !strings.Contains(e.Error(), "base URL") {
			t.Errorf("%q: %v", u, err)
		}
	}
	c, err := New("https://factory.example:8120/")
	if err != nil || c.BaseURL() != "https://factory.example:8120" {
		t.Fatalf("%v %v", c, err)
	}
	if c.API() == nil || c.Streams() == nil || c.Session() == nil {
		t.Fatal("accessors")
	}
	if _, ok := c.hc.Transport.(*http.Transport); !ok {
		t.Fatal("default transport")
	}
	hc := &http.Client{}
	c, _ = New("http://x", WithHTTPClient(hc), WithConnectTimeout(time.Second))
	if c.hc != hc {
		t.Fatal("WithHTTPClient")
	}
	if tr := defaultTransport(time.Second); tr.MaxIdleConnsPerHost != 8 || tr.DialContext == nil {
		t.Fatal("defaultTransport")
	}
}

func TestRequestHeaders(t *testing.T) {
	var got []*http.Request
	c, _ := funcClient(t, func(r *http.Request) (*http.Response, error) {
		got = append(got, r)
		return reply(200, `{"ok":true,"worker":"running","viewers":0}`), nil
	}, WithHeader("X-Team", "line-a"), WithBearerToken("tok"), WithUserAgentSuffix("my-app/2"))
	ctx := testCtx(t)
	if _, err := c.API().GetHealth(ctx); err != nil {
		t.Fatal(err)
	}
	r := got[0]
	if r.Header.Get("User-Agent") != UserAgent+" my-app/2" || r.Header.Get("Authorization") != "Bearer tok" ||
		r.Header.Get("X-Team") != "line-a" || r.Header.Get("Accept") != "application/json" ||
		r.URL.String() != "http://factory.test/api/v2/health" {
		t.Fatalf("headers %v %s", r.Header, r.URL)
	}
	if UserAgent != "pdigger-factory-sdk-go/1.1.0" || Version != "1.1.0" {
		t.Fatal("version")
	}
	// a control sends a JSON body
	got = nil
	_, _ = c.API().SessionPause(ctx, &ControlParams{DryRun: true})
	if r := got[0]; r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" ||
		r.URL.RawQuery != "dry_run=1" {
		t.Fatalf("control %s %v %s", r.Method, r.Header, r.URL)
	}
}

// failClock fails every sleep.
type failClock struct{ factorytest.Clock }

func (*failClock) Sleep(context.Context, time.Duration) error { return errBoom }

func TestRetryPolicy(t *testing.T) {
	ctx := testCtx(t)
	type step struct {
		status int
		body   string
		header []string
		err    error
	}
	cases := []struct {
		name     string
		method   string
		steps    []step
		opts     []Option
		wantErr  ErrorCode
		attempts int
		sleeps   []time.Duration
	}{
		{name: "503 then ok", method: "GET", steps: []step{{status: 503, body: `{"error":"x","code":"busy"}`}, {status: 200}},
			attempts: 2},
		{name: "retry_ms", method: "GET",
			steps:    []step{{status: 503, body: `{"error":"x","code":"world_unavailable","retry_ms":700}`}, {status: 200}},
			attempts: 2, sleeps: []time.Duration{700 * time.Millisecond}},
		{name: "Retry-After seconds", method: "GET",
			steps:    []step{{status: 429, body: `{"error":"x","code":"too_many_requests"}`, header: []string{"Retry-After", "2"}}, {status: 200}},
			attempts: 2, sleeps: []time.Duration{2 * time.Second}},
		{name: "Retry-After date in the past", method: "GET",
			steps: []step{{status: 502, body: `bad gateway`, header: []string{"Retry-After", "Mon, 02 Jan 2006 15:04:05 GMT"}},
				{status: 200}},
			attempts: 2, sleeps: []time.Duration{0}},
		{name: "Retry-After date ahead", method: "GET",
			steps: []step{{status: 504, body: ``, header: []string{"Retry-After", "Wed, 18 Nov 2026 12:00:03 GMT"}},
				{status: 200}},
			attempts: 2, sleeps: []time.Duration{3 * time.Second}},
		{name: "Retry-After junk", method: "GET",
			steps:    []step{{status: 503, body: ``, header: []string{"Retry-After", "soon"}}, {status: 200}},
			attempts: 2},
		{name: "exhausted", method: "GET", opts: []Option{WithRetries(2)},
			steps:   []step{{status: 503, body: `{"error":"x","code":"busy"}`}, {status: 503, body: `{"error":"x","code":"busy"}`}, {status: 503, body: `{"error":"x","code":"busy"}`}},
			wantErr: CodeBusy, attempts: 3},
		{name: "no retries", method: "GET", opts: []Option{WithRetries(0)},
			steps: []step{{status: 503, body: `{"error":"x","code":"busy"}`}}, wantErr: CodeBusy, attempts: 1},
		{name: "404 is final", method: "GET", steps: []step{{status: 404, body: `{"error":"x","code":"not_found"}`}},
			wantErr: CodeNotFound, attempts: 1},
		{name: "POST 503 is final", method: "POST", steps: []step{{status: 503, body: `{"error":"x","code":"busy"}`}},
			wantErr: CodeBusy, attempts: 1},
		{name: "POST 429 retried", method: "POST",
			steps:    []step{{status: 429, body: `{"error":"x","code":"too_many_controls","retry_ms":300}`}, {status: 200}},
			attempts: 2, sleeps: []time.Duration{300 * time.Millisecond}},
		{name: "GET connection error retried", method: "GET", steps: []step{{err: errBoom}, {status: 200}}, attempts: 2},
		{name: "POST connection error final", method: "POST", steps: []step{{err: errBoom}}, wantErr: CodeTransport,
			attempts: 1},
		{name: "backoff sleep fails", method: "GET", opts: []Option{WithClock(&failClock{})},
			steps: []step{{err: errBoom}}, wantErr: CodeTransport, attempts: 1},
		{name: "status sleep fails", method: "GET", opts: []Option{WithClock(&failClock{})},
			steps: []step{{status: 503, body: `{"error":"x","code":"busy"}`}}, wantErr: CodeTransport, attempts: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := 0
			c, clk := funcClient(t, func(r *http.Request) (*http.Response, error) {
				s := tc.steps[n]
				n++
				if s.err != nil {
					return nil, s.err
				}
				body := s.body
				if s.status == 200 {
					body = `{"ok":true,"worker":"running","viewers":0}`
				}
				return reply(s.status, body, s.header...), nil
			}, append([]Option{WithRand(rand.New(rand.NewPCG(1, 2)))}, tc.opts...)...)
			var err error
			if tc.method == "GET" {
				_, err = c.API().GetHealth(ctx)
			} else {
				_, err = c.API().SessionPlay(ctx, nil)
			}
			if tc.wantErr != "" {
				mustCode(t, err, tc.wantErr)
			} else if tc.method == "GET" && err != nil {
				t.Fatal(err)
			}
			if n != tc.attempts {
				t.Fatalf("attempts %d, want %d", n, tc.attempts)
			}
			if tc.sleeps != nil {
				if got := clk.Sleeps(); len(got) != len(tc.sleeps) || got[0] != tc.sleeps[0] {
					t.Fatalf("sleeps %v, want %v", got, tc.sleeps)
				}
			}
		})
	}
}

func TestBackoffJitterIsSeeded(t *testing.T) {
	b := Backoff{Base: 100 * time.Millisecond, Factor: 2, Max: 300 * time.Millisecond}
	if b.Delay(0, 0.5) != 50*time.Millisecond || b.Delay(5, 0.5) != 150*time.Millisecond || b.Delay(1, 0) != 0 {
		t.Fatal("delay")
	}
	// a base of 0 gives 0 at every attempt, also once the power overflows (0 * +Inf would be NaN)
	zero := Backoff{Base: 0, Factor: 2, Max: 5 * time.Second}
	for _, attempt := range []int{0, 64, 1024, math.MaxInt} {
		if d := zero.Delay(attempt, 0.5); d != 0 {
			t.Fatal(attempt, d)
		}
	}
	a, _ := New("http://x", WithRand(rand.New(rand.NewPCG(7, 7))), WithBackoff(b))
	c, _ := New("http://x", WithRand(rand.New(rand.NewPCG(7, 7))), WithBackoff(b))
	for i := 0; i < 4; i++ {
		if a.backoffDelay(i) != c.backoffDelay(i) {
			t.Fatal("seeded jitter differs")
		}
	}
	d, _ := New("http://x")
	if v := d.jitter(); v < 0 || v >= 1 {
		t.Fatal("jitter range")
	}
}

func TestCancelAndTimeouts(t *testing.T) {
	// a context cancelled during the request: not retried, not retryable
	ctx, cancel := context.WithCancel(context.Background())
	n := 0
	c, _ := funcClient(t, func(r *http.Request) (*http.Response, error) {
		n++
		cancel()
		return nil, r.Context().Err()
	})
	_, err := c.API().GetHealth(ctx)
	if e := mustCode(t, err, CodeTransport); e.IsRetryable() || !errors.Is(err, context.Canceled) || n != 1 {
		t.Fatalf("%v (%d attempts)", err, n)
	}
	// cancelled while waiting to retry a 503
	ctx, cancel = context.WithCancel(context.Background())
	c, _ = funcClient(t, func(r *http.Request) (*http.Response, error) {
		cancel()
		return reply(503, `{"error":"x","code":"busy"}`), nil
	})
	_, err = c.API().GetHealth(ctx)
	if e := mustCode(t, err, CodeTransport); e.IsRetryable() {
		t.Fatal(err)
	}
	// a request timeout: every attempt times out, and the error is retryable
	block := func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	c, _ = funcClient(t, block, WithTimeout(5*time.Millisecond), WithRetries(1))
	_, err = c.API().GetHealth(testCtx(t))
	if e := mustCode(t, err, CodeTransport); !e.IsRetryable() || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	// a stream's timeout covers the reply headers
	c, _ = funcClient(t, block, WithTimeout(5*time.Millisecond), WithRetries(0))
	for _, err := range c.API().StreamFrames(testCtx(t), nil) {
		if e := mustCode(t, err, CodeTransport); !e.IsRetryable() {
			t.Fatal(err)
		}
	}
	// no timeout at all
	c, _ = funcClient(t, func(r *http.Request) (*http.Response, error) {
		if _, ok := r.Context().Deadline(); ok {
			t.Error("a deadline without a timeout")
		}
		return reply(200, `{"ok":true}`), nil
	}, WithTimeout(0))
	if _, err := c.API().GetHealth(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestBadRequestAndBodies(t *testing.T) {
	c, _ := funcClient(t, func(r *http.Request) (*http.Response, error) { return reply(200, `{}`), nil })
	ctx := testCtx(t)
	if _, _, err := c.send(ctx, &request{op: "x", method: "BAD METHOD", path: "/"}); !HasCode(err, CodeTransport) {
		t.Fatalf("bad method: %v", err)
	}
	cases := []struct {
		resp *http.Response
		code ErrorCode
		msg  string
	}{
		{reply(502, "<html>bad gateway</html>"), CodeInternal, "Bad Gateway: <html>bad gateway</html>"},
		{reply(500, ""), CodeInternal, "Internal Server Error"},
		{reply(500, strings.Repeat("x", 300)), CodeInternal, "Internal Server Error: " + strings.Repeat("x", 200)},
		{&http.Response{StatusCode: 500, Header: http.Header{}, Body: errReader{errBoom}}, CodeInternal, "Internal Server Error"},
		{reply(400, "<html>proxy</html>"), CodeInvalidParam, "Bad Request: <html>proxy</html>"},
		{reply(403, ""), CodeCrossOrigin, "Forbidden"},
		{reply(404, ""), CodeNotFound, "Not Found"},
		{reply(405, ""), CodeMethodNotAllowed, "Method Not Allowed"},
		{reply(413, ""), CodeBodyTooLarge, "Request Entity Too Large"},
		{reply(415, ""), CodeJSONOnly, "Unsupported Media Type"},
		{reply(416, "", "Content-Range", "bytes */9"), CodeInvalidParam, "Requested Range Not Satisfiable"},
		{reply(418, ""), CodeDecode, "I'm a teapot"},
		{reply(429, ""), CodeTooManyRequests, "Too Many Requests"},
		{reply(503, ""), CodeNotReady, "Service Unavailable"},
		{reply(400, `{"error":"stone_id must be an integer","code":"invalid_type","field":"stone_id"}`), CodeInvalidType,
			"stone_id must be an integer"},
		{reply(503, `{"error":"starting","code":"not_ready","waiting":["engine"]}`), CodeNotReady, "starting"},
	}
	for _, tc := range cases {
		e := readError("op", tc.resp, time.Time{})
		if e.Code != tc.code || e.Message != tc.msg || e.Status != tc.resp.StatusCode {
			t.Errorf("%+v", e)
		}
	}
	if e := readError("op", reply(416, ""), time.Time{}); e.Field != "range" {
		t.Errorf("a 416 names the field range: %+v", e)
	}
	if e := readError("op", reply(503, `{"error":"starting","code":"not_ready","waiting":["engine"]}`), time.Time{}); len(e.Waiting) != 1 {
		t.Fatal("waiting")
	}
	// a reply body that fails, an empty body, a body that is not JSON
	for _, resp := range []*http.Response{
		{StatusCode: 200, Header: http.Header{}, Body: errReader{errBoom}},
		reply(200, "  "),
		reply(200, "{"),
	} {
		c, _ := funcClient(t, func(r *http.Request) (*http.Response, error) { return resp, nil })
		_, err := c.API().GetHealth(ctx)
		if e := AsError(err); e == nil || (e.Code != CodeTransport && e.Code != CodeDecode) {
			t.Errorf("%v", err)
		}
	}
	if _, err := jsonBody(make(chan int)); err == nil {
		t.Fatal("jsonBody")
	}
	// a 304 is not decoded
	c, _ = funcClient(t, func(r *http.Request) (*http.Response, error) {
		return reply(304, "", "ETag", `"v1"`), nil
	})
	list, err := c.API().ListVideos(ctx, &ListVideosParams{IfNoneMatch: `"v1"`})
	if err != nil || !list.NotModified || list.ETag != `"v1"` {
		t.Fatalf("%+v %v", list, err)
	}
}

func TestErrorType(t *testing.T) {
	ms := int64(250)
	e := &Error{Status: 429, Code: CodeTooManyControls, Message: "too many controls; wait a moment", Field: "time",
		RetryMS: &ms, Op: "sessionSeek", Err: errBoom}
	want := "pdiggerfactory: sessionSeek: 429 too_many_controls: too many controls; wait a moment (field time): boom"
	if e.Error() != want || !e.IsRetryable() || !errors.Is(e, errBoom) || e.Unwrap() != errBoom {
		t.Fatalf("%q", e.Error())
	}
	if d, ok := e.RetryDelay(); !ok || d != 250*time.Millisecond {
		t.Fatal("RetryDelay")
	}
	if _, ok := (&Error{}).RetryDelay(); ok {
		t.Fatal("no hint")
	}
	if s := (&Error{Code: CodeTransport, Err: errBoom}).Error(); s != "pdiggerfactory: transport: boom" {
		t.Fatalf("%q", s)
	}
	if s := (&Error{Code: CodeDecode, Message: "x: boom", Err: errBoom}).Error(); s != "pdiggerfactory: decode: x: boom" {
		t.Fatalf("%q", s)
	}
	for status, want := range map[int]bool{429: true, 502: true, 503: true, 504: true, 500: false, 404: false} {
		if (&Error{Status: status, Code: CodeInternal}).IsRetryable() != want {
			t.Errorf("%d", status)
		}
	}
	if (&Error{Code: CodeTransport}).IsRetryable() != true || (&Error{Code: CodeTransport, canceled: true}).IsRetryable() {
		t.Fatal("transport retryable")
	}
	wrapped := errors.Join(errors.New("context"), e)
	if AsError(wrapped) != e || !IsRetryable(wrapped) || !HasCode(wrapped, CodeTooManyControls) {
		t.Fatal("wrapped")
	}
	if AsError(errBoom) != nil || IsRetryable(errBoom) || HasCode(nil, CodeBusy) || IsNotRetained(nil) || IsSessionGone(errBoom) {
		t.Fatal("not ours")
	}
	if l := localError("seek", CodeNotSeekable, "time", "no"); l.Status != 0 || l.Op != "seek" {
		t.Fatal("localError")
	}
}

func TestSystemClock(t *testing.T) {
	var c SystemClock
	if time.Since(c.Now()) > time.Second {
		t.Fatal("Now")
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := c.Sleep(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if err := c.Sleep(ctx, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := c.Sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := c.Sleep(ctx, -1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if epoch(time.Unix(12, 500000000)) != 12.5 {
		t.Fatal("epoch")
	}
	if *Bool(true) != true || *Int64(3) != 3 || *Float64(1.5) != 1.5 {
		t.Fatal("pointer helpers")
	}
}

// TestConcurrentClient uses one client from many goroutines (run with -race).
func TestConcurrentClient(t *testing.T) {
	f := newFake(t, factorytest.WithReplies(fixturesAPI))
	c, _ := fakeClient(t, f)
	ctx := testCtx(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.API().GetHealth(ctx); err != nil {
				t.Error(err)
			}
			_ = c.backoffDelay(1)
		}()
	}
	wg.Wait()
}

// TestBodyReadRetries: a GET whose reply body breaks off is tried again like one whose connection failed; a control
// is not (it may have been applied).
func TestBodyReadRetries(t *testing.T) {
	ctx := testCtx(t)
	broken := func() *http.Response {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}},
			Body: errReader{io.ErrUnexpectedEOF}}
	}
	n := 0
	c, clk := funcClient(t, func(r *http.Request) (*http.Response, error) {
		if n++; n == 1 {
			return broken(), nil
		}
		return reply(200, `{"ok":true,"worker":"running","viewers":1}`), nil
	})
	if h, err := c.API().GetHealth(ctx); err != nil || !h.OK || n != 2 || len(clk.Sleeps()) != 1 {
		t.Fatal(h, err, n, clk.Sleeps())
	}
	n = 0
	c, _ = funcClient(t, func(r *http.Request) (*http.Response, error) { n++; return broken(), nil })
	if _, err := c.API().SessionPlay(ctx, nil); !HasCode(err, CodeTransport) || n != 1 {
		t.Fatal(err, n)
	}
	// every try breaks off: the retries are used up
	n = 0
	if _, err := c.API().GetHealth(ctx); !HasCode(err, CodeTransport) || n != 1+DefaultRetries ||
		!errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err, n)
	}
}
