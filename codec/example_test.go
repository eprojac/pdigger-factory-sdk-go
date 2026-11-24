package codec_test

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
	"github.com/eprojac/pdigger-factory-sdk-go/codec"
)

// Decode the packed parts of a frame's raw layer: the tray map and the stone tuples. The frame here is a fixture
// recorded on 20.20, as API.GetFrame returns it with the raw layer.
func Example() {
	b, err := os.ReadFile("../fixtures/frames/dry1-plain-full.json")
	if err != nil {
		log.Fatal(err)
	}
	var frame pdiggerfactory.Frame
	if err := json.Unmarshal(b, &frame); err != nil {
		log.Fatal(err)
	}
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
	fmt.Printf("g %d: tray map %dx%d, %d cells are tray\n", frame.G, tray.Rows, tray.Cols, cells)
	stones := codec.StoneItems(frame.Raw.Stones.Items)
	first := stones[0]
	fmt.Printf("%d stones; the first at u %.4f, v %.4f, %.3f m by %.3f m\n", len(stones), first.U, first.V, first.WM,
		first.HM)
	// Output:
	// g 1480: tray map 40x64, 727 cells are tray
	// 40 stones; the first at u 0.5126, v 0.4132, 0.140 m by 0.216 m
}

// A bitset packs booleans least significant bit first: 0x05 is bits 0 and 2.
func ExampleDecodeBitset() {
	bits, err := codec.DecodeBitset("BQ==", 4)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(bits)
	// Output: [true false true false]
}

// Normalised image coordinates to pixels of a 1920 x 1080 frame (pixel centres at .5).
func ExampleUVToPx() {
	x, y := codec.UVToPx(0.5126, 0.4132, 1920, 1080)
	fmt.Printf("%.1f, %.1f\n", x, y)
	// Output: 983.7, 445.8
}
