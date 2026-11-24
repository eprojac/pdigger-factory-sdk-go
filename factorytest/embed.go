package factorytest

import _ "embed" // the default topology and the sample recording

// defaultTopology is the 20.20 unit's skeleton topology (fixtures/api/getTopology.json), answered when no recorded
// reply is given.
//
//go:embed topology.json
var defaultTopology []byte

// sampleRecording is a copy of the fixture recording fixtures/recordings/aug28-1270-1300.ndjson.gz (a test checks
// that the two are the same file).
//
//go:embed aug28-1270-1300.ndjson.gz
var sampleRecording []byte

// SampleRecording is the name of the recording WithSampleRecording serves, embedded in this package: the fixture
// recording aug28-1270-1300, 30 s of the video aug28 (21:10.0 to 21:40.0, frames g 6350 to 6500) as the 20.20 unit's
// model and build interpreted it in a replay on a Mac (its header's recorded_from says how): 151 frames with the world
// and cards layers, 277 events, one move (the right hand lifts, tips, carries and places stone 2660 at 21:17.8 to
// 21:21.8) and six push bursts. Its golden outputs are fixtures/golden/
// aug28-1270-1300.*. Tests outside this repository can use it without a file.
const SampleRecording = "aug28-1270-1300"

// SampleRecordingFile is the sample recording as a file's bytes (gzip NDJSON in the pfr1 format), for code that
// reads recordings: write it to a file, or serve it, and open it with pdiggerfactory.OpenReplay.
func SampleRecordingFile() []byte { return append([]byte(nil), sampleRecording...) }
