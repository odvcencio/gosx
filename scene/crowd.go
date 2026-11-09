package scene

import (
	"errors"
	"fmt"
	"strings"

	"m31labs.dev/gosx/det"
)

// Crowd is a seated or standing audience for a stadium, an arena or a concert:
// tens of thousands of animated fans drawn by the GPU. Put it on
// Environment.Crowds.
//
// A crowd is described, not listed: each CrowdStand gives the shape of a
// seating bank (rows, rake, aisles and sections), and Layout expands the seats
// from that description and Seed. The browser expands the same seats from the
// same description, so the page carries about a kilobyte per crowd instead of
// one record per fan. Equal descriptions and seeds always give equal seats.
//
// Fans animate on the GPU from a small library of actions (CrowdAction). A
// CrowdDirector changes what each section does at run time: its action, its
// excitement, synchronized gestures on a shared clock, travelling waves and
// raised props. Only those changes cross from Go to the browser, never per-fan
// data. See docs/scene3d-crowd.md.
type Crowd struct {
	// ID names the crowd. Directors and crowd frames address it by ID.
	// Empty means "crowd".
	ID string
	// Seed makes the layout reproducible: which seats are filled, each fan's
	// colours, prop, body variation and animation phase.
	Seed uint32
	// Position and Rotation (radians around Y) place the crowd frame in the
	// world. Stand shapes are given in the crowd frame; its origin is the
	// centre of the field or stage, which fans face and waves travel around.
	Position Vector3
	Rotation float64
	// Stands are the seating banks. A crowd holds at most CrowdMaxSections
	// sections and CrowdMaxSeats occupied seats in all.
	Stands []CrowdStand
	// Colors and Accents are the default shirt and accent palettes for
	// sections that set none. Nil gives a neutral mix.
	Colors  []CrowdColor
	Accents []CrowdColor
	// Props is the default prop mix for sections that set none.
	Props CrowdPropMix
	// Fill is the default fraction of seats that hold a fan, in (0, 1]. Zero
	// means 1.
	Fill float64
	// Fan sets the body of every fan.
	Fan CrowdFan
	// LOD sets the distance level of detail.
	LOD CrowdLOD
	// Material sets the fan and prop surfaces.
	Material CrowdMaterial
	// ReceiveShadow lets fans darken under the scene's shadows. Nil means
	// true. Fans never cast shadows.
	ReceiveShadow *bool
}

// CrowdShape selects how a CrowdStand's front edge runs in plan view.
type CrowdShape string

const (
	// CrowdShapeLine is a straight stand from Start to End.
	CrowdShapeLine CrowdShape = "line"
	// CrowdShapeArc is a stand on a circular arc around Center.
	CrowdShapeArc CrowdShape = "arc"
	// CrowdShapeBowl is a closed or open rounded rectangle around Center: a
	// stadium or arena bowl. HalfWidth = HalfDepth = CornerRadius makes a
	// circle.
	CrowdShapeBowl CrowdShape = "bowl"
	// CrowdShapeFloor is a flat standing area (a concert floor or a terrace)
	// filled in a jittered grid at Density fans per square metre.
	CrowdShapeFloor CrowdShape = "floor"
)

