package pdiggerfactory

import (
	"container/list"
	"maps"
	"math"
	"sort"
)

// burstGap is the longest pause, in media seconds, between two pushes of one burst.
const burstGap = 1.0

// handStates are the hand states counted in the summary.
var handStates = []string{"free", "touch", "grasp", "hinge", "carry"}

// trackPoints is how many points of a stone's track the core keeps (the server keeps 512 per path), and trackStones
// how many stones' tracks (the most recently moved ones); variables so tests can make them small.
var (
	trackPoints = 512
	trackStones = 4096
)

// keepFrames is how many grid frames of revision bookkeeping the core and the streams keep (the server retains
// 512); a variable so tests can make it small.
var keepFrames = 4096

// HandView is one hand in the interpreter state.
type HandView struct {
	// State is free, touch, grasp, hinge or carry ("" before the first world).
	State string
	// Stone is the id of the stone the hand holds, if any.
	Stone *int64
	// Card is the title of the hand's card from the cards layer, such as "Holding a stone" ("" without one).
	Card string
}

// Hands are both hands.
type Hands struct {
	Left  HandView
	Right HandView
}

// Counters are the running counts of the interpreter.
type Counters struct {
	Frames          int64
	FramesWithWorld int64
	MissingWorld    int64
	Revisions       int64
	Sessions        int64
	Gaps            int64
	Events          EventCounts
}

// InterpreterState is the state after a frame. It is a snapshot: later frames do not change it (its maps and
// pointers are shared with later snapshots and must be treated as read-only).
type InterpreterState struct {
	Session int64
	Video   string
	G       int64
	T       float64
	// Wall is the frame's wall time (epoch seconds) from the newest clock state, as Event.Wall; nil until one is known.
	Wall *float64
	// Stones are the stones of the newest world by id.
	Stones map[int64]*StoneView
	Hands  Hands
	// Board is the newest world's board rectangle (world.draw.board), Stats its counters.
	Board *Rect
	Stats *WorldStats
	// OpenMoves are the moves begun and not yet ended, by stone id (Outcome is "" and End is zero).
	OpenMoves map[int64]Activity
	// Tray is the open tray with its tray stones (section 4.6 of the SDK design), nil between trays.
	Tray     *Tray
	Counters Counters
}

// Stone returns a stone of the newest world, or nil.
func (s *InterpreterState) Stone(id int64) *StoneView { return s.Stones[id] }

