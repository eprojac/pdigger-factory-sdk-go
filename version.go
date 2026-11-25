package pdiggerfactory

// Version is the SDK version.
const Version = "1.0.0"

// UserAgent is the User-Agent the client sends (an optional suffix is appended after a space).
const UserAgent = "pdigger-factory-sdk-go/" + Version

// Bool returns a pointer to b, for optional boolean fields.
func Bool(b bool) *bool { return &b }

// Int64 returns a pointer to v, for optional integer fields such as a grid frame or a cursor.
func Int64(v int64) *int64 { return &v }

// Float64 returns a pointer to v, for optional number fields such as a media time.
func Float64(v float64) *float64 { return &v }
