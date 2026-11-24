package pdiggerfactory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// request is one API request.
type request struct {
	op      string
	method  string
	path    string // escaped, from the API root, e.g. "/api/v2/videos/dry1"
	query   url.Values
	header  http.Header
	body    []byte
	hasBody bool
	accept  string
	noRetry bool
	stream  bool // a stream: the timeout covers the reply headers only
	// check runs on a success reply before its body is read; an error closes the reply unread and is returned
	// (no retry).
	check func(*http.Response) error
}

const maxErrorBody = 64 << 10

// idempotent methods are retried on connection errors and on 429, 502, 503 and 504.
func idempotent(method string) bool { return method == http.MethodGet || method == http.MethodHead }

// retryStatus reports whether a reply status may be retried for this method. A POST, PUT or DELETE is retried only
// on 429: the server checks rate limits before it applies anything, so a 429 is never half-applied.
func retryStatus(method string, status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	if !idempotent(method) {
		return false
	}
	switch status {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// retryAfter parses a Retry-After header (seconds, or an HTTP date).
func retryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return time.Duration(s) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		d := t.Sub(now)
		if d < 0 {
			d = 0
		}
		return d, true
	}
	return 0, false
}

// transportError wraps an error that got no HTTP reply.
func transportError(op string, ctx context.Context, err error) *Error {
	e := &Error{Op: op, Code: CodeTransport, Err: err}
	if ctx.Err() != nil {
		e.Err = ctx.Err()
		e.canceled = true
	}
	return e
}

// send performs r, retrying as section 2.1 says. On success it returns the reply (status below 400) with its body
// open and the attempt's cancel func, which the caller calls after closing the body.
func (c *Client) send(ctx context.Context, r *request) (*http.Response, context.CancelFunc, error) {
	resp, _, cancel, err := c.do(ctx, r, false)
	return resp, cancel, err
}

// do performs r, retrying as section 2.1 says: GET and HEAD on connection errors and on 429, 502, 503 and 504, every
// method on 429, waiting the server's hint (retry_ms, else Retry-After), else the backoff. With read it also reads the
// reply body inside the attempt, so a GET whose body breaks off is tried again like one whose connection failed, and
// returns the body (the attempt is then done: the cancel func is nil); without, the body is left open.
func (c *Client) do(ctx context.Context, r *request, read bool) (*http.Response, []byte, context.CancelFunc, error) {
	for attempt := 0; ; attempt++ {
		canRetry := !r.noRetry && attempt < c.retries
		resp, cancel, err := c.attempt(ctx, r)
		var body []byte
		if err == nil && read && resp.StatusCode < 400 && r.check != nil {
			if cerr := r.check(resp); cerr != nil {
				resp.Body.Close()
				cancel()
				return nil, nil, nil, cerr
			}
		}
		if err == nil && read && resp.StatusCode < 400 {
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			cancel()
			cancel = nil
		}
		if err != nil {
			terr := transportError(r.op, ctx, err)
			if terr.canceled || !canRetry || !idempotent(r.method) {
				return nil, nil, nil, terr
			}
			if serr := c.sleep(ctx, c.backoffDelay(attempt)); serr != nil {
				return nil, nil, nil, transportError(r.op, ctx, serr)
			}
			continue
		}
		if resp.StatusCode < 400 {
			return resp, body, cancel, nil
		}
		apiErr := readError(r.op, resp, c.clock.Now())
		cancel()
		if !canRetry || !retryStatus(r.method, resp.StatusCode) {
			return nil, nil, nil, apiErr
		}
		wait, ok := apiErr.RetryDelay()
		if !ok {
			wait = c.backoffDelay(attempt)
		}
		if serr := c.sleep(ctx, wait); serr != nil {
			return nil, nil, nil, transportError(r.op, ctx, serr)
		}
	}
}

// attempt performs one try of r.
func (c *Client) attempt(ctx context.Context, r *request) (*http.Response, context.CancelFunc, error) {
	var actx context.Context
	var cancel context.CancelFunc
	var timer *time.Timer
	switch {
	case c.timeout <= 0:
		actx, cancel = context.WithCancel(ctx)
	case r.stream:
		actx, cancel = context.WithCancel(ctx)
		timer = time.AfterFunc(c.timeout, cancel)
	default:
		actx, cancel = context.WithTimeout(ctx, c.timeout)
	}
	req, err := c.newHTTPRequest(actx, r)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	resp, err := c.hc.Do(req)
	if timer != nil {
		timer.Stop()
	}
	if err != nil {
		cancel()
		return nil, nil, err
	}
	return resp, cancel, nil
}

