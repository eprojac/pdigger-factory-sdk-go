package pdiggerfactory

import (
	"encoding/json"
)

// This file has one Go type per schema of components/schemas in spec/openapi.yaml (the names follow the schema
// names, Go-cased; schema Error is ErrorReply, since Error is the SDK's error type). Parsing is tolerant: unknown
// fields are ignored, optional and redactable fields are pointers, omitted slices or empty strings. Integers of the
// spec are int64.

// ---- errors and meta ----------------------------------------------------------------------------------------------

// Retained is the retained frame window of the current session.
type Retained struct {
	Session int64    `json:"session"`
	G0      *int64   `json:"g0,omitempty"`
	G1      *int64   `json:"g1,omitempty"`
	T0      *float64 `json:"t0,omitempty"`
	T1      *float64 `json:"t1,omitempty"`
	N       int64    `json:"n"`
}

// ErrorReply is the body of an API error (schema Error). The SDK turns it into an *Error.
type ErrorReply struct {
	Error    string    `json:"error"`
	Code     ErrorCode `json:"code"`
	RetryMS  *int64    `json:"retry_ms,omitempty"`
	Field    string    `json:"field,omitempty"`
	Retained *Retained `json:"retained,omitempty"`
	Waiting  []string  `json:"waiting,omitempty"`
}

// DocsPolicy is ApiRoot.docs_policy.
type DocsPolicy struct {
	ForceDryRun bool `json:"force_dry_run"`
}

// APIRoot is the entry point (schema ApiRoot).
type APIRoot struct {
	API        string            `json:"api"`
	Build      string            `json:"build"`
	Boot       string            `json:"boot"`
	Backend    string            `json:"backend,omitempty"`
	Spec       string            `json:"spec"`
	Docs       string            `json:"docs"`
	Layers     map[string]string `json:"layers"`
	Render     map[string]string `json:"render"`
	Limits     map[string]any    `json:"limits,omitempty"`
	DocsPolicy *DocsPolicy       `json:"docs_policy,omitempty"`
}

// ModelEngine is one engine of the running model.
type ModelEngine struct {
	Stage          string `json:"stage"`
	SHA256         string `json:"sha256"`
	Bytes          *int64 `json:"bytes,omitempty"`
	ONNXSHA256     string `json:"onnx_sha256,omitempty"`
	INT4Weights    *int64 `json:"int4_weights,omitempty"`
	INT4Exceptions *int64 `json:"int4_exceptions,omitempty"`
}

// ModelInfo is the running model (schema ModelInfo, "mdl1").
type ModelInfo struct {
	Schema     string        `json:"schema"`
	Checkpoint string        `json:"checkpoint"`
	Name       string        `json:"name,omitempty"`
	Stage      string        `json:"stage,omitempty"`
	Backend    string        `json:"backend"`
	Profile    *string       `json:"profile"`
	TensorRT   *string       `json:"tensorrt"`
	Engines    []ModelEngine `json:"engines"`
}

// TopologySides holds a joint index per hand side.
type TopologySides struct {
	Left  *int64 `json:"left,omitempty"`
	Right *int64 `json:"right,omitempty"`
}

// Topology is the skeleton topology, class names, palette and coordinate constants (schema Topology, "top1"). Its
// methods are the topology helpers of the codec section: BonePairs, WristJoint, TipJoints, IsNever.
type Topology struct {
	Schema  string             `json:"schema"`
	Joints  int64              `json:"joints"`
	Never   []int64            `json:"never"`
	Wrist   TopologySides      `json:"wrist"`
	Tips    map[string][]int64 `json:"tips"`
	Bones   [][]int64          `json:"bones"`
	Actions []string           `json:"actions"`
	Held    []string           `json:"held"`
	Colors  map[string]string  `json:"colors"`
	Grid    []int64            `json:"grid"`
}

// Health is the liveness reply.
type Health struct {
	OK      bool   `json:"ok"`
	Worker  string `json:"worker"`
	Viewers int64  `json:"viewers"`
}

// Ready is the readiness reply.
type Ready struct {
	Ready   bool     `json:"ready"`
	Waiting []string `json:"waiting"`
}

// HostStatus is one helper host of the server.
type HostStatus struct {
	State    string   `json:"state"`
	Restarts int64    `json:"restarts"`
	RSSMB    *float64 `json:"rss_mb,omitempty"`
	Queue    *int64   `json:"queue,omitempty"`
}

// Status is everything an operator wants on one page.
type Status struct {
	Build     string                `json:"build"`
	Boot      string                `json:"boot"`
	UptimeS   float64               `json:"uptime_s"`
	Page      string                `json:"page"`
	Worker    WorkerStatus          `json:"worker"`
	Retained  *Retained             `json:"retained,omitempty"`
	Stable    StableStatus          `json:"stable"`
	Hosts     map[string]HostStatus `json:"hosts"`
	Memory    map[string]any        `json:"memory,omitempty"`
	Viewers   int64                 `json:"viewers"`
	LongPolls *int64                `json:"long_polls,omitempty"`
	Streams   *int64                `json:"streams,omitempty"`
}

// ---- sources ------------------------------------------------------------------------------------------------------

// Camera is a source's camera model (intrinsics K, camera-to-world rotation and translation).
type Camera struct {
	K          [][]float64 `json:"K,omitempty"`
	RC2W       [][]float64 `json:"R_c2w,omitempty"`
	TC2W       []float64   `json:"t_c2w,omitempty"`
	Gauge      string      `json:"gauge,omitempty"`
	Calibrated *bool       `json:"calibrated,omitempty"`
}

// Source kinds.
const (
	SourceKindRecording = "recording"
	SourceKindCamera    = "camera"
)

// LiveSourceID is the id the design gave the live camera before the plant cameras shipped (each camera now has its
// own id, its serial, such as "CAM00000001"): a state whose video is "live" counts as a camera.
const LiveSourceID = "live"

