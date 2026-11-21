package author

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// JointBand is a joint's natural range as local Euler angles in degrees
// (EulerOf order: X, then Y, then Z).
type JointBand struct {
	Min, Max Vec3
}

// AuditOptions says which joints are which and where the limits sit. Up is
// +Y and the body's left-right axis is X, with the midline at X = 0.
type AuditOptions struct {
	// Waist names the joint below and the joint above the waist, for
	// example "hips" and "spine".
	Waist [2]string
	// Legs names the two upper-leg joints. Their weights (and their
	// descendants') must stay below the hip joint and on their own side.
	Legs [2]string
	// HipTolerance is how far above its hip joint a leg weight may reach;
	// MidlineTolerance how far past the midline. Defaults: 2 % and 1 % of
	// the mesh height.
	HipTolerance, MidlineTolerance float64
	// Probe lists the joints tested for open seams and cross-section loss.
	// Empty means every joint with a parent.
	Probe []string
	// Reach is the ray length for the open-seam test and the radius of the
	// cross-section band. SectionBand is the band's half length along the
	// bone. Defaults: 15 % and 4 % of the mesh height.
	Reach, SectionBand float64
	// Rays is the number of ray directions around a joint, in each of
	// three rings (level and 25 degrees either side). Default 24.
	Rays int
	// NotchRatio is how far the outer surface may sink toward the joint,
	// as a share of its bind distance, before a direction counts as open.
	// Default 0.6.
	NotchRatio float64
	// Bands are natural joint ranges; joints not listed are not checked.
	Bands map[string]JointBand
	// Edge limits: torn beyond TornLong or below TornShort times its bind
	// length, strained beyond StrainLong or below StrainShort. Defaults 3,
	// 0.25, 1.5 and 1/1.5.
	TornLong, TornShort, StrainLong, StrainShort float64
}

// JointAudit is one joint's worst frame in one clip.
type JointAudit struct {
	Name string
	// Open is the share of directions around the joint where the outer
	// surface has sunk below NotchRatio of its bind distance from the joint:
	// a gap, notch or crease you see from outside.
	Open, OpenAt float64
	// Section is the posed mean distance of the band's vertices from the
	// bone axis over the bind distance: below 1 is a pinch, above a bulge.
	Section, SectionAt float64
}

// BandFault is a joint angle outside its band: the clip's worst excess on one
// axis.
type BandFault struct {
	Joint  string
	Axis   int
	Angle  float64
	Excess float64
	At     float64
}

// ClipAudit is one clip's numbers.
type ClipAudit struct {
	Name   string
	Frames int
	// Waist is the lowest cross-section ratio at the upper waist joint.
	Waist, WaistAt float64
	// Torn and Strained are the worst shares of edges past the limits;
	// Longest and Shortest the extreme edge-length ratios.
	Torn, Strained    float64
	Longest, Shortest float64
	// LongestEdge and ShortestEdge are the vertex pairs of those edges, and
	// LongestAt and ShortestAt their frames.
	LongestEdge, ShortestEdge [2]int
	LongestAt, ShortestAt     float64
	Joints                    []JointAudit
	OutOfBand                 []BandFault
}

// AuditReport is the whole audit.
type AuditReport struct {
	Vertices, Triangles, Edges int
	// HipCrossing counts vertices weighted to a leg that lie above its hip
	// or across the midline.
	HipCrossing int
	// OpposedNormals counts vertices whose stored normal points against the
	// face winding.
	OpposedNormals int
	// Closed counts closed components; InsideOut those with negative
	// volume.
	Components, Closed, InsideOut int
	Clips                         []ClipAudit
}

type probe struct {
	name              string
	joint, parent, to int // to: the joint whose bind position sets the axis
	leaf              bool
}

