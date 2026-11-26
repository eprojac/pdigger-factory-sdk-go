# The low level

Use `client.API()` to request a specific frame, fetch a model output or control the shared session. The 32 typed
methods correspond to the [OpenAPI operations](../spec/openapi.yaml). The codec decodes packed maps, bitsets, depth
and joint data.

```go
api := client.API()
root, err := api.GetAPIRoot(ctx)
if err != nil {
	log.Fatal(err)
}
fmt.Printf("API %s, build %s\n", root.API, root.Build)

// The newest world, then the model's raw output of the same frame (the very newest frame may still be waiting
// for its world).
world, err := api.GetWorld(ctx, nil)
if err != nil {
	log.Fatal(err)
}
frame, err := api.GetFrame(ctx, pdiggerfactory.FrameAt(world.G), &pdiggerfactory.FrameParams{
	Layers: []string{pdiggerfactory.LayerRaw}})
if err != nil {
	log.Fatal(err)
}
fmt.Printf("frame g %d at %s: %d stones\n", frame.G, pdiggerfactory.FormatClock(frame.T), len(world.Stones))

// The raw layer's maps are packed: the tray map is a zlib-compressed byte grid. A frame can come without the
// layer (one listed in frame.Missing, or a fake unit playing a recording made without it).
if frame.Raw == nil || len(frame.Raw.Maps.Grid) < 2 {
	fmt.Printf("frame g %d has no raw layer, so no tray map\n", frame.G)
} else {
	maps := frame.Raw.Maps
	tray, err := codec.DecodeZlibMap(maps.Tray, int(maps.Grid[0]), int(maps.Grid[1]))
	if err != nil {
		log.Fatal(err)
	}
	cells := 0
	for _, v := range tray.Data {
		if v > 127 {
			cells++
		}
	}
	fmt.Printf("tray map %dx%d: %d of %d cells are tray\n", tray.Rows, tray.Cols, cells, len(tray.Data))
}

// A render-ready 3-D draw list of the same frame, with the compact surface.
scene, err := api.RenderScene3D(ctx, &pdiggerfactory.Scene3DParams{
	RenderParams: pdiggerfactory.RenderParams{G: pdiggerfactory.Int64(frame.G)}, Surface: "compact"})
if err != nil {
	log.Fatal(err)
}
flat, err := codec.DecodeBitset(scene.Surface.Flat, scene.Surface.Rows*scene.Surface.Cols)
if err != nil {
	log.Fatal(err)
}
n := 0
for _, f := range flat {
	if f {
		n++
	}
}
fmt.Printf("scene3d: %d stones, surface %dx%d with %d flat cells\n", len(scene.Stones), scene.Surface.Rows,
	scene.Surface.Cols, n)
```

(from [examples/lowlevel](../examples/lowlevel/main.go); against the fake unit with the fixture recording
dry1-0296-0306-full and the recorded replies it prints `API 2.2.0, build claude-20261122-hr4x1`,
`frame g 1530 at 5:05.1: 41 stones`, `tray map 40x64: 727 of 2560 cells are tray` and
`scene3d: 42 stones, surface 40x64 with 731 flat cells`)

## The operations

