package pdiggerfactory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// API is the low level: one method per operation of spec/openapi.yaml, named after its operationId. Every method
// takes a context; options are a params struct where nil means the server's defaults. Controls (SessionSelect,
// SessionSeek, SessionPlay, SessionPause, PutFocus, DeleteFocus) move the shared session for every viewer unless
// DryRun is set.
type API struct{ c *Client }

const apiRoot = "/api/v2"

// ControlParams are the query parameters of every control.
type ControlParams struct {
	// DryRun validates and answers without moving the shared session.
	DryRun bool
}

func (p *ControlParams) query() url.Values {
	q := url.Values{}
	if p != nil && p.DryRun {
		q.Set("dry_run", "1")
	}
	return q
}

// FrameRef addresses a grid frame: a frame index or the newest frame.
type FrameRef struct {
	g      int64
	latest bool
}

// FrameAt addresses grid frame g.
func FrameAt(g int64) FrameRef { return FrameRef{g: g} }

// LatestFrame addresses the newest retained frame.
func LatestFrame() FrameRef { return FrameRef{latest: true} }

// String is "latest" or the frame index.
func (r FrameRef) String() string {
	if r.latest {
		return "latest"
	}
	return strconv.FormatInt(r.g, 10)
}

func setLayers(q url.Values, layers []string) {
	if len(layers) > 0 {
		q.Set("layers", strings.Join(layers, ","))
	}
}

func setPrecision(q url.Values, p string) {
	if p != "" {
		q.Set("precision", p)
	}
}

func setInt(q url.Values, key string, v *int64) {
	if v != nil {
		q.Set(key, strconv.FormatInt(*v, 10))
	}
}

func setPositive(q url.Values, key string, v int) {
	if v > 0 {
		q.Set(key, strconv.Itoa(v))
	}
}

func (a *API) getJSON(ctx context.Context, op, path string, q url.Values, out any) error {
	_, err := a.c.doJSON(ctx, &request{op: op, method: http.MethodGet, path: path, query: q}, out)
	return err
}

// ---- Interpreter ----------------------------------------------------------------------------------------------------

// WorldParams are the query parameters of GetWorld.
type WorldParams struct {
	// Precision is "rounded" (default) or "full".
	Precision string
}

