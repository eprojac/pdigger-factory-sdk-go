package pdiggerfactory

// The topology helpers of the codec section of the SDK design, on the Topology model of getTopology.

// BonePairs are the skeleton's bones as joint index pairs (entries that are not pairs are skipped).
func (t *Topology) BonePairs() [][2]int {
	out := make([][2]int, 0, len(t.Bones))
	for _, b := range t.Bones {
		if len(b) == 2 {
			out = append(out, [2]int{int(b[0]), int(b[1])})
		}
	}
	return out
}

// WristJoint is the joint index of a hand's wrist ("left" or "right").
func (t *Topology) WristJoint(side string) (int, bool) {
	var j *int64
	switch side {
	case HandLeft:
		j = t.Wrist.Left
	case HandRight:
		j = t.Wrist.Right
	}
	if j == nil {
		return 0, false
	}
	return int(*j), true
}

// TipJoints are the finger tip joint indices of a hand ("left" or "right").
func (t *Topology) TipJoints(side string) []int {
	tips := t.Tips[side]
	out := make([]int, len(tips))
	for i, j := range tips {
		out[i] = int(j)
	}
	return out
}

// IsNever reports whether joint j is never predicted (its entries are always null).
func (t *Topology) IsNever(j int) bool {
	for _, n := range t.Never {
		if int(n) == j {
			return true
		}
	}
	return false
}

// HandOfJoint is "left" or "right" for the joints of a hand (its wrist and the bones below it), "" for the body.
func (t *Topology) HandOfJoint(j int) string {
	for _, side := range []string{HandLeft, HandRight} {
		w, ok := t.WristJoint(side)
		if !ok {
			continue
		}
		if j == w {
			return side
		}
		// the hand's bones hang off the wrist: walk child joints from it
		seen := map[int]bool{w: true}
		frontier := []int{w}
		for len(frontier) > 0 {
			next := frontier[:0:0]
			for _, p := range frontier {
				for _, b := range t.BonePairs() {
					if b[0] == p && !seen[b[1]] {
						if b[1] == j {
							return side
						}
						seen[b[1]] = true
						next = append(next, b[1])
					}
				}
			}
			frontier = next
		}
	}
	return ""
}