// Source is one source of the shared session (schema VideoEntry): a recording, or one of the plant's live cameras
// (Kind "camera", Live, not Seekable). The unit sends kind, live and seekable for its cameras only: an entry without
// them is a seekable recording. Parsing fills each missing one from the others (no live: Kind is "camera"; no kind:
// "camera" when Live, else "recording"; no seekable: not Live), as the other languages do. Duration, Frames and DefaultStart
// are nil for a camera, Proxy is empty (its pictures come with its frames: Session.Picture), and Hold is its
// warm-up: the seconds of camera frames the interpreter runs on before the first is shown.
type Source struct {
	ID           string         `json:"id"`
	Title        string         `json:"title,omitempty"`
	Subtitle     string         `json:"subtitle,omitempty"`
	Duration     *float64       `json:"duration"`
	FPS          float64        `json:"fps"`
	Frames       *int64         `json:"frames"`
	GridStep     int64          `json:"grid_step"`
	DefaultStart *float64       `json:"default_start,omitempty"`
	Hold         *float64       `json:"hold,omitempty"`
	Proxy        string         `json:"proxy"`
	ProxyWH      []int64        `json:"proxy_wh"`
	NativeWH     []int64        `json:"native_wh"`
	SourceWH     []int64        `json:"source_wh,omitempty"`
	Crop         []int64        `json:"crop,omitempty"`
	Profile      string         `json:"profile,omitempty"`
	Camera       *Camera        `json:"camera,omitempty"`
	HeldOut      [][]float64    `json:"held_out,omitempty"`
	StoneModel   map[string]any `json:"stone_model,omitempty"`
	Kind         string         `json:"kind"`
	Live         bool           `json:"live"`
	Seekable     bool           `json:"seekable"`
	// Station is a camera's plant station (empty for a recording).
	Station string `json:"station,omitempty"`
}

// VideoEntry is the schema name of Source.
type VideoEntry = Source

// UnmarshalJSON parses a catalog entry and fills the defaults of kind, live and seekable.
func (s *Source) UnmarshalJSON(b []byte) error {
	type plain Source
	var aux struct {
		plain
		Kind     *string `json:"kind"`
		Live     *bool   `json:"live"`
		Seekable *bool   `json:"seekable"`
	}
	if err := json.Unmarshal(b, &aux); err != nil {
		return err
	}
	*s = Source(aux.plain)
	kind := ""
	if aux.Kind != nil {
		kind = *aux.Kind
	}
	s.Live = kind == SourceKindCamera
	if aux.Live != nil {
		s.Live = *aux.Live
	}
	switch {
	case kind != "":
		s.Kind = kind
	case s.Live:
		s.Kind = SourceKindCamera
	default:
		s.Kind = SourceKindRecording
	}
	s.Seekable = !s.Live
	if aux.Seekable != nil {
		s.Seekable = *aux.Seekable
	}
	return nil
}

// IsLive reports whether this source is a live camera (no duration, no seek, rate 1.0).
func (s *Source) IsLive() bool { return s.Live }

// Name is the source's title, else its id.
func (s *Source) Name() string {
	if s.Title != "" {
		return s.Title
	}
	return s.ID
}

// VideoList is the public catalog.
type VideoList struct {
	Schema       string         `json:"schema"`
	Title        string         `json:"title,omitempty"`
	DefaultVideo string         `json:"default_video,omitempty"`
	Model        map[string]any `json:"model,omitempty"`
	Videos       []Source       `json:"videos"`
	// ETag is the reply's entity tag (send it back with ListVideosParams.IfNoneMatch).
	ETag string `json:"-"`
	// NotModified is true for a 304 reply to IfNoneMatch; the other fields are then empty.
	NotModified bool `json:"-"`
}

// ---- session --------------------------------------------------------------------------------------------------------

