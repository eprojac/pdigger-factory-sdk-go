package pdiggerfactory

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func readSSE(t *testing.T, r io.Reader) ([]RawEvent, *SSEParser) {
	t.Helper()
	p := NewSSEParser(r)
	var out []RawEvent
	for {
		ev, err := p.Next()
		if errors.Is(err, io.EOF) {
			return out, p
		}
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
}

func TestSSEParser(t *testing.T) {
	stream := "\xef\xbb\xbf: a comment\r\n" +
		"id: b-1\r\nevent: frame\r\ndata: {\"a\":\r\ndata: 1}\r\n\r\n" + // CRLF, two data lines
		"retry: 1500\rdata: {}\r\r" + // CR line ends, retry
		"event: clock\n\n" + // an event without data is not dispatched
		"id: b-2\nretry: soon\nretry: 12345678901\nretry:\nid: x\x00y\nunknown: 1\ndata\n\n" + // bad retries, NUL id
		"event: frame\ndata: {\"open\":true}\n" // still open at the end: discarded
	for _, r := range []io.Reader{strings.NewReader(stream), iotest.OneByteReader(strings.NewReader(stream))} {
		evs, p := readSSE(t, r)
		if len(evs) != 3 {
			t.Fatalf("%d events: %+v", len(evs), evs)
		}
		if evs[0].Event != "frame" || evs[0].ID != "b-1" || string(evs[0].Data) != "{\"a\":\n1}" || evs[0].Retry != 0 {
			t.Fatalf("first %+v", evs[0])
		}
		if evs[1].Event != "message" || evs[1].ID != "b-1" || string(evs[1].Data) != "{}" || evs[1].Retry != 1500 {
			t.Fatalf("second %+v", evs[1])
		}
		if evs[2].Event != "message" || evs[2].ID != "b-2" || string(evs[2].Data) != "" || evs[2].Retry != 1500 {
			t.Fatalf("third %+v", evs[2])
		}
		if p.LastEventID() != "b-2" {
			t.Fatal(p.LastEventID())
		}
	}
	// a CR as the very last byte ends the line
	evs, _ := readSSE(t, strings.NewReader("data: x\r\r"))
	if len(evs) != 1 || string(evs[0].Data) != "x" {
		t.Fatalf("%+v", evs)
	}
	// a read error
	if _, err := NewSSEParser(io.MultiReader(strings.NewReader("data: x\n"), errReader{errBoom})).Next(); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
	if n, ok := digits("123"); !ok || n != 123 {
		t.Fatal("digits")
	}
}

func TestNDJSONParser(t *testing.T) {
	in := "{\"cursor\":1}\n\n  \r\n{\"gap\":3}\n{\"gap\":3,\"x\":1}\n[1]\n{\"gap\":" + strings.Repeat(" ", 70) + "3}\n{bad\n{\"cursor\":2}"
	p := NewNDJSONParser(strings.NewReader(in))
	var kinds []string
	for {
		ev, err := p.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		kinds = append(kinds, ev.Event)
	}
	if strings.Join(kinds, ",") != "frame,gap,frame,frame,frame,frame,frame" {
		t.Fatal(kinds)
	}
	if _, err := NewNDJSONParser(errReader{errBoom}).Next(); !errors.Is(err, errBoom) {
		t.Fatal(err)
	}
}

// TestSSEParserLastEventID: an id line takes effect when its event is dispatched, so after a stream that broke
// inside an event LastEventID still names the last event delivered; an id-only block sets it without an event.
// TestLineSplitterScansOnce: a long line that arrives a byte at a time is scanned once for its end, not once a read;
// a CR at the end of a read waits for the next byte, which may be the LF of a CRLF.
func TestLineSplitterScansOnce(t *testing.T) {
	line := strings.Repeat("x", 1<<20)
	scanned := 0
	s := &lineSplitter{inspect: func(n int) { scanned += n }}
	sc := bufio.NewScanner(iotest.OneByteReader(strings.NewReader("data: " + line + "\r\nnext\rlast")))
	sc.Buffer(make([]byte, 0, 64), 4<<20)
	sc.Split(s.split)
	var got []string
	for sc.Scan() {
		got = append(got, sc.Text())
	}
	if len(got) != 3 || got[0] != "data: "+line || got[1] != "next" || got[2] != "last" || sc.Err() != nil {
		t.Fatal(len(got), sc.Err())
	}
	if total := len(line) + 20; scanned > 3*total {
		t.Fatalf("scanned %d bytes for %d", scanned, total)
	}
}

func TestSSEParserLastEventID(t *testing.T) {
	p := NewSSEParser(strings.NewReader("id: b-1\nevent: frame\ndata: {}\n\nid: b-2\nevent: frame\ndata: {\"partial"))
	ev, err := p.Next()
	if err != nil || ev.ID != "b-1" || p.LastEventID() != "b-1" {
		t.Fatal(ev, err)
	}
	if _, err := p.Next(); !errors.Is(err, io.EOF) || p.LastEventID() != "b-1" {
		t.Fatal(err, p.LastEventID())
	}
	p = NewSSEParser(strings.NewReader("id: c-3\n\nevent: frame\ndata: {}\n\n"))
	if ev, err := p.Next(); err != nil || ev.ID != "c-3" || p.LastEventID() != "c-3" {
		t.Fatal(ev, err)
	}
}

// TestStreamLimits: a line, or the data lines of one event, over the limit is a decode error (not a transport one:
// trying again gets the same event).
func TestStreamLimits(t *testing.T) {
	old := maxLine
	maxLine = 64
	defer func() { maxLine = old }()
	line := "data: " + strings.Repeat("x", 40) + "\n"
	_, err := NewSSEParser(strings.NewReader(line + line + "\n")).Next()
	if e := mustCode(t, err, CodeDecode); !strings.Contains(e.Message, "an event of the stream is larger than 64 bytes") {
		t.Fatal(e)
	}
	_, err = NewSSEParser(strings.NewReader("data: " + strings.Repeat("x", 100) + "\n\n")).Next()
	if e := mustCode(t, err, CodeDecode); !strings.Contains(e.Message, "a line of the stream") || e.IsRetryable() {
		t.Fatal(e)
	}
	_, err = NewNDJSONParser(strings.NewReader(`{"cursor":1,"pad":"` + strings.Repeat("x", 100) + `"}` + "\n")).Next()
	mustCode(t, err, CodeDecode)
}