// CrowdStand is one seating bank: a front edge in plan view, rows that step
// back and up from it, and sections separated by aisles. Fans face the field:
// toward the bowl or arc centre, or toward Facing for lines and floors.
//
// Positions are in the crowd frame, in metres. Y values in Start, End, Center
// and Facing are ignored; FrontHeight sets the height.
type CrowdStand struct {
	// ID names the stand. Empty means "stand-<n>".
	ID string
	// Shape is the front edge. Empty means CrowdShapeBowl when HalfWidth is
	// set, else CrowdShapeLine.
	Shape CrowdShape

	// Line: the front edge runs from Start to End.
	Start, End Vector3
	// Facing is the point line and floor fans face. Default: the crowd
	// origin.
	Facing Vector3

	// Arc and bowl: the centre in plan view.
	Center Vector3
	// Arc: front-row radius and the angles (radians from +X toward +Z) the
	// stand spans, from StartAngle to EndAngle.
	Radius               float64
	StartAngle, EndAngle float64

	// Bowl and floor: the half extents of the front-row rectangle (bowl) or of
	// the standing area (floor) along X and Z.
	HalfWidth, HalfDepth float64
	// Bowl: the corner radius, clamped to [0.5, min(HalfWidth, HalfDepth)].
	// Zero means a quarter of the smaller half extent.
	CornerRadius float64
	// Bowl: LoopStart and LoopEnd cut an open bowl (a horseshoe) from the
	// loop, as fractions of its length. The loop starts at the middle of the
	// +X side and runs toward +Z. Equal values (the default) keep the whole
	// loop.
	LoopStart, LoopEnd float64

	// Floor: fans per square metre. Zero means 2.
	Density float64

	// Rows is the number of seat rows. Zero means 20. Floors ignore it.
	Rows int
	// RowDepth is the tread depth of one row in metres. Zero means 0.85.
	RowDepth float64
	// RowRise is how much each row rises above the one in front, in metres.
	// Zero means 0.42 (a rake of about 26 degrees). Floors ignore it.
	RowRise float64
	// FrontHeight is the height of the first row's floor. Zero means 1 for
	// seated stands and 0 for floors.
	FrontHeight float64
	// SeatWidth is the seat pitch along a row in metres. Zero means 0.5.
	SeatWidth float64
	// AisleWidth is the gap between sections in metres. Zero means 1.2; a
	// negative value means no aisle. Floors ignore it.
	AisleWidth float64

	// Sections split the stand along its front edge, in order. Nil gives one
	// section with the crowd's defaults.
	Sections []CrowdSection
}

// CrowdSection is one block of seats between two aisles: the unit a
// CrowdDirector drives.
type CrowdSection struct {
	// ID names the section; it must be unique in the crowd. Empty means
	// "<stand id>-<n>".
	ID string
	// Span is the section's share of the stand's front edge, relative to the
	// other sections of the stand. Zero means 1.
	Span float64
	// Colors are the shirt colours (CSS hex), at most four, chosen per fan by
	// weight. Nil uses Crowd.Colors.
	Colors []CrowdColor
	// Accents are the hat, scarf, towel and banner colours, at most four. Nil
	// uses Crowd.Accents.
	Accents []CrowdColor
	// Props is the prop mix. Nil uses Crowd.Props.
	Props *CrowdPropMix
	// Fill is the fraction of seats that hold a fan. Zero uses Crowd.Fill.
	Fill float64
	// Standing sections idle on their feet instead of seated.
	Standing bool
	// Action and Excitement are the state the section starts in.
	Action     CrowdAction
	Excitement float64
	// NoWave keeps the section seated while waves pass.
	NoWave bool
}

// CrowdColor is one palette entry.
type CrowdColor struct {
	// Color is a CSS hex colour such as "#c8102e".
	Color string
	// Weight is the entry's share of fans, relative to the other entries.
	// Zero means 1.
	Weight float64
}

// CrowdPropMix is the share of fans holding each prop, from 0 to 1; the rest
// hold none. Props take the fan's accent colour, and show while the section's
// props are raised (CrowdDirector.RaiseProps; raised by default).
type CrowdPropMix struct {
	// Lights are handheld lights (phones, lanterns) that glow.
	Lights float64
	// Scarves are held overhead in both hands.
	Scarves float64
	// Towels are waved in one hand.
	Towels float64
	// Banners are small flags on a pole.
	Banners float64
}

// CrowdProp names the prop one fan holds.
type CrowdProp uint8

const (
	CrowdPropNone CrowdProp = iota
	CrowdPropLight
	CrowdPropScarf
	CrowdPropTowel
	CrowdPropBanner
)

