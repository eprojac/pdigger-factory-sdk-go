# Concepts

A factory unit follows one camera or recording at a time. Its **world** is the tracker’s current view of the stones,
hands and events. Use this guide to interpret frames, coordinate systems and changes to the shared session.
[Measurements from the 20.20 unit](BEHAVIOUR-20.20.md).

## The unit and its API

A pDigger Factory unit is a small computer over a work area (the 20.20 test unit is a Jetson Orin NX on the factory
LAN). It runs a model on one camera's frames and keeps a *world*: the stones on the board, the hands, and the events
that change them. Its HTTP API (v2, [spec/openapi.yaml](../spec/openapi.yaml)) serves that world as it happens, plus
everything the model saw on the way.

```text
http://factory-orin.local:8120/api/v2/      the API root (the SDK's base URL is http://factory-orin.local:8120)
```

`pdiggerfactory.New(baseURL)` makes a `Client`. The docs use `http://factory-orin.local:8120`; the examples and the
SDK's tests read `PDIGGER_FACTORY_URL`.

## The shared session

Every client shares one source, playhead and interpretation. Selecting a source or seeking starts a new session and
assigns a new session ID. Play and pause keep the current ID. Frames, events and activities carry that ID. When it
changes, the interpreter closes any open moves as `interrupted` and starts tracking the new session.

The controls (`Session().Watch`, `Seek`, `Pause`, `Play`, `Focus`, `ClearFocus`) change the session for every viewer.
Each one takes `DryRun()`, which asks the server to check the control and say what it would do without doing it.
`Seek` also takes `InVideo(id)`, which sends the seek for that video without reading the shared state first, and
`Focus` takes `PickedAt(g)` (the frame the stone was picked on) and `InSession(id)`, which makes the unit refuse the
highlight with `409 stale_session` when another viewer has started a new session since the stone was picked.
Today any client on the LAN can use them, and the shipped API may require a token
(`pdiggerfactory.WithBearerToken`).

