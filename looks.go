package pdiggerfactory

import (
	"encoding/base64"
	"math"
)

// Looks (section 4.6.2 of the SDK design): a stone's look against another's, from exact integer sums, so that every
// SDK gives the same double. The only floating-point steps are one product under a square root and one division, which
// Go cannot fuse into a multiply-add.
const (
	lookShift  = 3    // looks are compared at whole-cell shifts of up to this many cells each way ...
	lookMin    = 24   // ... where at least this many cells (and half the smaller look's read cells) pair
	lookSquare = 0.12 // faces whose sides differ by less than this share are also compared a quarter turn round
)

// LookCells is the most cells (Nu x Nv) a look may have: one of more is not a look, so every sum stays exact.
const LookCells = 4096

// lookGrid is a look's cells as rows: values (0 where not read), read marks (1 or 0) and squared values.
type lookGrid struct{ v, m, vv [][]int64 }

// lookCells is a look the rules can read.
type lookCells struct {
	g      int64
	square bool
	n      int64
	grid   lookGrid
}

// readLook decodes a look; nil when the rules cannot read it (another version, more than LookCells cells, cells that
// are not canonical base64 of Nu x Nv bytes).
func readLook(lk *StoneLook) *lookCells {
	if lk == nil || lk.V != 1 || lk.Nu < 1 || lk.Nv < 1 || lk.Nu > LookCells || lk.Nv > LookCells ||
		lk.Nu*lk.Nv > LookCells {
		return nil
	}
	if int64(len(lk.D)) != 4*((lk.Nu*lk.Nv+2)/3) { // not the length of Nu x Nv bytes: not decoded
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(lk.D)
	if err != nil || base64.StdEncoding.EncodeToString(raw) != lk.D || int64(len(raw)) != lk.Nu*lk.Nv {
		return nil
	}
	nu, nv := int(lk.Nu), int(lk.Nv)
	c := &lookCells{g: lk.G}
	c.grid = lookGrid{v: make([][]int64, nv), m: make([][]int64, nv), vv: make([][]int64, nv)}
	for j := 0; j < nv; j++ {
		v, m, vv := make([]int64, nu), make([]int64, nu), make([]int64, nu)
		for i := 0; i < nu; i++ {
			if x := int64(int8(raw[j*nu+i])); x != -128 {
				v[i], m[i], vv[i] = x, 1, x*x
				c.n++
			}
		}
		c.grid.v[j], c.grid.m[j], c.grid.vv[j] = v, m, vv
	}
	l, s := math.Max(lk.At.L, lk.At.S), math.Min(lk.At.L, lk.At.S)
	c.square = l > 0 && (l-s)/l < lookSquare
	return c
}

// turned is the grid half a turn round, or a quarter (q 1) or three quarters (q 3) round.
func (g lookGrid) turned(q int) lookGrid {
	nv, nu := len(g.v), len(g.v[0])
	turn := func(rows [][]int64) [][]int64 {
		var out [][]int64
		switch q {
		case 2:
			out = make([][]int64, nv)
			for j := range out {
				out[j] = make([]int64, nu)
				for i := range out[j] {
					out[j][i] = rows[nv-1-j][nu-1-i]
				}
			}
		case 1:
			out = make([][]int64, nu)
			for r := range out {
				out[r] = make([]int64, nv)
				for c := range out[r] {
					out[r][c] = rows[c][nu-1-r]
				}
			}
		default:
			out = make([][]int64, nu)
			for r := range out {
				out[r] = make([]int64, nv)
				for c := range out[r] {
					out[r][c] = rows[nv-1-c][r]
				}
			}
		}
		return out
	}
	return lookGrid{v: turn(g.v), m: turn(g.m), vv: turn(g.vv)}
}

// lookSum is the six integer sums of a placement: pairs, the sums of both sides' values, of their squares and of their
// products.
type lookSum struct{ n, sa, sb, saa, sbb, sab int64 }

// lookSums are the sums of a's cells against b's, a's cell (0, 0) on b's cell (ov, ou).
func lookSums(a, b lookGrid, ov, ou int) (s lookSum) {
	nva, nua, nvb, nub := len(a.v), len(a.v[0]), len(b.v), len(b.v[0])
	i0, i1 := max(0, -ou), min(nua, nub-ou)
	for j := max(0, -ov); j < min(nva, nvb-ov); j++ {
		av, am, aa := a.v[j], a.m[j], a.vv[j]
		bv, bm, bb := b.v[j+ov], b.m[j+ov], b.vv[j+ov]
		for i := i0; i < i1; i++ {
			k := i + ou
			s.n += am[i] * bm[k]
			s.sa += av[i] * bm[k]
			s.sb += bv[k] * am[i]
			s.saa += aa[i] * bm[k]
			s.sbb += bb[k] * am[i]
			s.sab += av[i] * bv[k]
		}
	}
	return
}

// zncc is the normalised cross-correlation of the sums; false below need pairs or for a flat side.
func (s lookSum) zncc(need int64) (float64, bool) {
	if s.n < need {
		return 0, false
	}
	da, db := s.n*s.saa-s.sa*s.sa, s.n*s.sbb-s.sb*s.sb
	if da <= 0 || db <= 0 {
		return 0, false
	}
	return float64(s.n*s.sab-s.sa*s.sb) / math.Sqrt(float64(da)*float64(db)), true
}

// floorHalf is x / 2 rounded down (Go's division rounds toward zero).
func floorHalf(x int) int { return x >> 1 }

// pairCells is LookPairing of two read looks: the score and the pair count of the placement that gave it.
func pairCells(a, b *lookCells) (float64, int64, bool) {
	need := max(int64(lookMin), (min(a.n, b.n)+1)/2)
	nva, nua := len(a.grid.v), len(a.grid.v[0])
	turns := []lookGrid{b.grid, b.grid.turned(2)}
	if a.square && b.square {
		turns = append(turns, b.grid.turned(1), b.grid.turned(3))
	}
	best, bestN, found := 0.0, int64(0), false
	for _, t := range turns {
		nvb, nub := len(t.v), len(t.v[0])
		ov, ou := floorHalf(nvb-nva), floorHalf(nub-nua)
		top, tn, have, tv, tu := 0.0, int64(0), false, 0, 0
		for dv := -lookShift; dv <= lookShift; dv++ {
			for du := -lookShift; du <= lookShift; du++ {
				sums := lookSums(a.grid, t, ov+dv, ou+du)
				if z, ok := sums.zncc(need); ok && (!have || z > top) {
					top, tn, have, tv, tu = z, sums.n, true, dv, du
				}
			}
		}
		if !have {
			continue
		}
		// half-cell shifts: each cell the sum of the 2 or 4 cells of b around the half position, all read
		for hv := -1; hv <= 1; hv++ {
			for hu := -1; hu <= 1; hu++ {
				if hu == 0 && hv == 0 {
					continue
				}
				sums := lookSums(a.grid, halfGrid(t, nva, nua, ov+tv, ou+tu, hv, hu), 0, 0)
				if z, ok := sums.zncc(need); ok && z > top {
					top, tn = z, sums.n
				}
			}
		}
		if !found || top > best {
			best, bestN, found = top, tn, true
		}
	}
	return best, bestN, found
}

// usableLook is the cells of a look the tray rules use (section 4.6.3): one readLook reads, with pitch Sp 2 and scale
// Q 32, at least 3 x 3 cells, both sides of its face above 0 and at least lookMin cells read; else nil.
func usableLook(lk *StoneLook) *lookCells {
	c := readLook(lk)
	if c == nil || lk.Sp != 2 || lk.Q != 32 || lk.Nu < 3 || lk.Nv < 3 || !(lk.At.L > 0 && lk.At.S > 0) ||
		c.n < lookMin {
		return nil
	}
	return c
}

// permilleOf is a score as the integer the tray rules compare: floor(score * 1000 + 0.5), an IEEE multiply, then an
// add (the conversion keeps Go from fusing them).
func permilleOf(score float64) int64 { return int64(math.Floor(float64(score*1000) + 0.5)) }

// halfGrid is b sampled half a cell from (ov, ou) toward (hv, hu): each of nv x nu cells the sum of the 2 or 4 cells
// around, read only when all of them are.
func halfGrid(b lookGrid, nv, nu, ov, ou, hv, hu int) lookGrid {
	rows, cols := halfSpan(hv), halfSpan(hu)
	nvb, nub := len(b.v), len(b.v[0])
	h := lookGrid{v: make([][]int64, nv), m: make([][]int64, nv), vv: make([][]int64, nv)}
	for j := 0; j < nv; j++ {
		v, m, vv := make([]int64, nu), make([]int64, nu), make([]int64, nu)
		for i := 0; i < nu; i++ {
			var s int64
			ok := true
			for _, a := range rows {
				for _, c := range cols {
					y, x := j+ov+a, i+ou+c
					if y >= 0 && y < nvb && x >= 0 && x < nub && b.m[y][x] == 1 {
						s += b.v[y][x]
					} else {
						ok = false
					}
				}
			}
			if ok {
				v[i], m[i], vv[i] = s, 1, s*s
			}
		}
		h.v[j], h.m[j], h.vv[j] = v, m, vv
	}
	return h
}

func halfSpan(h int) []int {
	switch {
	case h > 0:
		return []int{0, 1}
	case h < 0:
		return []int{-1, 0}
	}
	return []int{0}
}

// LookScore says how well look a agrees with look b (section 4.6.2 of the SDK design): the best normalised
// cross-correlation of a's read cells with b's over b's turns (half a turn round, and a quarter and three quarters round
// when both faces are near square), whole-cell shifts of up to 3 cells and then half-cell shifts around each turn's
// best, from exact integer sums: the same number in every SDK. 1 is the same texture; other stones score about 0.2 to
// 0.4. ok is false when either is not a look the rules can read or no placement pairs enough cells (24, and half the
// smaller look's read cells).
func LookScore(a, b *StoneLook) (score float64, ok bool) {
	score, _, ok = LookPairing(a, b)
	return score, ok
}

// LookPairing is LookScore with n, the pair count of the placement that gave the score (section 4.6.2 of the SDK
// design); ok is false exactly when LookScore's is.
func LookPairing(a, b *StoneLook) (score float64, n int64, ok bool) {
	x, y := readLook(a), readLook(b)
	if x == nil || y == nil {
		return 0, 0, false
	}
	return pairCells(x, y)
}

// LookPermille is a score as the integer the tray rules compare (section 4.6.2): floor(score * 1000 + 0.5), from -1000
// to 1000.
func LookPermille(score float64) int64 { return permilleOf(score) }

// UsableLook says the tray rules use this look (section 4.6.3): LookScore reads it, with pitch Sp 2 and scale Q 32,
// at least 3 x 3 cells, both sides of its face above 0 and at least 24 cells read.
func UsableLook(lk *StoneLook) bool { return usableLook(lk) != nil }
