package pdiggerfactory

import (
	"strings"
	"testing"
)

func TestShapeNamesTheFirstFieldThatIsWrong(t *testing.T) {
	table := shapeTable{
		{"a", shapeInt, nil}, {"b?", shapeNum, nil}, {"c?", shapeStr, nil}, {"d?", shapeBool, nil},
		{"e?", shapeArr, nil}, {"f?", shapeObjs, nil}, {"g?", shapeOf, gapShape}, {"h?", shapeObj, nil},
	}
	for _, c := range []struct{ data, want string }{
		{`{"a": 1}`, ""},
		{`{"a": -1, "b": 2, "c": "x", "d": false, "e": [], "f": [{}], "g": {"gap": 3}, "h": {}}`, ""},
		{`{"a": null}`, "no a"},
		{`{"a": 1.5}`, "a is not a whole number"},
		{`{"a": 2e0}`, "a is not a whole number"},
		{`{"a": 1, "b": "2"}`, "b is not a number"},
		{`{"a": 1, "b": null, "c": 5}`, "c is not a string"},
		{`{"a": 1, "d": 0}`, "d is not a boolean"},
		{`{"a": 1, "e": {}}`, "e is not an array"},
		{`{"a": 1, "f": [{}, 5]}`, "f is not an array of objects"},
		{`{"a": 1, "f": {}}`, "f is not an array of objects"},
		{`{"a": 1, "g": 5}`, "g: not an object"},
		{`{"a": 1, "g": {"gap": "x"}}`, "g: gap is not a whole number"},
		{`{"a": 1, "h": []}`, "h is not an object"},
		{`[1]`, "not an object"},
		{`5`, "not an object"},
		{`null`, "not an object"},
	} {
		if got := table.problem([]byte(c.data)); got != c.want {
			t.Errorf("%s: %q, want %q", c.data, got, c.want)
		}
	}
	if got := table.problem([]byte(`{nope`)); !strings.HasPrefix(got, "not JSON: ") {
		t.Fatal(got)
	}
}

func TestDecodeDataSaysWhy(t *testing.T) {
	var g GapEvent
	if err := decodeData([]byte(`{"gap": 4}`), "a gap event", gapShape, &g); err != nil || g.Gap != 4 {
		t.Fatal(g, err)
	}
	for _, c := range []struct{ data, want string }{
		{`[]`, "a gap event that does not decode: not an object"},
		{`{"gap": "x"}`, "a gap event that does not decode: gap is not a whole number"},
	} {
		err := decodeData([]byte(c.data), "a gap event", gapShape, &g)
		if e := mustCode(t, err, CodeDecode); e.Op != "decode" || e.Message != c.want || e.Err != nil {
			t.Fatalf("%s: %+v", c.data, e)
		}
	}
	// a whole number the model cannot hold: the shape fits, the model does not, and the error keeps its cause
	err := decodeData([]byte(`{"gap": 99999999999999999999}`), "a gap event", gapShape, &g)
	if e := mustCode(t, err, CodeDecode); e.Err == nil || !strings.HasPrefix(e.Message, "a gap event that does not") {
		t.Fatalf("%+v", e)
	}
	// RawEvent.Decode is the plain decode, without the shape
	if err := (RawEvent{Event: StreamEventGap, Data: []byte(`{}`)}).Decode(&g); err != nil {
		t.Fatal(err)
	}
	if err := (RawEvent{Event: StreamEventGap, Data: []byte(`5`)}).Decode(&g); !HasCode(err, CodeDecode) {
		t.Fatal(err)
	}
}

// lineKind is what parseLine makes of a line: "header <name>", the item's kind, "ignored", or "error <message>".
func lineKind(line string, first bool) string {
	item, h, err := parseLine([]byte(line), first)
	switch {
	case err != nil:
		return "error " + AsError(err).Message
	case h != nil:
		return "header " + h.Name
	case item != nil:
		return item.Kind()
	}
	return "ignored"
}