| Operation | Method | Returns |
|---|---|---|
| `GET /api/v2` getApiRoot | `GetAPIRoot(ctx)` | `*APIRoot`: API version, build, boot, the layers and renders it serves, its limits |
| `GET /api/v2/openapi.yaml` getOpenApi | `GetOpenAPI(ctx)` | the unit's OpenAPI document, bytes |
| `GET /api/v2/model` getModel | `GetModel(ctx)` | `*ModelInfo`: the model, its engines and profiles |
| `GET /api/v2/topology` getTopology | `GetTopology(ctx)` | `*Topology`: joints, bones, wrists and tips (helpers `BonePairs()`, `WristJoint(side)`, `TipJoints(side)`, `HandOfJoint(j)`, `IsNever(j)`) |
| `GET /api/v2/videos` listVideos | `ListVideos(ctx, *ListVideosParams)` | `*VideoList` (`IfNoneMatch`: a 304 sets `NotModified`) |
| `GET /api/v2/videos/{id}` getVideo | `GetVideo(ctx, id)` | `*Source` |
| `GET /api/v2/videos/{id}/media` getVideoMedia | `GetVideoMedia(ctx, id, *MediaParams)`; `HeadVideoMedia`; `OpenVideoMedia` (a body read as it arrives) | `*Media`: status, content range, ETag, bytes (a camera has none: 404 `not_found`) |
| `GET /api/v2/session` getSession | `GetSession(ctx)` | `*SessionReply`: the state, the worker, the viewers, the Stable sim, `server_now` and `server_recv` |
| `POST /api/v2/session/select` sessionSelect | `SessionSelect(ctx, SelectRequest, *ControlParams)` | `*ControlReply` |
| `POST /api/v2/session/seek` sessionSeek | `SessionSeek(ctx, SeekRequest, *ControlParams)` | `*ControlReply` |
| `POST /api/v2/session/play` sessionPlay | `SessionPlay(ctx, *ControlParams)` | `*ControlReply` |
| `POST /api/v2/session/pause` sessionPause | `SessionPause(ctx, *ControlParams)` | `*ControlReply` |
| `GET /api/v2/session/focus` getFocus | `GetFocus(ctx)` | `*FocusReply` |
| `PUT /api/v2/session/focus` putFocus | `PutFocus(ctx, PutFocusParams)` | `*ControlReply` |
| `DELETE /api/v2/session/focus` deleteFocus | `DeleteFocus(ctx, *ControlParams)` | `*ControlReply` |
| `GET /api/v2/session/frames/stream` streamFrames | `StreamFrames(ctx, *StreamFramesParams)` | `iter.Seq2[RawEvent, error]`, one connection |
| `GET /api/v2/session/frames` pollFrames | `PollFrames(ctx, *PollParams)` | `*LiveReply`: one long-poll |
| `GET /api/v2/session/frames/{g}` getFrame | `GetFrame(ctx, FrameAt(g) or LatestFrame(), *FrameParams)` | `*Frame` with the layers asked for |
| `GET /api/v2/session/frames/{g}/image.jpg` getFramePicture | `GetFramePicture(ctx, FrameAt(g) or LatestFrame(), *PictureParams)` | `*Picture`: a live camera frame's own JPEG and its `G` (from `X-Interp-G`); a recording has none (404 `not_found`) |
| `GET /api/v2/session/frames/{g}/{raster}.png` getFrameRaster | `GetFrameRaster(ctx, g, raster, *RasterParams)` | PNG bytes of `regions`, `mask` or `depth` |
| `GET /api/v2/session/world` getWorld | `GetWorld(ctx, *WorldParams)` | `*WorldLayer`: the newest world |
| `GET /api/v2/session/world/events` getWorldEvents | `GetWorldEvents(ctx, *WorldEventsParams)` | `*EventLog`; continue with `After = NextAfter` |
| `GET /api/v2/session/world/stones/{id}` getStone | `GetStone(ctx, id)` | `*StoneTrack`: the stone and its 2-D and 3-D paths |
| `GET /api/v2/session/render/overlay2d` renderOverlay2d | `RenderOverlay2D(ctx, *RenderParams)` | `*Overlay2D`: a 2-D draw list for the video |
| `GET /api/v2/session/render/scene3d` renderScene3d | `RenderScene3D(ctx, *Scene3DParams)` | `*Scene3D`: a 3-D draw list (stones, skeleton, board, surface) |
| `GET /api/v2/raw/stream` rawStream | `RawStream(ctx, *RawStreamParams)` | `iter.Seq2[RawEvent, error]`, one connection |
| `GET /api/v2/raw/packets/{g}` rawPacket | `RawPacket(ctx, FrameRef, *RawPacketParams)` | `*Sip1Packet` |
| `GET /api/v2/raw/tensors/{g}` rawTensors | `RawTensors(ctx, g)` | npz bytes (`debug_disabled` when the unit has debug off) |
| `GET /api/v2/health` getHealth | `GetHealth(ctx)` | `*Health` |
| `GET /api/v2/ready` getReady | `GetReady(ctx)` | `*Ready` (a not-ready unit answers 503 with `Waiting`) |
| `GET /api/v2/status` getStatus | `GetStatus(ctx)` | `*Status`: build, boot, uptime, the worker, the retained window, the hosts, the viewers and streams |
| `GET /metrics` getMetrics | `GetMetrics(ctx)` | `*Metrics`: the text and its samples (`Find(name, labels)`) |

A render takes exactly one of `G` and `T`; a frame, a picture, a raw packet or a render can be guarded with `Session`,
which answers `410 session_gone` when the session changed (a raster cannot). Every control takes
`ControlParams{DryRun: true}` to check it without changing the shared session.

A media request takes one byte range, `MediaParams.Range`: `pdiggerfactory.Bytes(0, 1023)` is `bytes=0-1023`,
`&pdiggerfactory.ByteRange{Start: 1024}` reads from byte 1024 to the end and `&pdiggerfactory.ByteRange{Suffix: 512}`
is the last 512 bytes; a 206 reply comes with its `ContentRange` parsed.

The API's frozen legacy surface (`/api/live`, `/api/control`, `/api/health`, `/catalog.json`, `/videos/*.mp4`) is not
wrapped.

**Models.** Every schema of the spec has a Go type in the root package. Integers of the spec (`g`, `cursor`,
`session`, ids) are `int64`; fields that may be absent or null are pointers or `omitempty`. Unknown fields are ignored,
so a newer unit does not break an older SDK. Fields that the public recordings redact (face joints, appearance masks,
depth textures) are optional, and the codec returns nil for an absent map.

## Errors and retries

Every method returns `*pdiggerfactory.Error` (wrapped where there is a cause) with:

