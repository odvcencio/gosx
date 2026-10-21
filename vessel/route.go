package vessel

import "m31labs.dev/gosx/det"

// Flow tracks how busy each portal is, so a route can include queueing time and
// a simulation can charge people for it. A zero Flow is not usable; make one
// with [Layout.NewFlow]. It holds one float per portal and no pointers into the
// layout, so it is cheap to copy and to save.
type Flow struct {
	// FreeAt[i] is the simulation time at which portal i can start loading the
	// next person (stairs) or the next car (elevators).
	FreeAt []float64
}

// NewFlow returns an idle Flow for the layout.
func (l *Layout) NewFlow() *Flow { return &Flow{FreeAt: make([]float64, len(l.Portals))} }

// Wait returns the queueing delay in seconds that a party arriving at portal
// index at time now would face before it starts loading.
func (f *Flow) Wait(portal int, now float64) float64 {
	return det.Max(f.FreeAt[portal]-now, 0)
}

// Admit sends a party of n people from deck `from` to deck `to` through the
// portal, arriving at time now. It returns when the party starts to board and
// when it arrives. It updates the portal's busy time. It returns ok=false if
// the portal cannot link the two decks.
//
// Stairs pass Capacity persons per second, so a party of n occupies the stairs
// for n/Capacity seconds, and the last person arrives TripTime later. An
// elevator takes Capacity people per car; each extra car waits for the first car
// to come back, 2*TripTime apart.
func (f *Flow) Admit(l *Layout, portal, from, to, n int, now float64) (start, arrive float64, ok bool) {
	p := &l.Portals[portal]
	if n < 1 || !p.CanTravel(from, to) {
		return 0, 0, false
	}
	start = det.Max(now, f.FreeAt[portal])
	trip := p.TripTime(from, to)
	if p.Kind == Stairs {
		busy := float64(n) / det.Max(p.Capacity, 1e-9)
		f.FreeAt[portal] = start + busy
		return start, start + busy + trip, true
	}
	cars := (n + int(p.Capacity) - 1) / int(det.Max(p.Capacity, 1))
	last := start + float64(float64(cars-1)*float64(2*trip))
	f.FreeAt[portal] = last + float64(2*trip)
	return start, last + trip, true
}

// Hop is one step of a [Route]: walk to a portal, then ride it.
type Hop struct {
	Portal   int     // index into Layout.Portals
	From, To int     // deck indices
	Walk     float64 // metres walked on the From deck to reach the portal
	Wait     float64 // seconds queued (zero when the route ignores flow)
	Ride     float64 // seconds in transit
}

// Route is a path between two points on (possibly) different decks.
type Route struct {
	Hops []Hop
	// Time is the total seconds: walking, queueing and riding, plus the final
	// walk on the destination deck.
	Time float64
	// EndWalk is the metres walked on the destination deck after the last hop.
	EndWalk float64
}

// RouteOptions tunes [Layout.Route].
type RouteOptions struct {
	WalkSpeed float64 // metres per second; default 1.2
	Flow      *Flow   // if set, queueing at each portal is added to the cost
	Now       float64 // simulation time used with Flow
	// Avoid skips portals for which it returns true, for example closed lifts.
	Avoid func(portal int) bool
}

// Route finds the quickest way from (fromX, fromZ) on deck `from` to
// (toX, toZ) on deck `to`. Walking on a deck is a straight line. It returns
// ok=false when no route exists. The search is a Dijkstra over portal stops
// with fixed tie-breaking, so every platform returns the same route.
func (l *Layout) Route(from int, fromX, fromZ float64, to int, toX, toZ float64, opt RouteOptions) (Route, bool) {
	if from < 0 || to < 0 || from >= len(l.Decks) || to >= len(l.Decks) {
		return Route{}, false
	}
	speed := opt.WalkSpeed
	if speed <= 0 {
		speed = 1.2
	}
	if from == to {
		d := det.Hypot(toX-fromX, toZ-fromZ)
		return Route{Time: d / speed, EndWalk: d}, true
	}
	// Nodes: 0 = start, 1 = goal, then one per (portal, stop).
	type node struct {
		deck, portal int
		x, z         float64
	}
	nodes := []node{{from, -1, fromX, fromZ}, {to, -1, toX, toZ}}
	for i := range l.Portals {
		if opt.Avoid != nil && opt.Avoid(i) {
			continue
		}
		p := &l.Portals[i]
		for _, s := range p.Stops {
			nodes = append(nodes, node{s, i, p.X, p.Z})
		}
	}
	const inf = 1e300
	cost := make([]float64, len(nodes))
	prev := make([]int, len(nodes))
	done := make([]bool, len(nodes))
	for i := range cost {
		cost[i], prev[i] = inf, -1
	}
	cost[0] = 0
	for {
		u := -1
		for i := range nodes {
			if !done[i] && cost[i] < inf && (u < 0 || cost[i] < cost[u]) {
				u = i
			}
		}
		if u < 0 || u == 1 {
			break
		}
		done[u] = true
		for v := range nodes {
			if done[v] || v == u {
				continue
			}
			var w float64
			a, b := &nodes[u], &nodes[v]
			switch {
			case a.deck == b.deck && !(a.portal >= 0 && a.portal == b.portal):
				w = det.Hypot(a.x-b.x, a.z-b.z) / speed // walk on a deck
			case a.portal >= 0 && a.portal == b.portal && l.Portals[a.portal].CanTravel(a.deck, b.deck):
				p := &l.Portals[a.portal]
				w = p.TripTime(a.deck, b.deck)
				if opt.Flow != nil {
					w += opt.Flow.Wait(a.portal, opt.Now+cost[u])
				}
			default:
				continue
			}
			if c := cost[u] + w; c < cost[v] {
				cost[v], prev[v] = c, u
			}
		}
	}
	if cost[1] >= inf {
		return Route{}, false
	}
	// Walk the chain back and turn it into hops.
	var chain []int
	for n := 1; n >= 0; n = prev[n] {
		chain = append(chain, n)
		if n == 0 {
			break
		}
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	r := Route{Time: cost[1]}
	walkFrom := nodes[chain[0]]
	for k := 1; k < len(chain); k++ {
		a, b := nodes[chain[k-1]], nodes[chain[k]]
		if a.portal >= 0 && a.portal == b.portal {
			h := Hop{Portal: a.portal, From: a.deck, To: b.deck, Walk: det.Hypot(a.x-walkFrom.x, a.z-walkFrom.z)}
			h.Ride = l.Portals[a.portal].TripTime(a.deck, b.deck)
			if opt.Flow != nil {
				// Wait is measured at the time the walker reaches the portal.
				h.Wait = opt.Flow.Wait(a.portal, opt.Now+cost[chain[k-1]])
			}
			r.Hops = append(r.Hops, h)
			walkFrom = b
		}
	}
	end := nodes[chain[len(chain)-1]]
	r.EndWalk = det.Hypot(end.x-walkFrom.x, end.z-walkFrom.z)
	return r, true
}