// CrowdFan sets the body every fan of a crowd shares: a built-in low-poly
// figure whose actions are baked into a bone-palette texture.
type CrowdFan struct {
	// Height is the standing height in metres. Zero means 1.75. Each fan
	// varies by up to 6% from its seed.
	Height float64
	// SkinTones are the head and hand colours (CSS hex), chosen per fan by
	// its seed. Nil gives a range of six tones.
	SkinTones []string
	// Legs is the trouser colour. Empty means "#2b2f3a".
	Legs string
}

// CrowdLOD chooses how much detail each fan gets from its height on screen.
// Fans are culled and graded in blocks of up to CrowdBlockSeats seats.
type CrowdLOD struct {
	// FullPixels: fans at least this many pixels tall draw the full mesh with
	// blended, interpolated animation. Zero means 64.
	FullPixels float64
	// LowPixels: fans at least this tall draw the low mesh with
	// nearest-frame animation; smaller fans draw as camera-facing impostors.
	// Zero means 20.
	LowPixels float64
	// MaxFull caps how many fans draw the full mesh in one frame; the farthest
	// qualifying blocks drop to the low mesh. Zero means 8000.
	MaxFull int
}

// CrowdMaterial sets the surfaces of fans and props. Both draw with the
// renderer's standard lighting: scene lights, received shadows, fog, exposure
// and tone mapping. Fan colours multiply the base colour.
type CrowdMaterial struct {
	// Roughness of skin and clothing. Zero means 0.85.
	Roughness float64
	// PropRoughness of scarves, towels and banners. Zero means 0.7.
	PropRoughness float64
	// LightGlow is the emissive strength of handheld lights. Zero means 1.5.
	LightGlow float64
	// Surface optionally names a Selena surface material (a CustomMaterial
	// whose material returns a Surface) that shades fan bodies through the
	// renderer's material hook; see docs/scene3d-crowd.md for its status.
	Surface *CustomMaterial
}

// Crowd limits. The browser enforces the same limits.
const (
	CrowdMaxSections = 1024
	CrowdMaxSeats    = 200000
	CrowdMaxColors   = 4
	// CrowdBlockSeats is the most fans one culling and LOD block holds.
	CrowdBlockSeats = 128
)

// CrowdIR is the browser record for one Crowd: the normalized description the
// browser expands seats from. Weights and fill fractions are already integer
// thresholds out of 1<<24, so both sides choose identically.
type CrowdIR struct {
	ID            string           `json:"id"`
	Seed          uint32           `json:"seed"`
	X             float64          `json:"x,omitempty"`
	Y             float64          `json:"y,omitempty"`
	Z             float64          `json:"z,omitempty"`
	Rotation      float64          `json:"rotation,omitempty"`
	Stands        []CrowdStandIR   `json:"stands"`
	Sections      []CrowdSectionIR `json:"sections"`
	Fan           CrowdFanIR       `json:"fan"`
	LOD           CrowdLODIR       `json:"lod"`
	Material      CrowdMaterialIR  `json:"material"`
	ReceiveShadow bool             `json:"receiveShadow"`
	// Seats is the occupied seat count Layout produced. The browser checks its
	// own expansion against it.
	Seats int `json:"seats"`
}

