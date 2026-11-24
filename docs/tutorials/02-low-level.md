# Read a frame through the HTTP API

<a id="low-level-one-call-per-operation"></a>

Use `client.API()` to check the unit’s status, request a frame and draw its stones and hands. Each method maps to
one HTTP operation. The codec unpacks data such as depth images, joint positions and stone shapes.

From the `examples` directory, run the [examples/tutoriallowlevel/main.go](../../examples/tutoriallowlevel/main.go)
program.

```sh
PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./tutoriallowlevel
```

<details>
<summary>Show the complete Go example</summary>

```go
// Command tutoriallowlevel is the low-level tutorial (docs/tutorials/02-low-level.md) in one program: one call per
// API operation, typed replies, and the codec for what the replies pack. Every step only reads, or sends a dry run
// that changes nothing. The frame is the newest one, or the g given as the argument.
//
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./tutoriallowlevel [g]
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strconv"
	"strings"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
	"github.com/eprojac/pdigger-factory-sdk-go/codec"
)

func main() {
	ctx := context.Background()
	client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL"))
	if err != nil {
		log.Fatal(err)
	}
	api := client.API()
	at := pdiggerfactory.LatestFrame()
	if len(os.Args) > 1 {
		if g, err := strconv.ParseInt(os.Args[1], 10, 64); err == nil {
			at = pdiggerfactory.FrameAt(g)
		}
	}

	// 1. Is it ready, and how fast? While it starts: the error not_ready, with what it waits for
	ready := must(api.GetReady(ctx))
	worker := must(api.GetStatus(ctx)).Worker
	fps, ms := value(worker.FPS), value(worker.FrameMS)
	fmt.Printf("ready %t, worker %s: %v frames a second, %v ms each\n", ready.Ready, worker.State, fps, ms)

	// 2. The model, and what to draw with
	model := must(api.GetModel(ctx))
	fmt.Printf("checkpoint %s on %s: %d engines\n", model.Checkpoint, model.Backend, len(model.Engines))
	topology := must(api.GetTopology(ctx))
	fmt.Printf("%d joints, %d bones, %d colours\n", topology.Joints, len(topology.Bones), len(topology.Colors))

	// 3. A stone's track, and the focus on it as a dry run: the unit checks it, and nothing changes
	world := must(api.GetWorld(ctx, nil))
	if len(world.Stones) > 0 {
		track := must(api.GetStone(ctx, world.Stones[0].ID)) // its path, from frame FirstG to LastG
		fmt.Printf("stone %d: %d points, g %d to %d\n", track.ID, len(track.Path2D), track.FirstG, track.LastG)
		focus := must(api.PutFocus(ctx, pdiggerfactory.PutFocusParams{StoneID: track.ID, DryRun: true}))
		fmt.Printf("focus on stone %d: dry run %t\n", track.ID, focus.DryRun)
	}

	// 4. A frame, and the model's maps of it
	layers := []string{pdiggerfactory.LayerWorld, pdiggerfactory.LayerCards}
	frame := must(api.GetFrame(ctx, at, &pdiggerfactory.FrameParams{Layers: layers}))
	var stones []pdiggerfactory.StoneView
	if frame.World != nil {
		stones = frame.World.Stones
	}
	var families []string
	for _, s := range stones {
		families = append(families, s.Family)
	}
	slices.Sort(families)
	families = slices.Compact(families)
	clock := pdiggerfactory.FormatClock(frame.T)
	fmt.Printf("frame g %d at %s: %d stones, %s\n", frame.G, clock, len(stones), strings.Join(families, ", "))
	session := pdiggerfactory.Int64(frame.Session)
	packet := must(api.RawPacket(ctx, pdiggerfactory.FrameAt(frame.G), &pdiggerfactory.RawPacketParams{Session: session}))
	rows, cols := 0, 0 // the model's grid of cells
	if grid := packet.Maps.Grid; len(grid) == 2 {
		rows, cols = int(grid[0]), int(grid[1])
	}
	tray := must(codec.DecodeZlibMap(packet.Maps.Tray, rows, cols)) // one byte a cell
	stoneCells := must(codec.DecodeZlibMap(packet.Maps.Stone, rows, cols))
	fmt.Printf("maps of g %d: %d x %d cells, %d of the tray, %d of stones\n", packet.G, rows, cols, cells(tray),
		cells(stoneCells))

	// 5. Draw lists, ready to paint: shapes with their style in video pixels, the scene in scene units
	g := pdiggerfactory.Int64(frame.G)
	overlay := must(api.RenderOverlay2D(ctx, &pdiggerfactory.RenderParams{G: g, Draw: "primitives"}))
	polygons, segments, circles := 0, 0, 0
	if p := overlay.Primitives; p != nil {
		polygons, segments, circles = len(p.Polygons), len(p.Segments), len(p.Circles)
	}
	fmt.Printf("overlay of g %d: %d polygons, %d segments, %d circles at %s px\n", overlay.G, polygons, segments,
		circles, size(overlay.NativeWH))
	compact := &pdiggerfactory.Scene3DParams{RenderParams: pdiggerfactory.RenderParams{G: g}, Surface: "compact"}
	scene := must(api.RenderScene3D(ctx, compact)) // the stones, the hands and the work plane
	fmt.Printf("scene of g %d: %d stones, %d bones\n", scene.G, len(scene.Stones), len(scene.Skeleton.Bones))

	// 6. Stone shapes, drawn by hand: each outline a polygon in video pixels, coloured from the topology
	type outline struct {
		fill   string
		points [][]float64 // [x, y] in video pixels: hand them to any drawing library
	}
	var outlines []outline
	for _, s := range stones {
		fill := topology.Colors["stoneBox"] // a stone the model did not see in this frame
		if s.Detected {
			fill = topology.Colors["stones"]
		}
		outlines = append(outlines, outline{fill, s.Outline})
	}
	points := 0
	for _, o := range outlines {
		points += len(o.points)
	}
	fmt.Printf("g %d: %d outlines, %d points\n", frame.G, len(outlines), points)

	// 7. Hands and bones, drawn by hand: the joints in video pixels, the bones between those in view
	layers = []string{pdiggerfactory.LayerOverlay, pdiggerfactory.LayerCards}
	view := must(api.GetFrame(ctx, pdiggerfactory.FrameAt(frame.G), &pdiggerfactory.FrameParams{Layers: layers}))
	wh := must(api.GetVideo(ctx, view.Video)).NativeWH
	var joints []*codec.Joint // u, v, visibility, x, y, z, or nil
	if view.Overlay != nil {
		joints = codec.Joints(view.Overlay.Joints)
	}
	px := make([]*[2]float64, len(joints))
	for i, j := range joints {
		if j != nil && len(wh) == 2 {
			x, y := codec.UVToPx(j.U, j.V, int(wh[0]), int(wh[1]))
			px[i] = &[2]float64{x, y}
		}
	}
	inView := func(j int) bool { return j >= 0 && j < len(px) && px[j] != nil }
	bones := 0
	for _, b := range topology.BonePairs() { // lines to draw, between px[a] and px[b]
		if inView(b[0]) && inView(b[1]) {
			bones++
		}
	}
	fmt.Printf("g %d: %d of %d bones\n", view.G, bones, len(topology.Bones))
	for _, side := range []string{"left", "right"} {
		title := "no card"
		if view.Cards != nil {
			title = view.Cards.Left.Title
			if side == "right" {
				title = view.Cards.Right.Title
			}
		}
		where := "not in view"
		if j, ok := topology.WristJoint(side); ok && inView(j) {
			where = fmt.Sprintf("wrist at %.0f, %.0f px", px[j][0], px[j][1])
		}
		fmt.Printf("%s hand: %s, %s\n", side, title, where)
	}

	// 8. A frame's image data: the model's rasters as PNG, and the picture it was made from
	png := must(api.GetFrameRaster(ctx, frame.G, pdiggerfactory.RasterRegions, nil)) // also RasterMask, RasterDepth
	fmt.Printf("regions of g %d: a %d-byte PNG\n", frame.G, len(png))
	source := must(api.GetVideo(ctx, frame.Video))
	if source.IsLive() {
		ref := &pdiggerfactory.PictureParams{Session: session}
		picture := must(api.GetFramePicture(ctx, pdiggerfactory.FrameAt(frame.G), ref)) // a camera's own JPEG
		fmt.Printf("picture: a %d-byte JPEG of g %d\n", len(picture.JPEG), picture.G)
	} else {
		fmt.Printf("picture: %s at %s, %s\n", source.Proxy, clock, size(source.ProxyWH)) // its video at frame.T
	}
}

// must stops the program on an error; a program of yours would handle it, by its code.
func must[T any](v T, err error) T {
	if err != nil {
		log.Fatal(err)
	}
	return v
}

// value is a number the unit may leave out, else 0.
func value(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

// cells counts the cells of a map that are set (nil: a map the packet did not have).
func cells(m *codec.U8Map) int {
	n := 0
	if m != nil {
		for _, v := range m.Data {
			if v > 0 {
				n++
			}
		}
	}
	return n
}

// size is a width and height as "w x h".
func size(wh []int64) string {
	parts := make([]string, len(wh))
	for i, v := range wh {
		parts[i] = strconv.FormatInt(v, 10)
	}
	return strings.Join(parts, " x ")
}
```

