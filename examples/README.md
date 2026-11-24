# Examples

Run these commands from the `examples` directory. It is a Go module with a local replacement for the SDK in the
directory above. Start with `quickstartreports` to see what the SDK records for a single tray.

## Choose a source

Recording examples use the checked-in fixtures. To run a live example, set `PDIGGER_FACTORY_URL` to your unit’s
address. To use a fixture as a fake unit, run `make fake-unit` from the repository root in a second terminal and set
`PDIGGER_FACTORY_URL=http://127.0.0.1:8120` here.

The `livecamera` example selects a camera for everyone watching that unit. The `session` and `errors` examples
preview their controls with dry runs. Viewer streams keep the unit processing while they are open.

| Folder | What it shows | Run it |
|---|---|---|
| `quickstartreports` | Print the tray reports in a recording | `go run ./quickstartreports ../fixtures/recordings/dry1-0018-0153.ndjson.gz` |
| `tutorialreports` | Export a row per tray and inspect the uncertain changes | `go run ./tutorialreports ../fixtures/recordings/dry1-0018-0153.ndjson.gz` |
| `quickstart` | follow the unit's shared session and print each event (Ctrl-C stops) | `go run ./quickstart` |
| `replay` | replay a published recording or a local file: every event, then the summary | `go run ./replay ../fixtures/recordings/aug28-1270-1300.ndjson.gz` |
| `interpreter` | high level: wait for a lift, follow that stone until its move ends, the hands and the summary | `go run ./interpreter` |
| `activities` | high level: each move and push burst as one sentence when it completes, reconnects logged | `go run ./activities` |
| `pacing` | a replay paced like a live session (at any speed), printing the hands as they change | `go run ./pacing ../fixtures/recordings/aug28-1270-1300.ndjson.gz 10` |
| `traypoints` | the trays of a recording: when each arrived and was about to leave, the stones removed for certain, dry-run seeks to the two points | `go run ./traypoints ../fixtures/recordings/dry1-0018-0153.ndjson.gz 1` |
| `inventory` | `InterpreterCore` fed by hand, then the stone inventory of the last frame | `go run ./inventory ../fixtures/recordings/dry2-0582-0600.ndjson.gz` |
| `session` | sources, the live cameras, a dry-run select, the session clock | `go run ./session` |
| `livecamera` | select a plant camera for the shared session, wait out its warm-up, then print its events with their wall times (this one moves the session for every viewer) | `go run ./livecamera` |
| `streams` | mid level: frames with side events, the long-poll as a channel | `go run ./streams` |
| `rawpackets` | mid level: the model's raw packets, decoded with the codec | `go run ./rawpackets` |
| `lowlevel` | low level: one call per operation, the codec, a scene3d draw list | `go run ./lowlevel` |
| `tutorialhighlevel` | the high-level tutorial, offline: activities as sentences and as data, tray facts, trays, one pass with a core of your own | `go run ./tutorialhighlevel ../fixtures/recordings/dry1-0018-0153.ndjson.gz 130` |
| `tutoriallowlevel` | the low-level tutorial: one call per operation, typed replies, the codec, stones and hands drawn by hand | `go run ./tutoriallowlevel` |
| `errors` | the error type, codes, retries, dry runs and deadlines | `go run ./errors` |
| `liftcount` | a package built on the SDK and its unit test with `factorytest` | `go test ./liftcount` |

## Run the example tests

```sh
go test ./...
```

The tests use recorded fixtures and the fake unit. The `docscheck` package verifies the Go samples quoted in the
guides.
