package pdiggerfactory

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// fixtures/damaged: recordings and streams with lines and events that do not decode (SDK-DESIGN.md section 6, 2.3
// rule 9). Every SDK replays and streams them and gets what their JSON files say.

// damaged is the path of a file of fixtures/damaged.
func damaged(name string) string { return filepath.Join("fixtures", "damaged", name) }

// damagedHeader is a header as the JSON files give it: its name, and whether it has a source.
type damagedHeader struct {
	Name   string `json:"name"`
	Source bool   `json:"source"`
}

// damagedEvent is an event of a stream of lines.json.
type damagedEvent struct {
	ID    string `json:"id"`
	Event string `json:"event"`
	Data  string `json:"data"`
}

// damagedStream is a stream of lines.json: its events, and the frames (or packets' g) and decode errors it gives.
type damagedStream struct {
	Events       []damagedEvent `json:"events"`
	Frames       []int64        `json:"frames"`
	G            []int64        `json:"g"`
	DecodeErrors int            `json:"decode_errors"`
}

// damagedMatch is the event WaitFor finds past the damaged lines.
type damagedMatch struct {
	Type string `json:"type"`
	G    int64  `json:"g"`
}

// damagedWant is what a JSON file of fixtures/damaged says (the header files have the header, the updates and
// skipped only).
type damagedWant struct {
	Header          *damagedHeader   `json:"header"`
	Updates         []int64          `json:"updates"`
	Summary         map[string]int64 `json:"summary"`
	Walls           []float64        `json:"walls"`
	FinalActivities []string         `json:"final_activities"`
	WaitFor         damagedMatch     `json:"wait_for"`
	Skipped         int64            `json:"skipped"`
	Stream          damagedStream    `json:"stream"`
	Packets         damagedStream    `json:"packets"`
}

func readDamaged(t *testing.T, name string) damagedWant {
	t.Helper()
	b, err := os.ReadFile(damaged(name))
	if err != nil {
		t.Fatal(err)
	}
	var want damagedWant
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	return want
}

// headerOf is a recording's header as the JSON files give it (nil for none).
func headerOf(h *RecordingHeader) *damagedHeader {
	if h == nil {
		return nil
	}
	return &damagedHeader{Name: h.Name, Source: h.Source != nil}
}

// sameHeader reports whether a header is the one a JSON file gives.
func sameHeader(h *RecordingHeader, want *damagedHeader) bool {
	got := headerOf(h)
	if got == nil || want == nil {
		return got == nil && want == nil
	}
	return *got == *want
}

// checkUpdates checks the frames' g and wall times and the final update's activities against lines.json.
func checkUpdates(t *testing.T, updates []*Update, want damagedWant) {
	t.Helper()
	if len(updates) == 0 || !updates[len(updates)-1].Final {
		t.Fatalf("%d updates, the last one not final", len(updates))
	}
	frames, last := updates[:len(updates)-1], updates[len(updates)-1]
	var gs []int64
	for _, u := range frames {
		gs = append(gs, u.Frame.G)
	}
	if !slices.Equal(gs, want.Updates) || len(frames) != len(want.Walls) {
		t.Fatalf("updates %v, want %v", gs, want.Updates)
	}
	for i, u := range frames {
		if u.State.Wall == nil || math.Abs(*u.State.Wall-want.Walls[i]) > 1e-6 {
			t.Fatalf("g %d: wall %v, want %v", u.Frame.G, u.State.Wall, want.Walls[i])
		}
	}
	var acts []string
	for _, a := range last.Activities {
		acts = append(acts, a.Describe())
	}
	if !slices.Equal(acts, want.FinalActivities) {
		t.Fatalf("final activities %q, want %q", acts, want.FinalActivities)
	}
}

func TestDamagedLinesReplay(t *testing.T) {
	ctx := testCtx(t)
	want := readDamaged(t, "lines.json")
	r, err := OpenReplay(ctx, damaged("lines.ndjson"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if !sameHeader(r.Header, want.Header) {
		t.Fatalf("header %+v", r.Header)
	}
	checkUpdates(t, collect(t, r.Updates(ctx), 0), want)
	// one pass: every line that does not decode once
	if r.Skipped() != want.Skipped {
		t.Fatalf("skipped %d, want %d", r.Skipped(), want.Skipped)
	}
	s, err := r.Summary(ctx)
	if err != nil || s.Frames != want.Summary["frames"] || s.Sessions != want.Summary["sessions"] {
		t.Fatalf("%+v %v", s, err)
	}
	if r.Skipped() != 2*want.Skipped {
		t.Fatalf("skipped %d after two passes", r.Skipped())
	}
}

func TestDamagedHeaders(t *testing.T) {
	ctx := testCtx(t)
	paths, err := filepath.Glob(damaged("header-*.ndjson"))
	if err != nil || len(paths) != 11 {
		t.Fatal(paths, err)
	}
	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), ".ndjson")
		want := readDamaged(t, name+".json")
		r, err := OpenReplay(ctx, path, nil)
		if err != nil {
			t.Fatal(name, err)
		}
		if !sameHeader(r.Header, want.Header) {
			t.Fatalf("%s: header %+v", name, r.Header)
		}
		var gs []int64
		for f, err := range r.Frames(ctx) {
			if err != nil {
				t.Fatal(name, err)
			}
			gs = append(gs, f.G)
		}
		if !slices.Equal(gs, want.Updates) || r.Skipped() != want.Skipped {
			t.Fatalf("%s: frames %v, skipped %d", name, gs, r.Skipped())
		}
		r.Close()
	}
}

