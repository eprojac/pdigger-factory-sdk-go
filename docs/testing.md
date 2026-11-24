# Testing

Code built on the SDK can be unit-tested without a unit, a network or a real clock: package `factorytest` is a fake
unit that answers the whole API from a recording and recorded replies. The SDK's own tests use it the same way
(how they run is in [TESTING.md](../TESTING.md)).

## A test against the fake unit

```go
func TestLifts(t *testing.T) {
	// A fake unit whose session plays the recording embedded in factorytest (aug28 from 21:10.0, one lift); the
	// client talks to it in process.
	fake, err := factorytest.New(factorytest.WithSampleRecording())
	if err != nil {
		t.Fatal(err)
	}
	client, err := pdiggerfactory.New(fake.URL(), pdiggerfactory.WithTransport(fake.Transport()),
		pdiggerfactory.WithClock(factorytest.NewClock(time.Time{})))
	if err != nil {
		t.Fatal(err)
	}
	// Faults: the first stream drops after 20 events, the next connection attempt fails with a 503.
	fake.DropAfter("streamFrames", 20)
	fake.FailNext("streamFrames", 1, factorytest.ErrorReply(503, "busy", "try again"))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	lifts, err := Lifts(ctx, client, 1)
	if err != nil {
		t.Fatal(err)
	}
	if want := "21:17.8 Right hand lifts stone 2660"; len(lifts) != 1 || lifts[0] != want {
		t.Fatalf("got %q, want %q", lifts, want)
	}
	if ops := fake.Ops(); len(ops) < 3 {
		t.Fatalf("the stream did not resume: %v", ops)
	}
}
```

(from [examples/liftcount](../examples/liftcount/liftcount_test.go), which tests the package next to it)

