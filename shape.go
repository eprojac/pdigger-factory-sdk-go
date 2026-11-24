package pdiggerfactory

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// What every SDK checks before it takes a stream event or a recording line (SDK-DESIGN.md 2.3 rule 9, section 6):
// the data is a JSON object, a member that is null counts as missing, and each type has the fields of its table, of
// their kinds. Further inside (a stone, an event, a hand card, the other layers) the models read the data as they
// do: a member of the wrong type there makes the data not decode, a missing one reads as its zero value.

// shapeKind is the kind of a field.
type shapeKind int

const (
	// shapeInt is a whole number, written without a fraction or an exponent (Go's int64 fields read no 2.0).
	shapeInt shapeKind = iota
	// shapeNum is a number.
	shapeNum
	// shapeStr is a string.
	shapeStr
	// shapeBool is a boolean.
	shapeBool
	// shapeObj is an object.
	shapeObj
	// shapeArr is an array.
	shapeArr
	// shapeObjs is an array of objects.
	shapeObjs
	// shapeOf is an object with the fields of the field's own table.
	shapeOf
)

// String is what the kind is called in a decode error.
func (k shapeKind) String() string {
	switch k {
	case shapeInt:
		return "a whole number"
	case shapeNum:
		return "a number"
	case shapeStr:
		return "a string"
	case shapeBool:
		return "a boolean"
	case shapeArr:
		return "an array"
	case shapeObjs:
		return "an array of objects"
	}
	return "an object"
}

// shapeField is a field of a table: its name (one that ends in "?" may be missing), its kind and, for shapeOf, the
// table of its own fields.
type shapeField struct {
	name  string
	kind  shapeKind
	table shapeTable
}

// shapeTable is the fields of a type, in the order they are checked.
type shapeTable []shapeField

// worldShape is the world layer.
var worldShape = shapeTable{
	{"schema", shapeStr, nil}, {"session", shapeInt, nil}, {"video", shapeStr, nil}, {"g", shapeInt, nil},
	{"t", shapeNum, nil}, {"motion", shapeStr, nil}, {"logF", shapeNum, nil}, {"stones", shapeObjs, nil},
	{"hands", shapeObj, nil}, {"events", shapeObjs, nil}, {"stats", shapeObj, nil}, {"draw?", shapeObj, nil},
	{"focus?", shapeObj, nil}, {"rev?", shapeInt, nil},
}

// cardsShape is the cards layer.
var cardsShape = shapeTable{
	{"schema", shapeStr, nil}, {"left", shapeObj, nil}, {"right", shapeObj, nil}, {"numbers", shapeObj, nil},
	{"stage?", shapeObj, nil},
}

// packetShape is a sip1 packet: the raw layer, and an event of the raw packet stream.
var packetShape = shapeTable{
	{"schema", shapeStr, nil}, {"maps", shapeObj, nil}, {"depth", shapeObj, nil}, {"boxes", shapeObj, nil},
	{"person", shapeObj, nil}, {"stones", shapeObj, nil}, {"tray", shapeObj, nil}, {"hands", shapeObj, nil},
	{"both", shapeNum, nil}, {"session", shapeInt, nil}, {"video", shapeStr, nil}, {"g", shapeInt, nil},
	{"i", shapeInt, nil}, {"t", shapeNum, nil}, {"past_valid", shapeBool, nil}, {"dt_past", shapeNum, nil},
	{"ms", shapeObj, nil}, {"cursor?", shapeInt, nil},
}

// frameShape is a frame: a stream event's data, or a recording line.
var frameShape = shapeTable{
	{"cursor", shapeInt, nil}, {"session", shapeInt, nil}, {"video", shapeStr, nil}, {"g", shapeInt, nil},
	{"i", shapeInt, nil}, {"t", shapeNum, nil}, {"past_valid", shapeBool, nil}, {"dt_past", shapeNum, nil},
	{"missing?", shapeArr, nil}, {"raw?", shapeOf, packetShape}, {"overlay?", shapeObj, nil},
	{"scene?", shapeObj, nil}, {"cards?", shapeOf, cardsShape}, {"world?", shapeOf, worldShape},
}

// clockShape is a clock event: the shared state and server_now.
var clockShape = shapeTable{
	{"session", shapeInt, nil}, {"revision", shapeInt, nil}, {"video", shapeStr, nil}, {"playing", shapeBool, nil},
	{"rate", shapeNum, nil}, {"anchor_media", shapeNum, nil}, {"anchor_wall", shapeNum, nil},
	{"hold_until", shapeNum, nil}, {"changed_by", shapeStr, nil}, {"media_time", shapeNum, nil},
	{"server_now", shapeNum, nil}, {"duration?", shapeNum, nil}, {"focus?", shapeObj, nil},
}

// workerShape is a worker event.
var workerShape = shapeTable{
	{"state", shapeStr, nil}, {"reason", shapeStr, nil}, {"fps?", shapeNum, nil}, {"frame_ms?", shapeNum, nil},
	{"cycle_ms?", shapeNum, nil}, {"gpu?", shapeNum, nil}, {"session?", shapeInt, nil}, {"g?", shapeInt, nil},
	{"errors?", shapeInt, nil}, {"stage_ms?", shapeObj, nil}, {"engines?", shapeObj, nil},
	{"model?", shapeStr, nil}, {"video?", shapeStr, nil},
}

// gapShape is a gap event.
var gapShape = shapeTable{{"gap", shapeInt, nil}}

// resetShape is a reset event.
var resetShape = shapeTable{{"boot", shapeStr, nil}, {"cursor", shapeInt, nil}}

