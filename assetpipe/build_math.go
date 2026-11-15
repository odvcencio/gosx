package assetpipe

import "encoding/base64"

func decodeBase64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

// mat4 is a column-major 4x4 matrix, the glTF layout.
type mat4 [16]float64

func identityMat4() mat4 {
	return mat4{1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1}
}

func (a mat4) mul(b mat4) mat4 {
	var out mat4
	for col := 0; col < 4; col++ {
		for row := 0; row < 4; row++ {
			var sum float64
			for k := 0; k < 4; k++ {
				sum += a[k*4+row] * b[col*4+k]
			}
			out[col*4+row] = sum
		}
	}
	return out
}

func (a mat4) point(p [3]float64) [3]float64 {
	return [3]float64{
		a[0]*p[0] + a[4]*p[1] + a[8]*p[2] + a[12],
		a[1]*p[0] + a[5]*p[1] + a[9]*p[2] + a[13],
		a[2]*p[0] + a[6]*p[1] + a[10]*p[2] + a[14],
	}
}

// localNodeMat4 returns a node's local transform from its matrix or its TRS.
func localNodeMat4(matrix, t, r, s []float64) mat4 {
	if len(matrix) == 16 {
		var m mat4
		copy(m[:], matrix)
		return m
	}
	tx, ty, tz := 0.0, 0.0, 0.0
	if len(t) == 3 {
		tx, ty, tz = t[0], t[1], t[2]
	}
	qx, qy, qz, qw := 0.0, 0.0, 0.0, 1.0
	if len(r) == 4 {
		qx, qy, qz, qw = r[0], r[1], r[2], r[3]
	}
	sx, sy, sz := 1.0, 1.0, 1.0
	if len(s) == 3 {
		sx, sy, sz = s[0], s[1], s[2]
	}
	return mat4{
		(1 - 2*(qy*qy+qz*qz)) * sx, (2 * (qx*qy + qz*qw)) * sx, (2 * (qx*qz - qy*qw)) * sx, 0,
		(2 * (qx*qy - qz*qw)) * sy, (1 - 2*(qx*qx+qz*qz)) * sy, (2 * (qy*qz + qx*qw)) * sy, 0,
		(2 * (qx*qz + qy*qw)) * sz, (2 * (qy*qz - qx*qw)) * sz, (1 - 2*(qx*qx+qy*qy)) * sz, 0,
		tx, ty, tz, 1,
	}
}
