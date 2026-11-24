package main

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fixtures = filepath.Join("..", "..", "fixtures")

func TestServe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, w := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"-addr", "127.0.0.1:0", "-live", "-replies", filepath.Join(fixtures, "api"),
			"-recording", filepath.Join(fixtures, "recordings", "joined-mid-move.ndjson.gz")}, w)
	}()
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = io.Copy(io.Discard, r) }()
	base, _, ok := strings.Cut(strings.TrimPrefix(line, "a fake unit on "), " plays joined-mid-move.ndjson.gz")
	if !ok {
		t.Fatal(line)
	}
	resp, err := http.Get(base + "/api/v2/videos")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), `"default_video":"dry1"`) ||
		!strings.Contains(string(b), `"id":"cam-live"`) {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	// the session shows the camera, on the real clock
	resp, err = http.Get(base + "/api/v2/session")
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), `"video":"cam-live"`) || !strings.Contains(string(b), `"phase":"live"`) {
		t.Fatalf("%s", b)
	}
	// a stream stays open until the server stops
	stream, err := http.Get(base + "/api/v2/session/frames/stream?layers=world&observe=1")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not stop")
	}
}

func TestServeErrors(t *testing.T) {
	ctx := context.Background()
	if err := run(ctx, []string{"-nope"}, io.Discard); err == nil {
		t.Error("an unknown flag")
	}
	if err := run(ctx, []string{"-recording", "missing.ndjson"}, io.Discard); err == nil {
		t.Error("a missing recording")
	}
	rec := filepath.Join(fixtures, "recordings", "joined-mid-move.ndjson.gz")
	if err := run(ctx, []string{"-addr", "256.0.0.1:1", "-replies", "", "-recording", rec}, io.Discard); err == nil {
		t.Error("an address that cannot be listened on")
	}
	// the banner cannot be written: the server stops and the error comes back
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	r, w := io.Pipe()
	r.Close()
	if err := run(ctx, []string{"-addr", addr, "-replies", "", "-recording", rec}, w); !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("a banner that cannot be written: %v", err)
	}
}
