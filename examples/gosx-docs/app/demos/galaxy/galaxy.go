package galaxy

import (
	"math"

	"github.com/odvcencio/gosx/scene"
)

// Constants matching the three.js m31labs reference exactly.
const (
	starCount           = 2000
	starSpread          = 2000.0
	galaxyParticleCount = 800
	galaxyRadius        = 200.0
)

func generateStarField() scene.Points {
	positions := make([]scene.Vector3, starCount)
	sizes := make([]float64, starCount)
	for i := range positions {
		positions[i] = scene.Vec3(
			(rand(i*3+0)-0.5)*starSpread,
			(rand(i*3+1)-0.5)*starSpread,
			(rand(i*3+2)-0.5)*starSpread,
		)
		sizes[i] = rand(i*3+7)*2.0 + 0.5 // [0.5, 2.5]
	}
	return scene.Points{
		ID:          "stars",
		Count:       starCount,
		Positions:   positions,
		Sizes:       sizes,
		Color:       "#ffffff",
		Size:        1.5,
		Opacity:     0.8,
		BlendMode:   scene.BlendAdditive,
		DepthWrite:  false,
		Attenuation: true,
		Spin:        scene.Euler{Y: 0.01, X: 0.005},
	}
}

func generateGalaxy() scene.Points {
	positions := make([]scene.Vector3, galaxyParticleCount)
	colors := make([]string, galaxyParticleCount)

	for i := 0; i < galaxyParticleCount; i++ {
		radius := rand(i*5+0) * galaxyRadius
		armAngle := float64(i%2)*math.Pi + (radius/galaxyRadius)*math.Pi*3
		scatter := (rand(i*5+1) - 0.5) * (radius * 0.3)

		x := math.Cos(armAngle)*radius + scatter
		y := (rand(i*5+2) - 0.5) * (galaxyRadius * 0.05)
		z := math.Sin(armAngle)*radius + (rand(i*5+3)-0.5)*(radius*0.3)

		positions[i] = scene.Vec3(x, y, z)

		t := radius / galaxyRadius
		colors[i] = lerpHexColor("#e8e8e8", "#4b0082", t)
	}

	return scene.Points{
		ID:          "galaxy",
		Count:       galaxyParticleCount,
		Positions:   positions,
		Colors:      colors,
		Size:        2,
		Opacity:     0.45,
		BlendMode:   scene.BlendAdditive,
		DepthWrite:  false,
		Attenuation: true,
		// three.js pivot: position=(100,50,-600), rotation.x=-0.5, rotation.z=0.3
		// three.js animates pivot.rotation.y = elapsed * 0.05
		// Euler XYZ order: Rz(0.3) * Ry(0.05*t) * Rx(-0.5) — exact match via combined approach
		Position: scene.Vec3(100, 50, -600),
		Rotation: scene.Euler{X: -0.5, Z: 0.3},
		Spin:     scene.Euler{Y: 0.05},
	}
}

func GalaxyScene() scene.Props {
	return scene.Props{
		Width:      920,
		Height:     560,
		Label:      "M31 Galaxy",
		Background: "#050008",
		Environment: scene.Environment{
			FogColor:   "#050008",
			FogDensity: 0.0004,
		},
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 0, 500),
			FOV:      60,
			Near:     1,
			Far:      3000,
		},
		Graph: scene.NewGraph(generateStarField(), generateGalaxy()),
	}
}

func rand(seed int) float64 {
	x := math.Sin(float64(seed)*12.9898+78.233) * 43758.5453
	return x - math.Floor(x)
}

func lerpHexColor(from, to string, t float64) string {
	r1, g1, b1 := parseHex(from)
	r2, g2, b2 := parseHex(to)
	r := int(float64(r1) + (float64(r2)-float64(r1))*t)
	g := int(float64(g1) + (float64(g2)-float64(g1))*t)
	b := int(float64(b1) + (float64(b2)-float64(b1))*t)
	return "#" + hexByte(r) + hexByte(g) + hexByte(b)
}

func parseHex(hex string) (int, int, int) {
	if len(hex) == 7 && hex[0] == '#' {
		hex = hex[1:]
	}
	if len(hex) != 6 {
		return 0, 0, 0
	}
	return hexVal(hex[0])<<4 | hexVal(hex[1]),
		hexVal(hex[2])<<4 | hexVal(hex[3]),
		hexVal(hex[4])<<4 | hexVal(hex[5])
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return 0
}

func hexByte(v int) string {
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	const digits = "0123456789abcdef"
	return string([]byte{digits[v>>4], digits[v&0x0f]})
}