// GetWorld returns the world of the newest retained frame that has one, at its newest revision.
func (a *API) GetWorld(ctx context.Context, p *WorldParams) (*WorldLayer, error) {
	q := url.Values{}
	if p != nil {
		setPrecision(q, p.Precision)
	}
	var out WorldLayer
	if err := a.getJSON(ctx, "getWorld", apiRoot+"/session/world", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// WorldEventsParams are the query parameters of GetWorldEvents.
type WorldEventsParams struct {
	// After returns only events with a larger g (default -1: all retained).
	After *int64
	// Limit is at most this many events, by whole frames (1..1024, default 256).
	Limit int
	// Types are event types to keep (default all).
	Types []string
}

// GetWorldEvents returns the event log of this session, oldest first. Continue with After = NextAfter.
func (a *API) GetWorldEvents(ctx context.Context, p *WorldEventsParams) (*EventLog, error) {
	q := url.Values{}
	if p != nil {
		setInt(q, "after", p.After)
		setPositive(q, "limit", p.Limit)
		if len(p.Types) > 0 {
			q.Set("type", strings.Join(p.Types, ","))
		}
	}
	var out EventLog
	if err := a.getJSON(ctx, "getWorldEvents", apiRoot+"/session/world/events", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetStone returns one stone of this session's world, with its path since the session began.
func (a *API) GetStone(ctx context.Context, id int64) (*StoneTrack, error) {
	var out StoneTrack
	path := apiRoot + "/session/world/stones/" + strconv.FormatInt(id, 10)
	if err := a.getJSON(ctx, "getStone", path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetFocus returns the stone highlighted for every viewer, and its path (nils when there is none).
func (a *API) GetFocus(ctx context.Context) (*FocusReply, error) {
	var out FocusReply
	if err := a.getJSON(ctx, "getFocus", apiRoot+"/session/focus", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PutFocusParams are the body and query of PutFocus.
type PutFocusParams struct {
	StoneID int64
	// G is the frame the stone was picked on.
	G *int64
	// Session refuses the change (409 stale_session) when it is not the current session.
	Session *int64
	DryRun  bool
}

// PutFocus highlights a stone of the world for every viewer. It changes the shared session unless DryRun is set.
func (a *API) PutFocus(ctx context.Context, p PutFocusParams) (*ControlReply, error) {
	return a.control(ctx, "putFocus", http.MethodPut, "/session/focus",
		FocusRequest{StoneID: p.StoneID, G: p.G, Session: p.Session}, &ControlParams{DryRun: p.DryRun})
}

// DeleteFocus clears the highlight for every viewer, unless DryRun is set.
func (a *API) DeleteFocus(ctx context.Context, p *ControlParams) (*ControlReply, error) {
	return a.control(ctx, "deleteFocus", http.MethodDelete, "/session/focus", nil, p)
}

// ---- Sources --------------------------------------------------------------------------------------------------------

// ListVideosParams are the headers of ListVideos.
type ListVideosParams struct {
	// IfNoneMatch is the ETag of a cached catalog; an unchanged catalog answers NotModified.
	IfNoneMatch string
}

// ListVideos returns the public catalog: the recordings, then the plant's live cameras (Kind "camera").
func (a *API) ListVideos(ctx context.Context, p *ListVideosParams) (*VideoList, error) {
	r := &request{op: "listVideos", method: http.MethodGet, path: apiRoot + "/videos", header: http.Header{}}
	if p != nil && p.IfNoneMatch != "" {
		r.header.Set("If-None-Match", p.IfNoneMatch)
	}
	var out VideoList
	resp, err := a.c.doJSON(ctx, r, &out)
	if err != nil {
		return nil, err
	}
	out.ETag = resp.Header.Get("ETag")
	out.NotModified = resp.StatusCode == http.StatusNotModified
	return &out, nil
}

// GetVideo returns one catalog entry.
func (a *API) GetVideo(ctx context.Context, id string) (*Source, error) {
	var out Source
	if err := a.getJSON(ctx, "getVideo", apiRoot+"/videos/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ByteRange is a single HTTP byte range. Its zero End reads to the end of the file: ByteRange{Start: 1024} is
// "bytes=1024-". With End set it is Start..End, both inclusive (Bytes(0, 1023) is "bytes=0-1023"); with Suffix above
// zero it is the last Suffix bytes instead (ByteRange{Suffix: 512} is "bytes=-512").
type ByteRange struct {
	// Start is the first byte, 0-based.
	Start int64
	// End is the last byte, inclusive; nil reads to the end.
	End *int64
	// Suffix, when above zero, asks for the last Suffix bytes; Start and End are then not used.
	Suffix int64
}

// Bytes is the byte range first..last, both inclusive.
func Bytes(first, last int64) *ByteRange { return &ByteRange{Start: first, End: &last} }

// String is the Range header value.
func (r ByteRange) String() string {
	switch {
	case r.Suffix > 0:
		return fmt.Sprintf("bytes=-%d", r.Suffix)
	case r.End == nil:
		return fmt.Sprintf("bytes=%d-", r.Start)
	}
	return fmt.Sprintf("bytes=%d-%d", r.Start, *r.End)
}

// MediaParams are the headers of the media operations.
type MediaParams struct {
	// Range is a single byte range.
	Range *ByteRange
	// IfRange is the ETag or Last-Modified of a previous reply.
	IfRange string
	// IfNoneMatch is the ETag of a cached copy.
	IfNoneMatch string
}

// ContentRange is a parsed Content-Range header: bytes First-Last of Size (Size -1 when unknown).
type ContentRange struct {
	First, Last, Size int64
}

// Media is a reply of the media operations.
type Media struct {
	// Status is 200 (whole file), 206 (a range) or 304 (not modified).
	Status        int
	ContentType   string
	ContentLength int64
	ContentRange  *ContentRange
	ETag          string
	AcceptRanges  string
	// Body is the bytes (empty for HEAD and 304).
	Body []byte
}

// MediaReader is an open media reply whose body is read as it arrives (also media that never ends). Close it.
type MediaReader struct {
	Media
	io.ReadCloser
	cancel context.CancelFunc
}

// Close closes the body and the request.
func (m *MediaReader) Close() error {
	err := m.ReadCloser.Close()
	m.cancel()
	return err
}

func (a *API) mediaRequest(op, method, id string, p *MediaParams) *request {
	r := &request{op: op, method: method, path: apiRoot + "/videos/" + url.PathEscape(id) + "/media",
		header: http.Header{}, accept: "video/mp4"}
	if p != nil {
		if p.Range != nil {
			r.header.Set("Range", p.Range.String())
		}
		if p.IfRange != "" {
			r.header.Set("If-Range", p.IfRange)
		}
		if p.IfNoneMatch != "" {
			r.header.Set("If-None-Match", p.IfNoneMatch)
		}
	}
	return r
}

func mediaOf(resp *http.Response) Media {
	m := Media{Status: resp.StatusCode, ContentType: resp.Header.Get("Content-Type"),
		ContentLength: resp.ContentLength, ETag: resp.Header.Get("ETag"), AcceptRanges: resp.Header.Get("Accept-Ranges")}
	if v := resp.Header.Get("Content-Length"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			m.ContentLength = n
		}
	}
	m.ContentRange = ParseContentRange(resp.Header.Get("Content-Range"))
	return m
}

// ParseContentRange parses "bytes a-b/n" (n may be "*"); nil when the header is absent or malformed.
func ParseContentRange(v string) *ContentRange {
	rest, ok := strings.CutPrefix(strings.TrimSpace(v), "bytes ")
	if !ok {
		return nil
	}
	span, size, ok := strings.Cut(rest, "/")
	if !ok {
		return nil
	}
	cr := &ContentRange{First: -1, Last: -1, Size: -1}
	if size != "*" {
		n, err := strconv.ParseInt(size, 10, 64)
		if err != nil {
			return nil
		}
		cr.Size = n
	}
	if span == "*" {
		return cr
	}
	first, last, ok := strings.Cut(span, "-")
	if !ok {
		return nil
	}
	f, err1 := strconv.ParseInt(first, 10, 64)
	l, err2 := strconv.ParseInt(last, 10, 64)
	if err1 != nil || err2 != nil {
		return nil
	}
	cr.First, cr.Last = f, l
	return cr
}

// GetVideoMedia downloads the proxy video of a recording, or one byte range of it, into memory. A range the file does
// not have is invalid_param (field "range", HTTP 416). A live camera has no media (404 not_found): its pictures come
// with its frames (GetFramePicture). A reply that has neither a length nor byte ranges would never end: it gets
// not_seekable (field "id") and is closed unread; read such media with OpenVideoMedia, which is also the way for
// large files.
func (a *API) GetVideoMedia(ctx context.Context, id string, p *MediaParams) (*Media, error) {
	r := a.mediaRequest("getVideoMedia", http.MethodGet, id, p)
	r.check = func(resp *http.Response) error {
		if resp.StatusCode == http.StatusOK && resp.ContentLength < 0 && resp.Header.Get("Accept-Ranges") != "bytes" {
			return endlessMedia(id, resp.StatusCode)
		}
		return nil
	}
	resp, b, err := a.c.readAll(ctx, r)
	if err != nil {
		return nil, err
	}
	m := mediaOf(resp)
	m.Body = b
	return &m, nil
}

// endlessMedia is the not_seekable error of a media reply that would never end.
func endlessMedia(id string, status int) *Error {
	return &Error{Op: "getVideoMedia", Status: status, Code: CodeNotSeekable, Field: "id",
		Message: "the media of " + id + " does not end: read it with OpenVideoMedia"}
}

// HeadVideoMedia returns the headers of GetVideoMedia without the body.
func (a *API) HeadVideoMedia(ctx context.Context, id string, p *MediaParams) (*Media, error) {
	resp, _, err := a.c.readAll(ctx, a.mediaRequest("headVideoMedia", http.MethodHead, id, p))
	if err != nil {
		return nil, err
	}
	m := mediaOf(resp)
	return &m, nil
}

// OpenVideoMedia opens the proxy video of a recording as a stream, read as it arrives (also media that never ends).
func (a *API) OpenVideoMedia(ctx context.Context, id string, p *MediaParams) (*MediaReader, error) {
	r := a.mediaRequest("getVideoMedia", http.MethodGet, id, p)
	r.stream = true
	resp, cancel, err := a.c.send(ctx, r)
	if err != nil {
		return nil, err
	}
	return &MediaReader{Media: mediaOf(resp), ReadCloser: resp.Body, cancel: cancel}, nil
}

// ---- Session --------------------------------------------------------------------------------------------------------

// GetSession returns the shared clock, the worker and the Stable sim.
func (a *API) GetSession(ctx context.Context) (*SessionReply, error) {
	return a.getSession(ctx, false)
}

// getSession is GetSession; noRetry makes it one request (a clock sample times its own round trip).
func (a *API) getSession(ctx context.Context, noRetry bool) (*SessionReply, error) {
	var out SessionReply
	r := &request{op: "getSession", method: http.MethodGet, path: apiRoot + "/session", noRetry: noRetry}
	if _, err := a.c.doJSON(ctx, r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *API) control(ctx context.Context, op, method, path string, body any, p *ControlParams) (*ControlReply, error) {
	r := &request{op: op, method: method, path: apiRoot + path, query: p.query()}
	if body != nil {
		b, err := jsonBody(body)
		if err != nil {
			return nil, &Error{Op: op, Code: CodeInvalidParam, Message: err.Error(), Err: err}
		}
		r.body, r.hasBody = b, true
	}
	var out ControlReply
	if _, err := a.c.doJSON(ctx, r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SessionSelect switches the shared session to another source; it starts a new interpretation run for every
// viewer, unless DryRun is set. For a live camera leave Time out (any time, null too: 400 not_seekable, field
// "time"): its session is connecting, then warming up on the camera's first frames, then live (State.Live).
func (a *API) SessionSelect(ctx context.Context, body SelectRequest, p *ControlParams) (*ControlReply, error) {
	return a.control(ctx, "sessionSelect", http.MethodPost, "/session/select", body, p)
}

// SessionSeek moves the shared playhead; it starts a new interpretation run for every viewer, unless DryRun is
// set. A seek whose video is a live camera (the one Video names, else the one shown) is refused (409 not_seekable,
// field "video"); one whose Video names a recording is applied even while a camera is shown.
func (a *API) SessionSeek(ctx context.Context, body SeekRequest, p *ControlParams) (*ControlReply, error) {
	return a.control(ctx, "sessionSeek", http.MethodPost, "/session/seek", body, p)
}

// SessionPlay resumes the shared playhead for every viewer (idempotent), unless DryRun is set.
func (a *API) SessionPlay(ctx context.Context, p *ControlParams) (*ControlReply, error) {
	return a.control(ctx, "sessionPlay", http.MethodPost, "/session/play", EmptyRequest{}, p)
}

// SessionPause pauses the shared playhead for every viewer (idempotent), unless DryRun is set.
func (a *API) SessionPause(ctx context.Context, p *ControlParams) (*ControlReply, error) {
	return a.control(ctx, "sessionPause", http.MethodPost, "/session/pause", EmptyRequest{}, p)
}

// ---- Streams --------------------------------------------------------------------------------------------------------

// Stream formats.
const (
	FormatSSE    = "sse"
	FormatNDJSON = "ndjson"
)

// StreamFramesParams are the parameters of StreamFrames.
type StreamFramesParams struct {
	// Layers is a subset of raw, overlay, scene, cards, world (default overlay, scene, cards, world).
	Layers []string
	// Format is FormatSSE (default) or FormatNDJSON.
	Format string
	// After starts after this cursor (default: the newest).
	After *int64
	// Limit ends the stream after this many frames (1..4096).
	Limit int
	// MaxS ends the stream after this many seconds (1..3600).
	MaxS int
	// Observe does not count this consumer as a viewer.
	Observe bool
	// Precision is "rounded" (default) or "full".
	Precision string
	// LastEventID resumes after "<boot>-<cursor>"; an id of another boot gets a reset event.
	LastEventID string
}

func (p *StreamFramesParams) query() url.Values {
	q := url.Values{}
	setLayers(q, p.Layers)
	format := p.Format
	if format == "" {
		format = FormatSSE
	}
	q.Set("format", format)
	setInt(q, "after", p.After)
	setPositive(q, "limit", p.Limit)
	setPositive(q, "max_s", p.MaxS)
	if p.Observe {
		q.Set("observe", "1")
	}
	setPrecision(q, p.Precision)
	return q
}

// StreamFrames opens one frames stream and yields its raw events; it does not resume (Streams.Frames does). The
// connection closes when the loop ends or ctx is cancelled. Limit and MaxS end the stream: the SDK counts the frames
// and the seconds itself and closes the connection when either is reached, so the loop ends without waiting for the
// server's end (the 20.20 unit closes a finished stream only about 10 s later).
func (a *API) StreamFrames(ctx context.Context, p *StreamFramesParams) iter.Seq2[RawEvent, error] {
	if p == nil {
		p = &StreamFramesParams{}
	}
	format := p.Format
	if format == "" {
		format = FormatSSE
	}
	q := p.query()
	return a.c.rawStream(ctx, "streamFrames", apiRoot+"/session/frames/stream", q, format, p.LastEventID, false,
		&connLimits{limit: p.Limit, maxS: p.MaxS})
}

// RawStreamParams are the parameters of RawStream.
type RawStreamParams struct {
	// Format is FormatNDJSON (the server's default) or FormatSSE.
	Format      string
	After       *int64
	Limit       int
	MaxS        int
	Observe     bool
	LastEventID string
}

// RawStream opens one raw model codec stream (one sip1 packet per event) and yields its raw events; it does not
// resume (Streams.RawPackets does). Limit and MaxS end it as they end StreamFrames.
func (a *API) RawStream(ctx context.Context, p *RawStreamParams) iter.Seq2[RawEvent, error] {
	if p == nil {
		p = &RawStreamParams{}
	}
	q := url.Values{}
	format := p.Format
	if format == "" {
		format = FormatNDJSON
	}
	q.Set("format", format)
	setInt(q, "after", p.After)
	setPositive(q, "limit", p.Limit)
	setPositive(q, "max_s", p.MaxS)
	if p.Observe {
		q.Set("observe", "1")
	}
	return a.c.rawStream(ctx, "rawStream", apiRoot+"/raw/stream", q, format, p.LastEventID, false,
		&connLimits{limit: p.Limit, maxS: p.MaxS})
}

// connLimits bound one stream connection. The SDK ends a connection itself after limit frames (frame events) or
// maxS seconds on the client's clock, without waiting for the server's end: the 20.20 unit sends nothing more then
// but closes the connection only about 10 s later. ended records that the SDK did; onOpen, when set, runs once the
// reply headers arrived, before the first event is read.
type connLimits struct {
	limit  int
	maxS   int
	onOpen func()
	ended  bool
}

// isFrameEvent reports whether a raw event is a frame (or packet): what a stream's limit counts.
func isFrameEvent(ev RawEvent) bool { return ev.Event == StreamEventFrame }

// rawStream performs one stream request and yields its events, until the server ends the stream, the connection
// fails (an error is yielded), the loop stops, or lim is reached (lim.ended is then set).
func (c *Client) rawStream(ctx context.Context, op, path string, q url.Values, format, lastID string,
	noRetry bool, lim *connLimits) iter.Seq2[RawEvent, error] {
	return func(yield func(RawEvent, error) bool) {
		r := &request{op: op, method: http.MethodGet, path: path, query: q, header: http.Header{}, stream: true,
			noRetry: noRetry, accept: "text/event-stream"}
		if format == FormatNDJSON {
			r.accept = "application/x-ndjson"
		}
		if lastID != "" {
			r.header.Set("Last-Event-ID", lastID)
		}
		cctx, closeConn := context.WithCancel(ctx)
		defer closeConn()
		resp, cancel, err := c.send(cctx, r)
		if err != nil {
			yield(RawEvent{}, err)
			return
		}
		defer cancel()
		defer resp.Body.Close()
		var expired atomic.Bool
		if lim.maxS > 0 {
			stop := afterFunc(c.clock, time.Duration(lim.maxS)*time.Second, func() {
				expired.Store(true)
				closeConn()
			})
			defer stop()
		}
		if lim.onOpen != nil {
			lim.onOpen()
		}
		next := streamParser(resp, format)
		frames := 0
		for {
			ev, err := next()
			if err != nil {
				var e *Error
				switch {
				case expired.Load() && ctx.Err() == nil:
					lim.ended = true // max_s: the SDK closed the connection
				case errors.As(err, &e) && ctx.Err() == nil: // a line or an event too large: not retried
					e.Op = op
					yield(RawEvent{}, e)
				case !errors.Is(err, io.EOF):
					yield(RawEvent{}, transportError(op, ctx, err))
				}
				return
			}
			if !yield(ev, nil) {
				return
			}
			if isFrameEvent(ev) {
				if frames++; lim.limit > 0 && frames >= lim.limit {
					lim.ended = true
					return
				}
			}
		}
	}
}

// streamParser picks the parser from the reply's Content-Type, else from the format asked for.
func streamParser(resp *http.Response, format string) func() (RawEvent, error) {
	mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	switch {
	case mt == "text/event-stream" || (mt != "application/x-ndjson" && format == FormatSSE):
		return NewSSEParser(resp.Body).Next
	default:
		return NewNDJSONParser(resp.Body).Next
	}
}

// PollParams are the parameters of PollFrames.
type PollParams struct {
	// Cursor is the last cursor this client has (default 0).
	Cursor int64
	// Rev is the last state revision this client has; 0 returns at once.
	Rev int64
	// CID is a per-tab id that counts this client as a viewer (^[A-Za-z0-9_-]{8,40}$).
	CID    string
	Layers []string
	// Limit is at most this many newest frames per reply (1..16, default 8).
	Limit int
	// WaitMS is the longest wait in ms (0..1000, default 1000); nil uses the default.
	WaitMS    *int64
	Precision string
}

// PollFrames is one long-poll: it returns when a frame newer than Cursor is released or the state revision differs
// from Rev, else after the wait. Continue from the reply's Cursor, never from a frame's.
func (a *API) PollFrames(ctx context.Context, p *PollParams) (*LiveReply, error) {
	q := url.Values{}
	if p != nil {
		if p.Cursor > 0 {
			q.Set("cursor", strconv.FormatInt(p.Cursor, 10))
		}
		if p.Rev > 0 {
			q.Set("rev", strconv.FormatInt(p.Rev, 10))
		}
		if p.CID != "" {
			q.Set("cid", p.CID)
		}
		setLayers(q, p.Layers)
		setPositive(q, "limit", p.Limit)
		setInt(q, "wait", p.WaitMS)
		setPrecision(q, p.Precision)
	}
	var out LiveReply
	if err := a.getJSON(ctx, "pollFrames", apiRoot+"/session/frames", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- Render-ready ---------------------------------------------------------------------------------------------------

// RenderParams are the parameters of RenderOverlay2D: exactly one of G and T.
type RenderParams struct {
	G *int64
	T *float64
	// Mode is "stable" (default) or "raw".
	Mode string
	// Draw is "shapes" (default) or "primitives".
	Draw string
	// Session answers 410 session_gone when it is not the current session.
	Session *int64
}

// Scene3DParams are the parameters of RenderScene3D: exactly one of G and T.
type Scene3DParams struct {
	RenderParams
	// Surface is "vertices" (default), "compact" or "none".
	Surface string
}

func (p *RenderParams) query(op string) (url.Values, error) {
	if p == nil || (p.G == nil) == (p.T == nil) {
		return nil, localError(op, CodeInvalidParam, "t", "give exactly one of g and t")
	}
	q := url.Values{}
	setInt(q, "g", p.G)
	if p.T != nil {
		q.Set("t", strconv.FormatFloat(*p.T, 'f', -1, 64))
	}
	if p.Mode != "" {
		q.Set("mode", p.Mode)
	}
	if p.Draw != "" {
		q.Set("draw", p.Draw)
	}
	setInt(q, "session", p.Session)
	return q, nil
}

// RenderOverlay2D returns everything the 2-D overlay draws at a grid frame or a retained time, final, in native px.
func (a *API) RenderOverlay2D(ctx context.Context, p *RenderParams) (*Overlay2D, error) {
	q, err := p.query("renderOverlay2d")
	if err != nil {
		return nil, err
	}
	var out Overlay2D
	if err := a.getJSON(ctx, "renderOverlay2d", apiRoot+"/session/render/overlay2d", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RenderScene3D returns everything the 3-D view draws at a grid frame or a retained time, in world coordinates.
func (a *API) RenderScene3D(ctx context.Context, p *Scene3DParams) (*Scene3D, error) {
	var rp *RenderParams
	if p != nil {
		rp = &p.RenderParams
	}
	q, err := rp.query("renderScene3d")
	if err != nil {
		return nil, err
	}
	if p.Surface != "" {
		q.Set("surface", p.Surface)
	}
	var out Scene3D
	if err := a.getJSON(ctx, "renderScene3d", apiRoot+"/session/render/scene3d", q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---- Frames and codec -----------------------------------------------------------------------------------------------

// FrameParams are the parameters of GetFrame.
type FrameParams struct {
	Layers []string
	// Session answers 410 session_gone when it is not the current session.
	Session   *int64
	Precision string
}

// GetFrame returns a retained frame with the layers asked for.
func (a *API) GetFrame(ctx context.Context, g FrameRef, p *FrameParams) (*Frame, error) {
	return a.getFrame(ctx, g, p, false)
}

func (a *API) getFrame(ctx context.Context, g FrameRef, p *FrameParams, noRetry bool) (*Frame, error) {
	q := url.Values{}
	if p != nil {
		setLayers(q, p.Layers)
		setInt(q, "session", p.Session)
		setPrecision(q, p.Precision)
	}
	var out Frame
	r := &request{op: "getFrame", method: http.MethodGet, path: apiRoot + "/session/frames/" + g.String(), query: q,
		noRetry: noRetry}
	if _, err := a.c.doJSON(ctx, r, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Picture is the camera picture a live frame was interpreted from (GetFramePicture).
type Picture struct {
	// G is the frame's grid index (the reply's X-Interp-G header: the frame LatestFrame() was).
	G int64
	// JPEG is the picture as it arrived (640 x 360 on the plant rigs; the layers' native pixels are this picture
	// stretched to the source's NativeWH).
	JPEG []byte
}

// PictureParams are the parameters of GetFramePicture.
type PictureParams struct {
	// Session answers 410 session_gone when it is not the current session.
	Session *int64
}

// GetFramePicture returns the camera picture a retained frame of a live camera session (FrameAt(g), or LatestFrame()
// for the newest) was interpreted from. Warm-up frames are never retained; a recording has none (404 not_found: its
// picture is the proxy video); a frame no longer retained is not_retained.
func (a *API) GetFramePicture(ctx context.Context, g FrameRef, p *PictureParams) (*Picture, error) {
	q := url.Values{}
	if p != nil {
		setInt(q, "session", p.Session)
	}
	resp, b, err := a.c.readAll(ctx, &request{op: "getFramePicture", method: http.MethodGet,
		path: apiRoot + "/session/frames/" + g.String() + "/image.jpg", query: q, accept: "image/jpeg"})
	if err != nil {
		return nil, err
	}
	shown, perr := strconv.ParseInt(strings.TrimSpace(resp.Header.Get("X-Interp-G")), 10, 64)
	if perr != nil {
		return nil, &Error{Op: "getFramePicture", Status: resp.StatusCode, Code: CodeDecode,
			Message: "the picture reply has no X-Interp-G header", Err: perr}
	}
	return &Picture{G: shown, JPEG: b}, nil
}

// Rasters of GetFrameRaster.
const (
	RasterRegions = "regions"
	RasterMask    = "mask"
	RasterDepth   = "depth"
)

// RasterParams are the parameters of GetFrameRaster.
type RasterParams struct {
	// Scale is the nearest-neighbour upscale, 1..8 (default 1).
	Scale int
}

// GetFrameRaster returns a retained frame's map as PNG bytes (64 x 40 times the scale), coloured with the portal's
// ramps: RasterRegions, RasterMask or RasterDepth.
func (a *API) GetFrameRaster(ctx context.Context, g int64, raster string, p *RasterParams) ([]byte, error) {
	q := url.Values{}
	if p != nil {
		setPositive(q, "scale", p.Scale)
	}
	path := fmt.Sprintf("%s/session/frames/%d/%s.png", apiRoot, g, url.PathEscape(raster))
	_, b, err := a.c.readAll(ctx, &request{op: "getFrameRaster", method: http.MethodGet, path: path, query: q,
		accept: "image/png"})
	return b, err
}

// RawPacketParams are the parameters of RawPacket.
type RawPacketParams struct {
	Session *int64
}

// RawPacket returns one raw packet of the retained window.
func (a *API) RawPacket(ctx context.Context, g FrameRef, p *RawPacketParams) (*Sip1Packet, error) {
	q := url.Values{}
	if p != nil {
		setInt(q, "session", p.Session)
	}
	var out Sip1Packet
	if err := a.getJSON(ctx, "rawPacket", apiRoot+"/raw/packets/"+g.String(), q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RawTensors returns the npz archive of the model's output tensors and RGB input for one frame (debug only: the
// error has code debug_disabled when the worker keeps none).
func (a *API) RawTensors(ctx context.Context, g int64) ([]byte, error) {
	_, b, err := a.c.readAll(ctx, &request{op: "rawTensors", method: http.MethodGet,
		path: apiRoot + "/raw/tensors/" + strconv.FormatInt(g, 10), accept: "application/octet-stream"})
	return b, err
}

// ---- Meta and Ops ---------------------------------------------------------------------------------------------------

// GetAPIRoot returns the entry point: links, layer schema ids, limits and the build.
func (a *API) GetAPIRoot(ctx context.Context) (*APIRoot, error) {
	var out APIRoot
	if err := a.getJSON(ctx, "getApiRoot", apiRoot, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetOpenAPI returns the server's OpenAPI document (YAML).
func (a *API) GetOpenAPI(ctx context.Context) ([]byte, error) {
	_, b, err := a.c.readAll(ctx, &request{op: "getOpenApi", method: http.MethodGet, path: apiRoot + "/openapi.yaml",
		accept: "application/yaml"})
	return b, err
}

// GetModel returns the model that is running, with engine hashes.
func (a *API) GetModel(ctx context.Context) (*ModelInfo, error) {
	var out ModelInfo
	if err := a.getJSON(ctx, "getModel", apiRoot+"/model", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetTopology returns the skeleton topology, class names, palette and coordinate constants.
func (a *API) GetTopology(ctx context.Context) (*Topology, error) {
	var out Topology
	if err := a.getJSON(ctx, "getTopology", apiRoot+"/topology", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetHealth is the liveness check.
func (a *API) GetHealth(ctx context.Context) (*Health, error) {
	var out Health
	if err := a.getJSON(ctx, "getHealth", apiRoot+"/health", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetReady is the readiness check; a server that is starting answers an error with code not_ready and Waiting.
func (a *API) GetReady(ctx context.Context) (*Ready, error) {
	var out Ready
	if err := a.getJSON(ctx, "getReady", apiRoot+"/ready", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetStatus returns everything an operator wants on one page.
func (a *API) GetStatus(ctx context.Context) (*Status, error) {
	var out Status
	if err := a.getJSON(ctx, "getStatus", apiRoot+"/status", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetMetrics returns the Prometheus text exposition, as text and parsed into samples.
func (a *API) GetMetrics(ctx context.Context) (*Metrics, error) {
	_, b, err := a.c.readAll(ctx, &request{op: "getMetrics", method: http.MethodGet, path: "/metrics",
		accept: "text/plain"})
	if err != nil {
		return nil, err
	}
	return ParseMetrics(string(b)), nil
}

// Decode decodes a raw event's data into out (a Frame, a Sip1Packet, a ClockEvent, ...).
func (ev RawEvent) Decode(out any) error {
	if err := json.Unmarshal(ev.Data, out); err != nil {
		return &Error{Op: "decode", Code: CodeDecode, Message: "stream event " + ev.Event + " is not the documented JSON",
			Err: err}
	}
	return nil
}
