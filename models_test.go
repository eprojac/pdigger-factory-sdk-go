package pdiggerfactory

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestParseMetrics(t *testing.T) {
	text := "# HELP interp_viewers Viewers now.\n# TYPE interp_viewers gauge\ninterp_viewers 1\n\n" +
		`interp_frames_total{video="dry1",note="a \"q\" \\ b\nc\td"} 3726 1700000000` + "\n" +
		"up +Inf\ndown -Inf\nodd NaN\nplain Inf\n" +
		"justaname\n{x=\"1\"} 2\nname{x=\"1\" 3\nname{x=1} 3\nname{=\"\"} 3\nname{x=\"1\"}\nname{x=\"1\"} abc\n" +
		"name abc\ntrail{x=\"1\",} 4\nname{x=\nname{x=\"abc\nname{x=\"ab\\\n"
	m := ParseMetrics(text)
	if len(m.Samples) != 7 || m.Text != text {
		t.Fatalf("%d samples: %+v", len(m.Samples), m.Samples)
	}
	got := m.Find("interp_frames_total", map[string]string{"video": "dry1"})
	if len(got) != 1 || got[0].Value != 3726 || got[0].Labels["note"] != "a \"q\" \\ b\nctd" {
		t.Fatalf("%+v", got)
	}
	if len(m.Find("interp_frames_total", map[string]string{"video": "aug28"})) != 0 {
		t.Fatal("label filter")
	}
	if v := m.Find("up", nil)[0].Value; !math.IsInf(v, 1) {
		t.Fatal("+Inf")
	}
	if v := m.Find("down", nil)[0].Value; !math.IsInf(v, -1) {
		t.Fatal("-Inf")
	}
	if v := m.Find("odd", nil)[0].Value; !math.IsNaN(v) {
		t.Fatal("NaN")
	}
	if m.Find("trail", nil)[0].Labels["x"] != "1" {
		t.Fatal("trailing comma")
	}
}