// CrowdStandIR is a normalized stand: every default applied.
type CrowdStandIR struct {
	ID           string  `json:"id"`
	Shape        string  `json:"shape"`
	X0           float64 `json:"x0,omitempty"`
	Z0           float64 `json:"z0,omitempty"`
	X1           float64 `json:"x1,omitempty"`
	Z1           float64 `json:"z1,omitempty"`
	FacingX      float64 `json:"facingX,omitempty"`
	FacingZ      float64 `json:"facingZ,omitempty"`
	CenterX      float64 `json:"centerX,omitempty"`
	CenterZ      float64 `json:"centerZ,omitempty"`
	Radius       float64 `json:"radius,omitempty"`
	StartAngle   float64 `json:"startAngle,omitempty"`
	EndAngle     float64 `json:"endAngle,omitempty"`
	HalfWidth    float64 `json:"halfWidth,omitempty"`
	HalfDepth    float64 `json:"halfDepth,omitempty"`
	Corner       float64 `json:"corner,omitempty"`
	LoopStart    float64 `json:"loopStart,omitempty"`
	LoopEnd      float64 `json:"loopEnd,omitempty"`
	Density      float64 `json:"density,omitempty"`
	Rows         int     `json:"rows"`
	RowDepth     float64 `json:"rowDepth"`
	RowRise      float64 `json:"rowRise"`
	FrontHeight  float64 `json:"frontHeight"`
	SeatWidth    float64 `json:"seatWidth"`
	AisleWidth   float64 `json:"aisleWidth"`
	FirstSection int     `json:"firstSection"`
	SectionCount int     `json:"sectionCount"`
}

// CrowdSectionIR is a normalized section. Colors, Accents and Props carry
// cumulative thresholds out of 1<<24: a fan takes the first entry whose
// threshold exceeds its roll.
type CrowdSectionIR struct {
	ID               string    `json:"id"`
	Stand            int       `json:"stand"`
	Span             float64   `json:"span"`
	Colors           []string  `json:"colors"`
	ColorThresholds  []uint32  `json:"colorThresholds"`
	Accents          []string  `json:"accents"`
	AccentThresholds []uint32  `json:"accentThresholds"`
	PropThresholds   [4]uint32 `json:"propThresholds"`
	Fill             uint32    `json:"fill"`
	Standing         bool      `json:"standing,omitempty"`
	Action           int       `json:"action,omitempty"`
	Excitement       float64   `json:"excitement,omitempty"`
	NoWave           bool      `json:"noWave,omitempty"`
}

// CrowdFanIR is the normalized fan body.
type CrowdFanIR struct {
	Height    float64  `json:"height"`
	SkinTones []string `json:"skinTones"`
	Legs      string   `json:"legs"`
}

// CrowdLODIR is the normalized level-of-detail policy.
type CrowdLODIR struct {
	FullPixels float64 `json:"fullPixels"`
	LowPixels  float64 `json:"lowPixels"`
	MaxFull    int     `json:"maxFull"`
}

// CrowdMaterialIR is the normalized material.
type CrowdMaterialIR struct {
	Roughness     float64         `json:"roughness"`
	PropRoughness float64         `json:"propRoughness"`
	LightGlow     float64         `json:"lightGlow"`
	Surface       *CustomMaterial `json:"-"`
	SurfaceProps  map[string]any  `json:"surface,omitempty"`
}

const crowdThresholdOne = 1 << 24

var (
	crowdDefaultColors  = []CrowdColor{{Color: "#d9d4c7"}, {Color: "#3d5a80"}, {Color: "#9a3b3b"}, {Color: "#4f6d4a"}}
	crowdDefaultAccents = []CrowdColor{{Color: "#f2c14e"}, {Color: "#e8e8e8"}}
	crowdDefaultSkin    = []string{"#f1c9a5", "#e0ac83", "#c68c5f", "#a26a42", "#7a4a2a", "#4f2f1c"}
)

// ErrCrowdTooLarge reports a crowd past CrowdMaxSeats or CrowdMaxSections.
var ErrCrowdTooLarge = errors.New("scene: crowd exceeds its seat or section limit")