**Viewers.** A stream or long-poll counts as a viewer unless it is opened with `Observe(true)`, and so does, for 15 s,
the client that sent a control (a select, a seek, a play, ..., not a dry run). The unit's worker interprets only while
someone watches, and idles 20 s after the last viewer left: with nobody watching, nothing is interpreted and the frames
of that time never exist. The `Interpreter` therefore counts as a viewer by default. An integration that must not miss anything keeps one
interpreter running; tools that only look (dashboards, the SDK's integration tests) observe.

## Sources and the live cameras

A *source* is what the session can show. The API calls sources videos: `GET /api/v2/videos` is the catalog, and
`client.Sources` returns it (`Source` is the catalog's `VideoEntry`: `ID`, `Title`, `Name()`, `Kind`, `IsLive()`,
`Seekable`, `Duration`, `Frames`, `FPS`, `Hold`, `Station`, `NativeWH`, `Camera`, ...). There are two kinds:

- **recordings** (`Kind` `recording`): a video file with a `Duration` and a `DefaultStart`. The session plays it at the
  unit's rate: 0.8 on build `claude-20261114-int17` (the worker keeps up at 0.8, not at 1.0, on the full-frame videos),
  while build `claude-20261122-hr4x1` sets the rate itself to what its worker keeps up with (`changed_by` `auto-rate`):
  dry1 played at 0.75, but for about a minute at 0.63; the dry2 sessions of the fixtures recorded on it play at 0.75;
  and `dry1-0000-0130` goes from 0.8 to 0.74, then 0.75, in its first 23 s. You can seek in it. When it ends, the
  session moves on to the next recording of the catalog (cameras are skipped; after the last recording, the first).
- **live cameras** (`Kind` `camera`, `Live` true, `Seekable` false), listed after the recordings by their serials
  (`CAM00000001`): the camera itself, at rate 1.0, with no duration, no frame count and no media. `Hold` is the
  warm-up: the seconds of camera frames the interpreter runs on before the first is shown (3), and `Station` the plant
  station of the camera's rig. The 20.20 unit (build `claude-20261122-hr4x1`) lists the six cameras of the plant rigs.
  On build `claude-20261120-looks1` they streamed at 1.6 to 3.9 frames a second; build `claude-20261122-hr4x1`
  interprets about 3.6 a second on a camera (274 ms a frame, 35 ms of it decoding the JPEG), live about 0.5 s behind
  the camera. `client.Cameras` returns the cameras, and `client.Live` the first (or an error with the code
  `live_unavailable` from a build that lists none).

```go
sources, err := client.Sources(ctx)
if err != nil {
	log.Fatal(err)
}
for _, source := range sources {
	kind := "recording"
	if source.IsLive() {
		kind = "live camera"
	}
	fmt.Printf("%-11s %s: %s\n", source.ID, kind, source.Name())
}
camera, err := client.Live(ctx) // the first camera
switch {
case pdiggerfactory.HasCode(err, pdiggerfactory.CodeLiveUnavailable):
	fmt.Println("this unit lists no camera: the shared session plays recordings")
case err != nil:
	log.Fatal(err)
default:
	state, err := client.Session().Watch(ctx, camera.ID, nil, pdiggerfactory.DryRun())
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("first live camera: %s (%s); watching it would start session %d (dry run: nothing changed)\n",
		camera.ID, camera.Station, state.Session)
}
```

(from [examples/session](../examples/session/main.go))

Call `client.Session().Watch(ctx, camera.ID, nil)` to start a camera session at media time zero and playback rate
1.0. Read `State.Live` (`Phase`, `Warmup`, `Since`, `Lag`) to follow the connection.

| Phase | What the unit is doing |
| --- | --- |
| `connecting` | Waiting for the camera’s first frame. The worker status explains a delay. |
| `warming` | Feeding the first frames to the interpreter. The unit discards these frames after warm-up. |
| `live` | Publishing each interpreted frame and its camera image. |

Each transition increments the session revision and reaches clients through long-poll or a `clock` event.
`Session().UntilLive(ctx, timeout)` polls every 250 ms. It reads once more at the deadline (`UntilLivePoll`) and
returns `live_timeout` on expiry.

`Play` starts a new connection, as does a playing camera that sends no frame for five seconds. The next frame starts
warm-up again. A pause keeps the current phase. A watch counts as a viewer for 15 seconds. Open an interpreter
stream to keep the unit processing frames after that.

Frame numbers start at zero and include warm-up. At five frames per second, the first visible frame is about 15.
Media time measures seconds since the camera started, and the playhead trails the camera by its reported lag. The
unit keeps the latest 64 camera JPEGs. Read one with `Session().Picture(ctx, pdiggerfactory.FrameAt(g))`. Use
`InSession(session)` to reject a frame from a session that has ended.

| | A recording | A live camera |
|---|---|---|
| `Source` | `Kind` `recording`, `IsLive()` false, `Seekable` true, a `Duration` | `Kind` `camera`, `IsLive()` true, `Seekable` false, no duration or frame count, a `Station` |
| Rate | the unit's: 0.8 on build `claude-20261114-int17`; build `claude-20261122-hr4x1` sets it to what its worker keeps up with (dry1 at 0.75, for about a minute 0.63) | 1.0: frames come at the camera's own rate as far as the worker keeps up (1.6 to 3.9 a second on build `claude-20261120-looks1`, which kept up; about 3.6 a second on build `claude-20261122-hr4x1`, as fast as its worker goes on a camera frame; `G` counts interpreted frames); `G` jumps only over a pause, a broken stream or a spell with nobody watching |
| Media time `T` | seconds from the start of the video | seconds since the camera session started (`Live.Since`) |
| `Watch` | at a time, or the source's default start | `connecting`, `warming`, then `live` (`UntilLive`); a time is refused (`not_seekable`, field `time`, without the select) |
| `Seek` | a new session at that time | refused with `not_seekable` (field `video`) and never sent: `Seek` first reads the shared state, which another viewer may have switched to a camera. With `InVideo` it does not read it: the unit refuses `InVideo(camera)` with `409 not_seekable`, and applies a seek in a recording, which leaves the camera for everybody |
| `Pause`, `Play` | pause the playhead, go on from it | freeze the shared view and stop the interpreter (the phase stays `live`); play goes back to `connecting`, then on at the live edge after a new warm-up (`G` jumps over the pause, for everybody) |
| End | the unit moves on to the next recording by itself | none |
| Pictures | the proxy video: an MP4 with byte ranges (`GetVideoMedia`, `OpenVideoMedia`) | no media (404 `not_found`): each frame's picture (`API().GetFramePicture`, `Session().Picture`) |

An entry without `kind`, `live` or `seekable` (the unit's recordings omit them) is a seekable recording; parsing
fills each missing field from the others (`kind` `camera` alone: live, not seekable).

On a live camera the useful timestamp of an event is its wall time (`Event.Wall`, `Activity.Start.Wall`), which the
SDK computes from the session clock.

## Frames, cursors and time

The unit interprets on a grid of about five frames per media second, whatever the video's frame rate (every 12th
frame of the 60 fps recordings, every 2nd of the 10 fps ones), numbered `g` from 0 at the start of the video. A
`Frame` carries:

- `G` and `T`: the grid index and the frame's media time in seconds;
- `Session` and `Video`: the session and the source;
- `Cursor`: the frame's position in the unit's output, increasing over a boot (across sessions). A stream event's id
  is `<boot>-<cursor>`, which is what `Last-Event-ID` resumes from;
- its layers, and `Missing`: the layers it was released without.

The unit keeps the newest 512 frames (102 s of media). A stream resumed with `Last-Event-ID` inside that window gets
every frame it missed; `GetFrame` of an older frame is answered with `404 not_retained` and the window the unit still
holds (`Error.Retained`).

**Wall time.** While a session plays, the playhead is `anchor_media + (now − max(anchor_wall, hold_until)) × rate`,
and the SDK turns media times into wall times with the newest clock state of the frame's session, the other way
round: `max(anchor_wall, hold_until) + (t − anchor_media) / rate` while it plays, and `anchor_wall + (t − anchor_media)
/ rate` while it is paused (mapped back from the pause), the wall time at which the shared playhead shows (or showed)
media time `t`, as `SessionClock.WallOf` gives it. `Event.Wall`, the state's `Wall` and every activity's `Start.Wall`
and `End.Wall` are these epoch seconds, nil until a clock state of that session is known (the states of the last 8
sessions are kept, so frames held back for a late layer keep their session's clock after a seek). `client.Session().Clock(ctx)`
returns a `SessionClock` that also corrects for the offset between the unit's clock and yours (from the request's
round trip):

```go
clock, err := client.Session().Clock(ctx)
if err != nil {
	log.Fatal(err)
}
fmt.Printf("session %d plays %s at %.1f s (playing %v)\n", clock.State.Session, clock.State.Video,
	clock.MediaTimeNow(), clock.State.Playing)
```

`MediaTimeAt(wall)` and `MediaTimeNow()` give the playhead; `WallOf(t)` is the server wall time at which the playhead
shows (or showed) media time `t` at the current rate, counted from the end of the hold while playing and back from the
pause while paused (nil only without a rate), and `LocalWallOf(t)` the same on your clock. `Progress()` is the
playhead as a fraction of a recording's duration (nil on a live camera).

## Layers

A frame has up to five layers; ask only for the ones you need (`Layers(...)`):

| Layer | What | Go type |
|---|---|---|
| `raw` | the model's packet (`sip1`): boxes, joints, stone tuples, packed maps (tray, depth, hands) | `Sip1Packet` |
| `overlay` | what to draw over the video: regions, joints, masks | `OverlayLayer` |
| `scene` | the 3-D scene: skeleton, observed surface | `SceneLayer` |
| `cards` | the hands' cards, such as "Holding a stone" | `CardsLayer` |
| `world` | the Stable sim's world (`stw1`): stones, hands, events, stats | `WorldLayer` |

The interpreter uses `world` and `cards`. A frame is released at most 300 ms after its packet with the layers that
are ready. On the 20.20 unit, on build `claude-20261114-int17` 3 to 4 % of the frames of dry1 and dry2 came without
their world (none of aug28 and jul30), and on build `claude-20261122-hr4x1` 507 of the 9,042 frames of dry1 (5.6 %; the
recordings of the other three videos made with that build were replayed on a Mac and say nothing about the unit);
every one was read back. The SDK's streams read such a world back 1.5 s later and release the frames in order
(`Backfill(true)`, the interpreter's default), so the interpreter sees every world.

**Revisions.** While the session is paused, the world of the paused frame is refined and released again with a higher
`World.Rev`. The streams pass such a frame on with `IsRevision` set, and the interpreter emits only the events it had
not emitted for that frame. A frame repeated with the same or a lower revision is dropped.

## The world, events and activities

The world's *stones* have persistent ids (the same for every viewer over a session), a family (`rect` or `dogbone`), a
shape (`model`, `provisional`, or `frozen` once final), a mode, a 2-D outline in image pixels and a 3-D solid. The two
*hands* are each `free`, `touch`, `grasp`, `hinge` or `carry`, with the stone they hold.

An *event* is what the world says happened to a stone in a frame: `new`, `confirm`, `freeze`, `push`, `lift`, `hinge`,
`carry`, `place`, `drop`, `gone`, `delete` or `split`. Most events are the world keeping its inventory (stones appear,
are confirmed, get their final shape, are removed); the others are hands pushing, lifting, carrying and putting down
stones. An *activity* is
what the SDK joins from events at the scale people talk about: a *move* (one stone taken and put somewhere) or a *push
burst* (a hand pushing stones about). [interpreter.md](interpreter.md) has the rules and every sentence.

## Coordinates and the template gauge

- **Image:** `u`, `v` in 0..1 of the source's native image; pixels are `px = u × W − 0.5`
  (`codec.UVToPx`, `codec.PxToUV`). Outlines are in native pixels.
- **Scene:** `[x, y, z]` with the board in the x-y plane and z up. three.js and other y-up renderers take
  `[x, z, −y]` (`codec.SceneToThree`).
- **Template gauge:** the camera is not calibrated, so the scene is measured in the scale of a template adult body,
  not in metres (the catalog's camera says `"gauge": "template (MHR adult), not metric"`). The SDK reports distances in
  *units* for that reason (`0.84 units`); compare them with each other, not with a tape measure.
