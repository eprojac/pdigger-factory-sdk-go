package pdiggerfactory

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrorCode is the API's stable machine-readable error code (schema ErrorCode), plus the SDK-only codes below. Code
// is what programs key on, never the message.
type ErrorCode string

// Error codes of the API (schema ErrorCode).
const (
	CodeInvalidJSON      ErrorCode = "invalid_json"
	CodeNotAnObject      ErrorCode = "not_an_object"
	CodeChunkedRefused   ErrorCode = "chunked_refused"
	CodeLengthRequired   ErrorCode = "length_required"
	CodeBodyTooLarge     ErrorCode = "body_too_large"
	CodeBodyNotAllowed   ErrorCode = "body_not_allowed"
	CodeCrossOrigin      ErrorCode = "cross_origin"
	CodeJSONOnly         ErrorCode = "json_only"
	CodeTooManyControls  ErrorCode = "too_many_controls"
	CodeTooManyRequests  ErrorCode = "too_many_requests"
	CodeTooManyViewers   ErrorCode = "too_many_viewers"
	CodeTooManyStreams   ErrorCode = "too_many_streams"
	CodeUnknownVideo     ErrorCode = "unknown_video"
	CodeTimeOutOfRange   ErrorCode = "time_out_of_range"
	CodeInvalidType      ErrorCode = "invalid_type"
	CodeMissingField     ErrorCode = "missing_field"
	CodeInvalidParam     ErrorCode = "invalid_param"
	CodeUnknownLayer     ErrorCode = "unknown_layer"
	CodeNotRetained      ErrorCode = "not_retained"
	CodeSessionGone      ErrorCode = "session_gone"
	CodeStaleSession     ErrorCode = "stale_session"
	CodeUnknownStone     ErrorCode = "unknown_stone"
	CodeWorldUnavailable ErrorCode = "world_unavailable"
	CodeDebugDisabled    ErrorCode = "debug_disabled"
	CodeBusy             ErrorCode = "busy"
	CodeNotReady         ErrorCode = "not_ready"
	CodeNotFound         ErrorCode = "not_found"
	CodeMethodNotAllowed ErrorCode = "method_not_allowed"
	CodeInternal         ErrorCode = "internal"
	CodeNotSeekable      ErrorCode = "not_seekable"
)

// SDK-only error codes.
const (
	// CodeTransport: no HTTP reply, or a reply or recording that could not be read to its end (connection refused
	// or reset, DNS, a timeout, a cancelled context, a read error).
	CodeTransport ErrorCode = "transport"
	// CodeDecode: a reply that is not the documented JSON (an error status without an API error body gets the code of
	// its status instead, when it has one: see Error), a stream line or event over 64 MiB, a recording that is not one
	// (a gzip stream that is corrupt or cut short), or a stream event or recording line that does not decode (Op
	// "decode", 2.3 rule 9: the stream yields it and goes on, a replay skips the line).
	CodeDecode ErrorCode = "decode"
	// CodeLiveUnavailable: the server's catalog has no live camera (builds before claude-20261120-live2).
	CodeLiveUnavailable ErrorCode = "live_unavailable"
	// CodeLiveTimeout: Session.UntilLive waited in vain; the message has the phase it saw last.
	CodeLiveTimeout ErrorCode = "live_timeout"
	// CodeStreamClosed: a stream gave up after MaxReconnects failed reconnects in a row, or the stream of
	// Interpreter.WaitFor ended without a match. (With Resume(false) a stream yields the failure itself instead.)
	CodeStreamClosed ErrorCode = "stream_closed"
)

