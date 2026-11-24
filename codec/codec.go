// Package codec decodes the compact encodings of the pDigger Factory layers: u8 maps (base64, and base64 of zlib
// for the raw sip1 packet), LSB-first bitsets, quantised log depth, little-endian f32 and u16 buffers of the
// surface mesh, joint and stone tuples, and the coordinate conventions (native px, content units, scene world,
// three.js axes). An absent map (an empty string: redacted in public recordings) decodes to nil without an error.
package codec

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// U8Map is a rows x cols map of bytes, row-major.
type U8Map struct {
	Rows, Cols int
	Data       []uint8
}

// At is the value at row r, column c.
func (m *U8Map) At(r, c int) uint8 { return m.Data[r*m.Cols+c] }

func decodeBase64(s string) ([]byte, error) {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("codec: base64: %w", err)
	}
	return b, nil
}

func u8Map(b []byte, rows, cols int) (*U8Map, error) {
	if rows < 0 || cols < 0 || len(b) != rows*cols {
		return nil, fmt.Errorf("codec: map of %d bytes is not %d x %d", len(b), rows, cols)
	}
	return &U8Map{Rows: rows, Cols: cols, Data: b}, nil
}

// DecodeU8Map decodes an overlay map: base64 of rows*cols bytes, row-major. An empty string gives nil.
func DecodeU8Map(b64 string, rows, cols int) (*U8Map, error) {
	if b64 == "" {
		return nil, nil
	}
	b, err := decodeBase64(b64)
	if err != nil {
		return nil, err
	}
	return u8Map(b, rows, cols)
}

// DecodeZlibMap decodes a sip1 map: base64 of zlib of rows*cols bytes, row-major. An empty string gives nil.
func DecodeZlibMap(b64 string, rows, cols int) (*U8Map, error) {
	if b64 == "" {
		return nil, nil
	}
	z, err := decodeBase64(b64)
	if err != nil {
		return nil, err
	}
	zr, err := zlib.NewReader(bytes.NewReader(z))
	if err != nil {
		return nil, fmt.Errorf("codec: zlib: %w", err)
	}
	defer zr.Close()
	b, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("codec: zlib: %w", err)
	}
	return u8Map(b, rows, cols)
}

// DecodeBitset decodes n booleans from base64, least significant bit first within each byte. An empty string
// gives nil.
func DecodeBitset(b64 string, n int) ([]bool, error) {
	if b64 == "" {
		return nil, nil
	}
	b, err := decodeBase64(b64)
	if err != nil {
		return nil, err
	}
	if n < 0 || len(b)*8 < n {
		return nil, fmt.Errorf("codec: bitset of %d bytes has no %d bits", len(b), n)
	}
	out := make([]bool, n)
	for i := range out {
		out[i] = b[i/8]>>(uint(i)%8)&1 == 1
	}
	return out, nil
}

// DepthLogZ is the log depth of a quantised value q (0..255): a*(lo + q/255*(hi-lo)) + b, every step rounded on its
// own as in the other languages (the conversions keep Go from fusing a product and a sum into one multiply-add).
func DepthLogZ(q, lo, hi, a, b float64) float64 {
	return float64(a*(lo+float64(q/255*(hi-lo)))) + b
}

// DepthZ is the depth exp(DepthLogZ(q, lo, hi, a, b)).
func DepthZ(q, lo, hi, a, b float64) float64 {
	return math.Exp(DepthLogZ(q, lo, hi, a, b))
}

// DecodeF32LE decodes base64 of little-endian float32 values (surface positions, x, y, z triplets).
func DecodeF32LE(b64 string) ([]float32, error) {
	b, err := decodeBase64(b64)
	if err != nil {
		return nil, err
	}
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("codec: %d bytes are not float32 values", len(b))
	}
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out, nil
}

// DecodeU16LE decodes base64 of little-endian uint16 values (surface triangle indices).
func DecodeU16LE(b64 string) ([]uint16, error) {
	b, err := decodeBase64(b64)
	if err != nil {
		return nil, err
	}
	if len(b)%2 != 0 {
		return nil, fmt.Errorf("codec: %d bytes are not uint16 values", len(b))
	}
	out := make([]uint16, len(b)/2)
	for i := range out {
		out[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return out, nil
}

// Triplets groups a flat list of floats as x, y, z points.
func Triplets(v []float32) [][3]float32 {
	out := make([][3]float32, len(v)/3)
	for i := range out {
		out[i] = [3]float32{v[3*i], v[3*i+1], v[3*i+2]}
	}
	return out
}

// Joint is a joint [u, v, vis, x, y, z]: content units, visibility, and camera-frame template metres.
type Joint struct {
	U, V, Vis, X, Y, Z float64
}

// JointOf reads a joint tuple; nil for a null (or short) entry.
func JointOf(t []float64) *Joint {
	if len(t) < 6 {
		return nil
	}
	return &Joint{U: t[0], V: t[1], Vis: t[2], X: t[3], Y: t[4], Z: t[5]}
}

// Joints reads a joints list (nil entries stay nil).
func Joints(list [][]float64) []*Joint {
	out := make([]*Joint, len(list))
	for i, t := range list {
		out[i] = JointOf(t)
	}
	return out
}

// StoneItem is a stone tuple [u, v, w, h, score, X, Y, Z, w_m, h_m, yaw]: the box centre and size in content
// units, the score, the camera-frame position, the size in template metres and the yaw.
type StoneItem struct {
	U, V, W, H, Score, X, Y, Z, WM, HM, Yaw float64
}

// StoneItemOf reads a stone tuple; nil for a short entry.
func StoneItemOf(t []float64) *StoneItem {
	if len(t) < 11 {
		return nil
	}
	return &StoneItem{U: t[0], V: t[1], W: t[2], H: t[3], Score: t[4], X: t[5], Y: t[6], Z: t[7], WM: t[8], HM: t[9],
		Yaw: t[10]}
}

// StoneItems reads a list of stone tuples (short entries are skipped).
func StoneItems(list [][]float64) []StoneItem {
	out := make([]StoneItem, 0, len(list))
	for _, t := range list {
		if s := StoneItemOf(t); s != nil {
			out = append(out, *s)
		}
	}
	return out
}

// UVToPx converts content units to native px of a W x H crop: pixel centres are at integers, px = u*W - 0.5.
func UVToPx(u, v float64, w, h int) (float64, float64) {
	return float64(u*float64(w)) - 0.5, float64(v*float64(h)) - 0.5
}

// PxToUV converts native px back to content units.
func PxToUV(px, py float64, w, h int) (float64, float64) {
	return (px + 0.5) / float64(w), (py + 0.5) / float64(h)
}

// SceneToThree converts a scene-world point (z up) to three.js axes (y up): [x, y, z] -> [x, z, -y].
func SceneToThree(p [3]float64) [3]float64 {
	return [3]float64{p[0], p[2], -p[1]}
}

// QuatToYaw is the rotation about z of a quaternion (x, y, z, w), radians.
func QuatToYaw(q [4]float64) float64 {
	x, y, z, w := q[0], q[1], q[2], q[3]
	return math.Atan2(2*(float64(w*z)+float64(x*y)), 1-float64(2*(float64(y*y)+float64(z*z))))
}

// Vec3 reads a 3-vector from a slice (missing entries are 0).
func Vec3(v []float64) [3]float64 {
	var out [3]float64
	copy(out[:], v)
	return out
}

// Quat reads a quaternion (x, y, z, w) from a slice (missing entries are 0).
func Quat(v []float64) [4]float64 {
	var out [4]float64
	copy(out[:], v)
	return out
}
