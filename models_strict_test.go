package pdiggerfactory

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/eprojac/pdigger-factory-sdk-go/internal/pfr"
)

// untyped walks a JSON value along a Go type and appends the path of every object member that no struct field takes:
// a field the models leave untyped. Maps of any, json.RawMessage and the tuple forms (a struct read from an array)
// end the walk.
func untyped(t reflect.Type, v any, path string, out *[]string) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		obj, ok := v.(map[string]any)
		if !ok {
			return // a tuple form (Box, HandBox) or a null
		}
		fields := map[string]reflect.Type{}
		collectFields(t, fields)
		keys := make([]string, 0, len(obj))
		for k := range obj {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			ft, ok := fields[k]
			if !ok {
				*out = append(*out, path+"."+k)
				continue
			}
			untyped(ft, obj[k], path+"."+k, out)
		}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return // json.RawMessage
		}
		if arr, ok := v.([]any); ok {
			for i, e := range arr {
				if i < 3 || i == len(arr)-1 { // the first items and the last are enough for a list of one kind
					untyped(t.Elem(), e, path+"[]", out)
				}
			}
		}
	case reflect.Map:
		if m, ok := v.(map[string]any); ok {
			for k, e := range m {
				untyped(t.Elem(), e, path+"{"+k+"}", out)
			}
		}
	}
}

// collectFields maps the JSON names of a struct's fields (embedded structs flattened, as encoding/json does) to
// their types.
func collectFields(t reflect.Type, fields map[string]reflect.Type) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "-" || (!f.IsExported() && !f.Anonymous) {
			continue
		}
		if f.Anonymous && name == "" {
			et := f.Type
			if et.Kind() == reflect.Pointer {
				et = et.Elem()
			}
			if et.Kind() == reflect.Struct {
				collectFields(et, fields)
				continue
			}
		}
		if name == "" {
			name = f.Name
		}
		fields[name] = f.Type
	}
}

// checkTyped fails for every member of a recorded JSON value that the model does not type, and for a value the model
// cannot decode.
func checkTyped(t *testing.T, where string, model any, data []byte) {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Errorf("%s: not JSON: %v", where, err)
		return
	}
	if err := json.Unmarshal(data, model); err != nil {
		t.Errorf("%s: %T cannot decode it: %v", where, model, err)
		return
	}
	var out []string
	untyped(reflect.TypeOf(model), v, "", &out)
	if len(out) > 0 {
		t.Errorf("%s: %T leaves untyped %v", where, model, out)
	}
}

