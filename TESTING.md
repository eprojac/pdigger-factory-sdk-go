# Testing the SDK

## The unit tests

```sh
make test         # docker build -f Dockerfile.test, then docker run --rm --network none
make test-local   # the same script with your Go toolchain (1.23 or later)
```

`make test` builds `Dockerfile.test` (Go 1.27.4 on Debian bookworm, the SDK compiled at build time) and runs
[scripts/test.sh](scripts/test.sh) in a container with no network. The script fails at the first step that fails:

1. `gofmt -l` is empty, and `go vet` passes in the three modules (the SDK, `examples/`, `tools/`);
2. the SDK's tests, with the race detector: `go test -race -covermode=atomic -coverpkg=./... ./...`;
3. every statement of the SDK is covered: [scripts/coverage-check.sh](scripts/coverage-check.sh) counts the
   profile's statements exactly and fails on one uncovered statement, listing the uncovered blocks and the functions
   below 100 % (`go tool cover -func` rounds: it prints 100.0 % with one statement in 3,500 uncovered);
4. the examples: every program of `examples/` runs against the fake unit and prints what its docs say, and
   `examples/docscheck` fails when a Go block of the README or `docs/` is not in an example, word for word, a
   relative link is broken, or a doc has a placeholder left (an upper-case token such as `JUL30_END`, or `TODO`,
   outside code; the shared copy `docs/BEHAVIOUR-20.20.md` included);
5. the tools: `tools/pictures` fails when a picture of `docs/img/` is not what the SDK prints now, and
   `tools/fakeunit` serves the fake unit on a port.

Nothing needs a unit or a network: the tests use the fixtures and the fake unit (`factorytest`), in process. Time and
randomness are injected (`factorytest.Clock`, `WithRand`), so retries, backoff, backfill, its read timeouts and pacing
run without waiting: a test of a read that hangs moves the manual clock past the read's timeout itself, so no
assertion depends on how fast the machine is. No test sleeps for more than 50 ms (the pacing example sleeps 30 ms in
all, in steps of 0.2 ms).

CI ([.github/workflows/ci.yml](.github/workflows/ci.yml)) runs the same script on Go 1.23.x and on the newest
stable Go, `make test` in Docker, the lint (gofmt, go vet, staticcheck 2026.2.2 in the three modules) and the API
reference.

## Coverage

Go measures statements. Every package of the SDK (`pdiggerfactory`, `codec`, `factorytest`, `internal/pfr`) is
measured together (`-coverpkg=./...`), and the run fails when a single statement is not covered (a block that
several test binaries report counts as covered when any of them ran it). Nothing is excluded: the SDK has no code that
cannot run in a container (no window, no GPU), so there is no exclusion list. The examples and tools are tested
programs, not measured.

## The golden tests

[golden_test.go](golden_test.go) replays every recording of `fixtures/recordings/` (nine excerpts of real recordings,
seven of the 20.20 unit's and two of a replay of its model and build on a Mac, and nine synthetic ones: revisions,
trays, trays with made-up looks and the same frames without them, four of presence questions, two of them with
made-up looks, and the unit's speckle answers) and compares the result with
`fixtures/golden/`, which the maintainers' reference interpreter made:

- `<name>.events.txt`: every event's sentence, byte for byte;
- `<name>.tray-stones.txt`: every event's tray stone, a revision's events included, byte for byte;
- `<name>.activities.txt`: every activity's sentence, byte for byte;
- `<name>.trays.txt`: every tray fact's sentence, byte for byte (from `Result.TrayFacts`, and from the facts of
  `Result.Trays`);
- `<name>.open.txt`: every leaving with its open account, byte for byte;
- `<name>.speckle.txt`: every speckle answer read, with its tray stones, byte for byte (from `Result.Speckle`);
- `<name>.summary.txt`: the summary's text, byte for byte;
- `<name>.summary.json`: the summary, compared after parsing.

The same files are the golden files of the SDK in Python, Rust and TypeScript, so all four languages print the same text.

## What else is tested

