package headless

import (
	"image/color"
	"math"
)

// clipLine clips all six homogeneous planes before perspective division.
func clipLine(a, b clipVertex) (clipVertex, clipVertex, bool) {
	lo, hi := float32(0), float32(1)
	for axis := 0; axis < 3; axis++ {
		for _, sign := range []float32{-1, 1} {
			da, db := a.clip[3]+sign*a.clip[axis], b.clip[3]+sign*b.clip[axis]
			if math.IsNaN(float64(da)) || math.IsInf(float64(da), 0) || math.IsNaN(float64(db)) || math.IsInf(float64(db), 0) {
				return a, b, false
			}
			if da < 0 && db < 0 {
				return a, b, false
			}
			if da < 0 {
				lo = max(lo, da/(da-db))
			}
			if db < 0 {
				hi = min(hi, da/(da-db))
			}
		}
	}
	if lo > hi {
		return a, b, false
	}
	lerp := func(t float32) clipVertex {
		v := a
		for i := 0; i < 4; i++ {
			v.clip[i] = a.clip[i] + t*(b.clip[i]-a.clip[i])
			v.color[i] = a.color[i] + t*(b.color[i]-a.color[i])
		}
		return v
	}
	return lerp(lo), lerp(hi), true
}

func (r *RenderPassEncoder) rasterizeWorldLines(count, first int) {
	target, ok := r.colorRasterTarget()
	if !ok {
		return
	}
	m := r.activeMVP()
	for i := 0; i+1 < count; i += 2 {
		var v [2]clipVertex
		valid := true
		for j := 0; j < 2; j++ {
			p, ok := readVec3(r.vertexBuffers[0], first+i+j)
			if !ok {
				valid = false
				break
			}
			v[j].clip = transformToClip(m, p)
			v[j].color = [4]float32{1, 1, 1, 1}
			buf := r.vertexBuffers[1]
			offset := (first + i + j) * 16
			if buf != nil && offset >= 0 && offset+16 <= len(buf.data) {
				v[j].color = readVec4At(buf.data, offset)
			}
		}
		if !valid {
			continue
		}
		a, b, ok := clipLine(v[0], v[1])
		if !ok {
			continue
		}
		x0, y0, z0, ok0 := projectClip(a.clip, target.width, target.height)
		x1, y1, z1, ok1 := projectClip(b.clip, target.width, target.height)
		if !ok0 || !ok1 {
			continue
		}
		steps := int(math.Ceil(math.Max(math.Abs(float64(x1-x0)), math.Abs(float64(y1-y0)))))
		if steps == 0 {
			continue
		}
		lastX, lastY := -1, -1
		// Sample a half-open parameter interval, writing each pixel once per
		// segment. Separate segments sharing a pixel blend in draw order.
		for s := 0; s < steps; s++ {
			t := float32(s) / float32(steps)
			x, y := int(math.Floor(float64(x0+(x1-x0)*t))), int(math.Floor(float64(y0+(y1-y0)*t)))
			if x < 0 || y < 0 || x >= target.width || y >= target.height || (x == lastX && y == lastY) {
				continue
			}
			lastX, lastY = x, y
			depth := z0 + (z1-z0)*t
			if !depthPasses(target, x, y, depth) {
				continue
			}
			wa, wb := (1-t)/a.clip[3], t/b.clip[3]
			sum := wa + wb
			if sum <= 0 {
				continue
			}
			var c [4]float32
			for k := 0; k < 4; k++ {
				c[k] = (a.color[k]*wa + b.color[k]*wb) / sum
			}
			writeRasterColor(target, x, y, color.RGBA{R: clampByte(c[0]), G: clampByte(c[1]), B: clampByte(c[2]), A: clampByte(c[3])})
			if target.id != nil {
				writeTextureUint32(target.id, target.idLayer, x, y, 0)
			}
		}
	}
}
