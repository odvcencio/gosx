package scene

import (
	"encoding/json"
	"math"
	"strings"

	"github.com/odvcencio/gosx/engine"
)

const DefaultEngineName = "GoSXScene3D"

var defaultCapabilities = []string{"canvas", "webgl", "animation"}

// Vector3 is a basic 3D point or direction.
type Vector3 struct {
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	Z float64 `json:"z,omitempty"`
}

// Euler stores XYZ rotation in radians.
type Euler struct {
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	Z float64 `json:"z,omitempty"`
}

// PerspectiveCamera describes the current Scene3D camera contract.
type PerspectiveCamera struct {
	Position Vector3
	FOV      float64
	Near     float64
	Far      float64
}

// Props is the typed Go-side Scene3D surface. It lowers into the current
// Scene3D compatibility prop bag while preserving room for a real scene graph.
type Props struct {
	ProgramRef          string   `json:"-"`
	Capabilities        []string `json:"-"`
	Width               int      `json:"width,omitempty"`
	Height              int      `json:"height,omitempty"`
	Label               string   `json:"label,omitempty"`
	AriaLabel           string   `json:"ariaLabel,omitempty"`
	Background          string   `json:"background,omitempty"`
	AutoRotate          *bool    `json:"autoRotate,omitempty"`
	Responsive          *bool    `json:"responsive,omitempty"`
	FillHeight          *bool    `json:"fillHeight,omitempty"`
	PreferWebGL         *bool    `json:"preferWebGL,omitempty"`
	ForceWebGL          *bool    `json:"forceWebGL,omitempty"`
	PreferCanvas        *bool    `json:"preferCanvas,omitempty"`
	DragToRotate        *bool    `json:"dragToRotate,omitempty"`
	DragSignalNamespace string   `json:"dragSignalNamespace,omitempty"`
	CapabilityTier      string   `json:"capabilityTier,omitempty"`
	MaxDevicePixelRatio float64  `json:"maxDevicePixelRatio,omitempty"`
	Camera              PerspectiveCamera
	Graph               Graph
}

// Graph is the typed scene graph lowered into the legacy Scene3D prop bag.
type Graph struct {
	Nodes []Node
}

// Node is a typed scene graph node.
type Node interface {
	sceneNode()
}

// Group applies a transform to descendant nodes.
type Group struct {
	ID       string
	Position Vector3
	Rotation Euler
	Children []Node
}

// Mesh lowers into one legacy scene object.
type Mesh struct {
	ID         string
	Geometry   Geometry
	Material   Material
	Position   Vector3
	Rotation   Euler
	Spin       Euler
	Drift      Vector3
	DriftSpeed float64
	DriftPhase float64
	Children   []Node
}

// Label lowers into one legacy scene label.
type Label struct {
	ID          string
	Target      string
	Text        string
	ClassName   string
	Position    Vector3
	Priority    float64
	Shift       Vector3
	DriftSpeed  float64
	DriftPhase  float64
	MaxWidth    float64
	MaxLines    int
	Overflow    string
	Font        string
	LineHeight  float64
	Color       string
	Background  string
	BorderColor string
	OffsetX     float64
	OffsetY     float64
	AnchorX     float64
	AnchorY     float64
	Collision   string
	Occlude     bool
	WhiteSpace  string
	TextAlign   string
}

// Geometry describes one supported legacy primitive.
type Geometry interface {
	sceneGeometry()
	legacyGeometry() (string, map[string]any)
}

// Material describes one supported legacy material adapter.
type Material interface {
	sceneMaterial()
	legacyMaterial() map[string]any
}

type CubeGeometry struct {
	Size float64
}

type BoxGeometry struct {
	Width  float64
	Height float64
	Depth  float64
}

type PlaneGeometry struct {
	Width  float64
	Height float64
}

type PyramidGeometry struct {
	Width  float64
	Height float64
	Depth  float64
}

type SphereGeometry struct {
	Radius   float64
	Segments int
}

type FlatMaterial struct {
	Color string
}

type quaternion struct {
	X float64
	Y float64
	Z float64
	W float64
}

type worldTransform struct {
	Position Vector3
	Rotation quaternion
}

type pendingLabel struct {
	label  Label
	parent worldTransform
}

type graphLowerer struct {
	objects      []ObjectIR
	pending      []pendingLabel
	anchors      map[string]worldTransform
	nextObjectID int
	nextLabelID  int
}

func (Group) sceneNode() {}
func (Mesh) sceneNode()  {}
func (Label) sceneNode() {}