func TestParseLine(t *testing.T) {
	// the first line that is not blank: a line with recording is the header, or is skipped, never a frame
	for _, c := range []struct{ line, want string }{
		{testHeader, "header t"},
		{`{"recording":5,"cursor":1,"g":1}`, "error a header that does not decode: recording is not a string"},
		{`{"recording":"pfr1","frames":"x"}`, "error a header that does not decode: frames is not a whole number"},
		{`{"recording":"pfr1","source":5}`, "header "},
		{testFrame(1, 1, ""), "frame"},
		{`{"x":1}`, "ignored"},
	} {
		if got := lineKind(c.line, true); got != c.want {
			t.Errorf("first %s: %q, want %q", c.line, got, c.want)
		}
	}
	// the other lines, by their keys
	for _, c := range []struct{ line, want string }{
		{testHeader, "ignored"},
		{`{"recording":"pfr1","cursor":1,"g":1}`, "error a frame line that does not decode: no session"},
		{`{"sse":"gap","data":{"gap":1},"recv":5}`, "gap"},
		{`{"sse":"gap","data":{"gap":1},"recv":null}`, "gap"},
		{`{"sse":"hello","data":5}`, "ignored"},
		{`{"sse":"frame","data":{}}`, "ignored"},
		{`{"sse":5,"data":{"gap":1}}`, "error a side line whose sse is not a string"},
		{`{"sse":null}`, "error a side line whose sse is not a string"},
		{`{"sse":"gap","data":{"gap":1},"recv":"x"}`, "error a side line whose recv is not a number"},
		{`{"sse":"gap"}`, "error a gap event that does not decode: no data"},
		{`{"sse":"gap","data":null}`, "error a gap event that does not decode: not an object"},
		{`{"cursor":null,"g":1}`, "error a frame line that does not decode: no cursor"},
		{`{"cursor":1,"g":null}`, "error a frame line that does not decode: no session"},
		{`{"g":1}`, "ignored"},
		{`5`, "error a recording line that is not an object"},
		{`null`, "error a recording line that is not an object"},
		{`[1,2]`, "error a recording line that is not an object"},
		{`{nope`, "error a recording line that is not JSON"},
	} {
		if got := lineKind(c.line, false); !strings.HasPrefix(got, c.want) {
			t.Errorf("%s: %q, want %q", c.line, got, c.want)
		}
	}
	// a header's source that is a catalog entry is kept; one that is none, or does not decode, is left out
	entry := `{"id":"v","fps":5,"grid_step":1,"proxy":"p.mp4","proxy_wh":[816,770],"native_wh":[1920,1080]}`
	_, h, err := parseLine([]byte(`{"recording":"pfr1","source":`+entry+`}`), true)
	if err != nil || h.Source == nil || h.Source.ID != "v" || string(h.Raw) == "" {
		t.Fatalf("%+v %v", h, err)
	}
	for _, source := range []string{`5`, `{"id":"v","fps":5}`, strings.Replace(entry, `[816,770]`, `["a"]`, 1)} {
		if _, h, _ := parseLine([]byte(`{"recording":"pfr1","source":`+source+`}`), true); h == nil || h.Source != nil {
			t.Fatalf("%s: %+v", source, h)
		}
	}
	// only the header's table decides: a member the header cannot hold is left out, the rest kept
	odd := `{"recording":"pfr1","name":"h","frames":99999999999999999999,"g_first":3,"g_last":-1,"layers":[1],` +
		`"redacted":[2],"sessions":["a"],"complete":{"why":5},"holes_filled":"x","frames_lost":1.5,` +
		`"backfilled_layers":null,"frames_still_missing":2}`
	_, h, err = parseLine([]byte(odd), true)
	if err != nil || h.Name != "h" || h.Frames != 0 || *h.GFirst != 3 || *h.GLast != -1 || h.Layers != nil ||
		h.Redacted != nil || h.Sessions != nil || h.Complete != nil || h.HolesFilled != nil || h.FramesLost != nil ||
		h.BackfilledLayers != nil || *h.FramesStillMissing != 2 {
		t.Fatalf("%+v %v", h, err)
	}
}
