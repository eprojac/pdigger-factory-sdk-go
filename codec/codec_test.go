package codec_test

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/codec"
)

func load(t *testing.T, path string, out any) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "fixtures", path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
}

// TestRecordedEncodings decodes the encodings of a frame recorded on 20.20 (dry1, g 1512, every layer) and of a
// recorded scene3d draw list.
func TestRecordedEncodings(t *testing.T) {
	var f struct {
		Raw struct {
			Maps struct {
				Grid []int  `json:"grid"`
				Tray string `json:"tray"`
			} `json:"maps"`
			Depth struct {
				Lo, Hi, A, B float64
			} `json:"depth"`
			Person struct {
				Joints [][]float64 `json:"joints"`
			} `json:"person"`
			Stones struct {
				Items [][]float64 `json:"items"`
			} `json:"stones"`
		} `json:"raw"`
		Overlay struct {
			Regions struct {
				Rows, Cols int
				Stone      string `json:"stone"`
			} `json:"regions"`
		} `json:"overlay"`
	}
	load(t, "frames/dry1-carry-full.json", &f)
	rows, cols := f.Raw.Maps.Grid[0], f.Raw.Maps.Grid[1]
	tray, err := codec.DecodeZlibMap(f.Raw.Maps.Tray, rows, cols)
	if err != nil || tray.Rows != 40 || tray.Cols != 64 || len(tray.Data) != 2560 {
		t.Fatalf("tray %v", err)
	}
	_ = tray.At(39, 63)
	regions, err := codec.DecodeU8Map(f.Overlay.Regions.Stone, f.Overlay.Regions.Rows, f.Overlay.Regions.Cols)
	if err != nil || regions.At(0, 0) != 0 {
		t.Fatalf("regions %v", err)
	}
	joints := codec.Joints(f.Raw.Person.Joints)
	nulls := 0
	for _, j := range joints {
		if j == nil {
			nulls++
		}
	}
	if len(joints) != 70 || nulls != 23 {
		t.Fatalf("%d joints, %d null", len(joints), nulls)
	}
	items := codec.StoneItems(append(f.Raw.Stones.Items, []float64{1, 2}))
	if len(items) != 33 || items[0].U != 0.5113 || items[0].Yaw != 1.494 {
		t.Fatalf("%d items %+v", len(items), items[0])
	}
	d := f.Raw.Depth
	if z := codec.DepthZ(255, d.Lo, d.Hi, d.A, d.B); math.Abs(z-math.Exp(d.A*d.Hi+d.B)) > 1e-12 {
		t.Fatal(z)
	}
	if lz := codec.DepthLogZ(0, d.Lo, d.Hi, d.A, d.B); math.Abs(lz-(d.A*d.Lo+d.B)) > 1e-12 {
		t.Fatal(lz)
	}

	var compact struct {
		Surface struct {
			Rows, Cols int
			Q          string `json:"q"`
			Flat       string `json:"flat"`
			Quads      struct {
				Raw string `json:"raw"`
			} `json:"quads"`
		} `json:"surface"`
	}
	load(t, "api/renderScene3d-compact.json", &compact)
	s := compact.Surface
	if q, err := codec.DecodeU8Map(s.Q, s.Rows, s.Cols); err != nil || len(q.Data) != 2560 {
		t.Fatalf("q %v", err)
	}
	if flat, err := codec.DecodeBitset(s.Flat, s.Rows*s.Cols); err != nil || len(flat) != 2560 {
		t.Fatalf("flat %v", err)
	}
	if quads, err := codec.DecodeBitset(s.Quads.Raw, (s.Rows-1)*(s.Cols-1)); err != nil || !quads[0] {
		t.Fatalf("quads %v", err)
	}
	var mesh struct {
		Surface struct {
			Positions string `json:"positions"`
			Indices   string `json:"indices"`
		} `json:"surface"`
	}
	load(t, "api/renderScene3d.json", &mesh)
	pos, err := codec.DecodeF32LE(mesh.Surface.Positions)
	if err != nil || len(pos)%3 != 0 {
		t.Fatalf("positions %v", err)
	}
	pts := codec.Triplets(pos)
	idx, err := codec.DecodeU16LE(mesh.Surface.Indices)
	if err != nil || len(idx) == 0 {
		t.Fatalf("indices %v", err)
	}
	for _, i := range idx {
		if int(i) >= len(pts) {
			t.Fatalf("index %d of %d points", i, len(pts))
		}
	}
}

func zlibOf(b []byte) []byte {
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	zw.Write(b)
	zw.Close()
	return buf.Bytes()
}

