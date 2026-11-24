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
