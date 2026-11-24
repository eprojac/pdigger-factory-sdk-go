package pdiggerfactory

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// Test helpers of the package's internal tests.

const fixturesAPI = "fixtures/api"

// recording is the path of a fixture recording.
func recording(name string) string {
	m, _ := filepath.Glob(filepath.Join("fixtures", "recordings", name+".ndjson*"))
	if len(m) != 1 {
		panic("no recording " + name)
	}
	return m[0]
}

// testCtx is a context that ends with the test (and after 2 min at most).
func testCtx(t *testing.T) context.Context {
	t.Helper()
	// a guard against a hang, not a speed test: under the race detector on a two-core CI runner the fake's stream of
	// the longest fixture recording takes about 20 s
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	return ctx
}

// newFake makes a fake server or fails the test.
func newFake(t *testing.T, opts ...factorytest.Option) *factorytest.Fake {
	t.Helper()
	f, err := factorytest.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// fakeClient is a client of a fake server with a manual clock and a seeded jitter source.
func fakeClient(t *testing.T, f *factorytest.Fake, opts ...Option) (*Client, *factorytest.Clock) {
	t.Helper()
	clk := factorytest.NewClock(time.Time{})
	all := append([]Option{WithTransport(f.Transport()), WithClock(clk)}, opts...)
	c, err := New(f.URL(), all...)
	if err != nil {
		t.Fatal(err)
	}
	return c, clk
}

// roundTripFunc is an http.RoundTripper made of a func.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// reply is a canned http.Response.
func reply(status int, body string, header ...string) *http.Response {
	h := http.Header{}
	for i := 0; i+1 < len(header); i += 2 {
		h.Set(header[i], header[i+1])
	}
	if h.Get("Content-Type") == "" {
		h.Set("Content-Type", "application/json")
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body))}
}

// errReader fails every read.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }
func (r errReader) Close() error             { return nil }

var errBoom = errors.New("boom")

// funcClient is a client over a RoundTripper func with a manual clock.
func funcClient(t *testing.T, rt roundTripFunc, opts ...Option) (*Client, *factorytest.Clock) {
	t.Helper()
	clk := factorytest.NewClock(time.Time{})
	all := append([]Option{WithTransport(rt), WithClock(clk)}, opts...)
	c, err := New("http://factory.test", all...)
	if err != nil {
		t.Fatal(err)
	}
	return c, clk
}

// mustCode fails unless err is an *Error with the code.
func mustCode(t *testing.T, err error, code ErrorCode) *Error {
	t.Helper()
	e := AsError(err)
	if e == nil || e.Code != code {
		t.Fatalf("want code %s, got %v", code, err)
	}
	return e
}

// collect takes n items of a stream (n <= 0: until it ends) and fails on the first error.
func collect[T any](t *testing.T, seq func(func(T, error) bool), n int) []T {
	t.Helper()
	var out []T
	for v, err := range seq {
		if err != nil {
			t.Fatalf("after %d items: %v", len(out), err)
		}
		out = append(out, v)
		if n > 0 && len(out) == n {
			break
		}
	}
	return out
}