| Field | What |
|---|---|
| `Status` | the HTTP status; 0 when there was no reply |
| `Code` | the stable code: one of the API's (`not_retained`, `session_gone`, `time_out_of_range`, `too_many_requests`, `busy`, ...) or the SDK's own: `transport` (no reply, or one that broke off), `decode` (a reply that is not the documented JSON, a stream line over 64 MiB, or, with `Op` "decode", a stream event that does not decode, after which the stream goes on), `live_unavailable` (no live camera), `live_timeout` (`Session().UntilLive` waited in vain), `stream_closed` (a stream gave up after `MaxReconnects`) |
| `Message`, `Field` | the server's message and the parameter or body field at fault |
| `RetryMS`, `Retained`, `Waiting` | the server's retry hint, the retained window (`not_retained`), what a not-ready unit waits for; `RetryDelay()` is the hint as a duration: `retry_ms`, else the reply's `Retry-After` header |
| `Op` | the operation that failed |

Branch on `Code` (`pdiggerfactory.HasCode(err, code)`), never on the message. `IsRetryable(err)` says whether trying
again later can succeed; `IsNotRetained` and `IsSessionGone` name the two errors a frame reader meets most.

GET and HEAD are retried on connection errors and on 429, 502, 503 and 504, waiting the server's `retry_ms`, else its
`Retry-After`, else the backoff (250 ms, doubling, at most 5 s, with full jitter). A control (POST, PUT, DELETE) is
retried only on 429: the unit checks its rate limits before it applies anything, so a 429 is never half-applied. Other
4xx replies are never retried. The context's cancellation and deadline end retries at once and come back as the
context's errors.

```go
if _, err := pdiggerfactory.New("factory-orin.local:8120"); err != nil {
	fmt.Println("a base URL needs its scheme:", pdiggerfactory.AsError(err).Code)
}
client, err := pdiggerfactory.New(os.Getenv("PDIGGER_FACTORY_URL"), pdiggerfactory.WithRetries(2))
if err != nil {
	log.Fatal(err)
}
```

(from [examples/errors](../examples/errors/main.go))

## Client options

| Option | Default | What |
|---|---|---|
| `WithTimeout(d)` | 10 s | the timeout of one request; for a stream, of its reply headers only |
| `WithConnectTimeout(d)` | 5 s | the connect timeout |
| `WithRetries(n)` | 3 | how many times a failed request is retried (0: never) |
| `WithBackoff(b)` | `DefaultBackoff()`: 250 ms × 2, at most 5 s | the backoff between retries and reconnects |
| `WithRand(r)` | a random source | the jitter's source; a seeded one makes retries deterministic |
| `WithHeader(k, v)` | none | an extra header on every request |
| `WithBearerToken(t)` | none | `Authorization: Bearer <t>` (the 20.20 unit ignores it; the shipped API may require it) |
| `WithUserAgentSuffix(s)` | none | appended to `pdigger-factory-sdk-go/1.0.1` |
| `WithHTTPClient(hc)`, `WithTransport(rt)` | the SDK's transport | your own HTTP client or transport (`factorytest` gives one) |
| `WithObserve(bool)` | false | the default of `Observe` for the streams |
| `WithClock(clk)` | the system clock | the clock of retries, reconnects, backfill and its read timeouts, the clock read on connect, `ConnectionMaxSeconds` and the session clock (`factorytest.NewClock` for tests) |

The SDK sends no `Origin` header, so the unit's same-origin check on controls lets it through.

## The codec

The `codec` package (`github.com/eprojac/pdigger-factory-sdk-go/codec`) decodes what the replies pack:

| Function | Decodes |
|---|---|
| `DecodeU8Map(b64, rows, cols)` | an overlay map: rows × cols bytes, row-major (`*U8Map` with `At(r, c)`) |
| `DecodeZlibMap(b64, rows, cols)` | a raw-layer map: base64 of zlib |
| `DecodeBitset(b64, n)` | n booleans, least significant bit first in each byte |
| `DepthLogZ(q, lo, hi, a, b)`, `DepthZ(...)` | a depth sample: `logZ = a × (lo + q/255 × (hi − lo)) + b`, `Z = exp(logZ)` |
| `DecodeF32LE(b64)`, `DecodeU16LE(b64)`, `Triplets(...)` | surface positions (x, y, z triplets) and triangle indices |
| `JointOf(tuple)`, `Joints(list)` | a joint from `[u, v, vis, x, y, z]`, nil for a null (redacted) joint |
| `StoneItemOf(tuple)`, `StoneItems(list)` | a stone detection from `[u, v, w, h, score, X, Y, Z, w_m, h_m, yaw]` |
| `UVToPx`, `PxToUV` | content units to native pixels and back (`px = u × W − 0.5`) |
| `SceneToThree`, `Vec3`, `Quat`, `QuatToYaw` | scene to three.js axes (`[x, z, −y]`), vectors, quaternions, a yaw |

An absent map (redacted in the public recordings) decodes to nil without an error.
