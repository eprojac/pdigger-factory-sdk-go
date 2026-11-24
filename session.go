package pdiggerfactory

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Sources lists what the shared session can show: the recordings, then the plant's live cameras (each a source like
// a recording: Session().Watch selects it for every viewer).
func (c *Client) Sources(ctx context.Context) ([]Source, error) {
	list, err := c.api.ListVideos(ctx, nil)
	if err != nil {
		return nil, err
	}
	return list.Videos, nil
}

// Source returns one source by id: a recording's, such as "dry1", or a camera's serial, such as "CAM00000001".
func (c *Client) Source(ctx context.Context, id string) (*Source, error) {
	return c.api.GetVideo(ctx, id)
}

// Cameras lists the live cameras of the catalog (Kind "camera"), in catalog order; empty when the server lists none.
// Each is a source like a recording: Session().Watch(ctx, camera.ID, nil) selects it for every viewer, and
// Session().UntilLive waits out its connection and warm-up.
func (c *Client) Cameras(ctx context.Context) ([]Source, error) {
	list, err := c.api.ListVideos(ctx, nil)
	if err != nil {
		return nil, err
	}
	cameras := []Source{}
	for _, s := range list.Videos {
		if s.IsLive() {
			cameras = append(cameras, s)
		}
	}
	return cameras, nil
}

// Live returns the first live camera of the catalog, or an error with code live_unavailable when it has none (builds
// before claude-20261120-live2 play recordings only). For code written for one camera; a unit may list several (20.20
// lists six plant cameras): prefer Cameras.
func (c *Client) Live(ctx context.Context) (*Source, error) {
	list, err := c.api.ListVideos(ctx, nil)
	if err != nil {
		return nil, err
	}
	for i := range list.Videos {
		if list.Videos[i].IsLive() {
			return &list.Videos[i], nil
		}
	}
	return nil, localError("live", CodeLiveUnavailable, "", "the server's catalog has no live camera")
}

// Session is the high-level helper of the one shared session. Every control (Watch, Seek, Pause, Play, Focus,
// ClearFocus) changes the session for every viewer, unless DryRun() is given.
type Session struct {
	c *Client

	mu   sync.Mutex
	last *State
}

type controlConfig struct {
	dryRun  bool
	video   string
	g       *int64
	session *int64
}

// ControlOption configures a session control: DryRun for every control, InVideo for Seek, PickedAt and InSession
// for Focus (a control ignores the options that are not its own).
type ControlOption func(*controlConfig)

// DryRun validates a control and answers without moving the shared session.
func DryRun() ControlOption { return func(c *controlConfig) { c.dryRun = true } }

// InVideo names the video a Seek is for: the seek is sent for that video without reading the shared state first
// (the server applies it to that video). InVideo(LiveSourceID) is refused locally with not_seekable, and the server
// refuses a seek in a camera with 409 not_seekable; a seek in a recording is applied even while the session shows a
// camera, and moves every viewer off the camera onto that recording. An empty id is the same as none.
func InVideo(id string) ControlOption { return func(c *controlConfig) { c.video = id } }

// PickedAt is the frame (g) a Focus stone was picked on: the server highlights the stone as it was in that frame.
func PickedAt(g int64) ControlOption { return func(c *controlConfig) { c.g = Int64(g) } }

// InSession guards a Focus or a Picture: the server refuses a Focus with 409 stale_session when the shared session is
// no longer this one (another viewer selected or seeked after the stone was picked), instead of highlighting a stone
// of another run, and a Picture of a session that has ended with 410 session_gone, instead of the picture of the new
// session's frame g.
func InSession(session int64) ControlOption {
	return func(c *controlConfig) { c.session = Int64(session) }
}

func controls(opts []ControlOption) (*controlConfig, *ControlParams) {
	var cfg controlConfig
	for _, o := range opts {
		o(&cfg)
	}
	return &cfg, &ControlParams{DryRun: cfg.dryRun}
}

func controlParams(opts []ControlOption) *ControlParams {
	_, p := controls(opts)
	return p
}