func (CubeGeometry) sceneGeometry()    {}
func (BoxGeometry) sceneGeometry()     {}
func (PlaneGeometry) sceneGeometry()   {}
func (PyramidGeometry) sceneGeometry() {}
func (SphereGeometry) sceneGeometry()  {}

func (FlatMaterial) sceneMaterial() {}

// Bool allocates a bool for opt-in Scene3D flags.
func Bool(value bool) *bool {
	return &value
}

// Vec3 builds a Vector3 without forcing struct literals at callsites.
func Vec3(x, y, z float64) Vector3 {
	return Vector3{X: x, Y: y, Z: z}
}

// Rotate builds an Euler rotation in radians.
func Rotate(x, y, z float64) Euler {
	return Euler{X: x, Y: y, Z: z}
}

// NewGraph builds a scene graph from typed nodes.
func NewGraph(nodes ...Node) Graph {
	return Graph{Nodes: append([]Node(nil), nodes...)}
}

// GoSXSpreadProps allows file-route spreads to lower typed scene props into
// the current Scene3D component contract.
func (p Props) GoSXSpreadProps() map[string]any {
	values := p.LegacyProps()
	if ref := strings.TrimSpace(p.ProgramRef); ref != "" {
		values["programRef"] = ref
	}
	values["capabilities"] = p.EngineCapabilities()
	return values
}

// LegacyProps lowers typed scene props into the current Scene3D prop bag.
func (p Props) LegacyProps() map[string]any {
	out := p.legacyBaseProps()
	if scene := p.SceneIR().legacyProps(); len(scene) > 0 {
		out["scene"] = scene
	}
	return out
}

func (p Props) legacyBaseProps() map[string]any {
	out := map[string]any{}
	setInt(out, "width", p.Width)
	setInt(out, "height", p.Height)
	setString(out, "label", p.Label)
	setString(out, "ariaLabel", p.AriaLabel)
	setString(out, "background", p.Background)
	setBool(out, "autoRotate", p.AutoRotate)
	setBool(out, "responsive", p.Responsive)
	setBool(out, "fillHeight", p.FillHeight)
	setBool(out, "preferWebGL", p.PreferWebGL)
	setBool(out, "forceWebGL", p.ForceWebGL)
	setBool(out, "preferCanvas", p.PreferCanvas)
	setBool(out, "dragToRotate", p.DragToRotate)
	setString(out, "dragSignalNamespace", p.DragSignalNamespace)
	setString(out, "capabilityTier", p.CapabilityTier)
	setNumeric(out, "maxDevicePixelRatio", p.MaxDevicePixelRatio)
	if !p.Camera.isZero() {
		out["camera"] = p.Camera.legacyProps()
	}
	return out
}

// MarshalJSON encodes only runtime props. Engine transport fields such as
// ProgramRef and Capabilities stay outside the JSON payload.
func (p Props) MarshalJSON() ([]byte, error) {
	values := p.LegacyProps()
	if len(values) == 0 {
		return []byte("{}"), nil
	}
	return json.Marshal(values)
}

// RawPropsJSON returns engine.Config-compatible runtime props.
func (p Props) RawPropsJSON() json.RawMessage {
	values := p.LegacyProps()
	if len(values) == 0 {
		return nil
	}
	data, err := json.Marshal(values)
	if err != nil {
		return nil
	}
	return data
}