// Audit poses the rig at every key of every clip and measures it.
func Audit(r *Rig, o AuditOptions) (AuditReport, error) {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, p := range r.P {
		lo, hi = math.Min(lo, p[1]), math.Max(hi, p[1])
	}
	height := hi - lo
	def := func(v *float64, d float64) {
		if *v == 0 {
			*v = d
		}
	}
	def(&o.HipTolerance, .02*height)
	def(&o.MidlineTolerance, .01*height)
	def(&o.Reach, .15*height)
	def(&o.SectionBand, .04*height)
	def(&o.NotchRatio, .6)
	def(&o.TornLong, 3)
	def(&o.TornShort, .25)
	def(&o.StrainLong, 1.5)
	def(&o.StrainShort, 1/1.5)
	if o.Rays == 0 {
		o.Rays = 24
	}
	rep := AuditReport{Vertices: len(r.P), Triangles: len(r.Tri)}

	bind := make([]Vec3, len(r.Names))
	for i := range bind {
		bind[i] = invertAffine(r.Inverse[i]).Origin()
	}
	children := make([][]int, len(r.Names))
	for i, p := range r.Parent {
		if p >= 0 {
			children[p] = append(children[p], i)
		}
	}
	var probes []probe
	want := map[string]bool{}
	for _, n := range o.Probe {
		want[n] = true
	}
	for i, p := range r.Parent {
		if p < 0 || (len(want) > 0 && !want[r.Names[i]]) {
			continue
		}
		pr := probe{name: r.Names[i], joint: i, parent: p}
		if len(children[i]) > 0 {
			pr.to = children[i][0]
		} else {
			pr.leaf = true
		}
		probes = append(probes, pr)
	}
	axisBind := func(pr probe) Vec3 {
		if pr.leaf {
			return bind[pr.joint].Sub(bind[pr.parent]).Unit()
		}
		return bind[pr.to].Sub(bind[pr.joint]).Unit()
	}

	// Edges.
	type edge struct{ a, b int }
	seen := map[edge]bool{}
	var edges []edge
	for _, t := range r.Tri {
		for k := 0; k < 3; k++ {
			a, b := t[k], t[(k+1)%3]
			if a > b {
				a, b = b, a
			}
			e := edge{a, b}
			if !seen[e] && r.P[a].Sub(r.P[b]).Len() > 1e-7 {
				seen[e] = true
				edges = append(edges, e)
			}
		}
	}
	rep.Edges = len(edges)

	// Static checks.
	subtree := func(root int) map[int]bool {
		in := map[int]bool{root: true}
		for i := range r.Parent {
			for j := i; j >= 0; j = r.Parent[j] {
				if j == root {
					in[i] = true
					break
				}
			}
		}
		return in
	}
	for _, leg := range o.Legs {
		j := r.Joint(leg)
		if leg == "" || j < 0 {
			continue
		}
		sub := subtree(j)
		side := math.Copysign(1, bind[j][0])
		for i, p := range r.P {
			s := r.Skin[i]
			on := false
			for k := 0; k < 4; k++ {
				if s.Weight[k] > .01 && sub[s.Joint[k]] {
					on = true
				}
			}
			if on && (p[1] > bind[j][1]+o.HipTolerance || p[0]*side < -o.MidlineTolerance) {
				rep.HipCrossing++
			}
		}
	}
	wound := Normals(r.P, r.Tri)
	for i := range r.N {
		if wound[i].Dot(r.N[i]) < 0 {
			rep.OpposedNormals++
		}
	}
	rep.Components, rep.Closed, rep.InsideOut = shellVolumes(r.P, r.Tri)

	// The outer surface around each joint in the bind pose.
	bindLocal := r.Rest
	bindPosed := r.Posed(bindLocal)
	bindWorld := r.World(bindLocal)
	bindSkin := skinMats(bindWorld, r.Inverse)
	bindRadii := make([][]float64, len(probes))
	bindRef := make([]Vec3, len(probes))
	for k, pr := range probes {
		axis := jointAxis(pr, bindSkin, axisBind(pr))
		bindRef[k] = refDir(axis)
		bindRadii[k] = surfaceRadii(bindPosed, r.Tri, bindWorld[pr.joint].Origin(), axis, bindRef[k], o)
	}

	// Cross-section bands, from the bind pose.
	bands := make([][]int, len(probes))
	bindRadius := make([]float64, len(probes))
	for k, pr := range probes {
		ax := axisBind(pr)
		c := bind[pr.joint]
		sum := 0.
		for i, p := range r.P {
			s := r.Skin[i]
			on := false
			for q := 0; q < 4; q++ {
				if s.Weight[q] > 0 && (s.Joint[q] == pr.joint || s.Joint[q] == pr.parent) {
					on = true
				}
			}
			d := p.Sub(c)
			along := d.Dot(ax)
			rad := d.Sub(ax.Scale(along)).Len()
			if on && math.Abs(along) < o.SectionBand && rad < o.Reach {
				bands[k] = append(bands[k], i)
				sum += rad
			}
		}
		if len(bands[k]) > 0 {
			bindRadius[k] = sum / float64(len(bands[k]))
		}
	}

	for _, c := range r.Clips {
		ca := ClipAudit{Name: c.Name, Waist: 1, Shortest: math.Inf(1)}
		ja := make([]JointAudit, len(probes))
		for k, pr := range probes {
			ja[k] = JointAudit{Name: pr.name, Section: math.Inf(1)}
		}
		worstBand := map[[2]int]BandFault{}
		for _, t := range c.Times() {
			ca.Frames++
			local := r.Local(c, t)
			world := r.World(local)
			skin := skinMats(world, r.Inverse)
			posed := make([]Vec3, len(r.P))
			for i, p := range r.P {
				posed[i] = SkinPoint(skin, r.Skin[i], p)
			}
			torn, strained := 0, 0
			for _, e := range edges {
				ratio := posed[e.a].Sub(posed[e.b]).Len() / r.P[e.a].Sub(r.P[e.b]).Len()
				if ratio > ca.Longest {
					ca.Longest, ca.LongestEdge, ca.LongestAt = ratio, [2]int{e.a, e.b}, t
				}
				if ratio < ca.Shortest {
					ca.Shortest, ca.ShortestEdge, ca.ShortestAt = ratio, [2]int{e.a, e.b}, t
				}
				if ratio > o.TornLong || ratio < o.TornShort {
					torn++
				}
				if ratio > o.StrainLong || ratio < o.StrainShort {
					strained++
				}
			}
			ca.Torn = math.Max(ca.Torn, float64(torn)/float64(len(edges)))
			ca.Strained = math.Max(ca.Strained, float64(strained)/float64(len(edges)))
			for k, pr := range probes {
				centre := world[pr.joint].Origin()
				axis := jointAxis(pr, skin, axisBind(pr))
				ref := jointAxis(pr, skin, bindRef[k])
				ref = ref.Sub(axis.Scale(ref.Dot(axis)))
				radii := surfaceRadii(posed, r.Tri, centre, axis, ref, o)
				open, n := 0., 0.
				for q, b := range bindRadii[k] {
					if b > 0 {
						n++
						if radii[q] < o.NotchRatio*b {
							open++
						}
					}
				}
				if n > 0 {
					open /= n
				}
				if open > ja[k].Open {
					ja[k].Open, ja[k].OpenAt = open, t
				}
				if len(bands[k]) > 0 && bindRadius[k] > 0 {
					sum := 0.
					for _, i := range bands[k] {
						d := posed[i].Sub(centre)
						sum += d.Sub(axis.Scale(d.Dot(axis))).Len()
					}
					ratio := sum / float64(len(bands[k])) / bindRadius[k]
					if ratio < ja[k].Section {
						ja[k].Section, ja[k].SectionAt = ratio, t
					}
				}
			}
			for name, band := range o.Bands {
				j := r.Joint(name)
				if j < 0 {
					continue
				}
				// The rest rotation is the zero of the band.
				rel := quatConj(r.Rest[j].R).Mul(local[j].R)
				ang := EulerOf(rel)
				for axis := 0; axis < 3; axis++ {
					excess := math.Max(band.Min[axis]-ang[axis], ang[axis]-band.Max[axis])
					if excess <= 0 {
						continue
					}
					key := [2]int{j, axis}
					if f, ok := worstBand[key]; !ok || excess > f.Excess {
						worstBand[key] = BandFault{name, axis, ang[axis], excess, t}
					}
				}
			}
		}
		for k := range ja {
			if math.IsInf(ja[k].Section, 1) {
				ja[k].Section = 1
			}
		}
		if wi := indexOfProbe(probes, o.Waist[1]); wi >= 0 {
			ca.Waist, ca.WaistAt = ja[wi].Section, ja[wi].SectionAt
		}
		ca.Joints = ja
		for _, f := range worstBand {
			ca.OutOfBand = append(ca.OutOfBand, f)
		}
		sort.Slice(ca.OutOfBand, func(a, b int) bool {
			x, y := ca.OutOfBand[a], ca.OutOfBand[b]
			if x.Joint != y.Joint {
				return x.Joint < y.Joint
			}
			return x.Axis < y.Axis
		})
		rep.Clips = append(rep.Clips, ca)
	}
	return rep, nil
}