func (s *Session) remember(st State, p *ControlParams) {
	if p != nil && p.DryRun {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	v := st
	s.last = &v
}

// Known is the newest state this client has seen (from State, a control or Clock), nil before any.
func (s *Session) Known() *State {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.last == nil {
		return nil
	}
	v := *s.last
	return &v
}

// State returns the shared session state now.
func (s *Session) State(ctx context.Context) (*State, error) {
	reply, err := s.c.api.GetSession(ctx)
	if err != nil {
		return nil, err
	}
	s.remember(reply.State, nil)
	return &reply.State, nil
}

// Watch switches the shared session to a source (a recording's id or a camera's serial) for every viewer: it plays
// first if the session is paused (a select keeps the paused or playing state, and playing first gives the worker its
// hold to warm up), then selects the source at media time at (nil: the source's default start), and returns the new
// state. A live camera has no time: Watch(ctx, camera.ID, nil) never sends one, and a time for an id the catalog
// lists as a camera is refused locally with not_seekable (field "time") without the select. On a camera the state
// returned is connecting: UntilLive waits out the connection and the warm-up.
func (s *Session) Watch(ctx context.Context, sourceID string, at *float64, opts ...ControlOption) (*State, error) {
	p := controlParams(opts)
	if at != nil {
		camera, err := s.isCamera(ctx, sourceID)
		if err != nil {
			return nil, err
		}
		if camera {
			return nil, localError("watch", CodeNotSeekable, "time",
				sourceID+" is a live camera: it has no time; leave `at` out")
		}
	}
	cur, err := s.c.api.GetSession(ctx)
	if err != nil {
		return nil, err
	}
	if !cur.State.Playing {
		if _, err := s.c.api.SessionPlay(ctx, p); err != nil {
			return nil, err
		}
	}
	req := SelectRequest{Video: sourceID, Time: at}
	reply, err := s.c.api.SessionSelect(ctx, req, p)
	if err != nil {
		return nil, err
	}
	s.remember(reply.State, p)
	return &reply.State, nil
}

// isCamera reports whether the catalog lists sourceID as a live camera (LiveSourceID always is one).
func (s *Session) isCamera(ctx context.Context, sourceID string) (bool, error) {
	if sourceID == LiveSourceID {
		return true, nil
	}
	list, err := s.c.api.ListVideos(ctx, nil)
	if err != nil {
		return false, err
	}
	for i := range list.Videos {
		if list.Videos[i].ID == sourceID {
			return list.Videos[i].IsLive(), nil
		}
	}
	return false, nil
}

// Seek moves the shared playhead to media time t, which starts a new interpretation run for every viewer. Without
// InVideo it reads the session first (another viewer may have switched it since this client last looked) and seeks
// in the video it shows; with InVideo(id) it sends the seek for that video at once. A live camera cannot be seeked
// (pause and play instead): a session that shows one is refused locally with not_seekable (field "video"), without
// sending the seek. The server refuses a seek whose video is a camera with 409 not_seekable: the one InVideo names,
// or without InVideo the one shown (a seek that races another viewer's switch to a camera). A seek with InVideo
// naming a recording is applied even while a camera is shown.
func (s *Session) Seek(ctx context.Context, t float64, opts ...ControlOption) (*State, error) {
	cfg, p := controls(opts)
	notSeekable := localError("seek", CodeNotSeekable, "video", "a live camera cannot be seeked; pause and play instead")
	if cfg.video == LiveSourceID {
		return nil, notSeekable
	}
	if cfg.video == "" {
		st, err := s.State(ctx)
		if err != nil {
			return nil, err
		}
		if st.IsLive() {
			return nil, notSeekable
		}
	}
	reply, err := s.c.api.SessionSeek(ctx, SeekRequest{Video: cfg.video, Time: t}, p)
	if err != nil {
		return nil, err
	}
	s.remember(reply.State, p)
	return &reply.State, nil
}

// Pause pauses the shared playhead for every viewer (idempotent). On a live camera it freezes the shared view and
// stops the interpreter; the phase stays live while paused.
func (s *Session) Pause(ctx context.Context, opts ...ControlOption) (*State, error) {
	p := controlParams(opts)
	reply, err := s.c.api.SessionPause(ctx, p)
	if err != nil {
		return nil, err
	}
	s.remember(reply.State, p)
	return &reply.State, nil
}

// Play resumes the shared playhead for every viewer (idempotent). On a live camera the reply is connecting again: the
// camera's next frame starts a new warm-up, then the session is live at the live edge (UntilLive waits it out, and G
// jumps over the pause).
func (s *Session) Play(ctx context.Context, opts ...ControlOption) (*State, error) {
	p := controlParams(opts)
	reply, err := s.c.api.SessionPlay(ctx, p)
	if err != nil {
		return nil, err
	}
	s.remember(reply.State, p)
	return &reply.State, nil
}

// UntilLivePoll is the wait between UntilLive's reads of the shared state (the last wait is cut short to end at the
// timeout).
const UntilLivePoll = 250 * time.Millisecond

// UntilLive waits until the session's camera is live (State.LivePhase() is LivePhaseLive) and returns that state.
// After Watch of a camera the session is connecting until the camera's first frame, then warming while the
// interpreter runs on its first Hold seconds (3 on the plant cameras; nothing is released meanwhile), then live; after
// Play, and when a playing camera sent no frame for 5 s, it is connecting again until a new warm-up ends. It reads
// the shared state every UntilLivePoll and once more at the timeout, waiting on the client's Clock, and sends no
// control; on a recording, and on a paused camera that was live (a pause keeps the phase), it returns the state at
// once. The unit interprets only while someone watches: an open stream (the Interpreter's), the page, or for 15 s the
// client that sent a control (its worker idles 20 s after the last): Watch counts, so Watch then UntilLive goes live
// on a quiet unit, while a camera session nobody watches stays connecting (the worker's reason: "nobody is
// watching"). After timeout it fails with live_timeout, with the phase it saw last in the message (the worker's
// reason in API().GetSession says why: nobody watching, or a camera that does not answer).
func (s *Session) UntilLive(ctx context.Context, timeout time.Duration) (*State, error) {
	clk := s.c.clock
	deadline := clk.Now().Add(timeout)
	for {
		st, err := s.State(ctx)
		if err != nil {
			return nil, err
		}
		phase := st.LivePhase()
		if phase == "" || phase == LivePhaseLive {
			return st, nil
		}
		left := deadline.Sub(clk.Now())
		if left <= 0 {
			return nil, localError("untilLive", CodeLiveTimeout, "",
				fmt.Sprintf("%s is still %s after %g s", st.Video, phase, timeout.Seconds()))
		}
		if err := s.c.sleep(ctx, min(UntilLivePoll, left)); err != nil {
			return nil, transportError("untilLive", ctx, err)
		}
	}
}

// Picture returns the camera picture live frame g (FrameAt(g), or LatestFrame() for the newest) was interpreted from:
// draw a frame's layers over its own picture with Picture(ctx, FrameAt(u.Frame.G)). The newest 64 frames of a camera
// session are kept (older and warm-up frames: not_retained); a recording has none (not_found: its picture is the
// proxy video). InSession(session) refuses a frame of a session that has ended (410 session_gone).
func (s *Session) Picture(ctx context.Context, g FrameRef, opts ...ControlOption) (*Picture, error) {
	cfg, _ := controls(opts)
	return s.c.api.GetFramePicture(ctx, g, &PictureParams{Session: cfg.session})
}

// Focus highlights a stone of the world for every viewer: with PickedAt(g), as it was in frame g; with
// InSession(session), only while the shared session is still that one (else 409 stale_session).
func (s *Session) Focus(ctx context.Context, stoneID int64, opts ...ControlOption) (*State, error) {
	cfg, p := controls(opts)
	reply, err := s.c.api.PutFocus(ctx, PutFocusParams{StoneID: stoneID, G: cfg.g, Session: cfg.session,
		DryRun: p.DryRun})
	if err != nil {
		return nil, err
	}
	s.remember(reply.State, p)
	return &reply.State, nil
}

// ClearFocus clears the highlight for every viewer.
func (s *Session) ClearFocus(ctx context.Context, opts ...ControlOption) (*State, error) {
	p := controlParams(opts)
	reply, err := s.c.api.DeleteFocus(ctx, p)
	if err != nil {
		return nil, err
	}
	s.remember(reply.State, p)
	return &reply.State, nil
}

// Clock reads the session and returns a SessionClock that maps between media time, server wall time and local
// wall time. The sample is one request, timed on its own (a failed one is measured again from the start, after the
// server's hint or the backoff), so the round trip and the offset never include a retry's wait.
func (s *Session) Clock(ctx context.Context) (*SessionClock, error) {
	clk := s.c.clock
	for attempt := 0; ; attempt++ {
		sent := clk.Now()
		reply, err := s.c.api.getSession(ctx, true)
		recv := clk.Now()
		if err == nil {
			s.remember(reply.State, nil)
			return NewSessionClock(reply, sent, recv, clk), nil
		}
		if !IsRetryable(err) || attempt >= s.c.retries {
			return nil, err
		}
		wait, ok := AsError(err).RetryDelay()
		if !ok {
			wait = s.c.backoffDelay(attempt)
		}
		if serr := s.c.sleep(ctx, wait); serr != nil {
			return nil, transportError("getSession", ctx, serr)
		}
	}
}

// SessionClock maps between media time, the server's wall clock and the local wall clock. The offset of the two
// wall clocks is offset = server_recv - (t_send + t_recv)/2 (the portal's sampler).
type SessionClock struct {
	// State is the session state the clock was read from.
	State State
	// ServerNow and ServerRecv are the reply's server times (epoch seconds).
	ServerNow  float64
	ServerRecv float64
	// Offset is server wall time minus local wall time, seconds.
	Offset float64
	// RTT is the request's round trip.
	RTT   time.Duration
	clock Clock
}

// NewSessionClock makes a clock from a getSession reply and the local send and receive times.
func NewSessionClock(reply *SessionReply, sent, recv time.Time, clk Clock) *SessionClock {
	if clk == nil {
		clk = SystemClock{}
	}
	return &SessionClock{State: reply.State, ServerNow: reply.ServerNow, ServerRecv: reply.ServerRecv,
		Offset: reply.ServerRecv - (epoch(sent)+epoch(recv))/2, RTT: recv.Sub(sent), clock: clk}
}

// IsLive reports whether the session shows a live camera.
func (c *SessionClock) IsLive() bool { return c.State.IsLive() }

// ServerTime is the server's wall time now (epoch seconds), estimated from the local clock and the offset.
func (c *SessionClock) ServerTime() float64 { return epoch(c.clock.Now()) + c.Offset }

// holdStart is when media time anchor_media is shown: after the hold of a select or seek.
func (c *SessionClock) holdStart() float64 {
	if c.State.HoldUntil > c.State.AnchorWall {
		return c.State.HoldUntil
	}
	return c.State.AnchorWall
}

// MediaTimeAt is the media time the shared playhead shows at a server wall time.
func (c *SessionClock) MediaTimeAt(serverWall float64) float64 {
	st := c.State
	t := st.AnchorMedia
	if st.Playing {
		if d := serverWall - c.holdStart(); d > 0 {
			t += float64(d * st.Rate) // the conversion keeps the product rounded on its own (no fused multiply-add)
		}
	}
	if st.Duration != nil && t > *st.Duration {
		t = *st.Duration
	}
	return t
}

// MediaTimeNow is the media time the shared playhead shows now (on a live camera, seconds since the session started,
// minus its lag once it is live).
func (c *SessionClock) MediaTimeNow() float64 { return c.MediaTimeAt(c.ServerTime()) }

// WallOf is the server wall time (epoch seconds) at which the playhead shows (or showed) media time t at the
// current rate: from the end of the hold while playing, and mapped back from the pause (anchor_wall) while paused;
// nil when the rate is not above 0.
func (c *SessionClock) WallOf(t float64) *float64 {
	st := c.State
	if !(st.Rate > 0) {
		return nil
	}
	start := st.AnchorWall
	if st.Playing {
		start = c.holdStart()
	}
	w := start + (t-st.AnchorMedia)/st.Rate
	return &w
}

// LocalWallOf is WallOf on the local wall clock.
func (c *SessionClock) LocalWallOf(t float64) *float64 {
	w := c.WallOf(t)
	if w == nil {
		return nil
	}
	v := *w - c.Offset
	return &v
}

// Progress is the media time now as a fraction of the duration, nil on a live camera (no duration).
func (c *SessionClock) Progress() *float64 {
	d := c.State.Duration
	if d == nil || *d <= 0 {
		return nil
	}
	p := c.MediaTimeNow() / *d
	return &p
}
