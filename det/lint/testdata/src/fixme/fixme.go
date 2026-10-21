//gosx:deterministic

package fixme

type Meters float64

func f(x, y, z float64) float64 {
	return x*y + z // want `fused-multiply-add`
}

func g(a, b Meters) Meters {
	return a*b - 1 // want `fused-multiply-add`
}
