# Changelog

All notable changes of the Go SDK. Versions follow [semantic versioning](https://semver.org); the release workflow
takes a version's notes from its section here.

## 0.1.0 - Unreleased

The first release, for the API v2.2: what the 20.20 unit serves (build `claude-20261123-speckle7`: four recordings,
six plant cameras, a `look` of each stone at rest, and the speckle answers).

- **Reports, simple mode** (SDK-DESIGN.md 4.7). One `TrayReport` per tray, from `Tray.Report`,
  `Interpreter.TrayReports` (as each tray leaves a unit) and `Replay.TrayReports` (every tray of a recording): the
  stones removed, added and moved (`TrayChange`), when and where (each one's final place on the tray), and whether
  that is the tray's whole account, as text (`Describe`) and as data (`json.Marshal`, with the other languages' names). The
  tray's open account as it leaves (`Tray.Open`, `TrayOpen`, SDK-DESIGN.md 4.6.8), each fact's reason as a code
  (`TrayFact.Code`), and the tray stones' `FirstPlace`, `LeftPlace` and `Touched`, which the report reads; with the
  stones' looks, `identified` facts (`TrayIdentified`) and the tray stones' `Question`, `Looks`, `ByLook`, `LookLost`
  and `Open`.
- **The unit's speckle answers** (SDK-DESIGN.md 4.6.12, `world.speckle` since build `claude-20261123-speckle3`), read
  as a tray's: `TraySpeckle` in `Update.Speckle`, `Interpreter.Speckle` and `InterpreterCore.Speckle` (the newest
  1024) and `Result.Speckle`, each tray stone's part as `TrayStone.Speckle` (`StoneSpeckle`: `Score`, `Second`,
  `Clean`, `Found`, `Pair`, `Same`), and `DescribeSpeckleTxt`, the golden `speckle.txt`; `WorldLayer.Speckle`
  (`WorldSpeckle`) is nil when the answer does not fit by value.
- **The leaving's speckle answer in every tray report** (SDK-DESIGN.md 4.6.13, tray rules version 8). The stones that
  the answer for the tray's leaving pairs are sure, confirmed on their world stone or linked again (a stone that was not
  sure is sure again; the world stone a moved stone stood on is unidentified again): `identified` facts with the codes
  `SpeckleSame`, `SpeckleMoved` and `SpeckleFound` (`speckle_same`, `speckle_moved`, `speckle_found`), and
  `TrayStone.BySpeckle`. Each tray stone's `Evidence` as its tray leaves (`EvidenceFound`, `EvidenceNotFound` or
  `EvidenceUndecided`), and every report change's `Confidence` (`TrayChange.Confidence`, from the measured constants
  `ConfidenceRemovedCertainNotFound` to `ConfidenceAddedCertainFound` and `MovedConfidence`), at the end of each change
  line of `Describe` and in its JSON. The self-check's C3, C4 and C9 allow exactly these facts. On dry1's tray 2 (the
  fixture `dry1-0018-0153`): 11 removed, 13 added, 8 moved and 1 not sure.
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
- **Presence questions** (SDK-DESIGN.md 4.6.1). A tray stone without a look chain that is not seen at two calm moments
  while nothing could have changed it is held in question (`TrayStone.Question`, cause `unseen`; not sure, no fact) and
  is sure again once the world sees it again on its place with no hand at work, no held stone, no stone a hand took away
  and no push on the tray meanwhile, no other stone of its size within 0.12 of its place, and no look of its world stone
  that matches another stone in question or lost; otherwise its fact `TrayUnsure` comes where the question ends,
  `Opened` at its doubt. A question settled on a calm moment that does not show the stone (at once at its doubt, or by a
  sight before that calm moment) holds only if the next calm moment that shows it, or the tray's last frame when the
  tray leaves first and that frame shows it, finds nothing at work since (else a plain `TrayUnsure` fact there). A stone
  with a look chain is never held in question: its doubt is a question only its looks settle. Frames the tray rules can
  not read (a frame without a world, a jump in `G`, a `gap` or `reset` of the stream, any revision, the moving frames of
  a tray that goes on) end the questions open then; `InterpreterCore.Gap` tells the core about a `gap` or `reset` event
  (`GapItem`, `ResetItem`), and `Interpreter` and `OpenReplay` call it. A settled stone gets no `TrayUnsure` fact, one
  whose question ends gets its fact there, and the leaving counts its last snapshot's own frame only once for a stone
  nothing could have changed since its last sight (a stone not detected only there gets no `TrayUnsure` fact). The fake
  unit serves a recording's `gap` and `reset` lines in their place, as the other languages' fakes do.
- **Certain removals and continuation** (SDK-DESIGN.md 4.6.1). A lost tray stone is removed for certain only when a hand
  touched its world id (a `lift`, `hinge`, `carry`, `place`, `drop` or `gone`, a `push` with a `side`, or a `delete`
  with why `hand` or `carried-away`) from the last calm moment before its loss, or from 3.0 s before it was last seen if
  that is earlier, up to its loss; else it stays lost, and a rename or the leaving pairs it, or the leaving reports it
  removed, unclear, with the open cause `no_hand`. A visit continues the open tray only when nine tenths of its first
  frame's stones were seen on it, so a new board that carries over many of the last board's world ids is a tray of its
  own. On the four published recordings: 52 certain removals, 49 of them right by the maintainers' camera check, and 628
  trays with 532 reports.
- **The leaving's evidence** (SDK-DESIGN.md 4.6.1, Leaving). As a tray leaves, a lost tray stone is linked to a new
  world stone near it only with evidence that it is the same stone: their looks agree, or the world stone was first seen
  at or after the lost stone was last seen (less 3.0 s), no hand worked within 0.40 of either place since, and their
  looks do not disagree. Without it the lost stone is removed and the world stone added, both unclear (each the other's
  candidate in the open account), so a stone's id never passes to a stone that may be another: by the maintainers'
  camera check, every stone that left dry1's and dry2's trays is reported removed. An unidentified stone that the
  leaving point's world shows detected and not held is present as the tray leaves, at its place there (positive evidence
  at departure). So is one that world keeps there (lists it, not detected), not held, with less than 0.30 of its box
  under one other unheld stone's box there: by the maintainers' camera check, 32 of the 63 stones this adds on the four
  published recordings were there (dry1's tray 2 accounts for the 44 stones the video shows), and 25 are boxes on
  jul30's bare boards that are not there.
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
- Five tutorials (`docs/tutorials/`: start here, high level, low level, live cameras, Go), whose programs
  (`examples/tutorialreports`, `examples/tutorialhighlevel`, `examples/tutoriallowlevel`) print what the other languages'
  tutorials print over the same frames.
- Unit tests in Docker with no network, the race detector and 100 % statement coverage, counted exactly; golden tests
  over the eighteen fixture recordings; opt-in integration tests against a unit (read-only) and over the complete
  recordings of the four videos.