// sseClient is a client whose every request is answered with an SSE body of these events, which then ends.
func sseClient(t *testing.T, events []damagedEvent) *Client {
	t.Helper()
	var b strings.Builder
	for _, e := range events {
		b.WriteString("id: " + e.ID + "\nevent: " + e.Event + "\ndata: " + e.Data + "\n\n")
	}
	body := b.String()
	c, _ := funcClient(t, func(r *http.Request) (*http.Response, error) {
		return reply(200, body, "Content-Type", "text/event-stream"), nil
	})
	return c
}

func TestDamagedLinesStream(t *testing.T) {
	ctx := testCtx(t)
	want := readDamaged(t, "lines.json")
	c := sseClient(t, want.Stream.Events)
	var frames []int64
	errs := 0
	for item, err := range c.Streams().Frames(ctx, Resume(false)) {
		if err != nil {
			if e := mustCode(t, err, CodeDecode); e.Op != "decode" {
				t.Fatal(e)
			}
			errs++
			continue
		}
		if fi, ok := item.(*FrameItem); ok {
			frames = append(frames, fi.Frame.G)
		}
	}
	if !slices.Equal(frames, want.Stream.Frames) || errs != want.Stream.DecodeErrors {
		t.Fatalf("frames %v, %d decode errors", frames, errs)
	}
	// the Interpreter's updates go on past each one, to the final update (the clock events give the wall times)
	var updates []*Update
	errs = 0
	for u, err := range c.Interpreter(Resume(false), ClockOnConnect(false)).Updates(ctx) {
		if err != nil {
			errs++
			continue
		}
		updates = append(updates, u)
	}
	if errs != want.Stream.DecodeErrors {
		t.Fatalf("%d decode errors", errs)
	}
	checkUpdates(t, updates, want)
	// and so does WaitFor
	in := c.Interpreter(Resume(false), ClockOnConnect(false))
	if e, err := in.WaitFor(ctx, OfType(want.WaitFor.Type), 5*time.Second); err != nil || e.G != want.WaitFor.G {
		t.Fatal(e, err)
	}
}

func TestDamagedPacketsStream(t *testing.T) {
	ctx := testCtx(t)
	want := readDamaged(t, "lines.json")
	c := sseClient(t, want.Packets.Events)
	var gs []int64
	errs := 0
	for item, err := range c.Streams().RawPackets(ctx, RawFormat(FormatSSE), Resume(false)) {
		if err != nil {
			if e := mustCode(t, err, CodeDecode); e.Op != "decode" {
				t.Fatal(e)
			}
			errs++
			continue
		}
		if pi, ok := item.(*PacketItem); ok {
			gs = append(gs, pi.Packet.G)
		}
	}
	if !slices.Equal(gs, want.Packets.G) || errs != want.Packets.DecodeErrors {
		t.Fatalf("packets %v, %d decode errors", gs, errs)
	}
}

// TestPacketWithoutCursor: a packet's cursor is optional (sip1). One without it has Cursor 0, and a reconnect asks for
// the packets after the last cursor the stream had.
func TestPacketWithoutCursor(t *testing.T) {
	var mu sync.Mutex
	var after []string
	noCursor := strings.Replace(rawJSON(2, 0), `,"cursor":0`, "", 1)
	c, _ := funcClient(t, func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		after = append(after, r.URL.Query().Get("after"))
		mu.Unlock()
		return reply(200, rawJSON(1, 7)+"\n"+noCursor+"\n", "Content-Type", "application/x-ndjson"), nil
	})
	var cursors []int64
	for item, err := range c.Streams().RawPackets(testCtx(t), MaxReconnects(2), After(0)) {
		if pi, ok := item.(*PacketItem); err == nil && ok {
			if cursors = append(cursors, pi.Cursor); len(cursors) == 3 {
				break
			}
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(cursors, []int64{7, 0, 7}) || len(after) < 2 || after[0] != "0" || after[1] != "7" {
		t.Fatal(cursors, after)
	}
}

// TestDamagedEventIDCountsForTheResume: the resume id moves past an event that does not decode (2.3 rule 9), so a
// reconnect does not read it again.
func TestDamagedEventIDCountsForTheResume(t *testing.T) {
	var mu sync.Mutex
	var asked []string
	c, _ := funcClient(t, func(r *http.Request) (*http.Response, error) {
		mu.Lock()
		asked = append(asked, r.Header.Get("Last-Event-ID"))
		first := len(asked) == 1
		mu.Unlock()
		// a frame, then an event that does not decode, and the server ends the stream; then the next frame
		body := "id: b-3\nevent: frame\ndata: " + fline(3, 1, 1, -1, "") + "\n\n"
		if first {
			body = "id: b-1\nevent: frame\ndata: " + fline(1, 1, 0, -1, "") + "\n\nid: b-2\nevent: frame\ndata: {nope\n\n"
		}
		return reply(200, body, "Content-Type", "text/event-stream"), nil
	})
	var gs []int64
	for item, err := range c.Streams().Frames(testCtx(t), MaxReconnects(2)) {
		if err != nil {
			continue
		}
		if gs = append(gs, item.(*FrameItem).Frame.G); len(gs) == 2 {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if !slices.Equal(gs, []int64{0, 1}) || len(asked) < 2 || asked[0] != "" || asked[1] != "b-2" {
		t.Fatal(gs, asked)
	}
}