</details>

Against the SDK's fake unit playing the fixture dry1-0296-0306-full
(`make fake-unit ARGS="-recording ../fixtures/recordings/dry1-0296-0306-full.ndjson.gz"`), at frame 1510, it prints
what the Python, Rust and TypeScript programs print there (the fake does not draw: its draw lists are the unit's
recorded ones, of dry2's frame 3079):

```text
ready true, worker idle: 3.8 frames a second, 241.6 ms each
checkpoint 8d09531c915f on trt: 6 engines
70 joints, 62 bones, 11 colours
stone 627: 51 points, g 1480 to 1530
focus on stone 627: dry run true
frame g 1510 at 5:01.2: 42 stones, rect
maps of g 1510: 40 x 64 cells, 920 of the tray, 1169 of stones
overlay of g 3079: 44 polygons, 7 segments, 8 circles at 1920 x 1080 px
scene of g 3079: 42 stones, 7 bones
g 1510: 42 outlines, 168 points
g 1510: 50 of 62 bones
left hand: Hand on a stone, wrist at 1528, 595 px
right hand: Holding a stone, wrist at 1270, 415 px
regions of g 1510: a 1106-byte PNG
picture: /api/v2/videos/dry1/media at 5:01.2, 960 x 540
```

## The steps

1. **Ready, and how fast.** `GetReady` says whether the unit serves frames (while it starts: the error `not_ready`,
   with what it waits for); the status has the worker's rate, the time of each stage of the model (`StageMS`), the
   frames it keeps and the viewers, long-polls and streams it serves. `GetHealth` is the short answer for a load
   balancer, `GetMetrics` the Prometheus text.
2. **The model, and what to draw with.** The model is the unit's cascade, engine by engine with its hash; the
   topology is what every drawing is made with: the skeleton's bones (`BonePairs`), which joints are the wrists and
   the fingertips (`WristJoint`, `TipJoints`), and the colours the unit's own page uses.
3. **A stone's track, and the focus.** The focus highlights one stone for every viewer, so here it is a dry run
   (`DryRun: true`): the unit checks the request and answers with the state it would have. The same holds for every
   control of the shared session: give `client.Session()`'s controls the option `pdiggerfactory.DryRun()`.
4. **A frame, and the model's maps.** A frame carries the layers you ask for: `LayerWorld` (the stones, hands and
   events), `LayerCards` (the hand cards), `LayerOverlay` (the model's 2-D view), `LayerScene` (the 3-D scene) and
   `LayerRaw`. `LatestFrame()` is the newest and `FrameAt(g)` one by its number; the unit keeps the newest frames of
   the session (512 on the 20.20 unit), and an older one gives `not_retained`, which says what is kept. The raw
   packet is what the model saw, before the world made sense of it.
5. **Draw lists, ready to paint.** The unit sends what its own views draw: the 2-D overlay as shapes with their
   style, in video pixels, and the 3-D scene in scene units. Paint them with any library; no SDK code is needed
   between the reply and your canvas.
6. **Stone shapes, drawn by hand.** Each stone of the world layer has its outline, a polygon in video pixels; the
   topology gives the colours.
7. **Hands and bones, drawn by hand.** The overlay layer has the 70 joints of the body and both hands;
   `codec.Joints` and `codec.UVToPx` give them in video pixels, and the topology the bones between them and the
   wrists. At frame 1510 that is 50 of the 62 bones, with the left hand on a stone and the right one holding one.
8. **A frame's image data.** The unit's rasters of a frame, as PNG: the regions, the person's mask and the depth. A
   recording's picture is its video at the frame's time, served by byte ranges (`GetVideoMedia`, or the
   `OpenVideoMedia` reader); a live camera has no video, and each of its frames comes with the camera's own JPEG
   (`GetFramePicture`), which is what the program prints on a camera.

`must` stops the program at the first error, which keeps the tutorial short; a program of yours would handle the error
by its code ([the Go tutorial](04-go.md#one-error-type)).

Between the operations and the interpreter are the streams: the frames as they are made, resumed with
`Last-Event-ID` when the connection drops, with the side events between them; the long-poll where a stream cannot
stay open; and the raw packets ([examples/streams](../../examples/streams/main.go),
[streams.md](../streams.md)). Every operation is listed in [low-level.md](../low-level.md).

## Next

- [Live cameras](03-live-cameras.md): select a plant camera and follow it.
- [Go](04-go.md): contexts, errors and testing in this SDK.