// IR normalizes the crowd into its browser record. It reports an error for an
// invalid description: a duplicate section ID, more than four colours, a
// degenerate stand, or a crowd past its limits.
func (c Crowd) IR() (CrowdIR, error) {
	ir := CrowdIR{
		ID:            strings.TrimSpace(c.ID),
		Seed:          c.Seed,
		X:             c.Position.X,
		Y:             c.Position.Y,
		Z:             c.Position.Z,
		Rotation:      c.Rotation,
		ReceiveShadow: c.ReceiveShadow == nil || *c.ReceiveShadow,
	}
	if ir.ID == "" {
		ir.ID = "crowd"
	}
	ir.Fan = normalizeCrowdFan(c.Fan)
	ir.LOD = normalizeCrowdLOD(c.LOD)
	ir.Material = normalizeCrowdMaterial(c.Material)
	defaultFill := crowdFillThreshold(c.Fill)
	seen := map[string]bool{}
	for si, stand := range c.Stands {
		sir, err := normalizeCrowdStand(si, stand)
		if err != nil {
			return CrowdIR{}, err
		}
		sir.FirstSection = len(ir.Sections)
		sections := stand.Sections
		if len(sections) == 0 {
			sections = []CrowdSection{{}}
		}
		for k, section := range sections {
			sec, err := normalizeCrowdSection(c, sir.ID, si, k, section, defaultFill)
			if err != nil {
				return CrowdIR{}, err
			}
			if seen[sec.ID] {
				return CrowdIR{}, fmt.Errorf("scene: crowd %q has two sections named %q", ir.ID, sec.ID)
			}
			seen[sec.ID] = true
			ir.Sections = append(ir.Sections, sec)
		}
		sir.SectionCount = len(sections)
		ir.Stands = append(ir.Stands, sir)
	}
	if len(ir.Sections) > CrowdMaxSections {
		return CrowdIR{}, ErrCrowdTooLarge
	}
	seats := 0
	if err := layoutCrowdIR(ir, func(CrowdSeat) bool {
		seats++
		return seats <= CrowdMaxSeats
	}); err != nil {
		return CrowdIR{}, err
	}
	ir.Seats = seats
	return ir, nil
}

func normalizeCrowdFan(fan CrowdFan) CrowdFanIR {
	out := CrowdFanIR{Height: positiveOr(fan.Height, 1.75), Legs: strings.TrimSpace(fan.Legs)}
	if out.Legs == "" {
		out.Legs = "#2b2f3a"
	}
	for _, tone := range fan.SkinTones {
		if tone = strings.TrimSpace(tone); tone != "" {
			out.SkinTones = append(out.SkinTones, tone)
		}
	}
	if len(out.SkinTones) == 0 {
		out.SkinTones = append([]string(nil), crowdDefaultSkin...)
	}
	if len(out.SkinTones) > 8 {
		out.SkinTones = out.SkinTones[:8]
	}
	return out
}

func normalizeCrowdLOD(lod CrowdLOD) CrowdLODIR {
	out := CrowdLODIR{FullPixels: positiveOr(lod.FullPixels, 64), LowPixels: positiveOr(lod.LowPixels, 20), MaxFull: lod.MaxFull}
	if out.LowPixels > out.FullPixels {
		out.LowPixels = out.FullPixels
	}
	if out.MaxFull <= 0 {
		out.MaxFull = 8000
	}
	return out
}

func normalizeCrowdMaterial(m CrowdMaterial) CrowdMaterialIR {
	out := CrowdMaterialIR{
		Roughness:     clampUnit(positiveOr(m.Roughness, 0.85)),
		PropRoughness: clampUnit(positiveOr(m.PropRoughness, 0.7)),
		LightGlow:     positiveOr(m.LightGlow, 1.5),
		Surface:       m.Surface,
	}
	if m.Surface != nil {
		out.SurfaceProps = m.Surface.legacyMaterial()
	}
	return out
}

