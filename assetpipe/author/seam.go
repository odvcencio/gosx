package author

import (
	"math"

	"m31labs.dev/gosx/assetpipe/gltfedit"
)

// A textured surface of revolution needs a seam: the first column of
// vertices repeated at the end with u = 1, so a painted design runs once
// round the body instead of the last strip of triangles squeezing the whole
// texture backwards between u = (n-1)/n and u = 0. The two columns share
// positions, and their normals are welded so the seam does not show in the
// shading.

// SeamStack lofts elliptical rings like RingStack but with n+1 vertices per
// ring, the last a copy of the first with u = 1, and v by distance along the
// profile instead of by ring count, so a band painted at v is level on the
// body. Vertex j of a ring sits at angle 2*pi*j/n, at (sin, cos) on (X, Z).
// Both ends are capped with fans.
func (m *Mesh) SeamStack(name string, joint, cell int, rings []Ring, n int) Shell {
	cols := n + 1
	start := m.Begin(name, joint, len(rings), cols)
	along := ProfileV(rings)
	for k, r := range rings {
		for j := 0; j <= n; j++ {
			a := 2 * math.Pi * float64(j%n) / float64(n)
			m.Vertex(Vec3{r.Centre[0] + r.RX*math.Sin(a), r.Centre[1], r.Centre[2] + r.RZ*math.Cos(a)}, joint, cell, float64(j)/float64(n), along[k])
		}
	}
	m.SeamQuads(start, len(rings), cols)
	top := start + (len(rings)-1)*cols
	for j := 1; j < n-1; j++ {
		m.Tri = append(m.Tri, [3]int{start, start + j + 1, start + j}, [3]int{top, top + j, top + j + 1})
	}
	m.End()
	return m.Shells[len(m.Shells)-1]
}

// SeamQuads joins consecutive rings of cols vertices each, where the last
// column repeats the first, with the same winding as Quads.
func (m *Mesh) SeamQuads(start, rings, cols int) {
	for k := 0; k < rings-1; k++ {
		for j := 0; j < cols-1; j++ {
			a := start + k*cols + j
			d := a + 1
			m.Tri = append(m.Tri, [3]int{a, d, d + cols}, [3]int{a, d + cols, a + cols})
		}
	}
}

// ProfileV returns each ring's v: its distance along the profile through
// the rings' centres and their mean radius, from 0 at the first ring to 1 at
// the last.
func ProfileV(rings []Ring) []float64 {
	out := make([]float64, len(rings))
	total := 0.
	for k := 1; k < len(rings); k++ {
		a, b := rings[k-1], rings[k]
		dr := (b.RX+b.RZ)/2 - (a.RX+a.RZ)/2
		total += math.Hypot(b.Centre.Sub(a.Centre).Len(), dr)
		out[k] = total
	}
	for k := range out {
		if total > 0 {
			out[k] /= total
		}
	}
	return out
}

// seamGroups returns the vertices that share an exact position inside one
// shell: the columns a seam repeats.
func (m *Mesh) seamGroups() [][]int {
	var groups [][]int
	for _, s := range m.Shells {
		at := map[Vec3][]int{}
		for i := s.First; i < s.End && i < len(m.P); i++ {
			at[m.P[i]] = append(at[m.P[i]], i)
		}
		for i := s.First; i < s.End && i < len(m.P); i++ {
			if g := at[m.P[i]]; len(g) > 1 && g[0] == i {
				groups = append(groups, g)
			}
		}
	}
	return groups
}

// weldNormals gives every vertex of a group the mean of the group's normals.
func weldNormals(n []Vec3, groups [][]int) []Vec3 {
	for _, g := range groups {
		var sum Vec3
		for _, i := range g {
			sum = sum.Add(n[i])
		}
		sum = sum.Scale(1 / math.Max(1e-7, sum.Len()))
		for _, i := range g {
			n[i] = sum
		}
	}
	return n
}

// Image is an encoded picture for a model. One with a URI is linked; one
// with data and no URI is embedded in the binary buffer.
type Image struct {
	URI  string
	Data []byte
	MIME string
}

// SetImages stores the model's images in order: image 0 is the base colour
// atlas, image 1 the metallic-roughness map when the material reads one.
func SetImages(d *gltfedit.Document, images ...Image) error {
	list := make([]map[string]any, 0, len(images))
	for _, im := range images {
		if im.URI != "" {
			list = append(list, map[string]any{"uri": im.URI, "mimeType": im.MIME})
			continue
		}
		list = append(list, map[string]any{"bufferView": d.AddBufferView(im.Data, 0), "mimeType": im.MIME})
	}
	return d.SetSection("images", list)
}