// EngineCapabilities returns the Scene3D capability set with built-in defaults.
func (p Props) EngineCapabilities() []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(defaultCapabilities)+len(p.Capabilities))
	appendCapability := func(raw string) {
		raw = strings.TrimSpace(strings.ToLower(raw))
		if raw == "" {
			return
		}
		if _, exists := seen[raw]; exists {
			return
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	for _, capability := range defaultCapabilities {
		appendCapability(capability)
	}
	for _, capability := range p.Capabilities {
		appendCapability(capability)
	}
	return out
}

// EngineConfig builds the built-in Scene3D engine.Config for pure Go callsites.
func (p Props) EngineConfig() engine.Config {
	capabilities := p.EngineCapabilities()
	cfg := engine.Config{
		Name:       DefaultEngineName,
		Kind:       engine.KindSurface,
		WASMPath:   strings.TrimSpace(p.ProgramRef),
		Props:      p.RawPropsJSON(),
		MountAttrs: map[string]any{"data-gosx-scene3d": true},
	}
	if len(capabilities) > 0 {
		cfg.Capabilities = make([]engine.Capability, 0, len(capabilities))
		for _, capability := range capabilities {
			cfg.Capabilities = append(cfg.Capabilities, engine.Capability(capability))
		}
	}
	if cfg.WASMPath != "" {
		cfg.Runtime = engine.RuntimeShared
	}
	return cfg
}

func (c PerspectiveCamera) legacyProps() map[string]any {
	out := map[string]any{}
	if c.Position.X != 0 {
		out["x"] = c.Position.X
	}
	if c.Position.Y != 0 {
		out["y"] = c.Position.Y
	}
	if c.Position.Z != 0 {
		out["z"] = c.Position.Z
	}
	if c.FOV != 0 {
		out["fov"] = c.FOV
	}
	if c.Near != 0 {
		out["near"] = c.Near
	}
	if c.Far != 0 {
		out["far"] = c.Far
	}
	return out
}

func (c PerspectiveCamera) isZero() bool {
	return c.Position == (Vector3{}) && c.FOV == 0 && c.Near == 0 && c.Far == 0
}

func (l *graphLowerer) lowerNode(node Node, parent worldTransform) {
	switch current := node.(type) {
	case Group:
		l.lowerGroup(current, parent)
	case *Group:
		if current != nil {
			l.lowerGroup(*current, parent)
		}
	case Mesh:
		l.lowerMesh(current, parent)
	case *Mesh:
		if current != nil {
			l.lowerMesh(*current, parent)
		}
	case Label:
		l.pending = append(l.pending, pendingLabel{label: current, parent: parent})
	case *Label:
		if current != nil {
			l.pending = append(l.pending, pendingLabel{label: *current, parent: parent})
		}
	}
}

func (l *graphLowerer) lowerGroup(group Group, parent worldTransform) {
	world := combineTransforms(parent, localTransform(group.Position, group.Rotation))
	if id := strings.TrimSpace(group.ID); id != "" {
		l.anchors[id] = world
	}
	for _, child := range group.Children {
		l.lowerNode(child, world)
	}
}

func (l *graphLowerer) lowerMesh(mesh Mesh, parent worldTransform) {
	world := combineTransforms(parent, localTransform(mesh.Position, mesh.Rotation))
	id := strings.TrimSpace(mesh.ID)
	if id == "" {
		l.nextObjectID += 1
		id = "scene-object-" + intString(l.nextObjectID)
	}
	kind, geometryProps := legacyGeometry(mesh.Geometry)
	record := ObjectIR{
		ID:   id,
		Kind: kind,
	}
	applyGeometryProps(&record, geometryProps)
	applyMaterialProps(&record, legacyMaterial(mesh.Material))
	record.X = world.Position.X
	record.Y = world.Position.Y
	record.Z = world.Position.Z
	rotation := eulerFromQuaternion(world.Rotation)
	record.RotationX = rotation.X
	record.RotationY = rotation.Y
	record.RotationZ = rotation.Z
	record.SpinX = mesh.Spin.X
	record.SpinY = mesh.Spin.Y
	record.SpinZ = mesh.Spin.Z
	record.ShiftX = mesh.Drift.X
	record.ShiftY = mesh.Drift.Y
	record.ShiftZ = mesh.Drift.Z
	record.DriftSpeed = mesh.DriftSpeed
	record.DriftPhase = mesh.DriftPhase
	l.objects = append(l.objects, record)
	l.anchors[id] = world
	for _, child := range mesh.Children {
		l.lowerNode(child, world)
	}
}

func (l *graphLowerer) resolveLabels() []LabelIR {
	if len(l.pending) == 0 {
		return nil
	}
	out := make([]LabelIR, 0, len(l.pending))
	for _, item := range l.pending {
		if record, ok := l.resolveLabel(item); ok {
			out = append(out, record)
		}
	}
	return out
}

func (l *graphLowerer) resolveLabel(item pendingLabel) (LabelIR, bool) {
	text := strings.TrimSpace(item.label.Text)
	if text == "" {
		return LabelIR{}, false
	}
	position := l.resolveLabelPosition(item)
	return LabelIR{
		ID:          l.nextSceneLabelID(item.label.ID),
		Text:        text,
		ClassName:   strings.TrimSpace(item.label.ClassName),
		X:           position.X,
		Y:           position.Y,
		Z:           position.Z,
		Priority:    item.label.Priority,
		ShiftX:      item.label.Shift.X,
		ShiftY:      item.label.Shift.Y,
		ShiftZ:      item.label.Shift.Z,
		DriftSpeed:  item.label.DriftSpeed,
		DriftPhase:  item.label.DriftPhase,
		MaxWidth:    item.label.MaxWidth,
		MaxLines:    item.label.MaxLines,
		Overflow:    strings.TrimSpace(item.label.Overflow),
		Font:        strings.TrimSpace(item.label.Font),
		LineHeight:  item.label.LineHeight,
		Color:       strings.TrimSpace(item.label.Color),
		Background:  strings.TrimSpace(item.label.Background),
		BorderColor: strings.TrimSpace(item.label.BorderColor),
		OffsetX:     item.label.OffsetX,
		OffsetY:     item.label.OffsetY,
		AnchorX:     item.label.AnchorX,
		AnchorY:     item.label.AnchorY,
		Collision:   strings.TrimSpace(item.label.Collision),
		Occlude:     item.label.Occlude,
		WhiteSpace:  strings.TrimSpace(item.label.WhiteSpace),
		TextAlign:   strings.TrimSpace(item.label.TextAlign),
	}, true
}

func (l *graphLowerer) resolveLabelPosition(item pendingLabel) Vector3 {
	position := combineTransforms(item.parent, localTransform(item.label.Position, Euler{})).Position
	target := strings.TrimSpace(item.label.Target)
	if target == "" {
		return position
	}
	anchor, ok := l.anchors[target]
	if !ok {
		return position
	}
	return addVectors(anchor.Position, anchor.Rotation.rotate(item.label.Position))
}

func (l *graphLowerer) nextSceneLabelID(raw string) string {
	id := strings.TrimSpace(raw)
	if id != "" {
		return id
	}
	l.nextLabelID += 1
	return "scene-label-" + intString(l.nextLabelID)
}

func applyGeometryProps(record *ObjectIR, props map[string]any) {
	if record == nil || len(props) == 0 {
		return
	}
	record.Size = mapFloat64(props["size"])
	record.Width = mapFloat64(props["width"])
	record.Height = mapFloat64(props["height"])
	record.Depth = mapFloat64(props["depth"])
	record.Radius = mapFloat64(props["radius"])
	record.Segments = mapInt(props["segments"])
}

func applyMaterialProps(record *ObjectIR, props map[string]any) {
	if record == nil || len(props) == 0 {
		return
	}
	if color, ok := props["color"].(string); ok {
		record.Color = strings.TrimSpace(color)
	}
}

func legacyGeometry(geometry Geometry) (string, map[string]any) {
	if geometry == nil {
		return "cube", nil
	}
	return geometry.legacyGeometry()
}

func (g CubeGeometry) legacyGeometry() (string, map[string]any) {
	if g.Size <= 0 {
		return "cube", nil
	}
	return "cube", map[string]any{"size": g.Size}
}

func (g BoxGeometry) legacyGeometry() (string, map[string]any) {
	out := map[string]any{}
	setNumeric(out, "width", g.Width)
	setNumeric(out, "height", g.Height)
	setNumeric(out, "depth", g.Depth)
	if len(out) == 0 {
		return "box", nil
	}
	return "box", out
}

func (g PlaneGeometry) legacyGeometry() (string, map[string]any) {
	out := map[string]any{}
	setNumeric(out, "width", g.Width)
	setNumeric(out, "height", g.Height)
	if len(out) == 0 {
		return "plane", nil
	}
	return "plane", out
}

func (g PyramidGeometry) legacyGeometry() (string, map[string]any) {
	out := map[string]any{}
	setNumeric(out, "width", g.Width)
	setNumeric(out, "height", g.Height)
	setNumeric(out, "depth", g.Depth)
	if len(out) == 0 {
		return "pyramid", nil
	}
	return "pyramid", out
}

func (g SphereGeometry) legacyGeometry() (string, map[string]any) {
	out := map[string]any{}
	setNumeric(out, "radius", g.Radius)
	if g.Segments > 0 {
		out["segments"] = g.Segments
	}
	if len(out) == 0 {
		return "sphere", nil
	}
	return "sphere", out
}

func legacyMaterial(material Material) map[string]any {
	if material == nil {
		return nil
	}
	return material.legacyMaterial()
}

func (m FlatMaterial) legacyMaterial() map[string]any {
	color := strings.TrimSpace(m.Color)
	if color == "" {
		return nil
	}
	return map[string]any{"color": color}
}

func localTransform(position Vector3, rotation Euler) worldTransform {
	return worldTransform{
		Position: position,
		Rotation: quaternionFromEuler(rotation),
	}
}

func identityTransform() worldTransform {
	return worldTransform{
		Rotation: quaternion{W: 1},
	}
}

func combineTransforms(parent, local worldTransform) worldTransform {
	return worldTransform{
		Position: addVectors(parent.Position, parent.Rotation.rotate(local.Position)),
		Rotation: parent.Rotation.mul(local.Rotation).normalized(),
	}
}

func addVectors(left, right Vector3) Vector3 {
	return Vector3{
		X: left.X + right.X,
		Y: left.Y + right.Y,
		Z: left.Z + right.Z,
	}
}

func quaternionFromEuler(rotation Euler) quaternion {
	qx := axisAngleQuaternion(Vector3{X: 1}, rotation.X)
	qy := axisAngleQuaternion(Vector3{Y: 1}, rotation.Y)
	qz := axisAngleQuaternion(Vector3{Z: 1}, rotation.Z)
	return qz.mul(qy).mul(qx).normalized()
}

func axisAngleQuaternion(axis Vector3, angle float64) quaternion {
	if angle == 0 {
		return quaternion{W: 1}
	}
	half := angle / 2
	sine := math.Sin(half)
	return quaternion{
		X: axis.X * sine,
		Y: axis.Y * sine,
		Z: axis.Z * sine,
		W: math.Cos(half),
	}
}

func (q quaternion) normalized() quaternion {
	length := math.Sqrt((q.X * q.X) + (q.Y * q.Y) + (q.Z * q.Z) + (q.W * q.W))
	if length == 0 {
		return quaternion{W: 1}
	}
	return quaternion{
		X: q.X / length,
		Y: q.Y / length,
		Z: q.Z / length,
		W: q.W / length,
	}
}

func (q quaternion) conjugate() quaternion {
	return quaternion{X: -q.X, Y: -q.Y, Z: -q.Z, W: q.W}
}

func (q quaternion) mul(other quaternion) quaternion {
	return quaternion{
		X: (q.W * other.X) + (q.X * other.W) + (q.Y * other.Z) - (q.Z * other.Y),
		Y: (q.W * other.Y) - (q.X * other.Z) + (q.Y * other.W) + (q.Z * other.X),
		Z: (q.W * other.Z) + (q.X * other.Y) - (q.Y * other.X) + (q.Z * other.W),
		W: (q.W * other.W) - (q.X * other.X) - (q.Y * other.Y) - (q.Z * other.Z),
	}
}

func (q quaternion) rotate(vector Vector3) Vector3 {
	point := quaternion{X: vector.X, Y: vector.Y, Z: vector.Z}
	result := q.mul(point).mul(q.conjugate())
	return Vector3{X: result.X, Y: result.Y, Z: result.Z}
}

func eulerFromQuaternion(q quaternion) Euler {
	q = q.normalized()
	m00 := 1 - (2 * ((q.Y * q.Y) + (q.Z * q.Z)))
	m10 := 2 * ((q.X * q.Y) + (q.Z * q.W))
	m20 := 2 * ((q.X * q.Z) - (q.Y * q.W))
	m21 := 2 * ((q.Y * q.Z) + (q.X * q.W))
	m22 := 1 - (2 * ((q.X * q.X) + (q.Y * q.Y)))
	m01 := 2 * ((q.X * q.Y) - (q.Z * q.W))
	m11 := 1 - (2 * ((q.X * q.X) + (q.Z * q.Z)))

	y := math.Asin(clamp(-m20, -1, 1))
	cosY := math.Cos(y)
	if math.Abs(cosY) > 1e-9 {
		return Euler{
			X: math.Atan2(m21, m22),
			Y: y,
			Z: math.Atan2(m10, m00),
		}
	}
	return Euler{
		X: 0,
		Y: y,
		Z: math.Atan2(-m01, m11),
	}
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func setString(target map[string]any, name, value string) {
	value = strings.TrimSpace(value)
	if target == nil || name == "" || value == "" {
		return
	}
	target[name] = value
}

func setInt(target map[string]any, name string, value int) {
	if target == nil || name == "" || value == 0 {
		return
	}
	target[name] = value
}

func setBool(target map[string]any, name string, value *bool) {
	if target == nil || name == "" || value == nil {
		return
	}
	target[name] = *value
}

func setNumeric(target map[string]any, name string, value float64) {
	if target == nil || name == "" || value == 0 {
		return
	}
	target[name] = value
}

func mapFloat64(value any) float64 {
	switch current := value.(type) {
	case float64:
		return current
	case float32:
		return float64(current)
	case int:
		return float64(current)
	case int64:
		return float64(current)
	case int32:
		return float64(current)
	default:
		return 0
	}
}

func mapInt(value any) int {
	switch current := value.(type) {
	case int:
		return current
	case int64:
		return int(current)
	case int32:
		return int(current)
	case float64:
		return int(current)
	case float32:
		return int(current)
	default:
		return 0
	}
}

func intString(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + (value % 10))}, digits...)
		value /= 10
	}
	return string(digits)
}