func indexOfProbe(ps []probe, name string) int {
	for i, p := range ps {
		if p.name == name {
			return i
		}
	}
	return -1
}

func quatConj(q Quat) Quat { return Quat{-q[0], -q[1], -q[2], q[3]} }

func skinMats(world, inverse []Mat4) []Mat4 {
	out := make([]Mat4, len(world))
	for i := range world {
		out[i] = world[i].Mul(inverse[i])
	}
	return out
}

// jointAxis is the posed bone axis at a joint: halfway between the bind
// axis carried by the parent and carried by the joint itself.
func jointAxis(pr probe, skin []Mat4, bindAxis Vec3) Vec3 {
	a := skin[pr.parent].ApplyDir(bindAxis).Unit()
	b := skin[pr.joint].ApplyDir(bindAxis).Unit()
	s := a.Add(b)
	if s.Len() < 1e-6 {
		return b
	}
	return s.Unit()
}

// refDir picks a direction square to axis to start the ray fan from.
func refDir(axis Vec3) Vec3 {
	ref := Vec3{1, 0, 0}
	if math.Abs(axis[0]) > .9 {
		ref = Vec3{0, 0, 1}
	}
	return axis.Cross(ref).Unit()
}

// surfaceRadii casts rays inward at the joint from Reach away, square to its
// axis and 25 degrees either side, starting the fan at ref, and returns how
// far from the joint each ray first meets a surface (0 for a miss).
func surfaceRadii(posed []Vec3, tri [][3]int, centre, axis, ref Vec3, o AuditOptions) []float64 {
	var near [][3]int
	r2 := o.Reach * o.Reach
	for _, t := range tri {
		for _, v := range t {
			d := posed[v].Sub(centre)
			if d.Dot(d) < r2 {
				near = append(near, t)
				break
			}
		}
	}
	e1 := ref.Unit()
	e2 := axis.Cross(e1).Unit()
	var out []float64
	for _, tilt := range []float64{-25, 0, 25} {
		ct, st := math.Cos(tilt*math.Pi/180), math.Sin(tilt*math.Pi/180)
		for k := 0; k < o.Rays; k++ {
			phi := 2 * math.Pi * float64(k) / float64(o.Rays)
			dir := e1.Scale(math.Cos(phi) * ct).Add(e2.Scale(math.Sin(phi) * ct)).Add(axis.Scale(st))
			from := centre.Add(dir.Scale(o.Reach))
			in := dir.Scale(-1)
			best := math.Inf(1)
			for _, t := range near {
				if d, ok := rayTriangle(from, in, posed[t[0]], posed[t[1]], posed[t[2]]); ok && d < best {
					best = d
				}
			}
			if math.IsInf(best, 1) || best > o.Reach {
				out = append(out, 0)
			} else {
				out = append(out, o.Reach-best)
			}
		}
	}
	return out
}

