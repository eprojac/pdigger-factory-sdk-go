package pdiggerfactory

// A tray can stop more than once. Keep observations for the current stop separate from an older calm snapshot.
type trayVisit struct {
	startG       int64
	ordinaryCalm int
	epoch        int
	clean        *visitPoint
	fallback     *visitPoint
	unseen       map[*trayStone]partialSight
}

type visitStone struct {
	id    int64
	place vec2
}

type visitPoint struct {
	world  *trayWorld
	places map[*trayStone]visitStone
}

type partialSight struct {
	id    int64
	g     int64
	epoch int
}

func (v *trayVisit) olderCalm(calm *TrayPoint) bool { return calm != nil && calm.G < v.startG }

func (v *trayVisit) frame(quiet, incomplete bool, calm *TrayPoint) {
	if !quiet {
		v.ordinaryCalm, v.clean = 0, nil
		v.epoch++
		return
	}
	v.ordinaryCalm++
	if incomplete && v.ordinaryCalm >= trayCalm && v.olderCalm(calm) && v.clean != nil {
		v.fallback = v.clean
	}
}

// If a resumed visit has no calm snapshot yet, keep a complete quiet world and its observed links. A subsequent
// omission may prevent an otherwise calm run from supplying its final frame.
func (tr *openTray) rememberVisitPoint(tw *trayWorld) {
	v := &tr.visit
	if v.ordinaryCalm == 0 || !v.olderCalm(tr.calmRef) {
		return
	}
	places := make(map[*trayStone]visitStone)
	for _, s := range tr.stones {
		if p := tw.observedPlace(s); p != nil {
			places[s] = visitStone{s.world, *p}
		}
	}
	v.clean = &visitPoint{world: tw, places: places}
}

// An incomplete inventory can still say a tracked stone was not detected. A second quiet run or a final check
// on another frame may raise a doubt. Neither observation is positive evidence of identity.
func (l *trayLedger) rememberUnseen(tr *openTray, tw *trayWorld, out *[]TrayFact) {
	v := &tr.visit
	if v.ordinaryCalm != trayCalm {
		return
	}
	for _, s := range tr.stones {
		ws := tw.byID[s.world]
		if s.linked && s.status == TrayStoneOn && !s.underSheet && s.sure && ws != nil && !ws.Detected {
			if p, ok := v.unseen[s]; ok && p.id == s.world && p.epoch != v.epoch {
				l.doubt(tr, s, "not seen at two calm moments", "unseen", tw.ref, tw.ref.G, out)
			}
			if v.unseen == nil {
				v.unseen = make(map[*trayStone]partialSight)
			}
			v.unseen[s] = partialSight{s.world, tw.ref.G, v.epoch}
		}
	}
}

func (v *trayVisit) stillUnseen(s *trayStone, ws *StoneView, g int64, final bool) bool {
	p, ok := v.unseen[s]
	if !ok || p.g == g || (!final && p.epoch == v.epoch) {
		return false
	}
	delete(v.unseen, s)
	return ws != nil && !ws.Detected && p.id == ws.ID
}
