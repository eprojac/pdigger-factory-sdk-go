package pdiggerfactory

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
)

// RawEvent is one event of a raw stream: a server-sent event (Event "frame", "clock", "worker", "gap", "reset"; the
// SSE default "message" when the server names none) or an NDJSON line (Event "frame", or "gap" for a {"gap": n}
// line). ID is the stream's last event id when the event was dispatched (the newest "id:" line so far; empty for
// NDJSON), Retry the last "retry:" value in ms (0 when none was sent).
type RawEvent struct {
	Event string
	ID    string
	Data  json.RawMessage
	Retry int
}

// Event names of the streams: RawEvent.Event, and StreamItem.Kind of the items made from them. (World event types,
// such as EventLift, are the Event* constants.)
const (
	StreamEventFrame  = "frame"
	StreamEventClock  = "clock"
	StreamEventWorker = "worker"
	StreamEventGap    = "gap"
	StreamEventReset  = "reset"
)

// maxLine bounds one line of a stream, and the data of one SSE event (its data lines joined); a variable so tests can
// make it small.
var maxLine = 64 << 20

// tooLarge is the error of a line or an event over maxLine.
func tooLarge(what string) *Error {
	return &Error{Code: CodeDecode, Message: what + " of the stream is larger than " + strconv.Itoa(maxLine) + " bytes"}
}

// scanError is the error of a line scanner: a decode error for a line over maxLine.
func scanError(err error) error {
	if errors.Is(err, bufio.ErrTooLong) {
		return tooLarge("a line")
	}
	return err
}

// lineSplitter splits on CRLF, LF or CR, as the SSE specification says. It scans only the bytes it has not scanned
// for a line end yet (a bufio.Scanner hands the unread bytes again, from the same first byte, until a line is taken),
// so a long line that arrives in many reads costs one pass.
type lineSplitter struct {
	scanned int         // the unread bytes before it have no line end
	inspect func(n int) // tests: called with the number of bytes each split scans
}

func (s *lineSplitter) split(data []byte, atEOF bool) (int, []byte, error) {
	from := min(s.scanned, len(data))
	if s.inspect != nil {
		s.inspect(len(data) - from)
	}
	i := bytes.IndexAny(data[from:], "\r\n")
	if i < 0 {
		if atEOF && len(data) > 0 {
			s.scanned = 0
			return len(data), data, nil
		}
		s.scanned = len(data)
		return 0, nil, nil
	}
	i += from
	switch {
	case data[i] == '\n':
		s.scanned = 0
		return i + 1, data[:i], nil
	case i+1 < len(data) && data[i+1] == '\n':
		s.scanned = 0
		return i + 2, data[:i], nil
	case i+1 < len(data) || atEOF:
		s.scanned = 0
		return i + 1, data[:i], nil
	}
	s.scanned = i // a CR at the end: the next byte may be the LF of a CRLF
	return 0, nil, nil
}

func newLineScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, min(64<<10, maxLine)), maxLine)
	sc.Split((&lineSplitter{}).split)
	return sc
}

// SSEParser parses a text/event-stream body as the HTML specification says: fields event, data (several lines
// joined with "\n"), id and retry; comment lines starting with ":"; CRLF, LF or CR line ends; a byte order mark at
// the start ignored. An "id:" line takes effect when its event is dispatched (at the empty line that ends it), and an
// event still open at the end of the stream is discarded with its id. A line, or the data of one event, over 64 MiB
// is an error with code decode.
type SSEParser struct {
	sc     *bufio.Scanner
	first  bool
	idBuf  string // the "last event ID buffer" of the specification: the newest id line
	lastID string // the id of the last event dispatched
	retry  int
}

// NewSSEParser reads events from r.
func NewSSEParser(r io.Reader) *SSEParser {
	return &SSEParser{sc: newLineScanner(r), first: true}
}

// LastEventID is the id of the last event dispatched, for Last-Event-ID: after a stream that broke inside an event,
// it still names the last event that Next returned (or an id-only block's id), so a resume gets the broken event
// again.
func (p *SSEParser) LastEventID() string { return p.lastID }

// Next returns the next event, or io.EOF at the end of the stream.
func (p *SSEParser) Next() (RawEvent, error) {
	var event string
	var data bytes.Buffer
	hasData := false
	for p.sc.Scan() {
		line := p.sc.Text()
		if p.first {
			p.first = false
			line = strings.TrimPrefix(line, "\xef\xbb\xbf")
		}
		if line == "" {
			p.lastID = p.idBuf
			if !hasData {
				event = ""
				continue
			}
			b := data.Bytes()
			b = b[:len(b)-1]
			if event == "" {
				event = "message"
			}
			return RawEvent{Event: event, ID: p.lastID, Data: append(json.RawMessage(nil), b...), Retry: p.retry}, nil
		}
		if line[0] == ':' {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if found {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			event = value
		case "data":
			if data.Len()+len(value)+1 > maxLine {
				return RawEvent{}, tooLarge("an event")
			}
			data.WriteString(value)
			data.WriteByte('\n')
			hasData = true
		case "id":
			if !strings.ContainsRune(value, 0) {
				p.idBuf = value
			}
		case "retry":
			if n, ok := digits(value); ok {
				p.retry = n
			}
		}
	}
	if err := p.sc.Err(); err != nil {
		return RawEvent{}, scanError(err)
	}
	return RawEvent{}, io.EOF
}

func digits(s string) (int, bool) {
	if s == "" || len(s) > 9 {
		return 0, false
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
		n = n*10 + int(r-'0')
	}
	return n, true
}

// NDJSONParser reads one JSON value per line; empty lines are skipped, a {"gap": n} line is a gap event.
type NDJSONParser struct {
	sc *bufio.Scanner
}

// NewNDJSONParser reads lines from r.
func NewNDJSONParser(r io.Reader) *NDJSONParser {
	return &NDJSONParser{sc: newLineScanner(r)}
}

// Next returns the next line as an event, or io.EOF at the end of the stream.
func (p *NDJSONParser) Next() (RawEvent, error) {
	for p.sc.Scan() {
		line := bytes.TrimSpace(p.sc.Bytes())
		if len(line) == 0 {
			continue
		}
		ev := RawEvent{Event: StreamEventFrame, Data: append(json.RawMessage(nil), line...)}
		if isGapLine(line) {
			ev.Event = StreamEventGap
		}
		return ev, nil
	}
	if err := p.sc.Err(); err != nil {
		return RawEvent{}, scanError(err)
	}
	return RawEvent{}, io.EOF
}

func isGapLine(line []byte) bool {
	if len(line) > 64 || !bytes.HasPrefix(line, []byte("{")) {
		return false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(line, &m) != nil || len(m) != 1 {
		return false
	}
	_, ok := m["gap"]
	return ok
}