`WithSampleRecording()` serves a real recording embedded in the package, `factorytest.SampleRecording`
(aug28-1270-1300: 30 s of aug28, 151 frames, one move, from a replay of the 20.20 unit's model and build on a Mac), so
a test needs no file; `factorytest.SampleRecordingFile()` is its bytes, for code that reads a recording file. The other
fixtures (recordings, recorded replies) are files of the SDK's module: in a project that requires the SDK they are
under the folder that

```sh
go list -m -f '{{.Dir}}' github.com/eprojac/pdigger-factory-sdk-go
```

prints (in the module cache, read-only), for example `<dir>/fixtures/recordings/dry1-0000-0130.ndjson.gz`.

Two ways to connect a client:

- **In process:** `pdiggerfactory.New(fake.URL(), pdiggerfactory.WithTransport(fake.Transport()))`. No sockets; a
  stream is delivered as the fake writes it, and a cancelled request ends the fake's handler. `fake.Client()` is an
  `*http.Client` over the same transport, for code that takes one.
- **On a loopback port:** `fake.Start()` serves it with `httptest` and returns its URL, for code that makes its own
  client (or another process); `fake.Close()` stops it.

Give the client `WithClock(factorytest.NewClock(time.Time{}))`: its `Sleep` advances the clock at once instead of
waiting, so retries, reconnect backoff, late-layer backfill and paced replays take no real time.
`clock.Sleeps()` lists what the SDK waited for. The timers the SDK sets on the clock (`AfterFunc`: the
`ConnectionMaxSeconds` deadline of a stream, the 1 s timeouts of the backfill reads and of the clock read on connect)
fire when a `Sleep` or `clock.Advance(d)` moves it past them; `clock.Timers()` counts those still waiting. So a test
of a unit that hangs moves the clock on in the fake's delay hook, and the SDK times the read out without the test
waiting:

```go
fake.SetDelay(func(op string, r *http.Request) {
	if op == "getFrame" {
		clock.Advance(time.Second) // the backfill read's timeout: the SDK gives up on the read
		<-r.Context().Done()
	}
})
```

(from `TestFrameReadsThatHang` in [examples/liftcount](../examples/liftcount/liftcount_test.go): the 13 frames of
the fixture recording dry2-0582-0600 that came without their world are released as they came, and the interpreter
goes on)

## What the fake answers

| Option | Serves |
|---|---|
| `WithRecording(path)` | a recording (`.ndjson` or `.ndjson.gz`): the shared session plays it |
| `WithSampleRecording()` | the recording embedded in the package (`SampleRecording`, aug28-1270-1300); of it and `WithRecording`, the last given wins |
| `WithLines(lines...)`, `WithFrames(frames...)` | a recording given as JSON lines, or as frame values (a `Frame`, a map, `json.RawMessage`) |
| `WithReplies(dir)` | recorded replies in the `fixtures/api` layout (`INDEX.json` and its files); with a recording, only what the recording does not answer |
| `WithReplyMap(map)` | replies by operation id, or `operationId-variant` (`getFrame-404`); these answer even where a recording would |
| `WithLiveCamera()` | a live camera, `cam-live` (`LiveCamera`), listed after the recording as the 20.20 unit lists its plant cameras, and shown by the session at the start, already live (see below) |
| `WithClock(clock)` | a wall clock that follows `clock` (anything with `Now() time.Time`) from where the fake's starts: the client's `factorytest.Clock`, so the client's sleeps move the fake's time (and a camera's phases), or `pdiggerfactory.SystemClock{}` for real time |
| `WithBoot(boot)` | the boot id of event ids (`<boot>-<cursor>`, default `fakeboot01`) |

What answers a GET, first match first:

1. `FailNext` and `ReplyNext`, for the requests they are given for;
2. a reply given with `WithReplyMap`: the key `operationId` answers every request of the operation, a 2xx
   `operationId-variant` the requests that name the variant (`getFrame-latest` answers `GET frames/latest`); error
   variants answer only through `ReplyNext`;
3. the recording: the fake makes up the catalog (from the header's source), the session state (the recording's
   clock line in force where playback is, then the controls), `frames/{g}`, raw packets, the world, its events, stone
   tracks and the focus;
4. the recorded replies of `WithReplies`: the other GET operations (the model, the topology, the render draw lists,
   the metrics, ...), and everything when there is no recording. With a recording they never answer what it does:
   the frames, world and session of `fixtures/api` come from another run of the unit and would contradict the
   recording's;
5. the fake's own answers (health, readiness, status, the API root).

The frame streams (SSE and NDJSON, with the recording's clock, worker, gap and reset lines as side events in their
place; NDJSON carries the gap lines only) and the long-poll come from the recording when there is one, else from
recorded replies. They play the recording as fast as they read it, and the session state follows: `GET /session`,
the long-poll replies and the controls start from the recording's first clock line, and each later one (the unit's
own rate, a pause, a seek) is the state once a stream (NDJSON too) or a long poll has passed it, as on the unit, whose
`clock` events say when its state changed. A long-poll reply ends before a clock line not passed yet, so its state is
the one in force for its frames. After a control the state is the fake's own.
Controls are checked like the unit checks them (`Origin`, body,
fields, ranges; `dry_run` honoured) and applied: a select or a seek starts a new fake session that replays the
recording from that time, with the session id one higher. The checks come in the unit's order: the video, a camera,
then the time (`"time": null` is a time of the wrong type). A seek whose video is a camera (the one it names, else the
one shown) is refused with `409 not_seekable` (field `video`); a seek naming a recording is applied, and leaves the
camera, as on the unit.

The fake does not simulate time: a long-poll answers at once with the next frames (after a camera's phases due
within its `wait`), a stream stays open after the
recording's last frame, `max_s` counts five frames a second, and there are no rate limits. A stream that reaches its
`limit` or `max_s` ends at once, unless `fake.HoldFinishedStreams(true)`: then it sends nothing more but stays open
until the client closes it, as the 20.20 unit does for about 10 s. Its wall clock stands still unless `Advance` moves
it or `WithClock` links it to a clock: playback passing a clock line of the recording changes the state, not the
clock.

**The live camera** (`WithLiveCamera()`) sends the recording's frames as a camera session does: `g` from 0 (`i` the
same, and the raw packet's `g`, `i`, `t` and `cursor` the frame's), `t` the arrival in seconds since the session started
(from `ConnectSeconds`, 0.5), its sessions end to end. A select of it starts a session as the unit does: `connecting`
for `ConnectSeconds`, then `warming` while the interpreter runs on the first `hold` (3) seconds of frames, which are
never released, then `live` (`LiveLagSeconds`, 0.25, behind the camera), each phase a new `revision` and a `clock`
event. The phases follow the fake's wall clock while the session plays; nothing runs while it is paused (a pause keeps
the phase). Play goes back to `connecting` in its own reply and plans the phases again from then (warming changes only
the phase, as on the unit: its `lag` stays the live one); on a camera that was live, the camera's next frames come after
a break (`g` jumps by 5) and after the frames it sent already (the fake's camera sends its whole run at once when it
goes live), and their warm-up is never released. With the clock standing still (no `WithClock`), a stream that waits for
the next phase moves the fake's clock on to it, and so does a long poll for the phases within its `wait`; a linked clock
is never moved: a stream waits until it has passed the phase (`SystemClock` in real time, `factorytest.Clock` when the
client sleeps or the test advances it). `Session().UntilLive` sleeps on the client's clock: give the fake that clock
with `WithClock`, or the wait runs into its timeout. `fake.LivePhase()` is the phase now. The newest `KeepPictures` (64)
released frames have a picture (`frames/{g}/image.jpg`: a small grey JPEG with `FakeFactory <video> g <g>` in a comment
segment); an older frame's is `404 not_retained`, without a `retained` window, as on the unit. The camera has no media
(404 `not_found`), and a select of it with any time, null too, is refused (400 `not_seekable`, field `time`). Play
continues at the live edge (`now - since - lag`); a recording selected after the camera plays at 0.8. Unlike the unit,
the fake interprets whether anyone watches or not, and its camera never stops: a session goes back to `connecting` only
on a play.

