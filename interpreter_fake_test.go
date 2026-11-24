package pdiggerfactory_test

import (
	"context"
	"strings"
	"testing"
	"time"

	pdiggerfactory "github.com/eprojac/pdigger-factory-sdk-go"
	"github.com/eprojac/pdigger-factory-sdk-go/factorytest"
)

// TestInterpreterOverFakeMatchesGolden follows the fake's frame stream of a recording with an Interpreter: the
// events must equal the reference's, as Replay's do (backfill, revisions and the session change included).
func TestInterpreterOverFakeMatchesGolden(t *testing.T) {
	for _, name := range []string{"dry2-0582-0600", "joined-mid-move", "revisions-synthetic", "session-change-mid-move"} {
		t.Run(name, func(t *testing.T) {
			path := recordingPath(t, name)
			fake, err := factorytest.New(factorytest.WithRecording(path))
			if err != nil {
				t.Fatal(err)
			}
			c, err := pdiggerfactory.New(fake.URL(), pdiggerfactory.WithTransport(fake.Transport()),
				pdiggerfactory.WithClock(factorytest.NewClock(time.Time{})))
			if err != nil {
				t.Fatal(err)
			}
			r, err := pdiggerfactory.OpenReplay(context.Background(), path, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			want := 0
			for u, err := range r.Updates(context.Background()) {
				if err != nil {
					t.Fatal(err)
				}
				if !u.Duplicate && !u.Final {
					want++
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			in := c.Interpreter()
			var ev strings.Builder
			n := 0
			for u, err := range in.Updates(ctx) {
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range u.Events {
					ev.WriteString(e.Describe() + "\n")
				}
				if n++; n == want {
					break
				}
			}
			if got, want := ev.String(), readGolden(t, name, ".events.txt"); got != want {
				t.Errorf("events differ: %s", firstDiff(got, want))
			}
		})
	}
}
