package pdiggerfactory_test

// The opt-in integration tests (TESTING.md). Each is skipped unless its environment variable is set, and the unit
// test run (make test) sets neither:
//
//	PDIGGER_FACTORY_URL=http://<unit>:8120 go test -run Integration -count=1 .
//
// checks a unit read-only: every GET operation, streams and long-polls that observe (observe=1: they do not count as
// a viewer, so they never wake an idle worker), and every control as a dry run (dry_run=1). It never changes the
// shared session.
//
//	PDIGGER_FACTORY_RECORDINGS=<dir> go test -run Integration -count=1 -timeout 2h .
//
// replays every <video>.world.ndjson.gz in <dir> and compares its summary with <dir>/<video>.summary.json (and the
// text with <dir>/<video>.summary.txt when there is one), or else with fixtures/golden/complete/<video>.summary.json.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
)

// readOnly refuses, before it is sent, any request that could move the shared session: a method other than GET or
// HEAD without dry_run=1.
type readOnly struct{ next http.RoundTripper }

func (r readOnly) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != http.MethodGet && req.Method != http.MethodHead && req.URL.Query().Get("dry_run") != "1" {
		return nil, fmt.Errorf("integration: %s %s without dry_run=1 refused: the shared session must not move",
			req.Method, req.URL.Path)
	}
	return r.next.RoundTrip(req)
}