// headerShape is a recording's header line (a source that does not decode is left out of it, not checked here).
var headerShape = shapeTable{
	{"recording", shapeStr, nil}, {"name?", shapeStr, nil}, {"video?", shapeStr, nil}, {"build?", shapeStr, nil},
	{"api?", shapeStr, nil}, {"model?", shapeStr, nil}, {"backend?", shapeStr, nil},
	{"recorded_from?", shapeStr, nil}, {"recorded_at?", shapeStr, nil}, {"notes?", shapeStr, nil},
	{"frames?", shapeInt, nil}, {"g_first?", shapeInt, nil}, {"g_last?", shapeInt, nil},
	{"layers?", shapeArr, nil}, {"redacted?", shapeArr, nil}, {"sessions?", shapeArr, nil},
	{"complete?", shapeObj, nil},
}

// sourceShape is a header's source, a catalog entry: a source without these fields, each of its type, is left out of
// the header (spec 6).
var sourceShape = shapeTable{
	{"id", shapeStr, nil}, {"fps", shapeNum, nil}, {"grid_step", shapeInt, nil}, {"proxy", shapeStr, nil},
	{"proxy_wh", shapeArr, nil}, {"native_wh", shapeArr, nil}, {"title?", shapeStr, nil}, {"subtitle?", shapeStr, nil},
	{"duration?", shapeNum, nil}, {"frames?", shapeInt, nil}, {"default_start?", shapeNum, nil},
	{"hold?", shapeNum, nil}, {"source_wh?", shapeArr, nil}, {"crop?", shapeArr, nil}, {"profile?", shapeStr, nil},
	{"camera?", shapeObj, nil}, {"held_out?", shapeArr, nil}, {"stone_model?", shapeObj, nil},
	{"kind?", shapeStr, nil}, {"live?", shapeBool, nil}, {"seekable?", shapeBool, nil}, {"station?", shapeStr, nil},
}

// objectFields are the members of the JSON object b, each as its JSON text; why says what b is instead when it is
// not one: "not JSON: ..." (NaN and Infinity are not JSON), or "not an object" (null included).
func objectFields(b []byte) (fields map[string]json.RawMessage, why string) {
	err := json.Unmarshal(b, &fields)
	var typeErr *json.UnmarshalTypeError
	switch {
	case err == nil && fields != nil:
		return fields, ""
	case err == nil || errors.As(err, &typeErr):
		return nil, "not an object"
	}
	return nil, "not JSON: " + err.Error()
}

// member is the member name of fields, unless it is missing or null (which counts as missing).
func member(fields map[string]json.RawMessage, name string) (json.RawMessage, bool) {
	v, ok := fields[name]
	return v, ok && string(v) != "null"
}

// isNumber reports whether v, a JSON value, is a number.
func isNumber(v json.RawMessage) bool {
	return v[0] == '-' || ('0' <= v[0] && v[0] <= '9')
}

// isObjects reports whether v, a JSON value, is an array whose items are all objects.
func isObjects(v json.RawMessage) bool {
	var items []json.RawMessage
	if json.Unmarshal(v, &items) != nil {
		return false
	}
	for _, item := range items {
		if item[0] != '{' {
			return false
		}
	}
	return true
}

// misfit is "" when v, a member's JSON value (not null), is of the field's kind, else what is wrong with it.
func (f shapeField) misfit(v json.RawMessage) string {
	var fits bool
	switch f.kind {
	case shapeInt:
		fits = isNumber(v) && !bytes.ContainsAny(v, ".eE")
	case shapeNum:
		fits = isNumber(v)
	case shapeStr:
		fits = v[0] == '"'
	case shapeBool:
		fits = v[0] == 't' || v[0] == 'f'
	case shapeObj:
		fits = v[0] == '{'
	case shapeArr:
		fits = v[0] == '['
	case shapeObjs:
		fits = isObjects(v)
	case shapeOf:
		return f.table.problem(v)
	}
	if fits {
		return ""
	}
	return f.kind.String()
}

// problemOf is "" when fields have the fields of the table, each of its kind; else what is wrong with the first one
// that does not.
func (t shapeTable) problemOf(fields map[string]json.RawMessage) string {
	for _, f := range t {
		name, optional := strings.CutSuffix(f.name, "?")
		v, ok := member(fields, name)
		if !ok {
			if optional {
				continue
			}
			return "no " + name
		}
		if wrong := f.misfit(v); wrong != "" {
			if f.kind == shapeOf {
				return name + ": " + wrong
			}
			return name + " is not " + wrong
		}
	}
	return ""
}

// problem is "" when b is a JSON object with the fields of the table, each of its kind; else what is wrong.
func (t shapeTable) problem(b []byte) string {
	fields, why := objectFields(b)
	if why != "" {
		return why
	}
	return t.problemOf(fields)
}

// decodeData checks data against the table and then decodes it into out (a Frame, a ClockEvent, ...). Data that does
// not decode is a decode error (Op "decode") that names what (an event or a line) and why: a stream yields it and
// goes on after it (2.3 rule 9), a replay skips the line.
func decodeData(data []byte, what string, table shapeTable, out any) error {
	why := "no data" // a side line without data
	if len(data) > 0 {
		why = table.problem(data)
	}
	var err error
	if why == "" {
		if err = json.Unmarshal(data, out); err == nil {
			return nil
		}
		why = err.Error()
	}
	return &Error{Op: "decode", Code: CodeDecode, Message: what + " that does not decode: " + why, Err: err}
}
