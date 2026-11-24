package pdiggerfactory

import (
	"encoding/json"
	"maps"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/codec"
)

// hr4Packet is a raw packet of the 4x stone head (dry1, g 736), redacted as the published recordings are: no face
// keypoints, appearance masks or depth texture.
const hr4Packet = "testdata/sample-packet-hr-dry1.json"

// stonesFields are the JSON names of the stones block of API 2.2.0: the base block and the 4x head's source, centres
// and centre_grid.
var stonesFields = []string{"box", "centre_grid", "centres", "count", "fg", "grid", "items", "present", "rot",
	"source", "tray", "tray_grid"}

// TestModelsStonesFieldsAreTheSpecs checks that Sip1Stones types exactly the stones block of spec/openapi.json.
func TestModelsStonesFieldsAreTheSpecs(t *testing.T) {
	data, err := os.ReadFile("spec/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Components struct {
			Schemas struct {
				Sip1Packet struct {
					Properties struct {
						Stones struct {
							Properties map[string]json.RawMessage `json:"properties"`
						} `json:"stones"`
					} `json:"properties"`
				} `json:"Sip1Packet"`
			} `json:"schemas"`
		} `json:"components"`
	}
	if err := json.Unmarshal(data, &spec); err != nil {
		t.Fatal(err)
	}
	listed := slices.Sorted(maps.Keys(spec.Components.Schemas.Sip1Packet.Properties.Stones.Properties))
	fields := map[string]reflect.Type{}
	collectFields(reflect.TypeOf(Sip1Stones{}), fields)
	typed := slices.Sorted(maps.Keys(fields))
	if !slices.Equal(listed, stonesFields) || !slices.Equal(typed, stonesFields) {
		t.Fatalf("stones block: the spec lists %v, Sip1Stones types %v, API 2.2.0 has %v", listed, typed, stonesFields)
	}
}

// TestModelsType4xStonesBlock checks a 4x packet as TestModelsTypeEveryRecordedField checks the fixtures (every
// field typed), also as a base block without the 4x fields, and decodes every map of the block on its own grid.
func TestModelsType4xStonesBlock(t *testing.T) {
	data, err := os.ReadFile(hr4Packet)
	if err != nil {
		t.Fatal(err)
	}
	checkTyped(t, "4x packet", &Sip1Packet{}, data)
	var p Sip1Packet
	if err := json.Unmarshal(data, &p); err != nil {
		t.Fatal(err)
	}
	s := p.Stones
	if s.Source != "hr4" || s.Centres != "base" || !slices.Equal(s.CentreGrid, []int64{20, 32}) ||
		!slices.Equal(s.Grid, []int64{160, 256}) || !slices.Equal(s.TrayGrid, []int64{80, 128}) {
		t.Fatalf("4x stones: %q %q %v %v %v", s.Source, s.Centres, s.CentreGrid, s.Grid, s.TrayGrid)
	}
	if s.Count == nil || *s.Count != 39 || len(s.Items) != 39 {
		t.Fatalf("4x stones: %d items", len(s.Items))
	}
	fg, err := codec.DecodeZlibMap(s.FG, int(s.Grid[0]), int(s.Grid[1]))
	if err != nil || fg == nil || len(fg.Data) != 160*256 {
		t.Fatalf("fg: %v", err)
	}
	tray, err := codec.DecodeZlibMap(s.Tray, int(s.TrayGrid[0]), int(s.TrayGrid[1]))
	if err != nil || tray == nil || len(tray.Data) != 80*128 {
		t.Fatalf("tray: %v", err)
	}

	// a base block: none of the 4x fields
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	stones := raw["stones"].(map[string]any)
	for _, k := range []string{"source", "centres", "centre_grid"} {
		delete(stones, k)
	}
	base, err := json.Marshal(stones)
	if err != nil {
		t.Fatal(err)
	}
	var b Sip1Stones
	checkTyped(t, "base block", &b, base)
	if b.Source != "" || b.Centres != "" || b.CentreGrid != nil {
		t.Fatalf("base block: %+v", b)
	}
}
