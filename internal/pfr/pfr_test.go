package pfr

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
)

var errBoom = errors.New("boom")

type failCloser struct{ err error }

func (c failCloser) Close() error { return c.err }

type badReader struct{}

func (badReader) Read([]byte) (int, error) { return 0, errBoom }
func (badReader) Close() error             { return nil }

func gz(s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	w.Write([]byte(s))
	w.Close()
	return b.Bytes()
}

func readAll(t *testing.T, rc io.ReadCloser) []string {
	t.Helper()
	defer rc.Close()
	r := NewReader(rc)
	var out []string
	for {
		l, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, string(l))
	}
}

func TestOpenFilesAndURLs(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	plain := filepath.Join(dir, "a.ndjson")
	os.WriteFile(plain, []byte("{\"a\":1}\n\n  \n{\"b\":2}"), 0o644)
	zipped := filepath.Join(dir, "a.ndjson.gz")
	os.WriteFile(zipped, gz("{\"a\":1}\n{\"b\":2}\n"), 0o644)
	for _, p := range []string{plain, zipped} {
		rc, err := Open(ctx, p, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, rc); strings.Join(got, "|") != `{"a":1}|{"b":2}` {
			t.Fatalf("%s: %v", p, got)
		}
	}
	if _, err := Open(ctx, filepath.Join(dir, "none"), nil, ""); err == nil {
		t.Fatal("a missing file")
	}
	bad := filepath.Join(dir, "bad.gz")
	os.WriteFile(bad, []byte{0x1f, 0x8b, 0, 0}, 0o644)
	if _, err := Open(ctx, bad, nil, ""); err == nil {
		t.Fatal("a bad gzip header")
	}
	var mu sync.Mutex
	var agents []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		agents = append(agents, r.UserAgent())
		mu.Unlock()
		if r.URL.Path != "/r.ndjson.gz" {
			http.NotFound(w, r)
			return
		}
		w.Write(gz("{\"g\":1,\"cursor\":1}\n"))
	}))
	defer srv.Close()
	for _, hc := range []*http.Client{nil, srv.Client()} {
		rc, err := Open(ctx, srv.URL+"/r.ndjson.gz", hc, "pdigger-factory-sdk-go/test")
		if err != nil {
			t.Fatal(err)
		}
		if got := readAll(t, rc); len(got) != 1 {
			t.Fatal(got)
		}
	}
	mu.Lock()
	if len(agents) != 2 || agents[0] != "pdigger-factory-sdk-go/test" || agents[1] != agents[0] {
		t.Fatal(agents)
	}
	mu.Unlock()
	_, err := Open(ctx, srv.URL+"/missing", nil, "")
	var se *StatusError
	if !errors.As(err, &se) || se.Status != 404 || !strings.Contains(string(se.Body), "404 page not found") ||
		err.Error() != "GET "+srv.URL+"/missing: 404 Not Found" {
		t.Fatal(err)
	}
	if _, err := Open(ctx, "http://[::1", nil, ""); err == nil {
		t.Fatal("a bad URL")
	}
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	if _, err := Open(ctx, closed.URL+"/r", nil, ""); err == nil {
		t.Fatal("a closed server")
	}
	if !IsURL("https://factory.pdigger.com/developers/data/dry1.world.ndjson.gz") || IsURL("dry1.ndjson") {
		t.Fatal("IsURL")
	}
}

func TestReaderAndClose(t *testing.T) {
	if _, err := NewReader(badReader{}).Next(); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	rc := &Source{Reader: strings.NewReader(""), closers: []io.Closer{failCloser{errBoom}, failCloser{errors.New("second")}}}
	if err := rc.Close(); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	// a stream too short to peek is passed through
	r, err := Decompress(io.NopCloser(strings.NewReader("x")))
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := io.ReadAll(r); string(b) != "x" {
		t.Fatal(string(b))
	}
}