// StoneIDs are the ids of the newest world's stones, ascending.
func (s *InterpreterState) StoneIDs() []int64 {
	ids := make([]int64, 0, len(s.Stones))
	for id := range s.Stones {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// Update is what one frame changed: its new events in world order, the activities it completed (section 4.3 of
// the SDK design orders them), the tray facts it made and the state after it.
type Update struct {
	// Frame is the frame (nil for the final update).
	Frame *Frame
	// Events are the frame's new events, typed, in world order.
	Events []Event
	// Activities are the activities this frame completed (or, for the final update, the ones still open).
	Activities []Activity
	// TrayFacts are the tray facts this frame made (section 4.6; after a session change, first those of the old
	// session's tray leaving); the final update has those of the open tray leaving.
	TrayFacts []TrayFact
	// Speckle is the unit's speckle answer this frame's world carried, read as a tray's (section 4.6.12): at most one.
	Speckle []TraySpeckle
	// State is the state after this frame.
	State *InterpreterState
	// IsRevision is set for a frame that revised the world of a frame already seen.
	IsRevision bool
	// IsBackfill is set for a stream frame whose late layers the SDK read back.
	IsBackfill bool
	// Final is set for the last update of a replay or of a stream the server ended (Resume(false)), after its last
	// frame: Frame is nil, Events is empty, Activities are the ones still open, closed as interrupted (moves) or ended
	// (push bursts), TrayFacts are the open tray's leaving, and State is the state after them. Check it in code that
	// handles each frame.
	Final bool
	// Duplicate is set when the frame repeated one already seen (same or lower world.rev) and was dropped; such
	// updates have no events and no state, and the iterators skip them.
	Duplicate bool
}

// idKey is a stone id that may be absent.
type idKey struct {
	has bool
	id  int64
}

func idKeyOf(p *int64) idKey {
	if p == nil {
		return idKey{}
	}
	return idKey{has: true, id: *p}
}

func copyID(p *int64) *int64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

type openBurst struct {
	act  Activity
	last Moment
}

// TrackPoint is a point of a stone's track: where its solid was at a grid frame (scene x, y, z in units of the
// template gauge).
type TrackPoint struct {
	G   int64
	T   float64
	Pos [3]float64
}

// stoneTrack is one stone's points, an element of the core's least-recently-moved list.
type stoneTrack struct {
	id     int64
	points []TrackPoint
}

// InterpreterCore is the Interpreter's pure, synchronous state machine: Feed turns a frame into an Update, SetClock
// takes the clock states as they come and Gap the stream's gap and reset events, End (or Finish) closes what is still
// open. Client streams and Replay feed it; it does no I/O. It is not safe for concurrent use.
type InterpreterCore struct {
	hasSession bool
	session    int64
	video      string
	hasG       bool
	g          int64
	t          float64

	stones    map[int64]*StoneView
	hands     WorldHands
	board     *Rect
	stats     *WorldStats
	cardLeft  string
	cardRight string

	moves        map[idKey]*Activity
	bursts       map[string]*openBurst
	emitted      map[int64]map[eventKey]struct{}
	revs         map[int64]int64
	links        map[int64]map[int64]string // g -> the tray links before that frame, for its revisions
	lastLinks    map[int64]string
	lastWorldRef *Moment
	clocks       []ClockEvent            // the newest clock state of each of the last 8 sessions, oldest first
	tracks       map[int64]*list.Element // of *stoneTrack
	trackLRU     *list.List              // the most recently moved stone at the back

	counters      Counters
	hasFirst      bool
	firstG, lastG int64
	firstT, lastT float64
	hasPrevG      bool
	prevG         int64
	hasSessionT   bool
	sessionFirstT float64
	sessionLastT  float64
	mediaSeconds  float64
	moveCounts    MoveCounts
	pushCounts    PushCounts
	sumDur        float64
	nDur          int64
	sumDist       float64
	nDist         int64
	stonesLast    *int64
	stonesMax     int64
	confirmedLast *int64
	handFrames    [2][5]int64

	ledger     *trayLedger
	trays      []Tray // trays that have left, oldest first
	traysKept  int
	trayCounts TrayCounts
	speckle    []TraySpeckle // the unit's speckle answers read, oldest first (section 4.6.12)
}

// NewInterpreterCore makes an empty core.
func NewInterpreterCore() *InterpreterCore {
	c := &InterpreterCore{traysKept: TraysKept}
	c.resetSession()
	c.moves = map[idKey]*Activity{}
	c.bursts = map[string]*openBurst{}
	c.ledger = c.newLedger(0, "")
	return c
}

// KeepTrays sets how many trays that have left Trays keeps, the newest (n <= 0: every one); the default is TraysKept.
func (c *InterpreterCore) KeepTrays(n int) { c.traysKept = n }

func (c *InterpreterCore) newLedger(session int64, video string) *trayLedger {
	return newTrayLedger(&c.trayCounts, session, video, c.keepTray)
}

func (c *InterpreterCore) keepTray(t Tray) {
	c.trays = append(c.trays, t)
	if c.traysKept > 0 && len(c.trays) >= 2*c.traysKept { // keep the newest, in a new array once in a while
		c.trays = append([]Tray(nil), c.trays[len(c.trays)-c.traysKept:]...)
	}
}

// Trays are the trays so far, oldest first: those that have left (the newest KeepTrays, across sessions) and the
// open one. Each has its Arrived and Leaving points, its tray stones and its facts (section 4.6 of the SDK design).
func (c *InterpreterCore) Trays() []Tray {
	kept := c.trays
	if c.traysKept > 0 && len(kept) > c.traysKept {
		kept = kept[len(kept)-c.traysKept:]
	}
	out := append([]Tray(nil), kept...)
	if t := c.ledger.current(); t != nil {
		out = append(out, *t)
	}
	return out
}

// Speckle are the unit's speckle answers read so far, oldest first (the newest 1024, across sessions), each with its
// tray and the tray stones in it (section 4.6.12 of the SDK design). An answer that comes after its tray left is here,
// not in the tray.
func (c *InterpreterCore) Speckle() []TraySpeckle {
	kept := c.speckle
	if len(kept) > speckleKept {
		kept = kept[len(kept)-speckleKept:]
	}
	return append([]TraySpeckle(nil), kept...)
}

func (c *InterpreterCore) resetSession() {
	c.stones = map[int64]*StoneView{}
	c.hands = WorldHands{}
	c.board = nil
	c.stats = nil
	c.cardLeft, c.cardRight = "", ""
	c.emitted = map[int64]map[eventKey]struct{}{}
	c.revs = map[int64]int64{}
	c.links, c.lastLinks = map[int64]map[int64]string{}, nil
	c.lastWorldRef = nil
	c.tracks = map[int64]*list.Element{}
	c.trackLRU = list.New()
}

// SetClock takes a clock state (a "clock" stream event, a clock line of a recording, GetSession's state): wall times
// of the frames of its session come from the newest state of that session, so the frames of a session that ended
// still get theirs after the next session's clock has come. A state with a lower revision than the one kept for its
// session is older and is ignored; the states of the last 8 sessions are kept.
func (c *InterpreterCore) SetClock(ev *ClockEvent) {
	if ev == nil {
		return
	}
	for i, k := range c.clocks {
		if k.Session == ev.Session {
			if ev.Revision < k.Revision {
				return
			}
			c.clocks = append(c.clocks[:i], c.clocks[i+1:]...)
			break
		}
	}
	c.clocks = append(c.clocks, *ev)
	if len(c.clocks) > 8 {
		c.clocks = append([]ClockEvent(nil), c.clocks[len(c.clocks)-8:]...)
	}
}

// wall is the server wall time at which the shared playhead shows (or showed) media time t, from the newest clock
// state of this session, as SessionClock.WallOf: start + (t - anchor_media) / rate, where start is max(anchor_wall,
// hold_until) while playing (the playhead runs from the end of the worker's hold) and anchor_wall while paused; nil
// without a clock state of the session, or when the rate is not above 0.
func (c *InterpreterCore) wall(session int64, t float64) *float64 {
	for i := len(c.clocks) - 1; i >= 0; i-- {
		k := &c.clocks[i]
		if k.Session != session {
			continue
		}
		if !(k.Rate > 0) {
			return nil
		}
		start := k.AnchorWall
		if k.Playing && k.HoldUntil > start {
			start = k.HoldUntil
		}
		w := start + (t-k.AnchorMedia)/k.Rate
		return &w
	}
	return nil
}

func (c *InterpreterCore) ref(f *Frame) Moment {
	return Moment{G: f.G, T: f.T, Wall: c.wall(f.Session, f.T)}
}

// Gap tells the core that the stream dropped frames here (a gap or a reset event): the tray rules count the next frame
// as one after frames they could not read, as after a frame without a world, a jump in G or a revision (section 4.6.1
// of the SDK design, presence questions). The Interpreter and Replay call it for every GapItem and ResetItem of their
// stream; call it yourself when you feed a core from Items.
func (c *InterpreterCore) Gap() { c.ledger.skipped() }

// Feed processes one frame (section 2.4 of the SDK design) and returns what it changed.
func (c *InterpreterCore) Feed(f *Frame) *Update {
	u := &Update{Frame: f}
	w := f.World
	var rev int64
	if w != nil {
		rev = w.Rev
	}
	if !c.hasSession || f.Session != c.session {
		c.closeSession(&u.Activities)
		u.TrayFacts = c.ledger.close()
		c.ledger = c.newLedger(f.Session, f.Video)
		c.hasSession, c.session, c.video = true, f.Session, f.Video
		c.resetSession()
		c.counters.Sessions++
		c.hasSessionT = false
		c.hasPrevG = false
	} else if c.hasPrevG && f.G <= c.prevG {
		seen, ok := c.revs[f.G]
		if w == nil || !ok || rev <= seen {
			u.Duplicate = true
			return u
		}
		// a revision: a higher world.rev of a grid frame already seen
		c.counters.Revisions++
		c.revs[f.G] = rev
		keys := c.emittedFor(f.G)
		links := c.links[f.G] // a revised event's tray stone is the one before its own frame
		for i := range w.Events {
			e := &w.Events[i]
			k := keyOf(e)
			if _, dup := keys[k]; dup {
				continue
			}
			keys[k] = struct{}{}
			u.Events = append(u.Events, c.makeEvent(f, e, w, links))
		}
		var closed []Activity
		c.applyActivities(f, w, u.Events, &closed)
		c.emit(closed, &u.Activities)
		if f.G >= c.g {
			c.takeState(w)
		}
		c.ledger.skipped() // a frame again, which the tray rules never read: it may hold a hand at work (4.6.1)
		u.IsRevision = true
		u.State = c.snapshot()
		return u
	}
	// a new grid frame of this session
	c.counters.Frames++
	if !c.hasFirst {
		c.hasFirst, c.firstG, c.firstT = true, f.G, f.T
	}
	c.lastG, c.lastT = f.G, f.T
	if c.hasPrevG && f.G > c.prevG+1 {
		c.counters.Gaps++
		c.ledger.skipped()
	}
	c.hasPrevG, c.prevG = true, f.G
	if !c.hasSessionT {
		c.hasSessionT, c.sessionFirstT = true, f.T
	}
	c.sessionLastT = f.T
	c.hasG, c.g, c.t = true, f.G, f.T
	if f.Cards != nil {
		c.cardLeft, c.cardRight = f.Cards.Left.Title, f.Cards.Right.Title
	}
	// the tray links before this frame: its events', later its revisions', and the speckle answers', which also read
	// those of a frame without a world (4.6.12; one copy while they do not change)
	links := c.ledger.links()
	if maps.Equal(links, c.lastLinks) {
		links = c.lastLinks
	}
	c.links[f.G], c.lastLinks = links, links
	if w == nil {
		c.counters.MissingWorld++
		c.ledger.skipped()
		u.State = c.snapshot()
		return u
	}
	c.counters.FramesWithWorld++
	c.revs[f.G] = rev
	c.prune(f.G)
	// stale push bursts close before this frame's events
	var closed []Activity
	for _, side := range []string{HandLeft, HandRight, HandBoth} {
		if b := c.bursts[side]; b != nil && f.T-b.last.T > burstGap {
			closed = append(closed, c.endBurst(side))
		}
	}
	keys := c.emittedFor(f.G)
	for i := range w.Events {
		e := &w.Events[i]
		keys[keyOf(e)] = struct{}{}
		u.Events = append(u.Events, c.makeEvent(f, e, w, links))
	}
	c.applyActivities(f, w, u.Events, &closed)
	c.emit(closed, &u.Activities)
	c.takeState(w)
	c.recordTracks(f.G, f.T, w)
	r := c.ref(f)
	c.lastWorldRef = &r
	n := int64(len(w.Stones))
	c.stonesLast = &n
	if n > c.stonesMax {
		c.stonesMax = n
	}
	c.confirmedLast = copyID(w.Stats.Confirmed)
	for i, h := range []*HandState{w.Hands.Left, w.Hands.Right} {
		if h == nil {
			continue
		}
		for k, st := range handStates {
			if h.State == st {
				c.handFrames[i][k]++
			}
		}
	}
	u.TrayFacts = append(u.TrayFacts, c.ledger.feed(w, r)...)
	if w.Speckle != nil { // read after the trays took the frame (section 4.6.12)
		if a := c.ledger.speckle(w.Speckle, r, func(g int64) map[int64]string { return c.links[g] }); a != nil {
			u.Speckle = []TraySpeckle{*a}
			c.speckle = append(c.speckle, *a)
			if len(c.speckle) >= 2*speckleKept { // keep the newest, in a new array once in a while
				c.speckle = append([]TraySpeckle(nil), c.speckle[len(c.speckle)-speckleKept:]...)
			}
		}
	}
	u.State = c.snapshot()
	return u
}

// recordTracks adds a point to the track of every stone of the world whose solid moved since its last point.
func (c *InterpreterCore) recordTracks(g int64, t float64, w *WorldLayer) {
	for i := range w.Stones {
		s := &w.Stones[i]
		if s.Solid == nil || len(s.Solid.Pos) < 3 {
			continue
		}
		p := TrackPoint{G: g, T: t, Pos: [3]float64{s.Solid.Pos[0], s.Solid.Pos[1], s.Solid.Pos[2]}}
		el, ok := c.tracks[s.ID]
		if !ok {
			c.tracks[s.ID] = c.trackLRU.PushBack(&stoneTrack{id: s.ID, points: []TrackPoint{p}})
			if c.trackLRU.Len() > trackStones {
				oldest := c.trackLRU.Remove(c.trackLRU.Front()).(*stoneTrack)
				delete(c.tracks, oldest.id)
			}
			continue
		}
		tr := el.Value.(*stoneTrack)
		if tr.points[len(tr.points)-1].Pos == p.Pos {
			continue
		}
		tr.points = append(tr.points, p)
		if len(tr.points) >= 2*trackPoints { // keep the newest trackPoints, in place, once in trackPoints points
			tr.points = tr.points[:copy(tr.points, tr.points[trackPoints:])]
		}
		c.trackLRU.MoveToBack(el)
	}
}

// Track is the path of a stone's solid in this session, oldest first: a point for every frame fed where it moved,
// at most the newest 512, for the 4096 most recently moved stones; nil for a stone the core has no track of. The
// server's own track since the session began is API.GetStone.
func (c *InterpreterCore) Track(stoneID int64) []TrackPoint {
	el, ok := c.tracks[stoneID]
	if !ok {
		return nil
	}
	points := el.Value.(*stoneTrack).points
	return append([]TrackPoint(nil), points[max(0, len(points)-trackPoints):]...)
}

func (c *InterpreterCore) emittedFor(g int64) map[eventKey]struct{} {
	keys := c.emitted[g]
	if keys == nil {
		keys = map[eventKey]struct{}{}
		c.emitted[g] = keys
	}
	return keys
}

// prune forgets the revision bookkeeping of frames far behind g, so a live interpreter runs in bounded memory.
func (c *InterpreterCore) prune(g int64) {
	if len(c.revs) <= 2*keepFrames {
		return
	}
	for k := range c.revs {
		if k < g-int64(keepFrames) {
			delete(c.revs, k)
			delete(c.emitted, k)
			delete(c.links, k)
		}
	}
}

// makeEvent makes the event of a world event; links are the tray links before the event's frame.
func (c *InterpreterCore) makeEvent(f *Frame, e *WorldEvent, w *WorldLayer, links map[int64]string) Event {
	var stone *StoneView
	var trayStone string
	if e.ID != nil {
		for i := range w.Stones {
			if w.Stones[i].ID == *e.ID {
				stone = &w.Stones[i]
				break
			}
		}
		trayStone = links[*e.ID]
	}
	*c.counters.Events.ptr(e.Type)++
	return Event{Type: e.Type, G: f.G, T: f.T, Wall: c.wall(f.Session, f.T), Session: f.Session, Video: f.Video,
		StoneID: copyID(e.ID), Hand: e.Side, Why: e.Why, Into: e.Into, Source: e.Source, Stone: stone,
		TrayStone: trayStone}
}

func (c *InterpreterCore) applyActivities(f *Frame, w *WorldLayer, events []Event, closed *[]Activity) {
	ref := c.ref(f)
	byID := make(map[int64]*StoneView, len(w.Stones))
	for i := range w.Stones {
		byID[w.Stones[i].ID] = &w.Stones[i]
	}
	pos := func(id *int64) []float64 {
		if id == nil {
			return nil
		}
		s := byID[*id]
		if s == nil || s.Solid == nil || len(s.Solid.Pos) < 2 {
			return nil
		}
		return append([]float64(nil), s.Solid.Pos...)
	}
	handClosed := map[idKey]bool{}
	for i := range events {
		switch events[i].Type {
		case EventPlace, EventDrop, EventGone:
			handClosed[idKeyOf(events[i].StoneID)] = true
		}
	}
	for i := range events {
		ev := &events[i]
		sid := idKeyOf(ev.StoneID)
		if ev.Type == EventDelete && handClosed[sid] {
			// the same frame has the hand's put-down or carry-away of this stone: that decides
			continue
		}
		if ev.Type == EventPush {
			if (ev.Hand == HandLeft || ev.Hand == HandRight || ev.Hand == HandBoth) && ev.Why == "" {
				c.push(f, ev, ref, closed)
			}
			continue
		}
		m := c.moves[sid]
		switch ev.Type {
		case EventLift, EventHinge:
			if m == nil {
				c.moves[sid] = &Activity{Kind: KindMove, StoneID: copyID(ev.StoneID), Hand: ev.Hand, Start: ref,
					From: pos(ev.StoneID), Tipped: ev.Type == EventHinge, Session: f.Session, Video: f.Video}
				continue
			}
			if ev.Type == EventHinge {
				m.Tipped = true
			}
			mergeHand(m, ev.Hand)
		case EventCarry:
			if m != nil {
				m.Carried = true
				mergeHand(m, ev.Hand)
			}
		case EventPlace, EventDrop, EventGone, EventDelete:
			outcome := map[string]string{EventPlace: OutcomePlaced, EventDrop: OutcomeDropped,
				EventGone: OutcomeCarriedAway, EventDelete: OutcomeRemoved}[ev.Type]
			if m == nil {
				if ev.Type == EventDelete {
					continue
				}
				m = &Activity{Kind: KindMove, StoneID: copyID(ev.StoneID), Hand: ev.Hand, Start: ref, Partial: true,
					Session: f.Session, Video: f.Video}
			} else {
				delete(c.moves, sid)
				mergeHand(m, ev.Hand)
			}
			m.Outcome = outcome
			m.End = ref
			m.Duration = m.End.T - m.Start.T
			m.To = nil
			if outcome == OutcomePlaced || outcome == OutcomeDropped {
				m.To = pos(ev.StoneID)
			}
			if m.From != nil && m.To != nil {
				d := Round3(planarDistance(m.From, m.To))
				m.Distance = &d
			}
			*closed = append(*closed, *m)
		}
	}
}

// planarDistance is the distance of two points in x and y, sqrt(dx*dx + dy*dy) with every step rounded on its own,
// as the other languages compute it (the conversions keep Go from fusing a product and a sum into one multiply-add).
func planarDistance(from, to []float64) float64 {
	dx, dy := to[0]-from[0], to[1]-from[1]
	return math.Sqrt(float64(dx*dx) + float64(dy*dy))
}

func (c *InterpreterCore) push(f *Frame, ev *Event, ref Moment, closed *[]Activity) {
	b := c.bursts[ev.Hand]
	if b != nil && f.T-b.last.T > burstGap {
		*closed = append(*closed, c.endBurst(ev.Hand))
		b = nil
	}
	if b == nil {
		b = &openBurst{act: Activity{Kind: KindPush, Hand: ev.Hand, Start: ref, Session: f.Session, Video: f.Video}}
		c.bursts[ev.Hand] = b
	}
	b.last = ref
	b.act.Pushes++
	if ev.StoneID == nil {
		return
	}
	id := *ev.StoneID
	i := sort.Search(len(b.act.Stones), func(i int) bool { return b.act.Stones[i] >= id })
	if i < len(b.act.Stones) && b.act.Stones[i] == id {
		return
	}
	b.act.Stones = append(b.act.Stones, 0)
	copy(b.act.Stones[i+1:], b.act.Stones[i:])
	b.act.Stones[i] = id
}

func mergeHand(m *Activity, side string) {
	switch {
	case side == "":
	case m.Hand == "":
		m.Hand = side
	case m.Hand != side:
		m.Hand = HandBoth
	}
}

func (c *InterpreterCore) endBurst(side string) Activity {
	b := c.bursts[side]
	delete(c.bursts, side)
	a := b.act
	a.End = b.last
	a.Duration = a.End.T - a.Start.T
	return a
}

func (c *InterpreterCore) emit(closed []Activity, out *[]Activity) {
	sortActivities(closed)
	for i := range closed {
		c.count(&closed[i])
	}
	*out = append(*out, closed...)
}

func (c *InterpreterCore) count(a *Activity) {
	if a.Kind == KindPush {
		p := &c.pushCounts
		p.Total++
		p.Pushes += int64(a.Pushes)
		switch a.Hand {
		case HandLeft:
			p.Left++
		case HandRight:
			p.Right++
		case HandBoth:
			p.Both++
		}
		return
	}
	m := &c.moveCounts
	m.Total++
	switch a.Outcome {
	case OutcomePlaced:
		m.Placed++
	case OutcomeDropped:
		m.Dropped++
	case OutcomeCarriedAway:
		m.CarriedAway++
	case OutcomeRemoved:
		m.Removed++
	default:
		m.Interrupted++
	}
	if a.Partial {
		m.Partial++
	}
	if a.Carried {
		m.Carried++
	}
	if !a.Partial && a.Outcome != OutcomeInterrupted {
		c.sumDur += a.Duration
		c.nDur++
	}
	if a.Distance != nil {
		c.sumDist += *a.Distance
		c.nDist++
	}
}

func (c *InterpreterCore) takeState(w *WorldLayer) {
	stones := make(map[int64]*StoneView, len(w.Stones))
	for i := range w.Stones {
		stones[w.Stones[i].ID] = &w.Stones[i]
	}
	c.stones = stones
	c.hands = w.Hands
	c.board = nil
	if w.Draw != nil {
		c.board = w.Draw.Board
	}
	c.stats = &w.Stats
}

// closeSession closes the open moves as interrupted and ends the open push bursts.
func (c *InterpreterCore) closeSession(out *[]Activity) {
	if !c.hasSession {
		return
	}
	keys := make([]idKey, 0, len(c.moves))
	for k := range c.moves {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.has != b.has {
			return !a.has
		}
		return a.id < b.id
	})
	var closed []Activity
	for _, k := range keys {
		m := c.moves[k]
		m.Outcome = OutcomeInterrupted
		m.End = m.Start
		if c.lastWorldRef != nil {
			m.End = *c.lastWorldRef
		}
		m.Duration = m.End.T - m.Start.T
		m.To = nil
		m.Distance = nil
		closed = append(closed, *m)
	}
	c.moves = map[idKey]*Activity{}
	for _, side := range []string{HandLeft, HandRight, HandBoth} {
		if c.bursts[side] != nil {
			closed = append(closed, c.endBurst(side))
		}
	}
	c.emit(closed, out)
	if c.hasSessionT {
		c.mediaSeconds += c.sessionLastT - c.sessionFirstT
	}
}

// End closes what is still open at the end of a replay or a stream and returns it as the final update (Final set,
// Frame nil): the open moves closed as interrupted and the push bursts ended, in the order of section 4.3, and the
// facts of the open tray leaving.
func (c *InterpreterCore) End() *Update {
	u := &Update{Final: true}
	c.closeSession(&u.Activities)
	u.TrayFacts = c.ledger.close()
	c.ledger = c.newLedger(0, "")
	c.hasSession = false
	u.State = c.snapshot()
	return u
}

// Finish is End's activities: what is still open closed (moves as interrupted, push bursts ended), in the order of
// section 4.3; the open tray leaves (its facts are in Trays).
func (c *InterpreterCore) Finish() []Activity { return c.End().Activities }

func (c *InterpreterCore) snapshot() *InterpreterState {
	st := &InterpreterState{Session: c.session, Video: c.video, G: c.g, T: c.t, Stones: c.stones, Board: c.board,
		Stats: c.stats, Counters: c.counters}
	if c.hasG {
		st.Wall = c.wall(c.session, c.t)
	}
	if h := c.hands.Left; h != nil {
		st.Hands.Left = HandView{State: h.State, Stone: copyID(h.Stone)}
	}
	if h := c.hands.Right; h != nil {
		st.Hands.Right = HandView{State: h.State, Stone: copyID(h.Stone)}
	}
	st.Hands.Left.Card, st.Hands.Right.Card = c.cardLeft, c.cardRight
	st.OpenMoves = make(map[int64]Activity, len(c.moves))
	for k, m := range c.moves {
		if k.has {
			st.OpenMoves[k.id] = *m
		}
	}
	st.Tray = c.ledger.current()
	return st
}

// State is the state after the last frame fed.
func (c *InterpreterCore) State() *InterpreterState { return c.snapshot() }

// Summary is the summary so far (section 4.4 of the SDK design); after Finish it is the summary of everything fed.
func (c *InterpreterCore) Summary() *Summary {
	s := &Summary{
		Frames: c.counters.Frames, FramesWithWorld: c.counters.FramesWithWorld, MissingWorld: c.counters.MissingWorld,
		Revisions: c.counters.Revisions, Sessions: c.counters.Sessions, Gaps: c.counters.Gaps,
		Events: c.counters.Events, Moves: c.moveCounts, PushBursts: c.pushCounts,
		Stones: StoneCounts{Last: copyID(c.stonesLast), Max: c.stonesMax, ConfirmedLast: copyID(c.confirmedLast)},
		Trays:  c.trayCounts,
	}
	if c.hasFirst {
		s.FirstG, s.LastG = Int64(c.firstG), Int64(c.lastG)
		s.FirstT, s.LastT = Float64(Round3(c.firstT)), Float64(Round3(c.lastT))
	}
	media := c.mediaSeconds
	if c.hasSession && c.hasSessionT {
		media += c.sessionLastT - c.sessionFirstT
	}
	s.MediaSeconds = Round3(media)
	if c.nDur > 0 {
		s.Moves.MeanDuration = Float64(Round3(c.sumDur / float64(c.nDur)))
	}
	if c.nDist > 0 {
		s.Moves.MeanDistance = Float64(Round3(c.sumDist / float64(c.nDist)))
	}
	times := func(i int) HandStateTimes {
		h := c.handFrames[i]
		return HandStateTimes{Free: Round3(0.2 * float64(h[0])), Touch: Round3(0.2 * float64(h[1])),
			Grasp: Round3(0.2 * float64(h[2])), Hinge: Round3(0.2 * float64(h[3])), Carry: Round3(0.2 * float64(h[4]))}
	}
	s.Hands = HandTimes{Left: times(0), Right: times(1)}
	return s
}