// rayTriangle is the Moller-Trumbore test, two-sided.
func rayTriangle(o, d, a, b, c Vec3) (float64, bool) {
	e1, e2 := b.Sub(a), c.Sub(a)
	p := d.Cross(e2)
	det := e1.Dot(p)
	if math.Abs(det) < 1e-12 {
		return 0, false
	}
	inv := 1 / det
	s := o.Sub(a)
	u := s.Dot(p) * inv
	if u < 0 || u > 1 {
		return 0, false
	}
	q := s.Cross(e1)
	v := d.Dot(q) * inv
	if v < 0 || u+v > 1 {
		return 0, false
	}
	t := e2.Dot(q) * inv
	return t, t > 1e-9
}

// shellVolumes splits the triangles into connected components and reports
// how many there are, how many are closed (every edge shared by exactly two
// triangles), and how many closed ones have negative volume.
func shellVolumes(p []Vec3, tri [][3]int) (components, closed, insideOut int) {
	parent := make([]int, len(p))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for _, t := range tri {
		a, b, c := find(t[0]), find(t[1]), find(t[2])
		parent[b] = a
		parent[find(c)] = a
	}
	type acc struct {
		vol   float64
		edges map[[2]int]int
	}
	comps := map[int]*acc{}
	for _, t := range tri {
		root := find(t[0])
		a := comps[root]
		if a == nil {
			a = &acc{edges: map[[2]int]int{}}
			comps[root] = a
		}
		a.vol += p[t[0]].Dot(p[t[1]].Cross(p[t[2]])) / 6
		for k := 0; k < 3; k++ {
			x, y := t[k], t[(k+1)%3]
			if x > y {
				x, y = y, x
			}
			a.edges[[2]int{x, y}]++
		}
	}
	for _, a := range comps {
		components++
		ok := true
		for _, n := range a.edges {
			if n != 2 {
				ok = false
				break
			}
		}
		if ok {
			closed++
			if a.vol < 0 {
				insideOut++
			}
		}
	}
	return
}

