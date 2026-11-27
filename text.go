package pdiggerfactory

import (
	"math"
	"strconv"
	"strings"
)

// The number formats of section 4.5 of the SDK design: identical in every SDK, so the same recording gives the
// same text everywhere. They never use the language's float formatting, which rounds ties differently. Every
// product is converted to float64 before it is added to: Go may otherwise fuse x*y + z into one fused multiply-add
// (arm64 does), which rounds once instead of twice and differs from the IEEE arithmetic of the other languages at the
// edge of a rounding step (FormatFixed(0.004999999999999999, 2) is "0.01" only without the fusion).

// FormatFixed formats x with d decimals: n = floor(x*10^d + 0.5) in IEEE doubles, then the integer n printed with a
// decimal point d places from the right (5, 1 -> "0.5"; 123, 2 -> "1.23"); a negative x prints "-" then
// FormatFixed(-x, d). NaN and infinities print "-".
func FormatFixed(x float64, d int) string {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return "-"
	}
	if x < 0 {
		return "-" + FormatFixed(-x, d)
	}
	n := math.Floor(float64(x*math.Pow(10, float64(d))) + 0.5)
	s := strconv.FormatFloat(n, 'f', 0, 64)
	if d <= 0 {
		return s
	}
	if len(s) < d+1 {
		s = strings.Repeat("0", d+1-len(s)) + s
	}
	return s[:len(s)-d] + "." + s[len(s)-d:]
}

// Round3 is floor(x*1000 + 0.5) / 1000, the rounding of the JSON summary.
func Round3(x float64) float64 {
	return math.Floor(float64(x*1000)+0.5) / 1000
}

// floorDiv and floorMod are integer division and modulo rounding towards minus infinity (Python's // and %).
func floorDiv(a, b int64) int64 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 {
	return a - floorDiv(a, b)*b
}

// FormatClock formats a media time as M:SS.d below one hour (0:05.2, 21:17.8) and H:MM:SS.d from one hour
// (1:02:03.4): n = floor(t*10 + 0.5) tenths, h = n/36000, m = (n/600)%60, s = (n/10)%60, d = n%10.
func FormatClock(t float64) string {
	if math.IsNaN(t) || math.IsInf(t, 0) {
		return "-"
	}
	n := int64(math.Floor(float64(t*10) + 0.5))
	h := floorDiv(n, 36000)
	m := floorMod(floorDiv(n, 600), 60)
	s := floorMod(floorDiv(n, 10), 60)
	d := floorMod(n, 10)
	var b strings.Builder
	if h != 0 {
		b.WriteString(strconv.FormatInt(h, 10))
		b.WriteByte(':')
		pad2(&b, m)
	} else {
		b.WriteString(strconv.FormatInt(m, 10))
	}
	b.WriteByte(':')
	pad2(&b, s)
	b.WriteByte('.')
	b.WriteString(strconv.FormatInt(d, 10))
	return b.String()
}

func pad2(b *strings.Builder, v int64) {
	if v < 10 {
		b.WriteByte('0')
	}
	b.WriteString(strconv.FormatInt(v, 10))
}

// Hand sides.
const (
	HandLeft  = "left"
	HandRight = "right"
	HandBoth  = "both"
)

// HandWord is "Left hand", "Right hand", "Both hands", or "A hand" for no (or an unknown) side.
func HandWord(side string) string {
	switch side {
	case HandLeft:
		return "Left hand"
	case HandRight:
		return "Right hand"
	case HandBoth:
		return "Both hands"
	}
	return "A hand"
}

var pluralVerbs = map[string]string{
	"pushes": "push", "lifts": "lift", "tips": "tip", "carries": "carry", "places": "place", "drops": "drop",
	"takes": "take", "moves": "move", "is": "are",
}

// verb agrees a third-person-singular verb with the hand: the plural for both hands.
func verb(side, v string) string {
	if side == HandBoth {
		return pluralVerbs[v]
	}
	return v
}

func idText(id *int64) string {
	if id == nil {
		return "?"
	}
	return strconv.FormatInt(*id, 10)
}

// joinIDs joins ids as "3", "3 and 5", "3, 5 and 9".
func idWords(ids []int64) []string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return parts
}

func joinIDs(ids []int64) string { return joinWords(idWords(ids)) }

func stoneNames(parts []string) string {
	if len(parts) <= 1 {
		return "stone " + joinWords(parts)
	}
	if len(parts) > 6 {
		return "stones " + strings.Join(parts[:6], ", ") + " and " + strconv.Itoa(len(parts)-6) + " more"
	}
	return "stones " + joinWords(parts)
}

func joinWords(parts []string) string {
	if len(parts) == 0 {
		return "?"
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}
