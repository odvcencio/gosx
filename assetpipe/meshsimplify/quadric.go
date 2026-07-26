// Package meshsimplify reduces triangle meshes with the quadric error metric
// of Garland and Heckbert. It runs in pure Go and reports the geometric error
// it introduced, so a build step can prove what it traded away.
package meshsimplify

import "math"

// quadric is a symmetric 4x4 error matrix stored as its ten unique entries.
// The entries follow the row-major upper triangle:
//
//	q[0] q[1] q[2] q[3]
//	     q[4] q[5] q[6]
//	          q[7] q[8]
//	               q[9]
type quadric [10]float64

// planeQuadric builds the quadric of the plane ax+by+cz+d = 0, scaled by
// weight. The scale carries the triangle area, so a large triangle counts for
// more than a sliver.
func planeQuadric(a, b, c, d, weight float64) quadric {
	return quadric{
		weight * a * a, weight * a * b, weight * a * c, weight * a * d,
		weight * b * b, weight * b * c, weight * b * d,
		weight * c * c, weight * c * d,
		weight * d * d,
	}
}

func (q quadric) add(o quadric) quadric {
	var out quadric
	for i := range q {
		out[i] = q[i] + o[i]
	}
	return out
}

// evaluate returns the squared distance the quadric assigns to a point.
func (q quadric) evaluate(x, y, z float64) float64 {
	return q[0]*x*x + 2*q[1]*x*y + 2*q[2]*x*z + 2*q[3]*x +
		q[4]*y*y + 2*q[5]*y*z + 2*q[6]*y +
		q[7]*z*z + 2*q[8]*z +
		q[9]
}

// optimum solves for the point of least error. It returns false when the
// matrix is close to singular, which happens on flat or symmetric regions.
func (q quadric) optimum() (float64, float64, float64, bool) {
	a11, a12, a13 := q[0], q[1], q[2]
	a22, a23 := q[4], q[5]
	a33 := q[7]
	b1, b2, b3 := -q[3], -q[6], -q[8]

	det := a11*(a22*a33-a23*a23) - a12*(a12*a33-a23*a13) + a13*(a12*a23-a22*a13)
	scale := math.Abs(a11) + math.Abs(a22) + math.Abs(a33)
	if scale == 0 || math.Abs(det) < 1e-12*scale*scale*scale {
		return 0, 0, 0, false
	}
	invDet := 1 / det
	x := (b1*(a22*a33-a23*a23) - a12*(b2*a33-a23*b3) + a13*(b2*a23-a22*b3)) * invDet
	y := (a11*(b2*a33-a23*b3) - b1*(a12*a33-a23*a13) + a13*(a12*b3-b2*a13)) * invDet
	z := (a11*(a22*b3-b2*a23) - a12*(a12*b3-b2*a13) + b1*(a12*a23-a22*a13)) * invDet
	if math.IsNaN(x) || math.IsNaN(y) || math.IsNaN(z) {
		return 0, 0, 0, false
	}
	return x, y, z, true
}
