// Package extest runs the example programs against the SDK's fake server, for their tests.
package extest

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// Fixture is the path of a file of the SDK's fixtures, from an example's folder.
func Fixture(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", "fixtures"}, parts...)...)
}

// Unit serves a fake unit on a loopback port for the test and points PDIGGER_FACTORY_URL at it.
func Unit(t *testing.T, opts ...factorytest.Option) *factorytest.Fake {
	t.Helper()
	f, err := factorytest.New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PDIGGER_FACTORY_URL", f.Start())
	t.Cleanup(f.Close)
	return f
}

// Args sets os.Args for the test.
func Args(t *testing.T, args ...string) {
	t.Helper()
	old := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = old })
}

// capture points os.Stdout at a pipe and returns its reader and a function that restores os.Stdout.
func capture(t *testing.T) (*os.File, func()) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	return r, func() {
		os.Stdout = old
		_ = w.Close()
	}
}

// Stdout runs fn and returns what it printed.
func Stdout(t *testing.T, fn func()) string {
	t.Helper()
	r, restore := capture(t)
	out := make(chan []byte)
	go func() {
		b, _ := io.ReadAll(r)
		out <- b
	}()
	fn()
	restore()
	return string(<-out)
}

// Lines runs fn until it printed n lines, then calls stop and waits up to 10 s for fn to return.
func Lines(t *testing.T, fn func(), n int, stop func()) []string {
	t.Helper()
	r, restore := capture(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	sc := bufio.NewScanner(r)
	var lines []string
	for len(lines) < n && sc.Scan() {
		lines = append(lines, sc.Text())
	}
	stop()
	go func() { _, _ = io.Copy(io.Discard, r) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Error("the program did not stop")
	}
	restore()
	return lines
}

// Interrupt sends os.Interrupt to this process, as Ctrl-C does (a program that waits for it with
// signal.NotifyContext stops; the test process goes on).
func Interrupt() {
	p, _ := os.FindProcess(os.Getpid())
	_ = p.Signal(os.Interrupt)
}