// unit is a client of the unit named by PDIGGER_FACTORY_URL, observing, whose transport refuses anything but reads
// and dry runs; the test is skipped without one.
func unit(t *testing.T) *pdiggerfactory.Client {
	t.Helper()
	base := os.Getenv("PDIGGER_FACTORY_URL")
	if base == "" {
		t.Skip("PDIGGER_FACTORY_URL is not set: no unit to check")
	}
	c, err := pdiggerfactory.New(base, pdiggerfactory.WithObserve(true), pdiggerfactory.WithUserAgentSuffix("integration"),
		pdiggerfactory.WithTransport(readOnly{http.DefaultTransport}))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// noFrames reports whether err says the unit has no frame to give (an idle worker: nobody is watching).
func noFrames(err error) bool {
	return pdiggerfactory.IsNotRetained(err) || pdiggerfactory.HasCode(err, pdiggerfactory.CodeNotReady) ||
		pdiggerfactory.HasCode(err, pdiggerfactory.CodeWorldUnavailable)
}

func TestIntegrationUnitReadOnly(t *testing.T) {
	c := unit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	api := c.API()

	root, err := api.GetAPIRoot(ctx)
	if err != nil || !strings.HasPrefix(root.API, "2.") {
		t.Fatalf("api root: %+v %v", root, err)
	}
	t.Logf("API %s, build %s, boot %s", root.API, root.Build, root.Boot)
	if spec, err := api.GetOpenAPI(ctx); err != nil || !bytes.Contains(spec, []byte("openapi:")) {
		t.Errorf("openapi: %d bytes, %v", len(spec), err)
	}
	if _, err := api.GetModel(ctx); err != nil {
		t.Error("model:", err)
	}
	if topo, err := api.GetTopology(ctx); err != nil || topo.Joints == 0 || len(topo.BonePairs()) == 0 {
		t.Errorf("topology: %v", err)
	}
	if _, err := api.GetHealth(ctx); err != nil {
		t.Error("health:", err)
	}
	if _, err := api.GetReady(ctx); err != nil && !pdiggerfactory.HasCode(err, pdiggerfactory.CodeNotReady) {
		t.Error("ready:", err)
	}
	if _, err := api.GetStatus(ctx); err != nil {
		t.Error("status:", err)
	}
	if m, err := api.GetMetrics(ctx); err != nil || len(m.Samples) == 0 {
		t.Errorf("metrics: %v", err)
	}

	// sources, the live cameras, media
	sources, err := c.Sources(ctx)
	if err != nil || len(sources) == 0 {
		t.Fatalf("sources: %v", err)
	}
	for _, s := range sources {
		if _, err := api.GetVideo(ctx, s.ID); err != nil {
			t.Errorf("video %s: %v", s.ID, err)
		}
		if s.IsLive() {
			continue
		}
		media, err := api.GetVideoMedia(ctx, s.ID, &pdiggerfactory.MediaParams{Range: pdiggerfactory.Bytes(0, 1023)})
		if err != nil || media.Status != 206 || len(media.Body) != 1024 {
			t.Errorf("media %s: %v", s.ID, err)
		}
	}
	if live, err := c.Live(ctx); err == nil {
		t.Logf("the unit's first live camera: %q (%s)", live.ID, live.Station)
	} else if !pdiggerfactory.HasCode(err, pdiggerfactory.CodeLiveUnavailable) {
		t.Error("live:", err)
	}
	// the cameras: no media; a time is refused before the select; a dry-run watch would connect
	cameras, err := c.Cameras(ctx)
	if err != nil {
		t.Error("cameras:", err)
	}
	for _, camera := range cameras {
		if _, err := api.GetVideoMedia(ctx, camera.ID, nil); !pdiggerfactory.HasCode(err, pdiggerfactory.CodeNotFound) {
			t.Errorf("media of camera %s: %v", camera.ID, err)
		}
	}
	if len(cameras) > 0 {
		id := cameras[0].ID
		if _, err := c.Session().Watch(ctx, id, pdiggerfactory.Float64(1), pdiggerfactory.DryRun()); !pdiggerfactory.HasCode(err,
			pdiggerfactory.CodeNotSeekable) {
			t.Error("a time for a camera was not refused:", err)
		}
		if st, err := c.Session().Watch(ctx, id, nil, pdiggerfactory.DryRun()); err != nil ||
			st.LivePhase() != pdiggerfactory.LivePhaseConnecting {
			t.Errorf("watch %s dry run: %+v %v", id, st, err)
		}
	}

	before, err := c.Session().State(ctx)
	if err != nil {
		t.Fatal("session:", err)
	}
	// a picture only while the session shows a camera
	_, err = c.Session().Picture(ctx, pdiggerfactory.LatestFrame(), pdiggerfactory.InSession(before.Session))
	switch {
	case before.IsLive() && err != nil && !pdiggerfactory.IsNotRetained(err) && !pdiggerfactory.IsSessionGone(err):
		t.Error("picture:", err)
	case !before.IsLive() && !pdiggerfactory.HasCode(err, pdiggerfactory.CodeNotFound) && !pdiggerfactory.IsSessionGone(err):
		t.Error("a recording's picture:", err)
	}
	if _, err := c.Session().Clock(ctx); err != nil {
		t.Error("clock:", err)
	}
	if _, err := api.GetFocus(ctx); err != nil {
		t.Error("focus:", err)
	}

	// the newest frame and what is derived from it; an idle unit may have none
	frame, err := api.GetFrame(ctx, pdiggerfactory.LatestFrame(), &pdiggerfactory.FrameParams{
		Layers: []string{pdiggerfactory.LayerRaw, pdiggerfactory.LayerOverlay, pdiggerfactory.LayerScene,
			pdiggerfactory.LayerCards, pdiggerfactory.LayerWorld}})
	switch {
	case noFrames(err):
		t.Log("no retained frame: the unit's worker is idle")
	case err != nil:
		t.Error("frame:", err)
	default:
		g := pdiggerfactory.Int64(frame.G)
		guard := pdiggerfactory.Int64(frame.Session)
		if _, err := api.RawPacket(ctx, pdiggerfactory.FrameAt(frame.G), &pdiggerfactory.RawPacketParams{Session: guard}); err != nil && !noFrames(err) {
			t.Error("raw packet:", err)
		}
		if png, err := api.GetFrameRaster(ctx, frame.G, pdiggerfactory.RasterRegions, nil); err != nil && !noFrames(err) {
			t.Error("raster:", err)
		} else if err == nil && !bytes.HasPrefix(png, []byte("\x89PNG")) {
			t.Error("raster: not a PNG")
		}
		if _, err := api.RawTensors(ctx, frame.G); err != nil && !pdiggerfactory.HasCode(err, pdiggerfactory.CodeDebugDisabled) &&
			!noFrames(err) {
			t.Error("tensors:", err)
		}
		render := pdiggerfactory.RenderParams{G: g, Session: guard}
		if _, err := api.RenderOverlay2D(ctx, &render); err != nil && !noFrames(err) && !pdiggerfactory.IsSessionGone(err) {
			t.Error("overlay2d:", err)
		}
		if _, err := api.RenderScene3D(ctx, &pdiggerfactory.Scene3DParams{RenderParams: render, Surface: "compact"}); err != nil &&
			!noFrames(err) && !pdiggerfactory.IsSessionGone(err) {
			t.Error("scene3d:", err)
		}
	}
	world, err := api.GetWorld(ctx, nil)
	switch {
	case noFrames(err):
	case err != nil:
		t.Error("world:", err)
	default:
		if _, err := api.GetWorldEvents(ctx, &pdiggerfactory.WorldEventsParams{Limit: 16}); err != nil {
			t.Error("world events:", err)
		}
		if len(world.Stones) > 0 {
			id := world.Stones[0].ID
			if _, err := api.GetStone(ctx, id); err != nil && !pdiggerfactory.HasCode(err, pdiggerfactory.CodeUnknownStone) {
				t.Error("stone:", err)
			}
			if _, err := c.Session().Focus(ctx, id, pdiggerfactory.DryRun()); err != nil &&
				!pdiggerfactory.HasCode(err, pdiggerfactory.CodeUnknownStone) && !pdiggerfactory.HasCode(err, pdiggerfactory.CodeStaleSession) {
				t.Error("focus dry run:", err)
			}
			// as picked in the world's frame, guarded by its session
			_, err := c.Session().Focus(ctx, id, pdiggerfactory.PickedAt(world.G), pdiggerfactory.InSession(world.Session),
				pdiggerfactory.DryRun())
			if err != nil && !pdiggerfactory.HasCode(err, pdiggerfactory.CodeUnknownStone) &&
				!pdiggerfactory.HasCode(err, pdiggerfactory.CodeStaleSession) && !pdiggerfactory.IsNotRetained(err) {
				t.Error("focus dry run, picked at a frame:", err)
			}
			if _, err := c.Session().Focus(ctx, id, pdiggerfactory.InSession(world.Session-1), pdiggerfactory.DryRun()); !pdiggerfactory.HasCode(err, pdiggerfactory.CodeStaleSession) &&
				!pdiggerfactory.HasCode(err, pdiggerfactory.CodeUnknownStone) {
				t.Error("a focus guarded by an earlier session was not refused:", err)
			}
		}
	}

	// every control as a dry run: the unit checks it and changes nothing
	s := c.Session()
	if _, err := s.Pause(ctx, pdiggerfactory.DryRun()); err != nil {
		t.Error("pause dry run:", err)
	}
	if _, err := s.Play(ctx, pdiggerfactory.DryRun()); err != nil {
		t.Error("play dry run:", err)
	}
	if _, err := s.ClearFocus(ctx, pdiggerfactory.DryRun()); err != nil {
		t.Error("clear focus dry run:", err)
	}
	if !before.IsLive() {
		if _, err := s.Seek(ctx, before.MediaTime, pdiggerfactory.DryRun()); err != nil &&
			!pdiggerfactory.HasCode(err, pdiggerfactory.CodeTimeOutOfRange) {
			t.Error("seek dry run:", err)
		}
		if _, err := s.Seek(ctx, before.MediaTime, pdiggerfactory.InVideo(before.Video), pdiggerfactory.DryRun()); err != nil &&
			!pdiggerfactory.HasCode(err, pdiggerfactory.CodeTimeOutOfRange) && !pdiggerfactory.HasCode(err, pdiggerfactory.CodeNotSeekable) {
			t.Error("seek dry run in a named video:", err)
		}
	}
	if _, err := s.Watch(ctx, before.Video, nil, pdiggerfactory.DryRun()); err != nil {
		t.Error("watch dry run:", err)
	}
	if _, err := s.Seek(ctx, -5, pdiggerfactory.DryRun()); !pdiggerfactory.HasCode(err, pdiggerfactory.CodeTimeOutOfRange) &&
		!pdiggerfactory.HasCode(err, pdiggerfactory.CodeNotSeekable) {
		t.Error("a seek to -5 s was not refused:", err)
	}
	after, err := s.State(ctx)
	if err != nil {
		t.Fatal("session:", err)
	}
	if after.Session != before.Session && after.ChangedBy == "viewer" {
		t.Logf("the session changed during the test (%d -> %d, by a viewer): another client moved it", before.Session,
			after.Session)
	}
}

// TestIntegrationUnitStreams reads a few frames of each stream, observing; an idle unit sends none, which is not an
// error (nobody is watching, so nothing is interpreted). An error a stream yields fails the test (the ones a resume
// rides out are not yielded); the end of the 20 s is the idle case.
func TestIntegrationUnitStreams(t *testing.T) {
	c := unit(t)
	upTo3 := func(items iter.Seq2[pdiggerfactory.StreamItem, error], want func(pdiggerfactory.StreamItem) bool) (int, error) {
		n := 0
		for item, err := range items {
			if err != nil {
				return n, err
			}
			if want(item) {
				if n++; n == 3 {
					return n, nil
				}
			}
		}
		return n, nil
	}
	frame := func(item pdiggerfactory.StreamItem) bool {
		_, ok := item.(*pdiggerfactory.FrameItem)
		return ok
	}
	packet := func(item pdiggerfactory.StreamItem) bool {
		_, ok := item.(*pdiggerfactory.PacketItem)
		return ok
	}
	world := pdiggerfactory.Layers(pdiggerfactory.LayerWorld)
	for name, stream := range map[string]func(context.Context) (int, error){
		"frames":      func(ctx context.Context) (int, error) { return upTo3(c.Streams().Frames(ctx, world), frame) },
		"poll":        func(ctx context.Context) (int, error) { return upTo3(c.Streams().Poll(ctx, world), frame) },
		"raw packets": func(ctx context.Context) (int, error) { return upTo3(c.Streams().RawPackets(ctx), packet) },
		"interpreter": func(ctx context.Context) (int, error) {
			if _, err := c.Interpreter(pdiggerfactory.Observe(true)).WaitFor(ctx, nil, 0); err != nil {
				return 0, err
			}
			return 1, nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			n, err := stream(ctx)
			if err != nil && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			if n == 0 {
				t.Log("nothing within 20 s: the unit's worker is idle or the session is paused")
			} else {
				t.Logf("%d items", n)
			}
		})
	}
}

