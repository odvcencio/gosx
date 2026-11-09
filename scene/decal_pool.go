package scene

import "math"

// DecalPoolConfig sizes a DecalPool.
type DecalPoolConfig struct {
	// Name matches an entry of Decals.Pools. It also names the browser stream.
	Name string
	// Capacity is the most decals alive at once; at least 1, at most 4096. When
	// a spawn finds the pool full, it recycles the oldest decal.
	Capacity int
	// Life is the default lifetime in seconds. Zero means decals live until
	// recycled.
	Life float64
	// FadeOut is how long before the end of its life a decal fades to
	// transparent, in seconds. It is clamped to Life.
	FadeOut float64
}

// DecalSpawn describes one decal to add. Spawn takes it by value and does not
// allocate.
type DecalSpawn struct {
	// Material indexes Decals.Materials of the effect that draws this pool.
	Material int
	Position Vector3
	// Normal is the surface normal the decal faces; zero means +Y.
	Normal Vector3
	// Rotation spins the decal about Normal, in radians.
	Rotation float64
	// Size is width, height and projection depth; a zero axis means 1.
	Size    Vector3
	Frame   int
	Opacity float64 // 0..1; zero means 1
	// Tint is normalized RGBA; the zero value means white.
	Tint  [4]float32
	Layer int
	// Life overrides the pool's lifetime in seconds. Zero uses the pool default.
	Life float64
}

type decalSlot struct {
	rec  [DecalRecordStride]float32 // final record; rec[3] holds base opacity until packed
	base float32
	age  float32
	life float32 // 0 = forever
	fade float32
}

// DecalPool holds many short-lived projected decals (bullet holes, footprints,
// scorch marks) in fixed storage. Spawn, Update and Records do not allocate
// once the pool exists. A DecalPool is not safe for concurrent use.
type DecalPool struct {
	cfg      DecalPoolConfig
	slots    []decalSlot
	n        int
	packed   []float32
	dirty    bool
	revision uint64
	seq      float32
}

// MaxDecalPoolCapacity bounds DecalPoolConfig.Capacity.
const MaxDecalPoolCapacity = 4096

// NewDecalPool allocates the pool's storage once.
func NewDecalPool(cfg DecalPoolConfig) *DecalPool {
	cfg.Capacity = clampDecalInt(cfg.Capacity, 1, MaxDecalPoolCapacity)
	if cfg.Life < 0 || math.IsNaN(cfg.Life) {
		cfg.Life = 0
	}
	if cfg.FadeOut < 0 || math.IsNaN(cfg.FadeOut) {
		cfg.FadeOut = 0
	}
	if cfg.Life > 0 && cfg.FadeOut > cfg.Life {
		cfg.FadeOut = cfg.Life
	}
	return &DecalPool{cfg: cfg, slots: make([]decalSlot, cfg.Capacity), packed: make([]float32, cfg.Capacity*DecalRecordStride)}
}

// Name returns the configured stream name.
func (p *DecalPool) Name() string { return p.cfg.Name }

// Capacity returns the most decals the pool holds.
func (p *DecalPool) Capacity() int { return p.cfg.Capacity }

// Len returns the number of live decals.
func (p *DecalPool) Len() int { return p.n }

// Revision increases whenever Records would return different data.
func (p *DecalPool) Revision() uint64 { return p.revision }

// Spawn adds a decal. A full pool recycles its oldest decal first.
func (p *DecalPool) Spawn(s DecalSpawn) {
	if p.n == len(p.slots) {
		copy(p.slots, p.slots[1:])
		p.n--
	}
	size := s.Size
	if size.X <= 0 {
		size.X = 1
	}
	if size.Y <= 0 {
		size.Y = 1
	}
	if size.Z <= 0 {
		size.Z = 1
	}
	opacity := s.Opacity
	if opacity <= 0 || opacity > 1 {
		opacity = 1
	}
	tint := s.Tint
	if tint == ([4]float32{}) {
		tint = [4]float32{1, 1, 1, 1}
	}
	life := s.Life
	if life <= 0 {
		life = p.cfg.Life
	}
	p.seq++
	slot := &p.slots[p.n]
	slot.rec = packDecalRecord(s.Position, s.Normal, s.Rotation, size, s.Frame, opacity, tint, s.Layer, p.seq, s.Material)
	slot.base = float32(opacity)
	slot.age = 0
	slot.life = float32(life)
	slot.fade = float32(math.Min(p.cfg.FadeOut, life))
	p.n++
	p.dirty = true
}

// Update ages every decal by dt seconds, drops expired decals and applies
// fade-out. Order is kept, so the oldest decal stays first.
func (p *DecalPool) Update(dt float64) {
	if dt < 0 || math.IsNaN(dt) {
		dt = 0
	}
	step := float32(dt)
	kept := 0
	for i := 0; i < p.n; i++ {
		slot := p.slots[i]
		if slot.life > 0 {
			slot.age += step
			if slot.age >= slot.life {
				p.dirty = true
				continue
			}
			opacity := slot.base
			if slot.fade > 0 {
				if left := slot.life - slot.age; left < slot.fade {
					opacity *= left / slot.fade
					p.dirty = true
				}
			}
			slot.rec[3] = opacity
		}
		p.slots[kept] = slot
		kept++
	}
	p.n = kept
}

// Records returns the live decals as Len()*DecalRecordStride float32s, oldest
// first. The slice is reused: copy it to keep it past the next Spawn or Update.
func (p *DecalPool) Records() []float32 {
	if p.dirty {
		for i := 0; i < p.n; i++ {
			copy(p.packed[i*DecalRecordStride:], p.slots[i].rec[:])
		}
		p.dirty = false
		p.revision++
	}
	return p.packed[:p.n*DecalRecordStride]
}

// Clear removes every decal.
func (p *DecalPool) Clear() {
	if p.n == 0 {
		return
	}
	p.n = 0
	p.dirty = true
}