// invertAffine inverts a rotation-scale-translation matrix.
func invertAffine(m Mat4) Mat4 {
	a := m
	det := a[0][0]*(a[1][1]*a[2][2]-a[1][2]*a[2][1]) - a[0][1]*(a[1][0]*a[2][2]-a[1][2]*a[2][0]) + a[0][2]*(a[1][0]*a[2][1]-a[1][1]*a[2][0])
	if math.Abs(det) < 1e-12 {
		return TRS(Vec3{}, Identity, Vec3{1, 1, 1})
	}
	var inv Mat4
	inv[0][0] = (a[1][1]*a[2][2] - a[1][2]*a[2][1]) / det
	inv[0][1] = (a[0][2]*a[2][1] - a[0][1]*a[2][2]) / det
	inv[0][2] = (a[0][1]*a[1][2] - a[0][2]*a[1][1]) / det
	inv[1][0] = (a[1][2]*a[2][0] - a[1][0]*a[2][2]) / det
	inv[1][1] = (a[0][0]*a[2][2] - a[0][2]*a[2][0]) / det
	inv[1][2] = (a[0][2]*a[1][0] - a[0][0]*a[1][2]) / det
	inv[2][0] = (a[1][0]*a[2][1] - a[1][1]*a[2][0]) / det
	inv[2][1] = (a[0][1]*a[2][0] - a[0][0]*a[2][1]) / det
	inv[2][2] = (a[0][0]*a[1][1] - a[0][1]*a[1][0]) / det
	t := Vec3{a[0][3], a[1][3], a[2][3]}
	for i := 0; i < 3; i++ {
		inv[i][3] = -(inv[i][0]*t[0] + inv[i][1]*t[1] + inv[i][2]*t[2])
	}
	inv[3][3] = 1
	return inv
}

