package scene

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"
)

// ParticleBurst is a finite, decorative event using the existing particle
// emitter, forces, and sprite material. Delay is measured from receipt.
// Each ID owns one burst per mount; repeating it replaces the previous burst.
type ParticleBurst struct {
	ID       string
	Count    int
	Emitter  ParticleEmitter
	Forces   []ParticleForce
	Material ParticleMaterial
	Bounds   float64
	Delay    time.Duration
}

type particleBurstJSON struct {
	Version   int                `json:"version"`
	ID        string             `json:"id"`
	Delay     float64            `json:"delay"`
	Duration  float64            `json:"duration"`
	Particles ComputeParticlesIR `json:"particles"`
}

// Duration bounds the delay, emission jitter, and longest randomized lifetime.
// Browser playback removes the emitter at this deadline, releasing its work.
func (b ParticleBurst) Duration() time.Duration {
	return b.Delay + time.Duration(math.Ceil((math.Max(.08, b.Emitter.Lifetime*.12)+b.Emitter.Lifetime*1.56)*float64(time.Second)))
}

// Validate bounds event allocations and rejects unsupported or non-finite data.
func (b ParticleBurst) Validate() error {
	if strings.TrimSpace(b.ID) == "" || b.ID != strings.TrimSpace(b.ID) || b.Count < 1 || b.Count > 4096 || b.Delay < 0 || b.Delay > time.Minute ||
		b.Emitter.Lifetime <= 0 || b.Emitter.Lifetime > 10 || len(b.Forces) > 8 || b.Bounds < 0 {
		return fmt.Errorf("scene particle burst needs an ID, 1..4096 particles, a 0..60s delay, and a lifetime in (0,10s]")
	}
	switch b.Emitter.Kind {
	case "", "point", "sphere", "disc", "spiral":
	default:
		return fmt.Errorf("unsupported particle burst emitter %q", b.Emitter.Kind)
	}
	values := []float64{b.Bounds, b.Emitter.Position.X, b.Emitter.Position.Y, b.Emitter.Position.Z,
		b.Emitter.Rotation.X, b.Emitter.Rotation.Y, b.Emitter.Rotation.Z, b.Emitter.Spin.X, b.Emitter.Spin.Y, b.Emitter.Spin.Z,
		b.Emitter.Radius, b.Emitter.Rate, b.Emitter.Lifetime, b.Emitter.Wind, b.Emitter.Scatter,
		b.Material.Size, b.Material.SizeEnd, b.Material.Opacity, b.Material.OpacityEnd, b.Material.MinPixelSize, b.Material.MaxPixelSize}
	for _, force := range b.Forces {
		if strings.TrimSpace(force.Kind) == "" {
			return fmt.Errorf("particle burst force needs a kind")
		}
		values = append(values, force.Strength, force.Direction.X, force.Direction.Y, force.Direction.Z, force.Frequency)
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("particle burst values must be finite")
		}
	}
	if b.Emitter.Radius < 0 || b.Material.Size < 0 || b.Material.SizeEnd < 0 || b.Material.MinPixelSize < 0 || b.Material.MaxPixelSize < 0 ||
		b.Material.Opacity < 0 || b.Material.Opacity > 1 || b.Material.OpacityEnd < 0 || b.Material.OpacityEnd > 1 {
		return fmt.Errorf("particle burst sizes must be nonnegative and opacity must be in [0,1]")
	}
	return nil
}

func (b ParticleBurst) record() ComputeParticlesIR {
	emitter := b.Emitter
	if emitter.Kind == "" {
		emitter.Kind = "point"
	}
	emitter.Once = true
	ir := NewGraph(ComputeParticles{ID: "gosx-burst/" + b.ID, Count: b.Count, Emitter: emitter, Forces: b.Forces, Material: b.Material, Bounds: b.Bounds}).SceneIR()
	return ir.ComputeParticles[0]
}

// MarshalJSON writes the versioned plan with seconds-based timing. Once is
// always enabled; a burst never converts into a persistent emitter.
func (b ParticleBurst) MarshalJSON() ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Version   int     `json:"version"`
		ID        string  `json:"id"`
		Delay     float64 `json:"delay"`
		Duration  float64 `json:"duration"`
		Particles any     `json:"particles"`
	}{1, b.ID, b.Delay.Seconds(), b.Duration().Seconds(), b.wireRecord()})
}

func (b ParticleBurst) wireRecord() any {
	record := b.record()
	// Zero endpoints are meaningful for event fades. The ordinary scene IR
	// omits them, so retain them explicitly in this finite playback plan.
	return struct {
		ComputeParticlesIR
		Material any `json:"material"`
	}{record, struct {
		ParticleMaterialIR
		SizeEnd    float64 `json:"sizeEnd"`
		OpacityEnd float64 `json:"opacityEnd"`
	}{record.Material, b.Material.SizeEnd, b.Material.OpacityEnd}}
}

// Sample builds a particle command from the host's current authoritative scene
// and elapsed receipt time. It preserves other point, compute, and water layers.
// Native hosts own the clock and suppress decorative bursts for reduced motion.
func (b ParticleBurst) Sample(base SceneIR, seconds float64) (Command, error) {
	if err := b.Validate(); err != nil {
		return Command{}, err
	}
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return Command{}, fmt.Errorf("particle burst sample time must be finite")
	}
	record := b.record()
	compute := make([]any, 0, len(base.ComputeParticles)+1)
	for _, existing := range base.ComputeParticles {
		if existing.ID != record.ID {
			compute = append(compute, existing)
		}
	}
	if seconds >= b.Delay.Seconds() && seconds < b.Duration().Seconds() {
		compute = append(compute, b.wireRecord())
	}
	return Command{Kind: CommandSetParticles, Data: map[string]any{"points": base.Points, "computeParticles": compute, "waterSystems": base.WaterSystems}}, nil
}
