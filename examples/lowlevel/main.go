// Command lowlevel shows the low level on a unit: one method per API operation with typed replies, the codec that
// decodes the packed maps of a frame's raw layer, and a render-ready scene3d draw list.
//
//	PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./lowlevel
package main

import (
	"context"
	"fmt"
	"log"
	"os"

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
	root, err := api.GetAPIRoot(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("API %s, build %s\n", root.API, root.Build)

	// The newest world, then the model's raw output of the same frame (the very newest frame may still be waiting
	// for its world).
	world, err := api.GetWorld(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	frame, err := api.GetFrame(ctx, pdiggerfactory.FrameAt(world.G), &pdiggerfactory.FrameParams{
		Layers: []string{pdiggerfactory.LayerRaw}})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("frame g %d at %s: %d stones\n", frame.G, pdiggerfactory.FormatClock(frame.T), len(world.Stones))

	// The raw layer's maps are packed: the tray map is a zlib-compressed byte grid. A frame can come without the
	// layer (one listed in frame.Missing, or a fake unit playing a recording made without it).
	if frame.Raw == nil || len(frame.Raw.Maps.Grid) < 2 {
		fmt.Printf("frame g %d has no raw layer, so no tray map\n", frame.G)
	} else {
		maps := frame.Raw.Maps
		tray, err := codec.DecodeZlibMap(maps.Tray, int(maps.Grid[0]), int(maps.Grid[1]))
		if err != nil {
			log.Fatal(err)
		}
		cells := 0
		for _, v := range tray.Data {
			if v > 127 {
				cells++
			}
		}
		fmt.Printf("tray map %dx%d: %d of %d cells are tray\n", tray.Rows, tray.Cols, cells, len(tray.Data))
	}

	// A render-ready 3-D draw list of the same frame, with the compact surface.
	scene, err := api.RenderScene3D(ctx, &pdiggerfactory.Scene3DParams{
		RenderParams: pdiggerfactory.RenderParams{G: pdiggerfactory.Int64(frame.G)}, Surface: "compact"})
	if err != nil {
		log.Fatal(err)
	}
	flat, err := codec.DecodeBitset(scene.Surface.Flat, scene.Surface.Rows*scene.Surface.Cols)
	if err != nil {
		log.Fatal(err)
	}
	n := 0
	for _, f := range flat {
		if f {
			n++
		}
	}
	fmt.Printf("scene3d: %d stones, surface %dx%d with %d flat cells\n", len(scene.Stones), scene.Surface.Rows,
		scene.Surface.Cols, n)
}