// TestIntegrationCompleteRecordings replays the complete recordings and compares their summaries.
func TestIntegrationCompleteRecordings(t *testing.T) {
	dir := os.Getenv("PDIGGER_FACTORY_RECORDINGS")
	if dir == "" {
		t.Skip("PDIGGER_FACTORY_RECORDINGS is not set: no recordings to replay")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.world.ndjson.gz"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no <video>.world.ndjson.gz in %s (%v)", dir, err)
	}
	for _, file := range files {
		video := strings.TrimSuffix(filepath.Base(file), ".world.ndjson.gz")
		t.Run(video, func(t *testing.T) {
			want, wantPath := completeGolden(t, dir, video, "json")
			if want == nil {
				t.Skipf("no golden summary for %s", video)
			}
			ctx := context.Background()
			replay, err := pdiggerfactory.OpenReplay(ctx, file, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer replay.Close()
			summary, err := replay.Summary(ctx)
			if err != nil {
				t.Fatal(err)
			}
			got, err := json.Marshal(summary)
			if err != nil {
				t.Fatal(err)
			}
			var g, w any
			if err := json.Unmarshal(got, &g); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(want, &w); err != nil {
				t.Fatalf("%s: %v", wantPath, err)
			}
			if !reflect.DeepEqual(g, w) {
				t.Errorf("the summary differs from %s:\ngot  %s\nwant %s", wantPath, got, bytes.TrimSpace(want))
			}
			if text, textPath := completeGolden(t, dir, video, "txt"); text != nil && summary.Describe() != string(text) {
				t.Errorf("the summary text differs from %s:\ngot\n%swant\n%s", textPath, summary.Describe(), text)
			}
			t.Logf("%s: %d frames, %d moves, %d push bursts", video, summary.Frames, summary.Moves.Total,
				summary.PushBursts.Total)
		})
	}
}

// completeGolden reads <dir>/<video>.summary.<ext>, else fixtures/golden/complete/<video>.summary.<ext>; nil when neither
// exists.
func completeGolden(t *testing.T, dir, video, ext string) ([]byte, string) {
	t.Helper()
	for _, p := range []string{filepath.Join(dir, video+".summary."+ext),
		filepath.Join("fixtures", "golden", "complete", video+".summary."+ext)} {
		b, err := os.ReadFile(p)
		if err == nil {
			return b, p
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
	}
	return nil, ""
}