func TestModelDecoding(t *testing.T) {
	var s Source
	if err := json.Unmarshal([]byte(`{"id":5}`), &s); err == nil {
		t.Fatal("a number id")
	}
	if err := json.Unmarshal([]byte(`{"id":"live","kind":"camera","live":true,"seekable":false,"duration":null}`),
		&s); err != nil || s.Kind != SourceKindCamera || !s.IsLive() || s.Seekable || s.Duration != nil {
		t.Fatalf("%+v %v", s, err)
	}
	if err := json.Unmarshal([]byte(`{"id":"dry1","kind":""}`), &s); err != nil || s.Kind != SourceKindRecording ||
		s.IsLive() || !s.Seekable {
		t.Fatalf("%+v %v", s, err)
	}
	// each missing field from the others, as in the other languages: kind camera is live and not seekable, live is a camera
	for _, entry := range []string{`{"id":"c","kind":"camera"}`, `{"id":"c","live":true}`, `{"id":"c","kind":"camera","seekable":null}`} {
		var c Source
		if err := json.Unmarshal([]byte(entry), &c); err != nil || c.Kind != SourceKindCamera || !c.IsLive() || c.Seekable {
			t.Fatalf("%s: %+v %v", entry, c, err)
		}
	}
	var c Source
	if err := json.Unmarshal([]byte(`{"id":"c","kind":"camera","live":false,"seekable":true}`), &c); err != nil ||
		c.Kind != SourceKindCamera || c.IsLive() || !c.Seekable {
		t.Fatalf("given fields win: %+v %v", c, err)
	}
	st := State{Video: LiveSourceID}
	if !st.IsLive() || (&State{Video: "dry1"}).IsLive() {
		t.Fatal("State.IsLive")
	}
	var b Box
	if err := json.Unmarshal([]byte(`[1, 2, 3, 4, true, 0.5]`), &b); err != nil || !b.Rot || b.Presence != 0.5 || b.X1 != 3 {
		t.Fatalf("%+v %v", b, err)
	}
	if err := json.Unmarshal([]byte(`[1, "x"]`), &b); err != nil || b.Y0 != 0 || b.Rot || b.Presence != 0 {
		t.Fatalf("%+v %v", b, err)
	}
	if err := json.Unmarshal([]byte(`[1, 2, 3, 4, false, 1]`), &b); err != nil || b.Rot {
		t.Fatal("false")
	}
	if err := json.Unmarshal([]byte(`{"x":1}`), &b); err == nil {
		t.Fatal("an object is not a box")
	}
	out, err := json.Marshal(Box{X0: 1, Y0: 2, X1: 3, Y1: 4, Rot: true, Presence: 0.5})
	if err != nil || string(out) != `[1,2,3,4,true,0.5]` {
		t.Fatalf("%s %v", out, err)
	}
	var h HandBox
	if err := json.Unmarshal([]byte(`[0.5, 0.25, 0.9]`), &h); err != nil || h.P != 0.9 {
		t.Fatalf("%+v %v", h, err)
	}
	if err := json.Unmarshal([]byte(`"x"`), &h); err == nil {
		t.Fatal("a string is not a hand box")
	}
	out, err = json.Marshal(HandBox{U: 0.5, V: 0.25, P: 1})
	if err != nil || string(out) != `[0.5,0.25,1]` {
		t.Fatalf("%s %v", out, err)
	}
	f := Frame{Raw: &Sip1Packet{}, Overlay: &OverlayLayer{}, Scene: &SceneLayer{}, Cards: &CardsLayer{}}
	for _, l := range []string{LayerRaw, LayerOverlay, LayerScene, LayerCards} {
		if !f.HasLayer(l) {
			t.Error(l)
		}
	}
	if f.HasLayer(LayerWorld) || f.HasLayer("depth") {
		t.Fatal("HasLayer")
	}
	// the recorded frames decode, every layer
	for _, name := range []string{"dry1-carry-full.json", "dry1-plain-full.json"} {
		raw, err := os.ReadFile(filepath.Join("fixtures", "frames", name))
		if err != nil {
			t.Fatal(err)
		}
		var fr Frame
		if err := json.Unmarshal(raw, &fr); err != nil || fr.Raw == nil || fr.World == nil || fr.Overlay == nil ||
			fr.Scene == nil || fr.Cards == nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestTopologyHelpers(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(fixturesAPI, "getTopology.json"))
	if err != nil {
		t.Fatal(err)
	}
	var top Topology
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	if w, ok := top.WristJoint(HandLeft); !ok || w != 62 {
		t.Fatal("left wrist")
	}
	if w, ok := top.WristJoint(HandRight); !ok || w != 41 {
		t.Fatal("right wrist")
	}
	if _, ok := top.WristJoint("tail"); ok {
		t.Fatal("no such side")
	}
	if tips := top.TipJoints(HandRight); len(tips) != 5 || tips[0] != 21 {
		t.Fatalf("%v", tips)
	}
	if !top.IsNever(15) || top.IsNever(0) {
		t.Fatal("IsNever")
	}
	for j, want := range map[int]string{42: HandLeft, 62: HandLeft, 45: HandLeft, 21: HandRight, 41: HandRight, 7: "", 69: ""} {
		if got := top.HandOfJoint(j); got != want {
			t.Errorf("joint %d: %q, want %q", j, got, want)
		}
	}
	odd := Topology{Bones: [][]int64{{1, 2}, {3}, {2, 3}}, Wrist: TopologySides{Right: Int64(1)}}
	if len(odd.BonePairs()) != 2 || odd.HandOfJoint(3) != HandRight || odd.HandOfJoint(9) != "" {
		t.Fatal("pairs and partial wrists")
	}
}

// TestPackageDefaults: the package's lists and defaults are functions that return new values, so a caller that
// changes one (sorts the event types for a menu, say) changes nothing else.
func TestPackageDefaults(t *testing.T) {
	types := EventTypes()
	sort.Strings(types)
	types[0] = "changed"
	if got := strings.Join(EventTypes(), ","); got != "new,confirm,freeze,push,lift,hinge,carry,place,drop,gone,delete,split" {
		t.Fatal(got)
	}
	if d := NewInterpreterCore().Summary().Describe(); !strings.Contains(d, "\nevents: new 0, confirm 0, freeze 0, push 0, "+
		"lift 0, hinge 0, carry 0, place 0, drop 0, gone 0, delete 0, split 0, other 0\n") {
		t.Fatal(d)
	}
	layers := DefaultLayers()
	layers[0] = LayerRaw
	if got := strings.Join(DefaultLayers(), ","); got != "overlay,scene,cards,world" {
		t.Fatal(got)
	}
	if b := DefaultBackoff(); b != (Backoff{Base: 250 * time.Millisecond, Factor: 2, Max: 5 * time.Second}) {
		t.Fatal(b)
	}
	if a, b := LatestFrame(), LatestFrame(); a != b || a.String() != "latest" {
		t.Fatal("LatestFrame")
	}
}
