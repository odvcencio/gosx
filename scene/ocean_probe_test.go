package scene

import "testing"

func TestOceanProbeReadingNormalY(t *testing.T) {
	var r OceanProbeReading
	r.NormalX[0], r.NormalZ[0] = 0.3, 0.4
	if y := r.NormalY(0); y < 0.8660 || y > 0.8661 {
		t.Fatalf("NormalY = %v, want sqrt(0.75)", y)
	}
	r.NormalX[1] = 1.2 // a normal that rounds past unit length must not give NaN
	if y := r.NormalY(1); y != 0 {
		t.Fatalf("NormalY past unit length = %v, want 0", y)
	}
	if MaxOceanProbePoints != 64 || len(r.Height) != MaxOceanProbePoints {
		t.Fatal("the probe holds 64 points")
	}
}