func normalizeCrowdStand(index int, s CrowdStand) (CrowdStandIR, error) {
	shape := CrowdShape(strings.TrimSpace(string(s.Shape)))
	if shape == "" {
		shape = CrowdShapeLine
		if s.HalfWidth > 0 {
			shape = CrowdShapeBowl
		}
	}
	out := CrowdStandIR{
		ID:         strings.TrimSpace(s.ID),
		Shape:      string(shape),
		Rows:       s.Rows,
		RowDepth:   positiveOr(s.RowDepth, 0.85),
		RowRise:    s.RowRise,
		SeatWidth:  positiveOr(s.SeatWidth, 0.5),
		AisleWidth: s.AisleWidth,
		FacingX:    s.Facing.X,
		FacingZ:    s.Facing.Z,
	}
	if out.ID == "" {
		out.ID = fmt.Sprintf("stand-%d", index)
	}
	if out.Rows <= 0 {
		out.Rows = 20
	}
	if out.RowRise == 0 {
		out.RowRise = 0.42
	}
	if out.AisleWidth == 0 {
		out.AisleWidth = 1.2
	} else if out.AisleWidth < 0 {
		out.AisleWidth = 0
	}
	out.FrontHeight = s.FrontHeight
	if out.FrontHeight == 0 && shape != CrowdShapeFloor {
		out.FrontHeight = 1
	}
	switch shape {
	case CrowdShapeLine:
		out.X0, out.Z0, out.X1, out.Z1 = s.Start.X, s.Start.Z, s.End.X, s.End.Z
		if det.Hypot(out.X1-out.X0, out.Z1-out.Z0) < out.SeatWidth {
			return CrowdStandIR{}, fmt.Errorf("scene: crowd stand %q is shorter than one seat", out.ID)
		}
	case CrowdShapeArc:
		out.CenterX, out.CenterZ = s.Center.X, s.Center.Z
		out.Radius, out.StartAngle, out.EndAngle = s.Radius, s.StartAngle, s.EndAngle
		if out.Radius <= 0 || det.Abs(float64(out.EndAngle-out.StartAngle)*out.Radius) < out.SeatWidth {
			return CrowdStandIR{}, fmt.Errorf("scene: crowd arc stand %q needs a radius and an angle span", out.ID)
		}
	case CrowdShapeBowl:
		out.CenterX, out.CenterZ = s.Center.X, s.Center.Z
		out.HalfWidth, out.HalfDepth = s.HalfWidth, s.HalfDepth
		if out.HalfDepth <= 0 {
			out.HalfDepth = out.HalfWidth
		}
		if out.HalfWidth <= 0 {
			return CrowdStandIR{}, fmt.Errorf("scene: crowd bowl stand %q needs HalfWidth", out.ID)
		}
		limit := det.Min(out.HalfWidth, out.HalfDepth)
		out.Corner = s.CornerRadius
		if out.Corner <= 0 {
			out.Corner = limit / 4
		}
		out.Corner = det.Max(0.5, det.Min(out.Corner, limit))
		out.LoopStart, out.LoopEnd = crowdLoopFraction(s.LoopStart), crowdLoopFraction(s.LoopEnd)
	case CrowdShapeFloor:
		out.CenterX, out.CenterZ = s.Center.X, s.Center.Z
		out.HalfWidth, out.HalfDepth = s.HalfWidth, s.HalfDepth
		out.Density = positiveOr(s.Density, 2)
		out.RowRise = 0
		if out.HalfWidth <= 0 || out.HalfDepth <= 0 {
			return CrowdStandIR{}, fmt.Errorf("scene: crowd floor %q needs HalfWidth and HalfDepth", out.ID)
		}
	default:
		return CrowdStandIR{}, fmt.Errorf("scene: crowd stand %q has unknown shape %q", out.ID, shape)
	}
	return out, nil
}

func crowdLoopFraction(v float64) float64 {
	v = v - det.Floor(v)
	if v >= 1 {
		v = 0
	}
	return v
}

