// Command fakeunit serves the SDK's fake unit (package factorytest) on a local port: a stand-in for a pDigger Factory
// unit that answers the whole API from a recording and the recorded replies, on the real clock (with -live its camera
// connects and warms up as a unit's does). Use it to try the examples, to develop against the API without a unit,
// and to check the integration tests (TESTING.md).
//
//	go run ./fakeunit                    # http://127.0.0.1:8120, the session plays the fixture aug28-1270-1300
//	go run ./fakeunit -live -recording ../fixtures/recordings/dry1-0000-0130.ndjson.gz
//
// and in examples/, in another terminal:
//
//	PDIGGER_FACTORY_URL=http://127.0.0.1:8120 go run ./quickstart
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

// run serves the fake unit until ctx ends.
func run(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("fakeunit", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8120", "the address to listen on")
	recording := fs.String("recording", filepath.Join("..", "fixtures", "recordings", "aug28-1270-1300.ndjson.gz"),
		"the recording the shared session plays (pfr1, .ndjson or .ndjson.gz)")
	replies := fs.String("replies", filepath.Join("..", "fixtures", "api"),
		"the recorded replies (INDEX.json and its files); empty for none")
	live := fs.Bool("live", false, "offer the live camera cam-live after the recording and show it, as the 20.20 unit "+
		"offers its plant cameras")
	if err := fs.Parse(args); err != nil {
		return err
	}
	opts := []factorytest.Option{factorytest.WithRecording(*recording), factorytest.WithClock(pdiggerfactory.SystemClock{})}
	if *replies != "" {
		opts = append(opts, factorytest.WithReplies(*replies))
	}
	if *live {
		opts = append(opts, factorytest.WithLiveCamera())
	}
	fake, err := factorytest.New(opts...)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: fake.Handler(), ReadHeaderTimeout: 10 * time.Second}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	w := bufio.NewWriter(out)
	fmt.Fprintf(w, "a fake unit on http://%s plays %s (Ctrl-C stops it)\n", ln.Addr(), filepath.Base(*recording))
	if err := w.Flush(); err != nil {
		srv.Close()
		return err
	}
	select {
	case <-ctx.Done():
		return srv.Close() // ends the open streams too: the fake's streams never end by themselves
	case err := <-served:
		return err
	}
}