// Limits are the pass marks Faults applies.
type Limits struct {
	Waist     float64 // lowest waist ratio, e.g. .95
	Section   float64 // lowest cross-section ratio at any probed joint
	Torn      float64 // highest torn-edge share, e.g. .01
	Open      float64 // highest share of notched directions at any joint
	HipCross  int
	Opposed   int
	InsideOut int
	OutOfBand bool // any band fault fails
}

// Faults lists every limit the report breaks, one line each.
func (r AuditReport) Faults(l Limits) []string {
	var out []string
	if r.HipCrossing > l.HipCross {
		out = append(out, fmt.Sprintf("%d vertices weighted to a leg sit above the hip or across the midline", r.HipCrossing))
	}
	if r.OpposedNormals > l.Opposed {
		out = append(out, fmt.Sprintf("%d vertex normals oppose the face winding", r.OpposedNormals))
	}
	if r.InsideOut > l.InsideOut {
		out = append(out, fmt.Sprintf("%d closed shells are inside out", r.InsideOut))
	}
	for _, c := range r.Clips {
		if l.Waist > 0 && c.Waist < l.Waist {
			out = append(out, fmt.Sprintf("%s: waist %.3f at %.3fs", c.Name, c.Waist, c.WaistAt))
		}
		if c.Torn > l.Torn {
			out = append(out, fmt.Sprintf("%s: %.2f%% of edges torn", c.Name, 100*c.Torn))
		}
		for _, j := range c.Joints {
			if l.Section > 0 && j.Section < l.Section {
				out = append(out, fmt.Sprintf("%s: %s cross-section %.3f at %.3fs", c.Name, j.Name, j.Section, j.SectionAt))
			}
			if j.Open > l.Open {
				out = append(out, fmt.Sprintf("%s: %s surface notched in %.1f%% of directions at %.3fs", c.Name, j.Name, 100*j.Open, j.OpenAt))
			}
		}
		if l.OutOfBand {
			for _, f := range c.OutOfBand {
				out = append(out, fmt.Sprintf("%s: %s %c %.0f deg, %.0f past its band at %.3fs", c.Name, f.Joint, "xyz"[f.Axis], f.Angle, f.Excess, f.At))
			}
		}
	}
	return out
}

// Table formats the report as plain text: the static checks, then one row
// per clip with the waist, edges and the worst probed joint.
func (r AuditReport) Table() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d vertices, %d triangles, %d edges; %d components (%d closed, %d inside out); %d opposed normals; %d hip-crossing vertices\n",
		r.Vertices, r.Triangles, r.Edges, r.Components, r.Closed, r.InsideOut, r.OpposedNormals, r.HipCrossing)
	fmt.Fprintf(&b, "%-10s %6s %6s %7s %7s %7s %7s  %s\n", "clip", "frames", "waist", "torn%", "strain%", "longest", "short", "most notched joints / lowest sections")
	for _, c := range r.Clips {
		js := append([]JointAudit(nil), c.Joints...)
		sort.Slice(js, func(a, b int) bool { return js[a].Open > js[b].Open })
		var worst []string
		for _, j := range js[:min(3, len(js))] {
			if j.Open > 0 {
				worst = append(worst, fmt.Sprintf("%s %.0f%%", j.Name, 100*j.Open))
			}
		}
		sort.Slice(js, func(a, b int) bool { return js[a].Section < js[b].Section })
		for _, j := range js[:min(2, len(js))] {
			worst = append(worst, fmt.Sprintf("%s x%.2f", j.Name, j.Section))
		}
		fmt.Fprintf(&b, "%-10s %6d %6.3f %7.2f %7.2f %7.2f %7.2f  %s\n", c.Name, c.Frames, c.Waist, 100*c.Torn, 100*c.Strained, c.Longest, c.Shortest, strings.Join(worst, ", "))
	}
	return b.String()
}
