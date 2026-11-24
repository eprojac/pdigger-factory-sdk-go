// Package pfr reads recordings in the pfr1 format: NDJSON (optionally gzip), a header line, then frames and side
// events in the order received. It is shared by the SDK's Replay and the factorytest fake.
package pfr

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
)

// Format is the recording format name of the header's "recording" field.
const Format = "pfr1"

// maxErrorBody is how much of an error reply's body Open keeps.
const maxErrorBody = 64 << 10

// Source is an open recording: Read returns its bytes, gunzipped when the file starts with the gzip magic bytes.
// Close it when done; Abort ends its reads from another goroutine.
type Source struct {
	io.Reader
	raw     *rawReader
	closers []io.Closer
	abort   func()
}

// Close closes the decompressor and the file or the HTTP body, and ends the request.
func (s *Source) Close() error {
	var first error
	for _, c := range s.closers {
		if err := c.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Abort ends the reads of the recording at once, from any goroutine: it cancels the HTTP request, or closes the
// file, so a Read waiting on a stalled connection returns with an error. The Source must still be closed.
func (s *Source) Abort() { s.abort() }

// ReadErr is the first error that the file or the HTTP body itself returned (io.EOF is not one), nil when there was
// none. It tells a read that failed (a dropped connection, a cancelled request, an I/O error) from bytes that are not
// a recording (a gzip stream that is corrupt or cut short). Call it from the goroutine that reads.
func (s *Source) ReadErr() error { return s.raw.err }

// rawReader is the file or the HTTP body; it keeps the first error it returned.
type rawReader struct {
	r   io.Reader
	err error
}

func (r *rawReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	if err != nil && err != io.EOF && r.err == nil {
		r.err = err
	}
	return n, err
}

// onceCloser closes once, so that an Abort and the Close after it close a file once.
type onceCloser struct {
	once sync.Once
	c    io.Closer
	err  error
}

func (o *onceCloser) Close() error {
	o.once.Do(func() { o.err = o.c.Close() })
	return o.err
}

// closeFunc is a func as an io.Closer.
type closeFunc func()

func (f closeFunc) Close() error {
	f()
	return nil
}

// StatusError is the reply of a recording URL that answered a status outside 2xx (any success is read).
type StatusError struct {
	URL    string
	Status int
	Header http.Header
	// Body is the start of the reply's body (64 KiB at most).
	Body []byte
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("GET %s: %d %s", e.URL, e.Status, http.StatusText(e.Status))
}

// IsURL reports whether src is an http(s) URL.
func IsURL(src string) bool {
	return strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://")
}

// Open opens a recording from a local path or an http(s) URL (streamed, with userAgent as the User-Agent when it is
// not empty), and gunzips it when it starts with the gzip magic bytes. ctx bounds the opening only (the connection,
// the reply's headers, the gzip header): once Open has returned, ending ctx does not end the reads, and a reader that
// must stop with a context ties Abort to it (context.AfterFunc). A URL that answers a status outside 2xx is a
// *StatusError; any 2xx is read (a 204 as an empty recording).
func Open(ctx context.Context, src string, hc *http.Client, userAgent string) (*Source, error) {
	if !IsURL(src) {
		f, err := os.Open(src)
		if err != nil {
			return nil, err
		}
		return Decompress(f)
	}
	// The request lives until the Source is closed or aborted; ctx ends it only while Open runs.
	rctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	stop := context.AfterFunc(ctx, cancel)
	s, err := openURL(rctx, src, hc, userAgent, cancel)
	if !stop() { // ctx ended while the recording was being opened
		if err == nil {
			s.Close()
		}
		cancel()
		return nil, ctx.Err()
	}
	if err != nil {
		cancel()
		return nil, err
	}
	return s, nil
}

func openURL(ctx context.Context, src string, hc *http.Client, userAgent string, cancel func()) (*Source, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return nil, err
	}
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 { // any success, as the other languages read a recording URL
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, &StatusError{URL: src, Status: resp.StatusCode, Header: resp.Header, Body: body}
	}
	return newSource(resp.Body, cancel, resp.Body, closeFunc(cancel))
}

// Decompress reads r, gunzipped when it starts with the gzip magic bytes. Abort and Close close r (once).
func Decompress(r io.ReadCloser) (*Source, error) {
	oc := &onceCloser{c: r}
	return newSource(r, func() { _ = oc.Close() }, oc)
}

// FormatError: the bytes start like gzip, but its header is corrupt or cut short; they are not a recording.
type FormatError struct{ Err error }

func (e *FormatError) Error() string { return "not a gzip stream: " + e.Err.Error() }

// Unwrap returns the gzip error.
func (e *FormatError) Unwrap() error { return e.Err }

// newSource reads r, gunzipped when it starts with the gzip magic bytes; abort ends its reads and closers close it.
func newSource(r io.Reader, abort func(), closers ...io.Closer) (*Source, error) {
	raw := &rawReader{r: r}
	br := bufio.NewReaderSize(raw, 256<<10)
	s := &Source{Reader: br, raw: raw, closers: closers, abort: abort}
	magic, err := br.Peek(2)
	if err == nil && magic[0] == 0x1f && magic[1] == 0x8b {
		gz, err := gzip.NewReader(br)
		if err != nil {
			s.Close()
			if raw.err == nil { // the file or the connection was fine: the bytes are wrong
				return nil, &FormatError{Err: err}
			}
			return nil, err
		}
		s.Reader = gz
		s.closers = append([]io.Closer{gz}, closers...)
	}
	return s, nil
}

// Reader returns the non-empty lines of a recording, trimmed.
type Reader struct {
	br *bufio.Reader
}

// NewReader reads lines from r.
func NewReader(r io.Reader) *Reader {
	return &Reader{br: bufio.NewReaderSize(r, 256<<10)}
}

// Next returns the next non-empty line, or io.EOF.
func (r *Reader) Next() ([]byte, error) {
	for {
		line, err := r.br.ReadBytes('\n')
		line = bytes.TrimSpace(line)
		if len(line) > 0 {
			return line, nil
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, err
		}
	}
}

// Line is a classified line of a recording.
type Line struct {
	// Bytes is the line.
	Bytes []byte
	// Recording: the line has a "recording" key (the header, when it is the first such line).
	Recording bool
	// SSE is the side event name of an {"sse": ..., "data": ..., "recv": ...} line ("" for other lines); IsSide is
	// set for every line with an "sse" key.
	SSE    string
	IsSide bool
	// IsFrame: the line has "g" (a number) and "cursor".
	IsFrame bool
	// Data is the side event's data.
	Data json.RawMessage
	// Recv is the side event's receive time (epoch seconds), 0 when absent.
	Recv float64
}

type probe struct {
	Recording json.RawMessage `json:"recording"`
	SSE       json.RawMessage `json:"sse"`
	G         json.RawMessage `json:"g"`
	Cursor    json.RawMessage `json:"cursor"`
	Data      json.RawMessage `json:"data"`
	Recv      *float64        `json:"recv"`
}

// Classify parses a line's keys. It returns an error for a line that is not a JSON object.
func Classify(b []byte) (Line, error) {
	var p probe
	if err := json.Unmarshal(b, &p); err != nil {
		return Line{}, err
	}
	l := Line{Bytes: b, Recording: p.Recording != nil}
	if p.SSE != nil {
		l.IsSide = true
		_ = json.Unmarshal(p.SSE, &l.SSE)
		l.Data = p.Data
		if p.Recv != nil {
			l.Recv = *p.Recv
		}
		return l, nil
	}
	if p.G != nil && p.Cursor != nil {
		var g float64
		l.IsFrame = json.Unmarshal(p.G, &g) == nil && !bytes.Equal(bytes.TrimSpace(p.G), []byte("null"))
	}
	return l, nil
}
