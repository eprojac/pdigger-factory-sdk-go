package pdiggerfactory

// A positive sight just after the leaving point can establish occupancy without establishing origin.
// Keep the actual observation while the frozen track remains in place and the neighbourhood stays clear.
type boundarySight struct {
	calmG      int64
	t          float64
	anchor     *StoneView
	latest     *StoneView
	neighbours map[int64]bool
}

// Also count neighbours that the world still tracks without a current detection.
func boundaryNeighbours(stone *StoneView, world *trayWorld) map[int64]bool {
	near := make(map[int64]bool)
	size, place := sizeOf(stone), placeOf(stone)
	for _, other := range world.stones {
		if other.ID != stone.ID && !inHand(other) && compatible(sizeOf(other), size) && dist(placeOf(other), place) <= renameD {
			near[other.ID] = true
		}
	}
	return near
}

func boundaryUncontested(stone *StoneView, world *trayWorld, previous map[int64]bool) bool {
	for id := range boundaryNeighbours(stone, world) {
		if !previous[id] {
			return false
		}
	}
	return overlapShare(stone, world.stones) < overlapMax
}

func (b *boundarySight) stillValid(stone *StoneView, world *trayWorld, calm *TrayPoint) bool {
	elapsed := world.ref.T - b.t
	if !world.quietHands || calm == nil || calm.G != b.calmG || elapsed < 0 || elapsed > renameT || stone.Shape != "frozen" ||
		!compatible(sizeOf(stone), sizeOf(b.anchor)) || dist(placeOf(stone), placeOf(b.anchor)) > renameD {
		return false
	}
	for _, e := range world.w.Events {
		if e.ID != nil && *e.ID == stone.ID && e.Type != EventConfirm && e.Type != EventFreeze {
			return false
		}
	}
	return boundaryUncontested(stone, world, b.neighbours)
}

// Count each positive observation after links, renames and settlement have determined which stones are still
// unidentified. An ID freed by a look match on this frame already has its first sight. A gap breaks the count.
func (tr *openTray) observeBoundary(previous, world *trayWorld) {
	adjacent := previous != nil && previous.ref.G+1 == world.ref.G && !world.hole
	canObserve := adjacent && previous.quietHands && world.quietHands && len(world.w.Events) == 0 &&
		len(tr.blinks) != 0 && tr.calmRef != nil && tr.calmRef.G+1 == world.ref.G
	for id, u := range tr.unid {
		stone := world.byID[id]
		if !adjacent || u.lastFrame != previous.ref.G {
			u.positives, u.boundary = 0, nil
		}
		if u.boundary != nil && !u.boundary.stillValid(stone, world, tr.calmRef) {
			u.boundary = nil
		}
		if u.boundary != nil && stone.Detected {
			u.boundary.latest = stone
		}
		if canObserve && u.positives >= 2 && u.born.G < tr.calmRef.G && stone.Detected && stone.Shape == "frozen" {
			prior := previous.byID[id]
			if prior != nil && !prior.Detected && prior.Shape == "frozen" && compatible(sizeOf(stone), sizeOf(prior)) &&
				dist(placeOf(stone), placeOf(prior)) <= nearD {
				near := boundaryNeighbours(prior, previous)
				if boundaryUncontested(stone, world, near) {
					u.boundary = &boundarySight{calmG: tr.calmRef.G, t: world.ref.T, anchor: stone, latest: stone, neighbours: near}
				}
			}
		}
		u.lastFrame = world.ref.G
		if stone.Detected && u.positives < 2 {
			u.positives++
		}
	}
}
