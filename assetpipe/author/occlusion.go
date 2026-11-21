package author

import (
	"math"
	"runtime"
	"sync"
)

// Occlusion sets up VertexOcclusion: how far a vertex looks for cover and
// how finely.
type Occlusion struct {
	// Reach is how far a ray looks for cover, in model units: local creases,
	// not the whole body.
	Reach float64
	// Rays is the number of directions each vertex casts, spread over its
	// hemisphere with a cosine weight. Default 48.
	Rays int
	// Lift moves each ray's origin off the surface along the normal, so a
	// vertex does not hit its own triangles. Default .003.
	Lift float64
	// Margin skips a triangle whose first corner is further than
	// Reach+Margin from the origin, a cheap reject. Default .15.
	Margin float64
}

// VertexOcclusion bakes ambient occlusion at the chosen vertices of p: the
// share of cosine-weighted directions about each vertex's normal that travel
// Reach without striking one of the triangles tris. A vertex not chosen
// stays 0. Vertices are computed in parallel; each result depends on its
// vertex alone, so the bake is the same on every run.
func VertexOcclusion(p, normals []Vec3, tris [][3]int, chosen []bool, o Occlusion) []float64 {
	if o.Rays <= 0 {
		o.Rays = 48
	}
	if o.Lift == 0 {
		o.Lift = .003
	}
	if o.Margin == 0 {
		o.Margin = .15
	}
	dirs := Hemisphere(o.Rays)
	ao := make([]float64, len(p))
	var wg sync.WaitGroup
	next := make(chan int, 256)
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				ao[i] = openShare(p, tris, p[i], normals[i], dirs, o)
			}
		}()
	}
	for i := range p {
		if chosen[i] {
			next <- i
		}
	}
	close(next)
	wg.Wait()
	return ao
}

// Hemisphere returns n directions over the +Z hemisphere, denser toward the
// pole as a cosine weight wants (a Fibonacci spiral).
func Hemisphere(n int) []Vec3 {
	out := make([]Vec3, n)
	golden := math.Pi * (3 - math.Sqrt(5))
	for i := range out {
		r := math.Sqrt((float64(i) + .5) / float64(n))
		a := float64(i) * golden
		out[i] = Vec3{r * math.Cos(a), r * math.Sin(a), math.Sqrt(math.Max(0, 1-r*r))}
	}
	return out
}

// openShare is the share of dirs, turned onto the normal n at at, that reach
// o.Reach without a hit.
func openShare(p []Vec3, tris [][3]int, at, n Vec3, dirs []Vec3, o Occlusion) float64 {
	t := Vec3{1, 0, 0}
	if math.Abs(n[0]) > .9 {
		t = Vec3{0, 1, 0}
	}
	tu := n.Cross(t).Unit()
	tv := n.Cross(tu)
	origin := at.Add(n.Scale(o.Lift))
	open := 0
	for _, d := range dirs {
		ray := tu.Scale(d[0]).Add(tv.Scale(d[1])).Add(n.Scale(d[2]))
		if !RayHits(p, tris, origin, ray, o.Reach, o.Margin) {
			open++
		}
	}
	return float64(open) / float64(len(dirs))
}

// RayHits reports whether a ray from o along the unit vector d meets one of
// the triangles within reach (Moller-Trumbore). A triangle whose first
// corner lies further than reach+margin from o is skipped unchecked.
func RayHits(p []Vec3, tris [][3]int, o, d Vec3, reach, margin float64) bool {
	for _, t := range tris {
		a, b, c := p[t[0]], p[t[1]], p[t[2]]
		if a.Sub(o).Len() > reach+margin {
			continue
		}
		e1, e2 := b.Sub(a), c.Sub(a)
		h := d.Cross(e2)
		det := e1.Dot(h)
		if math.Abs(det) < 1e-12 {
			continue
		}
		f := 1 / det
		s := o.Sub(a)
		u := f * s.Dot(h)
		if u < 0 || u > 1 {
			continue
		}
		q := s.Cross(e1)
		v := f * d.Dot(q)
		if v < 0 || u+v > 1 {
			continue
		}
		if dist := f * e2.Dot(q); dist > 1e-5 && dist < reach {
			return true
		}
	}
	return false
}