## Faults and inspection

| Method | What |
|---|---|
| `FailNext(op, n, reply)` | the next n requests of an operation get this reply (`ErrorReply(503, "busy", "...")`, `JSONReply(status, v)`) |
| `ReplyNext(op, n, key)` | the next n requests of an operation get a recorded reply (`getFrame-404`) |
| `DropAfter(op, k)` | the next stream of `streamFrames` or `rawStream` drops its connection after k events |
| `HoldFinishedStreams(on)` | a stream that reached its `limit` or `max_s` stays open, silent, until the client closes it (as on 20.20) |
| `InjectGap(n)`, `InjectReset()` | the next stream starts with a `gap` of n items, or a `reset` (another boot) |
| `SetDelay(fn)` | a hook called before every answer, with the operation and the request; it may block |
| `Requests()`, `Ops()` | the requests answered so far (operation, method, path, query, headers, body), or just their operations |
| `SessionState()`, `LivePhase()`, `Now()`, `Advance(s)` | the fake's shared session, the phase of its camera session, its wall clock, moving the clock on |

A request the client cancels before the fake gets to it is not answered, recorded or counted against a fault.

## Trying programs against the fake unit

`tools/fakeunit` serves the fake unit on a local port, on the real clock, for running the examples or your own program
without a unit (with `-live`, its camera connects and warms up as a unit's does):

```sh
cd tools && go run ./fakeunit                    # http://127.0.0.1:8120, playing fixtures/recordings/aug28-1270-1300
cd tools && go run ./fakeunit -live -recording ../fixtures/recordings/dry1-0000-0130.ndjson.gz
cd examples && PDIGGER_FACTORY_URL=http://127.0.0.1:8120 go run ./quickstart
cd examples && PDIGGER_FACTORY_URL=http://127.0.0.1:8120 go run ./livecamera 4
```

## Test data

[fixtures/](../fixtures) is real data, redacted, of the 20.20 unit (the replies, the single frames and the dry1 and
dry2 excerpts) and of a replay of its model and build on a Mac (the aug28 excerpts); each recording's header says
which:

- `recordings/`: eighteen recordings in the recording format. Nine are excerpts: `dry1-0000-0130`, `dry1-0018-0153`,
  `dry1-0296-0306-full` and `dry2-pause-seek` recorded on the unit with build `claude-20261122-hr4x1`;
  `joined-mid-move` and `session-change-mid-move` composed from unit recordings (the second joins a dry1 excerpt to
  `dry2-0582-0600`); `dry2-0582-0600` kept from build `claude-20261114-int17` on purpose, for its 13 frames without a
  world, which cannot be made again; `aug28-1270-1300` and `aug28-1276-1284-full` from the replay on a Mac. Nine are
  synthetic, and say so in their headers (`revisions-synthetic`, `trays-synthetic` for the tray rules the real excerpts
  never reach,
  `trays-looks-synthetic` with its copy without looks, `trays-looks-synthetic-nolooks`, and the presence questions of
  SDK-DESIGN 4.6.1, `trays-presence-synthetic`, `trays-presence-looks-synthetic`, `trays-presence-attacks-synthetic`
  and `trays-presence-attacks-looks-synthetic`; and `trays-speckle-synthetic` for the unit's speckle answers (SDK-DESIGN
  4.6.12) and the tray rules' use of the leaving's answer (4.6.13));
- `damaged/`: synthetic recordings with a line of each kind a reader skips and an event of each type that does not
  decode (a field of 2.3 rule 9 missing, null or of the wrong type), each next to
  a JSON file of what a replay of it gives (`damaged_test.go` replays and streams them, as the tests of all four
  languages do);
- `api/`: real replies of every GET operation and real error replies;
- `frames/`: single frames with all five layers, for the codec;
- `golden/`: what the maintainers' reference interpreter makes of each recording (events, tray stones, activities,
  tray facts, the open accounts of the leavings, the summary and its text), and in `golden/complete/` the summaries
  of the four complete videos (dry1, dry2, aug28, jul30), which the integration test over the published recordings
  compares with;
- `MANIFEST.json`: every file's sha256 and where it came from.

The four complete recordings are published at https://factory.pdigger.com/developers/data/.