func normalizeCrowdSection(c Crowd, standID string, stand, index int, s CrowdSection, defaultFill uint32) (CrowdSectionIR, error) {
	out := CrowdSectionIR{
		ID:         strings.TrimSpace(s.ID),
		Stand:      stand,
		Span:       positiveOr(s.Span, 1),
		Standing:   s.Standing,
		Excitement: clampUnit(s.Excitement),
		NoWave:     s.NoWave,
		Fill:       defaultFill,
	}
	if out.ID == "" {
		out.ID = fmt.Sprintf("%s-%d", standID, index)
	}
	if s.Fill != 0 {
		out.Fill = crowdFillThreshold(s.Fill)
	}
	action, ok := crowdActionIndex(s.Action)
	if !ok {
		return CrowdSectionIR{}, fmt.Errorf("scene: crowd section %q has unknown action %q", out.ID, s.Action)
	}
	out.Action = action
	colors := s.Colors
	if len(colors) == 0 {
		colors = c.Colors
	}
	if len(colors) == 0 {
		colors = crowdDefaultColors
	}
	accents := s.Accents
	if len(accents) == 0 {
		accents = c.Accents
	}
	if len(accents) == 0 {
		accents = crowdDefaultAccents
	}
	var err error
	if out.Colors, out.ColorThresholds, err = crowdPalette(out.ID, colors); err != nil {
		return CrowdSectionIR{}, err
	}
	if out.Accents, out.AccentThresholds, err = crowdPalette(out.ID, accents); err != nil {
		return CrowdSectionIR{}, err
	}
	props := c.Props
	if s.Props != nil {
		props = *s.Props
	}
	out.PropThresholds = crowdPropThresholds(props)
	return out, nil
}

// crowdPalette returns the colours and their cumulative thresholds out of
// 1<<24. The last threshold is always exactly 1<<24.
func crowdPalette(section string, colors []CrowdColor) ([]string, []uint32, error) {
	if len(colors) > CrowdMaxColors {
		return nil, nil, fmt.Errorf("scene: crowd section %q has %d colours; the limit is %d", section, len(colors), CrowdMaxColors)
	}
	total := 0.0
	for _, color := range colors {
		if color.Weight < 0 {
			return nil, nil, fmt.Errorf("scene: crowd section %q has a negative colour weight", section)
		}
		total += positiveOr(color.Weight, 1)
	}
	names := make([]string, len(colors))
	limits := make([]uint32, len(colors))
	running := 0.0
	for i, color := range colors {
		names[i] = strings.TrimSpace(color.Color)
		if names[i] == "" {
			return nil, nil, fmt.Errorf("scene: crowd section %q has an empty colour", section)
		}
		running += positiveOr(color.Weight, 1)
		limits[i] = uint32(det.Round(running / total * crowdThresholdOne))
	}
	limits[len(limits)-1] = crowdThresholdOne
	return names, limits, nil
}

func crowdPropThresholds(mix CrowdPropMix) [4]uint32 {
	shares := [4]float64{clampUnit(mix.Lights), clampUnit(mix.Scarves), clampUnit(mix.Towels), clampUnit(mix.Banners)}
	var out [4]uint32
	running := 0.0
	for i, share := range shares {
		running = det.Min(1, running+share)
		out[i] = uint32(det.Round(running * crowdThresholdOne))
	}
	return out
}

func crowdFillThreshold(fill float64) uint32 {
	if fill <= 0 || fill >= 1 {
		return crowdThresholdOne
	}
	return uint32(det.Round(fill * crowdThresholdOne))
}

func clampUnit(v float64) float64 {
	return det.Max(0, det.Min(1, v))
}

func normalizeCrowds(crowds []Crowd) []CrowdIR {
	if len(crowds) == 0 {
		return nil
	}
	out := make([]CrowdIR, 0, len(crowds))
	for _, crowd := range crowds {
		ir, err := crowd.IR()
		if err != nil {
			// An invalid crowd is dropped from the page rather than failing the
			// whole scene; Crowd.IR and Crowd.Layout report the error to Go.
			continue
		}
		out = append(out, ir)
	}
	return out
}