// Error is the one error type of the SDK. API errors carry the reply's status and body; SDK-side errors carry an
// SDK-only code and, where there is one, the cause (errors.Is/As see it through Unwrap). An error reply without the
// API's JSON error body (the empty 416 of a byte range a file does not have, a proxy's page) gets its code from its
// status, the same in every SDK: 400 and 416 invalid_param (a 416 names the field "range"), 403 cross_origin, 404
// not_found, 405 method_not_allowed, 413 body_too_large, 415 json_only, 429 too_many_requests, 503 not_ready, another
// 5xx internal, anything else decode.
type Error struct {
	// Status is the HTTP status, 0 when there was no reply (or the error was raised locally).
	Status int
	// Code is the stable code: an API ErrorCode or an SDK-only code.
	Code ErrorCode
	// Message is the API's human-readable "error" (or the SDK's description).
	Message string
	// Field is the parameter or body field at fault, when the API names one.
	Field string
	// RetryMS is the API's retry_ms hint, nil when absent.
	RetryMS *int64
	// Retained is the retained frame window, sent with not_retained.
	Retained *Retained
	// Waiting lists what a not-ready server waits for.
	Waiting []string
	// Op is the operationId (or SDK operation) that failed.
	Op string
	// Err is the underlying cause, if any (a network error, context.Canceled, a JSON syntax error).
	Err error

	// canceled is set when the caller's context ended the request: not retryable.
	canceled bool
	// retryAfter is the reply's Retry-After header, parsed; nil when it had none.
	retryAfter *time.Duration
}

// Error formats the error as "pdiggerfactory: <op>: <status> <code>: <message> (field <field>)".
func (e *Error) Error() string {
	var b strings.Builder
	b.WriteString("pdiggerfactory: ")
	if e.Op != "" {
		b.WriteString(e.Op)
		b.WriteString(": ")
	}
	if e.Status != 0 {
		fmt.Fprintf(&b, "%d ", e.Status)
	}
	b.WriteString(string(e.Code))
	if e.Message != "" {
		b.WriteString(": ")
		b.WriteString(e.Message)
	}
	if e.Field != "" {
		b.WriteString(" (field ")
		b.WriteString(e.Field)
		b.WriteString(")")
	}
	if e.Err != nil && (e.Message == "" || !strings.Contains(e.Message, e.Err.Error())) {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap returns the cause.
func (e *Error) Unwrap() error { return e.Err }

// IsRetryable reports whether trying again later can succeed: no reply at all (but not a cancelled context), 429,
// 502, 503 or 504.
func (e *Error) IsRetryable() bool {
	if e.Code == CodeTransport {
		return !e.canceled
	}
	switch e.Status {
	case 429, 502, 503, 504:
		return true
	}
	return false
}

// RetryDelay is how long the server asked the client to wait before it tries again: the reply's retry_ms, else its
// Retry-After header; false when it sent neither. The SDK's retries and stream reconnects wait this long, and the
// backoff when there is no hint.
func (e *Error) RetryDelay() (time.Duration, bool) {
	switch {
	case e.RetryMS != nil:
		return time.Duration(*e.RetryMS) * time.Millisecond, true
	case e.retryAfter != nil:
		return *e.retryAfter, true
	}
	return 0, false
}

// AsError returns the *Error in err's chain, or nil.
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}

// IsRetryable reports whether err is an *Error that can succeed when tried again later.
func IsRetryable(err error) bool {
	e := AsError(err)
	return e != nil && e.IsRetryable()
}

// IsNotRetained reports whether err says a frame or time is outside the retained window (code not_retained).
func IsNotRetained(err error) bool { return HasCode(err, CodeNotRetained) }

// IsSessionGone reports whether err says the session asked for has ended (code session_gone, HTTP 410).
func IsSessionGone(err error) bool { return HasCode(err, CodeSessionGone) }

// HasCode reports whether err is an *Error with the given code.
func HasCode(err error, code ErrorCode) bool {
	e := AsError(err)
	return e != nil && e.Code == code
}

// localError is an error raised by the SDK without a request.
func localError(op string, code ErrorCode, field, message string) *Error {
	return &Error{Op: op, Code: code, Field: field, Message: message}
}