func (c *Client) newHTTPRequest(ctx context.Context, r *request) (*http.Request, error) {
	u := c.base.String() + r.path
	if len(r.query) > 0 {
		u += "?" + r.query.Encode()
	}
	var body io.Reader
	if r.hasBody {
		body = bytes.NewReader(r.body)
	}
	req, err := http.NewRequestWithContext(ctx, r.method, u, body)
	if err != nil {
		return nil, err
	}
	for k, vs := range c.headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	for k, vs := range r.header {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("User-Agent", c.userAgent)
	if r.accept != "" {
		req.Header.Set("Accept", r.accept)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if r.hasBody {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// codeOfStatus is the code of an error reply without the API's JSON error body (the empty 416 of getVideoMedia, a
// proxy's page), the same in every SDK: 400 and 416 invalid_param, 403 cross_origin, 404 not_found, 405
// method_not_allowed, 413 body_too_large, 415 json_only, 429 too_many_requests, 503 not_ready, another 5xx internal,
// anything else decode.
func codeOfStatus(status int) ErrorCode {
	switch status {
	case http.StatusBadRequest, http.StatusRequestedRangeNotSatisfiable:
		return CodeInvalidParam
	case http.StatusForbidden:
		return CodeCrossOrigin
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusMethodNotAllowed:
		return CodeMethodNotAllowed
	case http.StatusRequestEntityTooLarge:
		return CodeBodyTooLarge
	case http.StatusUnsupportedMediaType:
		return CodeJSONOnly
	case http.StatusTooManyRequests:
		return CodeTooManyRequests
	case http.StatusServiceUnavailable:
		return CodeNotReady
	}
	if status >= 500 && status <= 599 {
		return CodeInternal
	}
	return CodeDecode
}

// readError turns an error reply into an *Error (and closes the body); now dates a Retry-After given as an HTTP date.
// A reply without the API's error body gets its code from its status (codeOfStatus).
func readError(op string, resp *http.Response, now time.Time) *Error {
	defer resp.Body.Close()
	b, rerr := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	e := &Error{Op: op, Status: resp.StatusCode}
	if d, ok := retryAfter(resp.Header, now); ok {
		e.retryAfter = &d
	}
	var body ErrorReply
	if rerr == nil && json.Unmarshal(b, &body) == nil && body.Code != "" {
		e.Code = body.Code
		e.Message = body.Error
		e.Field = body.Field
		e.RetryMS = body.RetryMS
		e.Retained = body.Retained
		e.Waiting = body.Waiting
		return e
	}
	e.Code = codeOfStatus(resp.StatusCode)
	e.Message = http.StatusText(resp.StatusCode)
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		e.Field = "range"
	}
	if text := strings.TrimSpace(string(b)); text != "" {
		if len(text) > 200 {
			text = text[:200]
		}
		e.Message += ": " + text
	}
	e.Err = rerr
	return e
}

// readAll sends r and reads the whole reply body (a GET whose body breaks off is tried again).
func (c *Client) readAll(ctx context.Context, r *request) (*http.Response, []byte, error) {
	resp, b, _, err := c.do(ctx, r, true)
	return resp, b, err
}

// doJSON sends r and decodes a JSON reply into out.
func (c *Client) doJSON(ctx context.Context, r *request, out any) (*http.Response, error) {
	if r.accept == "" {
		r.accept = "application/json"
	}
	resp, b, err := c.readAll(ctx, r)
	if err != nil {
		return resp, err
	}
	if resp.StatusCode == http.StatusNotModified {
		return resp, nil
	}
	if err := decodeJSON(b, out); err != nil {
		return resp, &Error{Op: r.op, Status: resp.StatusCode, Code: CodeDecode,
			Message: "reply is not the documented JSON", Err: err}
	}
	return resp, nil
}

func decodeJSON(b []byte, out any) error {
	if len(bytes.TrimSpace(b)) == 0 {
		return errors.New("empty body")
	}
	return json.Unmarshal(b, out)
}

// jsonBody marshals a control body.
func jsonBody(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	return b, nil
}