func TestClassify(t *testing.T) {
	if _, err := Classify([]byte("[1]")); err == nil {
		t.Fatal("an array")
	}
	for line, want := range map[string]Line{
		`{"recording":"pfr1","frames":3}`:                  {Recording: true},
		`{"sse":"clock","data":{"session":1},"recv":12.5}`: {IsSide: true, SSE: "clock", Recv: 12.5},
		`{"sse":5,"data":{}}`:                              {IsSide: true},
		`{"cursor":1,"g":7}`:                               {IsFrame: true},
		`{"cursor":1,"g":null}`:                            {},
		`{"cursor":1,"g":"x"}`:                             {},
		`{"g":7}`:                                          {},
	} {
		l, err := Classify([]byte(line))
		if err != nil || l.Recording != want.Recording || l.IsSide != want.IsSide || l.SSE != want.SSE ||
			l.Recv != want.Recv || l.IsFrame != want.IsFrame || string(l.Bytes) != line {
			t.Errorf("%s: %+v %v", line, l, err)
		}
	}
	if l, _ := Classify([]byte(`{"sse":"gap","data":{"gap":3}}`)); string(l.Data) != `{"gap":3}` {
		t.Fatal(string(l.Data))
	}
}

// ctxBody fails its reads once its context has ended, as an HTTP body does; it serves data and then stalls until
// then, closing stalled.
type ctxBody struct {
	ctx     context.Context
	data    *strings.Reader
	once    sync.Once
	stalled chan struct{}
}

func (b *ctxBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if b.data.Len() > 0 {
		return b.data.Read(p)
	}
	b.once.Do(func() { close(b.stalled) })
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *ctxBody) Close() error { return nil }

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stallingClient(data string, stalled chan struct{}) *http.Client {
	return &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{},
			Body: &ctxBody{ctx: r.Context(), data: strings.NewReader(data), stalled: stalled}}, nil
	})}
}

// TestOpenContextAndAbort: ctx bounds the opening only; Abort ends a read that waits on a stalled connection, and
// ReadErr tells the failed read from a bad gzip stream.
func TestOpenContextAndAbort(t *testing.T) {
	// the context ends after Open returned: the reads go on
	ctx, cancel := context.WithCancel(context.Background())
	stalled := make(chan struct{})
	s, err := Open(ctx, "http://data.test/r.ndjson", stallingClient("{\"a\":1}\n{\"b\":2}\n", stalled), "")
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	r := NewReader(s)
	for _, want := range []string{`{"a":1}`, `{"b":2}`} {
		if l, err := r.Next(); err != nil || string(l) != want {
			t.Fatal(string(l), err)
		}
	}
	// the next read stalls until Abort ends the request
	go func() {
		<-stalled
		s.Abort()
	}()
	if _, err := r.Next(); !errors.Is(err, context.Canceled) || !errors.Is(s.ReadErr(), context.Canceled) {
		t.Fatal(err, s.ReadErr())
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// the context ends while the recording is opened: the request ends, or the reply that came is closed
	for _, wait := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		hc := &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			cancel()
			if wait {
				<-r.Context().Done()
				return nil, r.Context().Err()
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("{}\n"))}, nil
		})}
		if _, err := Open(ctx, "http://data.test/r.ndjson", hc, ""); !errors.Is(err, context.Canceled) {
			t.Fatal(wait, err)
		}
	}
	// a file: Abort closes it, and a read after that fails (the error is the file's, not the data's)
	p := filepath.Join(t.TempDir(), "a.ndjson")
	os.WriteFile(p, []byte("{\"a\":1}\n"), 0o644)
	f, err := Open(context.Background(), p, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	f.Abort()
	if _, err := io.ReadAll(f); err == nil || f.ReadErr() == nil {
		t.Fatal("a read after Abort")
	}
	if err := f.Close(); err != nil {
		t.Fatal("Close after Abort:", err)
	}
	// a gzip stream cut short: the error is the data's
	b := gz(strings.Repeat("{\"a\":1}\n", 1000))
	os.WriteFile(p, b[:len(b)/2], 0o644)
	f, err = Open(context.Background(), p, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := io.ReadAll(f); !errors.Is(err, io.ErrUnexpectedEOF) || f.ReadErr() != nil {
		t.Fatal(err, f.ReadErr())
	}
}

// TestGzipHeaderReadError: a gzip header that cannot be read because the file or the connection failed is that
// failure, not a FormatError.
func TestGzipHeaderReadError(t *testing.T) {
	boom := errors.New("boom")
	_, err := Decompress(io.NopCloser(io.MultiReader(bytes.NewReader([]byte{0x1f, 0x8b}), iotest.ErrReader(boom))))
	var fe *FormatError
	if !errors.Is(err, boom) || errors.As(err, &fe) {
		t.Fatal(err)
	}
}