// TestModelsTypeEveryRecordedField decodes every recorded reply, stream event, frame and recording line of the
// fixtures and checks that the models type every field of them.
func TestModelsTypeEveryRecordedField(t *testing.T) {
	models := map[string]func() any{
		"getSession": func() any { return &SessionReply{} }, "getApiRoot": func() any { return &APIRoot{} },
		"getModel": func() any { return &ModelInfo{} }, "getTopology": func() any { return &Topology{} },
		"listVideos": func() any { return &VideoList{} }, "getVideo": func() any { return &Source{} },
		"getHealth": func() any { return &Health{} }, "getReady": func() any { return &Ready{} },
		"getStatus": func() any { return &Status{} }, "getFocus": func() any { return &FocusReply{} },
		"getWorld": func() any { return &WorldLayer{} }, "getWorldEvents": func() any { return &EventLog{} },
		"getFrame": func() any { return &Frame{} }, "rawPacket": func() any { return &Sip1Packet{} },
		"pollFrames": func() any { return &LiveReply{} }, "renderOverlay2d": func() any { return &Overlay2D{} },
		"renderScene3d": func() any { return &Scene3D{} }, "getStone": func() any { return &StoneTrack{} },
		"putFocus": func() any { return &ControlReply{} }, "deleteFocus": func() any { return &ControlReply{} },
		"sessionPause": func() any { return &ControlReply{} },
	}
	b, err := os.ReadFile(filepath.Join(fixturesAPI, "INDEX.json"))
	if err != nil {
		t.Fatal(err)
	}
	var index map[string]struct {
		File   string `json:"file"`
		Status int    `json:"status"`
	}
	if err := json.Unmarshal(b, &index); err != nil {
		t.Fatal(err)
	}
	checked := 0
	for key, e := range index {
		body, err := os.ReadFile(filepath.Join(fixturesAPI, e.File))
		if err != nil {
			t.Fatal(err)
		}
		op, _, _ := strings.Cut(key, "-")
		switch {
		case e.Status >= 400:
			checkTyped(t, key, &ErrorReply{}, body)
		case strings.HasSuffix(e.File, ".sse"):
			p := NewSSEParser(bytes.NewReader(body))
			for {
				ev, err := p.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(key, err)
				}
				checkTyped(t, key+" "+ev.Event, streamModel(op, ev.Event), ev.Data)
			}
		case strings.HasSuffix(e.File, ".ndjson"):
			p := NewNDJSONParser(bytes.NewReader(body))
			for {
				ev, err := p.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(key, err)
				}
				checkTyped(t, key+" "+ev.Event, streamModel(op, ev.Event), ev.Data)
			}
		case strings.HasSuffix(e.File, ".json"):
			newModel, ok := models[op]
			if !ok {
				t.Errorf("%s: no model in this test", key)
				continue
			}
			checkTyped(t, key, newModel(), body)
		default:
			continue // a PNG, a byte range, the metrics text, the OpenAPI document
		}
		checked++
	}
	if checked < 35 {
		t.Fatalf("only %d replies checked", checked)
	}
	// every recording: its header, every side line and frame (every fifth after the first ten, which is enough for
	// frames of one kind), and the full frames
	recs, _ := filepath.Glob(filepath.Join("fixtures", "recordings", "*.ndjson.gz"))
	frames, _ := filepath.Glob(filepath.Join("fixtures", "frames", "*.json"))
	if len(recs) < 8 || len(frames) < 2 {
		t.Fatalf("%d recordings, %d frames", len(recs), len(frames))
	}
	for _, rec := range recs {
		t.Run(filepath.Base(rec), func(t *testing.T) {
			t.Parallel()
			rc, err := pfr.Open(testCtx(t), rec, nil, "")
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			r := pfr.NewReader(rc)
			n := 0
			for {
				line, err := r.Next()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				l, err := pfr.Classify(line)
				if err != nil {
					t.Fatal(err)
				}
				switch {
				case l.Recording:
					checkTyped(t, "header", &RecordingHeader{}, line)
				case l.IsSide:
					checkTyped(t, l.SSE, streamModel("streamFrames", l.SSE), l.Data)
				case l.IsFrame:
					if n < 10 || n%5 == 0 {
						checkTyped(t, "frame", &Frame{}, line)
					}
					n++
				}
			}
			if n == 0 {
				t.Fatal("no frames")
			}
		})
	}
	for _, p := range frames {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		checkTyped(t, filepath.Base(p), &Frame{}, b)
	}
}

// streamModel is the model of a stream event's data.
func streamModel(op, event string) any {
	switch event {
	case StreamEventClock:
		return &ClockEvent{}
	case StreamEventWorker:
		return &WorkerStatus{}
	case StreamEventGap:
		return &GapEvent{}
	case StreamEventReset:
		return &ResetEvent{}
	}
	if op == "rawStream" {
		return &Sip1Packet{}
	}
	return &Frame{}
}

func TestUntypedWalker(t *testing.T) {
	type inner struct {
		A int `json:"a"`
	}
	type outer struct {
		inner
		B  []inner          `json:"b"`
		M  map[string]inner `json:"m"`
		R  json.RawMessage  `json:"r"`
		X  map[string]any   `json:"x"`
		Bx Box              `json:"bx"`
		S  string           `json:"-"`
		u  int
	}
	_ = outer{}.u
	var v any
	_ = json.Unmarshal([]byte(`{"a":1,"b":[{"a":1,"z":2}],"m":{"k":{"y":1}},"r":{"q":1},"x":{"q":1},"bx":[1,2,3,4],"S":1,"u":1,"w":2}`), &v)
	var out []string
	untyped(reflect.TypeOf(&outer{}), v, "", &out)
	sort.Strings(out)
	if strings.Join(out, " ") != ".S .b[].z .m{k}.y .u .w" {
		t.Fatal(out)
	}
}