// Focus is the shared stone focus.
type Focus struct {
	N       int64   `json:"n"`
	Session int64   `json:"session"`
	Video   string  `json:"video"`
	ID      int64   `json:"id"`
	G       int64   `json:"g"`
	T       float64 `json:"t"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
	Z       float64 `json:"z"`
	L       float64 `json:"L"`
	W       float64 `json:"W"`
	Yaw     float64 `json:"yaw"`
}

// Phases of a live camera session (Live.Phase).
const (
	// LivePhaseConnecting: no camera frame yet, or none for 5 s, or the session was just played (the worker's reason
	// says why: "nobody is watching" when the worker is idle).
	LivePhaseConnecting = "connecting"
	// LivePhaseWarming: the interpreter runs on the camera's first frames (the source's Hold seconds of them, 3 on
	// the plant cameras); nothing is released.
	LivePhaseWarming = "warming"
	// LivePhaseLive: the frames that follow are released, from a warm world. A pause keeps it (nothing arrives while
	// paused).
	LivePhaseLive = "live"
)

// Live is the live part of a camera session's state (schema Live; nil on a recording). Each phase change bumps the
// state's revision, so long polls and the clock stream event carry it. The phase is the unit's last word on the
// camera's stream: Play goes back to connecting (in the play's own revision), and so does a playing camera that sent
// no frame for 5 s (it stopped answering, or the worker idles with nobody watching: 20 s after the last viewer);
// the next frame then starts a new warm-up, and G jumps over the break.
type Live struct {
	// Phase is LivePhaseConnecting, LivePhaseWarming or LivePhaseLive.
	Phase string `json:"phase"`
	// Warmup is the seconds of camera frames interpreted before any is shown.
	Warmup float64 `json:"warmup"`
	// Since is when the session started (epoch seconds); a frame's T is its arrival in seconds since then.
	Since float64 `json:"since"`
	// Lag is the seconds the shown picture is behind its arrival, interpreter included, when the session went
	// live; play resumes at now - Since - Lag.
	Lag float64 `json:"lag"`
}

// State is the shared session state: the clock of the shared playhead and the focus.
type State struct {
	Session     int64    `json:"session"`
	Revision    int64    `json:"revision"`
	Video       string   `json:"video"`
	Playing     bool     `json:"playing"`
	Rate        float64  `json:"rate"`
	AnchorMedia float64  `json:"anchor_media"`
	AnchorWall  float64  `json:"anchor_wall"`
	HoldUntil   float64  `json:"hold_until"`
	Duration    *float64 `json:"duration"`
	ChangedBy   string   `json:"changed_by"`
	Focus       *Focus   `json:"focus"`
	// MediaTime is seconds into the source; on a live camera, seconds since the session started (minus Live.Lag
	// once it is live).
	MediaTime float64 `json:"media_time"`
	// Live is set on a live camera session: its phase, warm-up and lag.
	Live *Live `json:"live,omitempty"`
}

// IsLive reports whether the session shows a live camera.
func (s *State) IsLive() bool { return s.Live != nil || s.Video == LiveSourceID }

// LivePhase is the phase of a live camera session (LivePhaseConnecting, LivePhaseWarming or LivePhaseLive), "" on a
// recording.
func (s *State) LivePhase() string {
	if s.Live == nil {
		return ""
	}
	return s.Live.Phase
}

// WorkerStatus is the interpreter worker's status.
type WorkerStatus struct {
	State   string             `json:"state"`
	Reason  string             `json:"reason"`
	FPS     *float64           `json:"fps,omitempty"`
	FrameMS *float64           `json:"frame_ms,omitempty"`
	CycleMS *float64           `json:"cycle_ms,omitempty"`
	Session *int64             `json:"session,omitempty"`
	G       *int64             `json:"g,omitempty"`
	GPU     *float64           `json:"gpu,omitempty"`
	Errors  *int64             `json:"errors,omitempty"`
	StageMS map[string]float64 `json:"stage_ms,omitempty"`
	Engines map[string]string  `json:"engines,omitempty"`
	Model   string             `json:"model,omitempty"`
	Video   string             `json:"video,omitempty"`
}

// HoldStatus is the refinement of a paused frame by the world host.
type HoldStatus struct {
	Session int64   `json:"session"`
	G       int64   `json:"g"`
	At      *int64  `json:"at,omitempty"`
	State   string  `json:"state"`
	Rounds  int64   `json:"rounds"`
	Rev     int64   `json:"rev"`
	MS      float64 `json:"ms"`
	Resteps int64   `json:"resteps"`
}

// StableStatus is the Stable sim's status.
type StableStatus struct {
	State    string      `json:"state"`
	Fits     *int64      `json:"fits,omitempty"`
	Evidence *bool       `json:"evidence,omitempty"`
	Message  string      `json:"message,omitempty"`
	Fixer    string      `json:"fixer,omitempty"`
	Hold     *HoldStatus `json:"hold,omitempty"`
}

// SessionReply is the reply of getSession.
type SessionReply struct {
	ServerNow  float64      `json:"server_now"`
	ServerRecv float64      `json:"server_recv"`
	State      State        `json:"state"`
	Worker     WorkerStatus `json:"worker"`
	Viewers    int64        `json:"viewers"`
	Stable     StableStatus `json:"stable"`
}

// SelectRequest is the body of sessionSelect. Leave Time nil for a live camera.
type SelectRequest struct {
	Video string   `json:"video"`
	Time  *float64 `json:"time,omitempty"`
}

// SeekRequest is the body of sessionSeek.
type SeekRequest struct {
	Video string  `json:"video,omitempty"`
	Time  float64 `json:"time"`
}

// EmptyRequest is the (ignored) body of sessionPlay and sessionPause.
type EmptyRequest struct{}

// FocusRequest is the body of putFocus.
type FocusRequest struct {
	StoneID int64  `json:"stone_id"`
	G       *int64 `json:"g,omitempty"`
	Session *int64 `json:"session,omitempty"`
}

// ControlReply is the reply of every control.
type ControlReply struct {
	State      State   `json:"state"`
	ServerNow  float64 `json:"server_now"`
	ServerRecv float64 `json:"server_recv"`
	DryRun     bool    `json:"dry_run,omitempty"`
}

// ResolvedFocus is the focused stone's path.
type ResolvedFocus struct {
	ID     int64       `json:"id"`
	Path2D [][]float64 `json:"path2d"`
	Path3D [][]float64 `json:"path3d"`
}

// FocusReply is the reply of getFocus.
type FocusReply struct {
	Focus    *Focus         `json:"focus"`
	Resolved *ResolvedFocus `json:"resolved"`
}

// ClockEvent is the data of a "clock" stream event: the shared state and the server's wall clock.
type ClockEvent struct {
	State
	ServerNow float64 `json:"server_now"`
}

// ResetEvent is the data of a "reset" stream event: the server restarted (another boot).
type ResetEvent struct {
	Boot   string `json:"boot"`
	Cursor int64  `json:"cursor"`
}

// GapEvent is the data of a "gap" stream event: that many items were skipped for a slow consumer.
type GapEvent struct {
	Gap int64 `json:"gap"`
}

// ---- the raw layer (sip1) -------------------------------------------------------------------------------------------

// Box is a box tuple [x0, y0, x1, y1, rot, presence] in content units.
type Box struct {
	X0, Y0, X1, Y1 float64
	Rot            bool
	Presence       float64
}

// UnmarshalJSON parses the tuple form; booleans and numbers are accepted in every position.
func (b *Box) UnmarshalJSON(data []byte) error {
	v, err := tuple(data)
	if err != nil {
		return err
	}
	*b = Box{X0: v.num(0), Y0: v.num(1), X1: v.num(2), Y1: v.num(3), Rot: v.num(4) != 0, Presence: v.num(5)}
	return nil
}

// MarshalJSON writes the tuple form.
func (b Box) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{b.X0, b.Y0, b.X1, b.Y1, b.Rot, b.Presence})
}

// HandBox is a hand tuple [u, v, p].
type HandBox struct {
	U, V, P float64
}

// UnmarshalJSON parses the tuple form.
func (h *HandBox) UnmarshalJSON(data []byte) error {
	v, err := tuple(data)
	if err != nil {
		return err
	}
	*h = HandBox{U: v.num(0), V: v.num(1), P: v.num(2)}
	return nil
}

// MarshalJSON writes the tuple form.
func (h HandBox) MarshalJSON() ([]byte, error) { return json.Marshal([]float64{h.U, h.V, h.P}) }

type tupleValues []any

func tuple(data []byte) (tupleValues, error) {
	var v []any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return v, nil
}

func (t tupleValues) num(i int) float64 {
	if i >= len(t) {
		return 0
	}
	switch x := t[i].(type) {
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

// Sip1Maps are the packet's maps (base64 of zlib of u8 row-major); Human is redacted in public recordings.
type Sip1Maps struct {
	Grid  []int64 `json:"grid"`
	Human string  `json:"human,omitempty"`
	Tray  string  `json:"tray"`
	Stone string  `json:"stone"`
	Conf  string  `json:"conf"`
}

// Sip1Depth is the packet's depth map; Q is redacted in public recordings.
type Sip1Depth struct {
	Q      string  `json:"q,omitempty"`
	Lo     float64 `json:"lo"`
	Hi     float64 `json:"hi"`
	A      float64 `json:"a"`
	B      float64 `json:"b"`
	Anchor string  `json:"anchor"`
}

// Sip1Boxes are the packet's boxes.
type Sip1Boxes struct {
	Stone     *Box     `json:"stone,omitempty"`
	Person    *Box     `json:"person,omitempty"`
	HandLeft  *HandBox `json:"hand_left,omitempty"`
	HandRight *HandBox `json:"hand_right,omitempty"`
}

// Sip1Person is the person crop; Joints are 70 entries [u, v, vis, x, y, z] or nil (face joints are nil in public
// recordings, and Mask and Alpha are removed).
type Sip1Person struct {
	Present     *bool       `json:"present,omitempty"`
	Box         []float64   `json:"box,omitempty"`
	Rot         *bool       `json:"rot,omitempty"`
	Grid        []int64     `json:"grid,omitempty"`
	Mask        string      `json:"mask,omitempty"`
	Alpha       string      `json:"alpha,omitempty"`
	Joints      [][]float64 `json:"joints,omitempty"`
	Scale       *float64    `json:"scale,omitempty"`
	BonesUsed   *int64      `json:"bones_used,omitempty"`
	PersonScale *float64    `json:"person_scale,omitempty"`
	ScaleAnchor string      `json:"scale_anchor,omitempty"`
}

// Sip1Stones is the stone crop; Items are tuples [u, v, w, h, score, X, Y, Z, w_m, h_m, yaw]. On a 4x packet
// (Source "hr4") FG is on Grid [160, 256] and Tray on TrayGrid [80, 128], and the items come from the base lattice
// CentreGrid [20, 32]; a base-decode packet (recorded before the 4x stone head) has Grid [40, 64], TrayGrid [20, 32]
// and none of Source, Centres and CentreGrid. Read every map's size from its own grid.
type Sip1Stones struct {
	Present  *bool       `json:"present,omitempty"`
	Count    *int64      `json:"count,omitempty"`
	Box      []float64   `json:"box,omitempty"`
	Rot      *bool       `json:"rot,omitempty"`
	Grid     []int64     `json:"grid,omitempty"`
	FG       string      `json:"fg,omitempty"`
	TrayGrid []int64     `json:"tray_grid,omitempty"`
	Tray     string      `json:"tray,omitempty"`
	Items    [][]float64 `json:"items,omitempty"`
	// Source is "hr4" on a 4x packet (the masks, tray and yaw come from the 4x stone outputs), empty on a base one.
	Source string `json:"source,omitempty"`
	// Centres is "base" on a 4x packet: the instances come from the base centre peaks.
	Centres string `json:"centres,omitempty"`
	// CentreGrid is the lattice the instances are found on, [20, 32] (4x packets only).
	CentreGrid []int64 `json:"centre_grid,omitempty"`
}

// Sip1Tray is the tray plane.
type Sip1Tray struct {
	Valid       *bool     `json:"valid,omitempty"`
	Normal      []float64 `json:"normal,omitempty"`
	D           *float64  `json:"d,omitempty"`
	Anchor      string    `json:"anchor,omitempty"`
	Floor       *float64  `json:"floor,omitempty"`
	FloorAnchor string    `json:"floor_anchor,omitempty"`
}

// Sip1Hand is one hand of the packet, as the models wrote it. The temporal model's fields (PAction, Action,
// PInteraction, PHold, Contact, Held, PHeld, Stone) are written for both hands on every packet; when Visible is false
// they describe no hand, and the overlay and card layers leave them out.
type Sip1Hand struct {
	Visible      *bool     `json:"visible,omitempty"`
	PAction      []float64 `json:"p_action,omitempty"`
	Action       string    `json:"action,omitempty"`
	PInteraction *float64  `json:"p_interaction,omitempty"`
	PHold        *float64  `json:"p_hold,omitempty"`
	Contact      *float64  `json:"contact,omitempty"`
	Held         string    `json:"held,omitempty"`
	PHeld        []float64 `json:"p_held,omitempty"`
	Stone        *int64    `json:"stone,omitempty"`
	Wrist        []float64 `json:"wrist,omitempty"`
	PWrist       *float64  `json:"p_wrist,omitempty"`
}

// Sip1Hands are both hands of the packet.
type Sip1Hands struct {
	Left  *Sip1Hand `json:"left,omitempty"`
	Right *Sip1Hand `json:"right,omitempty"`
}

// Sip1Packet is the model codec packet (layer raw, schema "sip1").
type Sip1Packet struct {
	Schema    string             `json:"schema"`
	Maps      Sip1Maps           `json:"maps"`
	Depth     Sip1Depth          `json:"depth"`
	Boxes     Sip1Boxes          `json:"boxes"`
	Person    Sip1Person         `json:"person"`
	Stones    Sip1Stones         `json:"stones"`
	Tray      Sip1Tray           `json:"tray"`
	Hands     Sip1Hands          `json:"hands"`
	Both      float64            `json:"both"`
	Session   int64              `json:"session"`
	Video     string             `json:"video"`
	G         int64              `json:"g"`
	I         int64              `json:"i"`
	T         float64            `json:"t"`
	PastValid bool               `json:"past_valid"`
	DtPast    float64            `json:"dt_past"`
	MS        map[string]float64 `json:"ms"`
	Cursor    *int64             `json:"cursor,omitempty"`
}

// ---- overlay, scene and cards layers --------------------------------------------------------------------------------

// Rect is the board rectangle in the scene world.
type Rect struct {
	CX    float64 `json:"cx"`
	CY    float64 `json:"cy"`
	Theta float64 `json:"theta"`
	HW    float64 `json:"hw"`
	HH    float64 `json:"hh"`
}

// Wrist is one wrist of the overlay layer. Filled is visible and P(interaction) >= .5, Ring visible and contact >= .5;
// Stone is an index into this frame's items, or -1 (always for a hand that is not visible).
type Wrist struct {
	UV      []float64 `json:"uv,omitempty"`
	Visible bool      `json:"visible"`
	Filled  bool      `json:"filled"`
	Ring    bool      `json:"ring"`
	Stone   *int64    `json:"stone,omitempty"`
}

// Wrists are both wrists.
type Wrists struct {
	Left  *Wrist `json:"left,omitempty"`
	Right *Wrist `json:"right,omitempty"`
}

// OverlayRegions are the region maps (base64 u8 rows*cols); Human is redacted in public recordings.
type OverlayRegions struct {
	Rows  int    `json:"rows"`
	Cols  int    `json:"cols"`
	Human string `json:"human,omitempty"`
	Tray  string `json:"tray,omitempty"`
	Stone string `json:"stone,omitempty"`
}

// OverlayMask is the person-crop label map (0 none, 1 human, 2 left, 3 right); Label and Alpha are redacted in
// public recordings.
type OverlayMask struct {
	Rows    int       `json:"rows"`
	Cols    int       `json:"cols"`
	Label   string    `json:"label,omitempty"`
	Alpha   string    `json:"alpha,omitempty"`
	Box     []float64 `json:"box,omitempty"`
	Rot     *bool     `json:"rot,omitempty"`
	Present bool      `json:"present"`
}

// OverlayBoxes are the overlay's person and stone boxes.
type OverlayBoxes struct {
	Person *Box `json:"person,omitempty"`
	Stone  *Box `json:"stone,omitempty"`
}

// OverlayItem is one detected stone of the overlay layer.
type OverlayItem struct {
	T       []float64   `json:"t"`
	Prev    int64       `json:"prev"`
	Side    *string     `json:"side,omitempty"`
	Outline [][]float64 `json:"outline"`
}

// OverlayLayer is the 2-D per-packet layer ("ovl1").
type OverlayLayer struct {
	Schema   string         `json:"schema"`
	NativeWH []int64        `json:"native_wh"`
	Regions  OverlayRegions `json:"regions"`
	Mask     OverlayMask    `json:"mask"`
	Boxes    OverlayBoxes   `json:"boxes"`
	Joints   [][]float64    `json:"joints"`
	Scale    *float64       `json:"scale,omitempty"`
	Wrists   Wrists         `json:"wrists"`
	PrevG    *int64         `json:"prev_g,omitempty"`
	Items    []OverlayItem  `json:"items"`
}

// ScenePlane is the board plane.
type ScenePlane struct {
	N           []float64 `json:"n"`
	D           float64   `json:"d"`
	Floor       float64   `json:"floor"`
	Anchor      string    `json:"anchor,omitempty"`
	FloorAnchor string    `json:"floor_anchor,omitempty"`
}

// SceneBoard holds the per-packet board.
type SceneBoard struct {
	Raw *Rect `json:"raw,omitempty"`
}

// SurfaceQuads are the quads bitsets ((rows-1)*(cols-1), row-major).
type SurfaceQuads struct {
	Raw    string `json:"raw"`
	Stable string `json:"stable"`
}

// SceneSurface is the observed surface in compact form; Q is redacted in public recordings.
type SceneSurface struct {
	Rows  int          `json:"rows"`
	Cols  int          `json:"cols"`
	Q     string       `json:"q,omitempty"`
	Lo    float64      `json:"lo"`
	Hi    float64      `json:"hi"`
	A     float64      `json:"a"`
	B     float64      `json:"b"`
	Flat  string       `json:"flat"`
	Quads SurfaceQuads `json:"quads"`
	Lift  float64      `json:"lift"`
}

// SceneSkeleton is the skeleton in the scene world: 70 entries [x, y, z] or nil.
type SceneSkeleton struct {
	World [][]float64 `json:"world"`
}

// Ring is a wrist ring in the scene world.
type Ring struct {
	Side string    `json:"side"`
	Pos  []float64 `json:"pos"`
}

// SceneItem is one detected stone in the scene world.
type SceneItem struct {
	X    *float64 `json:"x,omitempty"`
	Y    *float64 `json:"y,omitempty"`
	Yaw  *float64 `json:"yaw,omitempty"`
	L    *float64 `json:"L,omitempty"`
	S    *float64 `json:"S,omitempty"`
	H    *float64 `json:"h,omitempty"`
	Side *string  `json:"side,omitempty"`
}

// SceneLayer is the 3-D per-packet layer ("scn1").
type SceneLayer struct {
	Schema   string        `json:"schema"`
	Plane    ScenePlane    `json:"plane"`
	Board    SceneBoard    `json:"board"`
	Surface  SceneSurface  `json:"surface"`
	Skeleton SceneSkeleton `json:"skeleton"`
	Rings    []Ring        `json:"rings,omitempty"`
	Items    []SceneItem   `json:"items,omitempty"`
}

// FineAction is one action probability of a hand card.
type FineAction struct {
	Key      string   `json:"key,omitempty"`
	Label    string   `json:"label,omitempty"`
	P        *float64 `json:"p"`
	Reliable *bool    `json:"reliable,omitempty"`
}

// HandCard is a hand's card: a title such as "Holding a stone" and the numbers behind it, the temporal model's
// readings of one hand. For a hand that is not visible they are all nil (ContactOn false, Stone -1, every Fine[i].P
// nil): the model's contact and action guesses there describe no hand.
type HandCard struct {
	Title        string       `json:"title"`
	Tone         string       `json:"tone"`
	Visible      bool         `json:"visible"`
	PInteraction *float64     `json:"pInteraction,omitempty"`
	Contact      *float64     `json:"contact,omitempty"`
	ContactOn    *bool        `json:"contactOn,omitempty"`
	Held         *string      `json:"held,omitempty"`
	PHeld        *float64     `json:"pHeld,omitempty"`
	Action       *string      `json:"action,omitempty"`
	Fine         []FineAction `json:"fine"`
	Hint         *string      `json:"hint,omitempty"`
	Stone        *int64       `json:"stone,omitempty"`
	PWrist       *float64     `json:"pWrist,omitempty"`
}

// StagePart is one stage timing of the cards layer.
type StagePart struct {
	Key   string  `json:"key,omitempty"`
	Label string  `json:"label,omitempty"`
	Fam   string  `json:"fam,omitempty"`
	MS    float64 `json:"ms,omitempty"`
}

// CardsStage is the per-stage timing of the cards layer.
type CardsStage struct {
	Parts []StagePart `json:"parts,omitempty"`
	Total float64     `json:"total,omitempty"`
	FPS   float64     `json:"fps,omitempty"`
}

// CardsNumbers are the cards layer's numbers.
type CardsNumbers struct {
	Stones      *int64   `json:"stones,omitempty"`
	PastValid   *bool    `json:"past_valid,omitempty"`
	DtPast      *float64 `json:"dt_past,omitempty"`
	TrayAnchor  string   `json:"tray_anchor,omitempty"`
	FloorAnchor string   `json:"floor_anchor,omitempty"`
	DepthAnchor string   `json:"depth_anchor,omitempty"`
	G           *int64   `json:"g,omitempty"`
	T           *float64 `json:"t,omitempty"`
	Session     *int64   `json:"session,omitempty"`
}

// CardsLayer is the hand cards layer ("crd1").
type CardsLayer struct {
	Schema  string       `json:"schema"`
	Left    HandCard     `json:"left"`
	Right   HandCard     `json:"right"`
	Stage   *CardsStage  `json:"stage,omitempty"`
	Numbers CardsNumbers `json:"numbers"`
}

// ---- the world layer ------------------------------------------------------------------------------------------------

// Solid is a stone's solid: position, orientation quaternion (x, y, z, w) and dimensions (L, W, h), scene world.
type Solid struct {
	Pos  []float64 `json:"pos"`
	Q    []float64 `json:"q"`
	Dims []float64 `json:"dims"`
}

// StoneView is a stone of the world.
type StoneView struct {
	ID        int64       `json:"id"`
	Mode      string      `json:"mode"`
	Family    string      `json:"family"`
	Shape     string      `json:"shape"`
	Detected  bool        `json:"detected"`
	Uncertain bool        `json:"uncertain"`
	Side      *string     `json:"side"`
	Outline   [][]float64 `json:"outline"`
	Solid     *Solid      `json:"solid"`
	Placed    *int64      `json:"placed,omitempty"`
	Source    string      `json:"source,omitempty"`
	// Look is the stone's look, sent only in the frame it was read on (the 20.20 unit sends looks since build
	// claude-20261120-looks1); nil otherwise. See StoneLook and LookScore.
	Look *StoneLook `json:"look,omitempty"`
}

// StoneLook is a stone's look: the band-pass texture of its face, format V 1, for telling stones apart by appearance.
// The 20.20 unit sends looks since build claude-20261120-looks1 (fixer thin-v1, on every camera unless a catalog
// entry's stone model turns thin.lookOut off): the recordings of 22 November (build claude-20261122-hr4x1) carry
// looks, and those of 17 November were made before it and have none. The tray rules use it when a server sends it
// (section 4.6.2 of the SDK design). The grid holds Nu x Nv cells at a pitch of Sp proxy px, centred on the face At
// and turned with it; D is the cells as base64 of Nu x Nv int8 bytes, row after row, -128 for a cell not read.
type StoneLook struct {
	V  int64     `json:"v"`
	G  int64     `json:"g"`
	Sp float64   `json:"sp"`
	Q  float64   `json:"q"`
	Nu int64     `json:"nu"`
	Nv int64     `json:"nv"`
	Px []float64 `json:"px"`
	At LookFace  `json:"at"`
	D  string    `json:"d"`
}

// LookFace is the face a look was read on, in proxy px: its centre, the long side's angle (radians, x right, y down)
// and its sides.
type LookFace struct {
	CX    float64 `json:"cx"`
	CY    float64 `json:"cy"`
	Theta float64 `json:"theta"`
	L     float64 `json:"L"`
	S     float64 `json:"S"`
}

// HandState is a hand's state in the world: free, touch, grasp, hinge or carry, and the stone it holds.
type HandState struct {
	// State is free, touch, grasp, hinge or carry. Touch and grasp are the temporal model's contact readings. With
	// fixer thin-v1 (dry videos and cameras), a hand in contact that holds no stone and whose wrist is off the board
	// (the regions' tray map) is sent as free, with Away true.
	State     string   `json:"state"`
	Stone     *int64   `json:"stone"`
	RayK      *float64 `json:"rayK,omitempty"`
	Untrusted *bool    `json:"untrusted,omitempty"`
	// Away is present (true) only on a hand sent as free because it is in contact away from the board: a worker's
	// hand busy beside the board, or a false hand on machinery. It does not hold a stone and does not work on the tray.
	Away *bool `json:"away,omitempty"`
}

// WorldHands are both hands of the world.
type WorldHands struct {
	Left  *HandState `json:"left,omitempty"`
	Right *HandState `json:"right,omitempty"`
}

// WorldEvent is an event of the world: what happened in this frame.
type WorldEvent struct {
	Type   string  `json:"type"`
	ID     *int64  `json:"id,omitempty"`
	Side   string  `json:"side,omitempty"`
	Why    string  `json:"why,omitempty"`
	Into   []int64 `json:"into,omitempty"`
	Source string  `json:"source,omitempty"`
}

// WorldStats are the world's counters.
type WorldStats struct {
	Stones    *int64   `json:"stones,omitempty"`
	Confirmed *int64   `json:"confirmed,omitempty"`
	Frozen    *int64   `json:"frozen,omitempty"`
	Fitted    *int64   `json:"fitted,omitempty"`
	Held      *int64   `json:"held,omitempty"`
	StepMS    *float64 `json:"stepMs,omitempty"`
}

// DrawSkeleton are per-packet skeleton draw terms.
type DrawSkeleton struct {
	LogF  float64              `json:"logF"`
	K     map[string]float64   `json:"k"`
	Elbow map[string][]float64 `json:"elbow"`
}

// WorldDraw are per-packet draw terms resolved on the server.
type WorldDraw struct {
	Board    *Rect         `json:"board,omitempty"`
	Skeleton *DrawSkeleton `json:"skeleton,omitempty"`
}

// WorldLayer is the Stable sim's persistent world at one grid frame ("stw1"). Rev is 0 as first released, higher
// after the refinement of a paused frame.
type WorldLayer struct {
	Schema  string         `json:"schema"`
	Session int64          `json:"session"`
	Video   string         `json:"video"`
	G       int64          `json:"g"`
	T       float64        `json:"t"`
	Motion  string         `json:"motion"`
	LogF    float64        `json:"logF"`
	Stones  []StoneView    `json:"stones"`
	Hands   WorldHands     `json:"hands"`
	Events  []WorldEvent   `json:"events"`
	Stats   WorldStats     `json:"stats"`
	Draw    *WorldDraw     `json:"draw,omitempty"`
	Focus   *ResolvedFocus `json:"focus,omitempty"`
	Rev     int64          `json:"rev,omitempty"`
	// Speckle is the unit's speckle answer for a tray (section 4.6.12 of the SDK design), sent once, in the world of
	// the first frame after it was ready (and in that frame's revisions); nil without one, and when it does not fit
	// WorldSpeckle by value.
	Speckle *WorldSpeckle `json:"speckle,omitempty"`
}

// SpeckleMoment is one of the two moments a speckle answer compares: its grid frame G and media time T.
type SpeckleMoment struct {
	G int64   `json:"g"`
	T float64 `json:"t"`
}

// SpeckleFind is one stone of one moment as the speckle check found it at the other moment.
type SpeckleFind struct {
	// ID is the stone's world id at its own moment.
	ID int64 `json:"id"`
	// At is the world id of the stone (not held) at the other moment whose outline holds the centre of the best place
	// (nil when no outline holds it, or when the stone could not be checked).
	At *int64 `json:"at"`
	// Score is the zero-mean normalised cross-correlation of the best place (the highest of the ten turned templates),
	// Second that of the best place outside the best's neighbourhood (-1: none); both nil when the stone was not
	// checked. A find is clean when Score >= 0.30 and Score >= 2 x Second.
	Score  *float64 `json:"score"`
	Second *float64 `json:"second"`
}

// WorldSpeckle is the unit's speckle stone identification of one tray (section 4.6.12 of the SDK design). Every stone
// not held at either moment is checked at the other by its speckle: the high-passed full-resolution luma of the inner
// 62 % of its outline's box (for a stone too large for the unit's GPU kernels, such as a long kerb, the central part
// of that box the kernels take), turned 0 and 180 degrees, each by -4 to 4 degrees.
type WorldSpeckle struct {
	// V is the version of the answer's format (1).
	V int64 `json:"v"`
	// Tray is the unit's count of trays in this session (nearly always the SDK's Tray.N).
	Tray int64 `json:"tray"`
	// Ref is the reference moment, the tray's arrival (or the first later calm frame that could be kept); At the
	// later moment, a calm frame of the tray.
	Ref SpeckleMoment `json:"ref"`
	At  SpeckleMoment `json:"at"`
	// Device is the GPU the check ran on; GPUMS the GPU time of the checks and WallMS the time the unit's speckle
	// service spent on the request, in milliseconds.
	Device string  `json:"device"`
	GPUMS  float64 `json:"gpu_ms"`
	WallMS float64 `json:"wall_ms"`
	// Stones are every stone not held at At, in world order, checked at the reference; Refs every stone not held at
	// the reference, in world order, checked at At.
	Stones []SpeckleFind `json:"stones"`
	Refs   []SpeckleFind `json:"refs"`
	// Pairs are [id at At, id at the reference], by the first id: the stones the unit pairs (a clean find from one
	// lands on the other, and the other's, when clean, on the first).
	Pairs [][2]int64 `json:"pairs"`
}

// Layer names.
const (
	LayerRaw     = "raw"
	LayerOverlay = "overlay"
	LayerScene   = "scene"
	LayerCards   = "cards"
	LayerWorld   = "world"
)

// Frame is one grid frame g (5 per media second) with the layers asked for. Missing lists layers asked for that
// were not ready at the release deadline.
type Frame struct {
	Cursor    int64         `json:"cursor"`
	Session   int64         `json:"session"`
	Video     string        `json:"video"`
	G         int64         `json:"g"`
	I         int64         `json:"i"`
	T         float64       `json:"t"`
	PastValid bool          `json:"past_valid"`
	DtPast    float64       `json:"dt_past"`
	Missing   []string      `json:"missing,omitempty"`
	Raw       *Sip1Packet   `json:"raw,omitempty"`
	Overlay   *OverlayLayer `json:"overlay,omitempty"`
	Scene     *SceneLayer   `json:"scene,omitempty"`
	Cards     *CardsLayer   `json:"cards,omitempty"`
	World     *WorldLayer   `json:"world,omitempty"`
}

// HasLayer reports whether the frame carries the named layer.
func (f *Frame) HasLayer(name string) bool {
	switch name {
	case LayerRaw:
		return f.Raw != nil
	case LayerOverlay:
		return f.Overlay != nil
	case LayerScene:
		return f.Scene != nil
	case LayerCards:
		return f.Cards != nil
	case LayerWorld:
		return f.World != nil
	}
	return false
}

// LiveReply is the reply of the long-poll pollFrames.
type LiveReply struct {
	ServerNow  float64      `json:"server_now"`
	ServerRecv float64      `json:"server_recv"`
	State      State        `json:"state"`
	Worker     WorkerStatus `json:"worker"`
	Viewers    int64        `json:"viewers"`
	Stable     StableStatus `json:"stable"`
	Cursor     int64        `json:"cursor"`
	Frames     []Frame      `json:"frames"`
}

// StoneTrack is one stone with its path since the session began ("sts1").
type StoneTrack struct {
	Schema  string      `json:"schema"`
	Session int64       `json:"session"`
	Video   string      `json:"video"`
	ID      int64       `json:"id"`
	G       int64       `json:"g"`
	T       float64     `json:"t"`
	Stone   *StoneView  `json:"stone"`
	FirstG  int64       `json:"first_g"`
	LastG   int64       `json:"last_g"`
	Path2D  [][]float64 `json:"path2d"`
	Path3D  [][]float64 `json:"path3d"`
}

// LoggedEvent is one event of the event log, with its frame.
type LoggedEvent struct {
	G      int64   `json:"g"`
	T      float64 `json:"t"`
	Type   string  `json:"type"`
	ID     *int64  `json:"id,omitempty"`
	Side   string  `json:"side,omitempty"`
	Why    string  `json:"why,omitempty"`
	Into   []int64 `json:"into,omitempty"`
	Source string  `json:"source,omitempty"`
}

// EventLog is the event log of this session.
type EventLog struct {
	Session   int64         `json:"session"`
	Video     string        `json:"video"`
	Events    []LoggedEvent `json:"events"`
	NextAfter int64         `json:"next_after"`
	OldestG   *int64        `json:"oldest_g,omitempty"`
}

// ---- render-ready draw lists ----------------------------------------------------------------------------------------

// DrawStone2D is a stone of the 2-D draw list.
type DrawStone2D struct {
	ID        *int64      `json:"id"`
	Outline   [][]float64 `json:"outline"`
	Style     string      `json:"style"`
	Side      *string     `json:"side"`
	Alpha     float64     `json:"alpha"`
	Detected  *bool       `json:"detected,omitempty"`
	Uncertain *bool       `json:"uncertain,omitempty"`
	Shape     *string     `json:"shape,omitempty"`
	Mode      *string     `json:"mode,omitempty"`
}

// Image2D is an image primitive (a map raster), native px: draw the image at URL into the box X, Y, W, H with Alpha.
// A map of the person crop (the mask) also has Rot and Transform: Transform is the affine map [a, b, c, d, e, f] from
// the image's own pixels to native px (x' = a*x + c*y + e, y' = b*x + d*y + f, the order of a 2-D canvas'
// setTransform), and Rot says whether the crop is rotated.
type Image2D struct {
	URL       string    `json:"url"`
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
	W         float64   `json:"w"`
	H         float64   `json:"h"`
	Alpha     float64   `json:"alpha"`
	Rot       *bool     `json:"rot,omitempty"`
	Transform []float64 `json:"transform,omitempty"`
}

// Polygon2D is a polygon primitive, native px.
type Polygon2D struct {
	Points    [][]float64 `json:"points"`
	Stroke    *string     `json:"stroke"`
	Fill      *string     `json:"fill"`
	FillAlpha float64     `json:"fill_alpha"`
	Width     float64     `json:"width"`
	Dash      []float64   `json:"dash,omitempty"`
	Alpha     float64     `json:"alpha"`
}

// Segment2D is a line segment primitive, native px.
type Segment2D struct {
	From  []float64 `json:"from"`
	To    []float64 `json:"to"`
	Color string    `json:"color"`
	Width float64   `json:"width"`
	Alpha float64   `json:"alpha"`
}

// Circle2D is a circle primitive, native px; a nil Stroke or Fill is not drawn.
type Circle2D struct {
	At     []float64 `json:"at"`
	R      float64   `json:"r"`
	Stroke *string   `json:"stroke"`
	Fill   *string   `json:"fill"`
	Alpha  float64   `json:"alpha"`
}

// Label2D is a text label primitive; At is given for the canvas textBaseline Baseline.
type Label2D struct {
	At         []float64 `json:"at"`
	Text       string    `json:"text"`
	Color      string    `json:"color"`
	Background string    `json:"background,omitempty"`
	Baseline   string    `json:"baseline,omitempty"`
}

// Primitives2D are flat draw lists in native px; sizes are in the line unit LW.
type Primitives2D struct {
	LW       *float64    `json:"lw,omitempty"`
	Images   []Image2D   `json:"images,omitempty"`
	Polygons []Polygon2D `json:"polygons,omitempty"`
	Segments []Segment2D `json:"segments,omitempty"`
	Circles  []Circle2D  `json:"circles,omitempty"`
	Labels   []Label2D   `json:"labels,omitempty"`
}

// Box2D is a box of the 2-D draw list, native px.
type Box2D struct {
	Kind     string  `json:"kind"`
	X0       float64 `json:"x0"`
	Y0       float64 `json:"y0"`
	X1       float64 `json:"x1"`
	Y1       float64 `json:"y1"`
	Rot      bool    `json:"rot"`
	Presence float64 `json:"presence"`
	Label    string  `json:"label,omitempty"`
}

// Bone2D is a bone of the 2-D skeleton, native px.
type Bone2D struct {
	A     int64     `json:"a"`
	B     int64     `json:"b"`
	From  []float64 `json:"from"`
	To    []float64 `json:"to"`
	Side  string    `json:"side"`
	Alpha float64   `json:"alpha"`
	Hand  bool      `json:"hand"`
}

// Joint2D is a joint of the 2-D skeleton, native px.
type Joint2D struct {
	J     int64     `json:"j"`
	At    []float64 `json:"at"`
	Side  string    `json:"side"`
	Alpha float64   `json:"alpha"`
	Hand  bool      `json:"hand"`
}

// Skeleton2D is the 2-D skeleton.
type Skeleton2D struct {
	Bones  []Bone2D  `json:"bones,omitempty"`
	Joints []Joint2D `json:"joints,omitempty"`
}

// Raster2D is a map raster reference of the 2-D draw list.
type Raster2D struct {
	URL  string    `json:"url,omitempty"`
	Rows int       `json:"rows,omitempty"`
	Cols int       `json:"cols,omitempty"`
	Box  []float64 `json:"box,omitempty"`
	Rot  *bool     `json:"rot,omitempty"`
}

// Overlay2D is the final 2-D draw list in native px ("r2d1").
type Overlay2D struct {
	Schema     string           `json:"schema"`
	Session    int64            `json:"session"`
	Video      string           `json:"video"`
	G          int64            `json:"g"`
	BG         *int64           `json:"bg"`
	F          float64          `json:"f"`
	T          float64          `json:"t"`
	Mode       string           `json:"mode"`
	Missing    []string         `json:"missing,omitempty"`
	NativeWH   []int64          `json:"native_wh"`
	Regions    *Raster2D        `json:"regions,omitempty"`
	Mask       *Raster2D        `json:"mask,omitempty"`
	Boxes      []Box2D          `json:"boxes,omitempty"`
	Stones     []DrawStone2D    `json:"stones,omitempty"`
	Skeleton   *Skeleton2D      `json:"skeleton,omitempty"`
	Wrists     []map[string]any `json:"wrists,omitempty"`
	Focus      map[string]any   `json:"focus,omitempty"`
	Primitives *Primitives2D    `json:"primitives,omitempty"`
}

// DrawStone3D is a stone of the 3-D draw list, scene world.
type DrawStone3D struct {
	ID        *int64    `json:"id"`
	Family    string    `json:"family"`
	Pos       []float64 `json:"pos"`
	Q         []float64 `json:"q"`
	Dims      []float64 `json:"dims"`
	Side      *string   `json:"side"`
	Alpha     float64   `json:"alpha"`
	Uncertain *bool     `json:"uncertain,omitempty"`
}

// SurfaceMesh is the observed surface of the 3-D scene: form "vertices" (explicit f32 positions and u16 triangle
// indices, base64 little-endian) or "compact" (the scene.surface form).
type SurfaceMesh struct {
	Form      string        `json:"form"`
	Rows      int           `json:"rows"`
	Cols      int           `json:"cols"`
	Positions string        `json:"positions,omitempty"`
	Indices   string        `json:"indices,omitempty"`
	Opacity   *float64      `json:"opacity,omitempty"`
	Q         string        `json:"q,omitempty"`
	Lo        *float64      `json:"lo,omitempty"`
	Hi        *float64      `json:"hi,omitempty"`
	A         *float64      `json:"a,omitempty"`
	B         *float64      `json:"b,omitempty"`
	Flat      string        `json:"flat,omitempty"`
	Quads     *SurfaceQuads `json:"quads,omitempty"`
	Lift      *float64      `json:"lift,omitempty"`
}

// Primitives3D are flat 3-D draw lists.
type Primitives3D struct {
	Boxes    []map[string]any `json:"boxes,omitempty"`
	Segments []map[string]any `json:"segments,omitempty"`
	Spheres  []map[string]any `json:"spheres,omitempty"`
	Mesh     map[string]any   `json:"mesh,omitempty"`
}

// Bone3D is a bone of the 3-D skeleton (joint indices a, b).
type Bone3D struct {
	A    int64  `json:"a"`
	B    int64  `json:"b"`
	Side string `json:"side"`
	Hand bool   `json:"hand"`
}

// Skeleton3D is the 3-D skeleton: 70 joints [x, y, z] or nil.
type Skeleton3D struct {
	Joints [][]float64 `json:"joints,omitempty"`
	Bones  []Bone3D    `json:"bones,omitempty"`
	Rings  []Ring      `json:"rings,omitempty"`
}

// Camera3D is the camera of the 3-D scene.
type Camera3D struct {
	K        [][]float64 `json:"K,omitempty"`
	RC2W     [][]float64 `json:"R_c2w,omitempty"`
	TC2W     []float64   `json:"t_c2w,omitempty"`
	NativeWH []int64     `json:"native_wh,omitempty"`
	Centre   []float64   `json:"centre,omitempty"`
}

// Scene3D is the final 3-D scene in the scene world ("r3d1").
type Scene3D struct {
	Schema     string         `json:"schema"`
	Session    int64          `json:"session"`
	Video      string         `json:"video"`
	G          int64          `json:"g"`
	BG         *int64         `json:"bg"`
	F          float64        `json:"f"`
	T          float64        `json:"t"`
	Mode       string         `json:"mode"`
	Missing    []string       `json:"missing,omitempty"`
	Camera     *Camera3D      `json:"camera,omitempty"`
	Plane      ScenePlane     `json:"plane"`
	Board      *Rect          `json:"board"`
	Stones     []DrawStone3D  `json:"stones"`
	Skeleton   Skeleton3D     `json:"skeleton"`
	Surface    *SurfaceMesh   `json:"surface,omitempty"`
	Focus      map[string]any `json:"focus,omitempty"`
	Primitives *Primitives3D  `json:"primitives,omitempty"`
}
