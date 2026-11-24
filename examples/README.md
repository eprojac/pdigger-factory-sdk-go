# Examples

Every code sample of the SDK's README and docs lives here, and the tests of this module run each program against
the SDK's fake server (`factorytest`) serving the fixture recordings, so the samples always compile and print what
the docs say. `docscheck` fails when a Go block of the docs is not found, word for word, in one of these files.

| Folder | What it shows | Run it |
|---|---|---|
| `quickstart` | follow the unit's shared session and print each event (Ctrl-C stops) | `PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./quickstart` |
| `replay` | replay a published recording or a local file: every event, then the summary | `go run ./replay ../fixtures/recordings/aug28-1270-1300.ndjson.gz` |
| `interpreter` | high level: wait for a lift, follow that stone until its move ends, the hands and the summary | `PDIGGER_FACTORY_URL=… go run ./interpreter` |
| `activities` | high level: each move and push burst as one sentence when it completes, reconnects logged | `PDIGGER_FACTORY_URL=… go run ./activities` |
| `pacing` | a replay paced like a live session (at any speed), printing the hands as they change | `go run ./pacing ../fixtures/recordings/aug28-1270-1300.ndjson.gz 10` |
| `traypoints` | the trays of a recording: when each arrived and was about to leave, the stones removed for certain, dry-run seeks to the two points | `PDIGGER_FACTORY_URL=… go run ./traypoints ../fixtures/recordings/dry1-0018-0153.ndjson.gz 1` |
| `inventory` | `InterpreterCore` fed by hand, then the stone inventory of the last frame | `go run ./inventory ../fixtures/recordings/dry2-0582-0600.ndjson.gz` |
| `session` | sources, the live cameras, a dry-run select, the session clock | `PDIGGER_FACTORY_URL=… go run ./session` |
| `livecamera` | select a plant camera for the shared session, wait out its warm-up, then print its events with their wall times (this one moves the session for every viewer) | `PDIGGER_FACTORY_URL=http://factory-orin.local:8120 go run ./livecamera [serial] [events]` |
| `streams` | mid level: frames with side events, the long-poll as a channel | `PDIGGER_FACTORY_URL=… go run ./streams` |
| `rawpackets` | mid level: the model's raw packets, decoded with the codec | `PDIGGER_FACTORY_URL=… go run ./rawpackets` |
| `lowlevel` | low level: one call per operation, the codec, a scene3d draw list | `PDIGGER_FACTORY_URL=… go run ./lowlevel` |
| `tutorialhighlevel` | the high-level tutorial, offline: activities as sentences and as data, tray facts, trays, one pass with a core of your own | `go run ./tutorialhighlevel ../fixtures/recordings/dry1-0018-0153.ndjson.gz 130` |
| `tutoriallowlevel` | the low-level tutorial: one call per operation, typed replies, the codec, stones and hands drawn by hand | `PDIGGER_FACTORY_URL=… go run ./tutoriallowlevel [g]` |
| `errors` | the error type, codes, retries, dry runs and deadlines | `PDIGGER_FACTORY_URL=… go run ./errors` |
| `liftcount` | a package built on the SDK and its unit test with `factorytest` | `go test ./liftcount` |

This folder is a Go module of its own that uses the SDK in the folder above (a `replace` in `go.mod`): run the
commands from here. Nothing here changes the shared session of a unit but `livecamera`, which selects a camera for
every viewer: the controls in `session` and `errors` are dry runs. The programs that follow the session without `Observe(true)` (`quickstart`, `interpreter`, `activities`) count as
a viewer while they run, which keeps the unit's worker interpreting.

No unit at hand? `make fake-unit` in the folder above serves the fake one on http://127.0.0.1:8120, playing a
fixture recording; point `PDIGGER_FACTORY_URL` at it.

```sh
go test ./...        # runs every example against the fake server (no unit, no network)
```