func TestEncodingErrors(t *testing.T) {
	b64 := base64.StdEncoding.EncodeToString
	for name, err := range map[string]error{
		"u8 base64":   second(codec.DecodeU8Map("@@", 1, 1)),
		"u8 size":     second(codec.DecodeU8Map(b64([]byte{1, 2, 3}), 2, 2)),
		"u8 negative": second(codec.DecodeU8Map(b64(nil)+"AA==", -1, 1)),
		"zlib base64": second(codec.DecodeZlibMap("@@", 1, 1)),
		"zlib header": second(codec.DecodeZlibMap(b64([]byte("xx")), 1, 1)),
		"zlib cut":    second(codec.DecodeZlibMap(b64(zlibOf(bytes.Repeat([]byte{7}, 4096))[:8]), 64, 64)),
		"zlib size":   second(codec.DecodeZlibMap(b64(zlibOf([]byte{1, 2, 3})), 2, 2)),
		"bits base64": second(codec.DecodeBitset("@@", 1)),
		"bits short":  second(codec.DecodeBitset(b64([]byte{1}), 9)),
		"bits n<0":    second(codec.DecodeBitset(b64([]byte{1}), -1)),
		"f32 base64":  second(codec.DecodeF32LE("@@")),
		"f32 size":    second(codec.DecodeF32LE(b64([]byte{1, 2, 3}))),
		"u16 base64":  second(codec.DecodeU16LE("@@")),
		"u16 size":    second(codec.DecodeU16LE(b64([]byte{1}))),
	} {
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// absent (redacted) maps
	if m, err := codec.DecodeU8Map("", 40, 64); m != nil || err != nil {
		t.Fatal("u8")
	}
	if m, err := codec.DecodeZlibMap("", 40, 64); m != nil || err != nil {
		t.Fatal("zlib")
	}
	if m, err := codec.DecodeBitset("", 10); m != nil || err != nil {
		t.Fatal("bits")
	}
	if m, err := codec.DecodeZlibMap(b64(zlibOf([]byte{1, 2, 3, 4})), 2, 2); err != nil || m.At(1, 0) != 3 {
		t.Fatal(err)
	}
	if bits, _ := codec.DecodeBitset(b64([]byte{0b0000_0101}), 3); !bits[0] || bits[1] || !bits[2] {
		t.Fatal("LSB first")
	}
	f32 := make([]byte, 4)
	f32[3] = 0x3f
	f32[2] = 0x80
	if v, _ := codec.DecodeF32LE(b64(f32)); v[0] != 1 {
		t.Fatal(v)
	}
	if v, _ := codec.DecodeU16LE(b64([]byte{0x34, 0x12})); v[0] != 0x1234 {
		t.Fatal(v)
	}
}

func second[T any](_ T, err error) error { return err }

func TestTuplesAndCoordinates(t *testing.T) {
	if codec.JointOf([]float64{1, 2, 3}) != nil || codec.JointOf(nil) != nil {
		t.Fatal("short joints are nil")
	}
	if j := codec.JointOf([]float64{0.1, 0.2, 1, 0.3, 0.4, 2.5}); j.Vis != 1 || j.Z != 2.5 {
		t.Fatal(j)
	}
	if codec.StoneItemOf(make([]float64, 10)) != nil {
		t.Fatal("short stone")
	}
	px, py := codec.UVToPx(0.5, 0.25, 1088, 1026)
	if px != 543.5 || py != 256 {
		t.Fatal(px, py)
	}
	if u, v := codec.PxToUV(px, py, 1088, 1026); u != 0.5 || v != 0.25 {
		t.Fatal(u, v)
	}
	if p := codec.SceneToThree([3]float64{1, 2, 3}); p != [3]float64{1, 3, -2} {
		t.Fatal(p)
	}
	// 90 degrees about z
	h := math.Sqrt(0.5)
	if yaw := codec.QuatToYaw([4]float64{0, 0, h, h}); math.Abs(yaw-math.Pi/2) > 1e-12 {
		t.Fatal(yaw)
	}
	// the stone of the recorded focus: yaw -1.5504
	if yaw := codec.QuatToYaw(codec.Quat([]float64{0, 0, -0.69988, 0.71426})); math.Abs(yaw+1.5504) > 1e-4 {
		t.Fatal(yaw)
	}
	if v := codec.Vec3([]float64{1}); v != [3]float64{1, 0, 0} {
		t.Fatal(v)
	}
	if q := codec.Quat([]float64{1, 2, 3, 4, 5}); q != [4]float64{1, 2, 3, 4} {
		t.Fatal(q)
	}
}