| Test files | What |
|---|---|
| `api_test.go`, `models_test.go` | every operation against the unit's recorded replies (`fixtures/api`), its error replies, parameters and validation |
| `models_strict_test.go` | every field of every recorded reply, stream event and recording line has a field in the Go models (nothing is dropped as unknown) |
| `example_test.go` | the godoc examples, run against the sample recording with their output checked |
| `transport_test.go` | retries, backoff, `retry_ms` and `Retry-After`, timeouts, headers, cancellation |
| `sse_test.go`, `streams_test.go` | the SSE parser (the last event id at dispatch, the 64 MiB limits); resume, reconnects and `Retry-After`, limits the SDK counts itself, the clock on connect, gaps, resets, revisions, duplicates, backfill and its timeouts, the long-poll, raw packets, `Chan` |
| `core_test.go`, `interpreter_test.go`, `text_test.go` | the interpreter's rules on synthetic frames; loops that follow one another; an event that does not decode (the final update after one, `WaitFor` past it, also when a second `WaitFor` resumes right before it); the sentences; numbers and clocks (with values where a fused multiply-add would round differently) |
| `trays_presence_test.go` | presence questions (SDK-DESIGN.md 4.6.1) beyond the golden files: every tray of the four presence recordings ends as its generator says; a stone held in question, a settle at once and its probation, a stone with a look chain, held stones, hand deletes, revisions, a crowd and another stone's look, the leaving's own frame; the frames the tray rules can not read (`InterpreterCore.Gap`, a replay's `gap` and `reset` lines, a live stream's `GapItem` and `ResetItem`) |
| `trays_presence_edits_test.go` | presence questions on edited copies of the fixture recordings, each for a rule no fixture tray decides alone, against the tray facts the Python SDK gives for the same copy: the probation after a sight, a last frame after the last snapshot, which stones make a crowd, which looks of another open stone count (a lost stone's, after the last sight, fresh, on 99 and 100 paired cells, of a sure stone), a hand event with both hands free, a hand not flagged untrusted, a hand mark near the stone's place now, a lift of the stone with its hand mark away from its places, the first and the last sight, a freed push at the doubt |
| `replay_test.go`, `golden_test.go` | recording files and URLs, damaged files, pacing, contexts (a stalled connection cancelled mid-read, `OpenReplay`'s context ending later), error codes; the golden outputs |
| `damaged_test.go`, `shape_test.go` | `fixtures/damaged`, the same in every language: a line of each kind a reader skips and an event of each type that does not decode (SDK-DESIGN.md section 6 and 2.3 rule 9), replayed (with `Skipped`), as headers, streamed (frames, the interpreter's updates, `WaitFor`) and as raw packets; the rule 9 checks of each kind, and the line reader |
| `session_test.go` | sources and the live cameras, the controls and dry runs, `InVideo`, `PickedAt` and `InSession` (with the unit's 409s), `UntilLive` through a camera's phases on the client's clock and its timeout, `Picture`, the session clock |
| `codec/`, `internal/pfr/`, `factorytest/` | the codec on recorded frames; the recording reader; the fake unit itself |

## The opt-in integration tests

[integration_test.go](integration_test.go) has two suites that the unit test run skips. Neither runs in Docker or
by default. `make integration` runs the ones whose variables are set, either or both (and stops when neither is),
with the local Go toolchain.

**Against a unit, read-only.** With `PDIGGER_FACTORY_URL` set:

```sh
PDIGGER_FACTORY_URL=http://factory-orin.local:8120 make integration
```

It calls every GET operation, reads a few frames of each stream (all with `observe=1`, so they never count as a
viewer and never wake an idle worker), and sends every control as a dry run only (`dry_run=1`): it never changes the
shared session. A unit whose worker is idle has no new frames to send; the stream checks then log that and pass.

**The complete recordings.** With `PDIGGER_FACTORY_RECORDINGS` set to a folder of `<video>.world.ndjson.gz` files:

```sh
PDIGGER_FACTORY_RECORDINGS=$HOME/recordings make integration
```

It replays each recording and compares its summary with `<video>.summary.json` (and its text with
`<video>.summary.txt`) in the same folder, or else with `fixtures/golden/complete/<video>.summary.json`. The
published folder https://factory.pdigger.com/developers/data/ has all of them; `recordings.json` lists the files with
their sha256.

[.github/workflows/integration.yml](.github/workflows/integration.yml) runs both weekly and by hand: the first against
the fake unit on the runner (`tools/fakeunit`, which keeps the checks themselves correct), the second over the
downloaded published recordings. A real unit is checked from a machine on its network, never from CI:
`PDIGGER_FACTORY_URL=http://<unit>:8120 make integration`.

To try the read-only suite without a unit:

```sh
make fake-unit ARGS=-live &                                   # the fake unit on http://127.0.0.1:8120
PDIGGER_FACTORY_URL=http://127.0.0.1:8120 make integration
```

## The pictures

`docs/img/quickstart-live.svg` and `docs/img/replay-quickstart.svg` are drawn by `tools/pictures` from what the SDK
prints for the fixture recordings named in their captions. After a change to the text the SDK prints, run
`make pictures`; the tools' test fails until the pictures are current.
