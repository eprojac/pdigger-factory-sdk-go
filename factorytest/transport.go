package factorytest

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
)

// transport serves requests in process: the handler runs in its own goroutine and writes into a pipe the response
// body reads from, so streams arrive as they are written. A closed body or a cancelled request cancels the
// handler's context; a handler that panics with http.ErrAbortHandler drops the connection (the body ends with
// io.ErrUnexpectedEOF).
type transport struct{ h http.Handler }

// RoundTrip implements http.RoundTripper.
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	var reqBody []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("factorytest: reading the request body: %w", err)
		}
		reqBody = b
	}
	ctx, cancel := context.WithCancel(req.Context())
	sreq := req.Clone(ctx)
	sreq.Body = io.NopCloser(bytes.NewReader(reqBody))
	sreq.ContentLength = int64(len(reqBody))
	sreq.RequestURI = req.URL.RequestURI()
	sreq.RemoteAddr = "192.0.2.1:41200"
	if sreq.Host == "" {
		sreq.Host = req.URL.Host
	}
	pr, pw := io.Pipe()
	rw := &responseWriter{header: http.Header{}, pw: pw, ready: make(chan struct{}),
		head: req.Method == http.MethodHead}
	stop := context.AfterFunc(ctx, func() { _ = pr.CloseWithError(ctx.Err()) })
	go func() {
		var end error
		defer func() {
			if v := recover(); v != nil {
				end = io.ErrUnexpectedEOF
				if v != http.ErrAbortHandler {
					end = fmt.Errorf("factorytest: handler panic: %v", v)
				}
			}
			if ctx.Err() != nil {
				end = ctx.Err()
			}
			rw.finish(end)
			stop()
		}()
		t.h.ServeHTTP(rw, sreq)
	}()
	select {
	case <-rw.ready:
	case <-ctx.Done():
		cancel()
		return nil, ctx.Err()
	}
	if rw.err != nil {
		cancel()
		return nil, rw.err
	}
	resp := &http.Response{
		Status:        strconv.Itoa(rw.status) + " " + http.StatusText(rw.status),
		StatusCode:    rw.status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        rw.header.Clone(),
		Body:          &body{pr: pr, ctx: ctx, cancel: cancel},
		ContentLength: -1,
		Request:       req,
	}
	if n, err := strconv.ParseInt(rw.header.Get("Content-Length"), 10, 64); err == nil {
		resp.ContentLength = n
	}
	return resp, nil
}

// responseWriter is the handler's side of the pipe.
type responseWriter struct {
	header http.Header
	pw     *io.PipeWriter
	head   bool

	once   sync.Once
	ready  chan struct{} // closed when the status is known (or the handler failed first)
	status int
	err    error // set before ready closes when the handler ended without a status
}

func (w *responseWriter) Header() http.Header { return w.header }

func (w *responseWriter) WriteHeader(code int) {
	w.once.Do(func() {
		w.status = code
		close(w.ready)
	})
}

func (w *responseWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	if w.head {
		return len(p), nil
	}
	return w.pw.Write(p)
}

// Flush sends the status if not sent; written bytes are already delivered (the pipe has no buffer).
func (w *responseWriter) Flush() { w.WriteHeader(http.StatusOK) }

// finish ends the body with err (nil: EOF). A handler that failed before a status makes RoundTrip fail.
func (w *responseWriter) finish(err error) {
	w.once.Do(func() {
		if err != nil {
			w.err = err
		} else {
			w.status = http.StatusOK
		}
		close(w.ready)
	})
	_ = w.pw.CloseWithError(err)
}

// body is the client's side of the pipe.
type body struct {
	pr     *io.PipeReader
	ctx    context.Context
	cancel context.CancelFunc
}

func (b *body) Read(p []byte) (int, error) {
	n, err := b.pr.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && b.ctx.Err() != nil {
		err = b.ctx.Err()
	}
	return n, err
}

func (b *body) Close() error {
	b.cancel()
	return b.pr.Close()
}
