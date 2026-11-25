# Changelog

This SDK follows [semantic versioning](https://semver.org/). The release workflow uses the matching version section
as its release notes.

## 1.0.0 - 2026-11-25

Initial SDK for API v2.2. The reference unit ran build `claude-20261123-speckle7` with four recordings and six plant
cameras. It supplies stone appearance data and surface-pattern comparisons.

### Tray reports and tracking

- Read one `TrayReport` for each completed tray, live or from a recording. Each change carries a stone ID, a time, a
  position and a confidence score. Use `json.Marshal(report)` for the complete data.
  [Report fields](docs/interpreter.md#report-fields). Inspect `tray.Open` for unresolved identities and use reason
  codes to investigate individual facts.
- Read the unit’s `world.speckle` comparisons (build `claude-20261123-speckle3` onwards). `TraySpeckle` groups
  matches by tray. `StoneSpeckle` gives a stone’s best and second-best scores, match quality and pairing. The
  interpreter keeps the latest 1,024 comparisons.
  [Speckle fields and examples](docs/interpreter.md#the-units-speckle-check).
- Version 8 of the tray rules uses the departure comparison to confirm or recover stone IDs. It records
  `speckle_same`, `speckle_moved` or `speckle_found` as the reason. The evidence can be found, absent or undecided.
  Each change’s confidence also appears in the printed report. [Confidence scores](docs/interpreter.md#confidence).
- If a stone disappears at two calm moments, the SDK can hold its identity open. It restores the identity if the
  stone returns in place and the intervening frames rule out a swap. Hand activity, competing matches, missing
  frames or a stream reset can end the question. Stones with an appearance history follow their own matching rules.
  [Presence and identity rules](spec/SDK-DESIGN.md#461-the-tray-rules).
- A certain removal requires evidence of recent hand contact. Otherwise a lost stone stays available for matching or
  becomes an unclear removal at departure. A visit continues the same tray only if at least 90% of its initial
  stones were already seen on it. Across the four recordings, the SDK found 628 trays and produced 532 reports. A
  video check confirmed 49 of 52 certain removals.
- At departure, the SDK uses appearance, timing and nearby hand activity to decide whether a new tracker ID belongs
  to a lost stone. If the evidence is ambiguous, it records an unclear removal and addition and lists each as a
  candidate for the other. The departure rules also allow some previously tracked stones that have no bounding box
  at departure. In a video check, 32 of the 63 extra stones counted this way were present. Another 25 were false
  detections on empty jul30 boards. [Departure rules](spec/SDK-DESIGN.md#461-the-tray-rules).

### Client, replay and streams

- **High level.** `Interpreter`: follows the shared session (a recording or a plant camera) and yields typed events,
  activities (moves and push bursts), hand states, the stone inventory, per-stone tracks and a summary, each with the
  sentence the four languages share; trays with tray stones, stone ids of the SDK's own that are never reused (the
  engine's tracker renames and reuses its ids; a second stone first seen on a tray in the same tenth of a second under
  one world id gets `-2` at the end, a third `-3`), with sure and unclear identities, certain and unclear facts
  (`TrayFacts`), and each tray's arrival and leaving points to jump between (`Trays`, `Tray.Window`); `WaitFor`; a later
  loop goes on where the last one stopped, with the rest of its frame first, so nothing is missed in between; a stream
  the server ends (`Resume(false)`) ends with the final update, as a replay does; the session clock is read on every
  connect, so wall times are known from the first frame. `OpenReplay`: the same over a recording file or URL, streamed,
  each pass under its own context (cancelling it closes the connection at once), with real-time pacing. Sources and the
  live cameras (`Sources`, `Cameras`, `Live`; a `Source` with `Hold`, `Station` and `Name()`); watching a camera:
  `Session.Watch` (a time for a camera refused locally), `Session.UntilLive` through its `connecting` and `warming`
  phases (`State.Live`, `State.LivePhase()`, `live_timeout`), and `Session.Picture`, the JPEG a live frame was
  interpreted from; the shared session's controls with dry runs (`Seek` in a named video with `InVideo`; `Focus` as
  picked in a frame, with `PickedAt` and the stale-session guard `InSession`), and the session clock.
- **Mid level.** Frame streams over SSE that resume with `Last-Event-ID`, wait the server's `retry_ms` or
  `Retry-After` before reconnecting, count `limit` and `max_s` themselves and close a finished connection at once
  (the 20.20 unit closes it only about 10 s later), backfill late worlds in order (the read timeouts on the client's
  clock), flag revisions and drop duplicates; the long-poll; raw packets; `Chan` for `select`-based code; the SSE and
  NDJSON parsers. Render-ready 2-D and 3-D draw lists
  (`RenderOverlay2D`, `RenderScene3D`).
- **Low level.** One method for each of the 32 operations of the OpenAPI document (`GetFramePicture` among them),
  typed models of every schema
  (every field of the recorded replies, stream events and recordings has a Go field), one error type with stable codes
  (`RetryDelay` gives the server's `retry_ms` or `Retry-After`), retries with backoff (a GET whose body is cut is
  tried again); media byte ranges (`Bytes`, open-ended and suffix ranges); the `codec` package (maps, bitsets, depth,
  surfaces, joints, stone tuples, coordinates).
- **Damaged input.** A stream event that does not decode (the fields of SDK-DESIGN.md 2.3 rule 9 missing, null or of
  the wrong type) is yielded as a `decode` error (`Op` "decode") and the stream goes on (its id still counts, an
  interpreter loop still ends with the final update, and `WaitFor` waits on; an unnamed SSE event is no frame, as in
  the other languages); a replay skips the lines a reader does not understand (not JSON, a header, frame or side line that
  does not decode; bytes that are not UTF-8 read as U+FFFD) and counts those that do not decode (`Skipped`), its
  header is the first line that is not blank when that decodes by its table (a member the header cannot hold is left
  out of it, as is a `source` that is no catalog entry), and a read error (a gzip file cut short, a dropped download)
  ends it with that error and no final update. A backoff with a `Base` of 0 waits 0 at every attempt.

### Testing and documentation

- **Testing.** `factorytest`: a fake unit that answers the whole API from a recording and recorded replies (a reply
  given by key wins over the recording), in process or on a port, with fault injection (including the unit's late
  close of finished streams) and a manual clock with timers; `WithSampleRecording` serves a real recording embedded
  in the package (aug28, from a replay of the 20.20 unit's model and build on a Mac), so tests need no file. Its
  session state is the recording's clock line in force where playback is (the first at the start, each later one,
  such as the unit's own rate, once a stream or a long poll has passed it), as on the unit, so an `Interpreter` that
  reads the state on connect stamps the wall times a replay of the same recording gives.
  `WithLiveCamera` offers a live camera, `cam-live` (`LiveCamera`), that connects, warms up and goes live on the
  fake's wall clock, which `WithClock` links to the client's. `tools/fakeunit` serves the fake unit on the real clock
  for trying programs.
- **Docs.** The README, guides for each level, testing and the unit's measured behaviour, runnable examples (each
  tested against the fake unit, every Go block of the docs among them) and godoc examples with checked output.
- Five [runnable tutorials](docs/tutorials/README.md) cover tray reports, the interpreter, frame data, live cameras
  and Go conventions. The first tutorial explains a real tray report and follows the uncertain changes back to their
  evidence.
- Unit tests in Docker with no network, the race detector and 100 % statement coverage, counted exactly; golden tests
  over the eighteen fixture recordings; opt-in integration tests against a unit (read-only) and over the complete
  recordings of the four videos.
