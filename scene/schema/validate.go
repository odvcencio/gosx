package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/scene/capability"
)

type Severity string

const (
	Info  Severity = "info"
	Warn  Severity = "warn"
	Error Severity = "error"
	Fatal Severity = "fatal"
)

type Diagnostic struct {
	Severity Severity       `json:"severity"`
	Code     string         `json:"code"`
	Message  string         `json:"message"`
	Path     string         `json:"path,omitempty"`
	ID       string         `json:"id,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

type Options struct {
	Strict           bool
	MaxTexturePixels int
}

type Report struct {
	Schema      string       `json:"schema"`
	Strict      bool         `json:"strict,omitempty"`
	Valid       bool         `json:"valid"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type Document struct {
	Schema             string                     `json:"schema,omitempty"`
	Objects            []scene.ObjectIR           `json:"objects,omitempty"`
	Models             []scene.ModelIR            `json:"models,omitempty"`
	Points             []scene.PointsIR           `json:"points,omitempty"`
	InstancedMeshes    []scene.InstancedMeshIR    `json:"instancedMeshes,omitempty"`
	InstancedGLBMeshes []scene.InstancedGLBMeshIR `json:"instancedGLBMeshes,omitempty"`
	ComputeParticles   []scene.ComputeParticlesIR `json:"computeParticles,omitempty"`
	WaterSystems       []scene.WaterSystemIR      `json:"waterSystems,omitempty"`
	Animations         []scene.AnimationClipIR    `json:"animations,omitempty"`
	Labels             []scene.LabelIR            `json:"labels,omitempty"`
	Sprites            []scene.SpriteIR           `json:"sprites,omitempty"`
	HTML               []scene.HTMLIR             `json:"html,omitempty"`
	Lights             []scene.LightIR            `json:"lights,omitempty"`
	PostEffects        []json.RawMessage          `json:"postEffects,omitempty"`
	PostFXMaxPixels    int                        `json:"postFXMaxPixels,omitempty"`
	ShadowMaxPixels    int                        `json:"shadowMaxPixels,omitempty"`
	BackendCaps        *capability.BackendCaps    `json:"backendCaps,omitempty"`
}

const Schema = "gosx.scene3d.schema.validation.v1"

func ValidateJSON(data []byte, opts Options) Report {
	report := Report{Schema: Schema, Strict: opts.Strict, Valid: true}
	var doc Document
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		report.add(Fatal, "scene.schema.invalid_json", "SceneIR JSON could not be decoded", "", "", map[string]any{"error": err.Error()})
		report.Valid = false
		return report
	}
	validateDocument(&report, doc, opts)
	report.Valid = !hasError(report.Diagnostics)
	return report
}

func validateDocument(report *Report, doc Document, opts Options) {
	if doc.Schema != "" && doc.Schema != scene.SceneIRSchema {
		severity := Warn
		if opts.Strict {
			severity = Error
		}
		report.add(severity, "scene.schema.version_mismatch", "SceneIR schema does not match the current runtime schema", "schema", "", map[string]any{
			"got":  doc.Schema,
			"want": scene.SceneIRSchema,
		})
	}
	if doc.PostFXMaxPixels < 0 {
		report.add(Error, "scene.postfx.invalid_max_pixels", "postFXMaxPixels must not be negative", "postFXMaxPixels", "", nil)
	}
	if doc.ShadowMaxPixels < 0 {
		report.add(Error, "scene.shadow.invalid_max_pixels", "shadowMaxPixels must not be negative", "shadowMaxPixels", "", nil)
	}

	ids := map[string]string{}
	targetIDs := map[string]struct{}{}
	addID := func(id, path string, required bool) {
		id = strings.TrimSpace(id)
		if id == "" {
			if required || opts.Strict {
				report.add(Error, "scene.id.missing", "Scene record requires a stable ID", path, "", nil)
			}
			return
		}
		if prev, ok := ids[id]; ok {
			report.add(Error, "scene.id.duplicate", "Scene record ID is duplicated", path, id, map[string]any{"firstPath": prev})
			return
		}
		ids[id] = path
	}
	addTargetID := func(id string) {
		if id = strings.TrimSpace(id); id != "" {
			targetIDs[id] = struct{}{}
		}
	}
	addOptionalID := func(id, path string) {
		if strings.TrimSpace(id) != "" {
			addID(id, path, false)
		}
	}

	for i, object := range doc.Objects {
		path := fmt.Sprintf("objects[%d]", i)
		addID(object.ID, path+".id", object.Pickable != nil && *object.Pickable)
		addTargetID(object.ID)
		if strings.TrimSpace(object.Kind) == "" {
			report.add(Error, "scene.object.kind_missing", "Object scene record requires kind", path+".kind", object.ID, nil)
		}
		validateObject(report, object, path)
	}
	for i, model := range doc.Models {
		path := fmt.Sprintf("models[%d]", i)
		addID(model.ID, path+".id", model.Pickable != nil && *model.Pickable)
		addTargetID(model.ID)
		validateObject(report, model.ObjectIR, path)
		if strings.TrimSpace(model.Src) == "" {
			report.add(Error, "scene.asset.missing", "Model scene record requires src", path+".src", model.ID, nil)
		}
		validateModel(report, model, path)
	}
	for i, points := range doc.Points {
		path := fmt.Sprintf("points[%d]", i)
		addID(points.ID, path+".id", true)
		validatePoints(report, points, path)
	}
	for i, mesh := range doc.InstancedMeshes {
		path := fmt.Sprintf("instancedMeshes[%d]", i)
		addID(mesh.ID, path+".id", true)
		addTargetID(mesh.ID)
		validateInstancedMesh(report, mesh, path)
	}
	for i, mesh := range doc.InstancedGLBMeshes {
		path := fmt.Sprintf("instancedGLBMeshes[%d]", i)
		addID(mesh.ID, path+".id", true)
		addTargetID(mesh.ID)
		if strings.TrimSpace(mesh.Src) == "" {
			report.add(Error, "scene.asset.missing", "Instanced GLB mesh requires src", path+".src", mesh.ID, nil)
		}
		if len(mesh.Instances) == 0 {
			report.add(Warn, "scene.instances.empty", "Instanced GLB mesh has no instances", path+".instances", mesh.ID, nil)
		}
		for j, instance := range mesh.Instances {
			instancePath := fmt.Sprintf("%s.instances[%d]", path, j)
			addOptionalID(instance.ID, instancePath+".id")
			validateMeshInstance(report, instance, instancePath, mesh.ID)
		}
		validateMaterialScalars(report, mesh.ID, path, mesh.Roughness, mesh.Metalness)
	}
	for i, particles := range doc.ComputeParticles {
		path := fmt.Sprintf("computeParticles[%d]", i)
		addID(particles.ID, path+".id", true)
		validateComputeParticles(report, particles, path)
	}
	for i, water := range doc.WaterSystems {
		path := fmt.Sprintf("waterSystems[%d]", i)
		addID(water.ID, path+".id", true)
		validateWaterSystem(report, water, path)
	}
	for i, label := range doc.Labels {
		path := fmt.Sprintf("labels[%d]", i)
		addID(label.ID, path+".id", true)
		validateLabel(report, label, path)
	}
	for i, sprite := range doc.Sprites {
		path := fmt.Sprintf("sprites[%d]", i)
		addID(sprite.ID, path+".id", true)
		if strings.TrimSpace(sprite.Src) == "" {
			report.add(Warn, "scene.asset.missing", "Sprite has no src", path+".src", sprite.ID, nil)
		}
		validateSprite(report, sprite, path)
	}
	for i, html := range doc.HTML {
		path := fmt.Sprintf("html[%d]", i)
		validateHTML(report, html, path, opts, targetIDs)
		addID(html.ID, path+".id", true)
	}
	for i, light := range doc.Lights {
		path := fmt.Sprintf("lights[%d]", i)
		addID(light.ID, path+".id", true)
		validateLight(report, light, path)
	}
	for i, animation := range doc.Animations {
		path := fmt.Sprintf("animations[%d]", i)
		addID(animation.Name, path+".name", true)
		validateAnimation(report, animation, path, len(doc.Objects)+len(doc.Models)+len(doc.InstancedMeshes)+len(doc.InstancedGLBMeshes))
	}
	for i, raw := range doc.PostEffects {
		validatePostEffect(report, raw, fmt.Sprintf("postEffects[%d]", i))
	}
}

func validateObject(report *Report, object scene.ObjectIR, path string) {
	validatePrimitiveParameters(report, object.Kind, object.ID, path, object.Size, object.Width, object.Height, object.Depth, object.Radius, object.RadiusTop, object.RadiusBottom, object.Tube, object.Segments, object.RadialSegments, object.TubularSegments)
	validateMaterialScalars(report, object.ID, path, object.Roughness, object.Metalness, object.Clearcoat, object.Sheen, object.Transmission, object.Iridescence, object.Anisotropy)
	validateNumericFields(report, object.ID, path, map[string]float64{
		"lineWidth":      object.LineWidth,
		"dashSize":       object.DashSize,
		"gapSize":        object.GapSize,
		"outlineWidth":   object.OutlineWidth,
		"lodMinDistance": object.LODMinDistance,
		"lodMaxDistance": object.LODMaxDistance,
		"x":              object.X,
		"y":              object.Y,
		"z":              object.Z,
		"rotationX":      object.RotationX,
		"rotationY":      object.RotationY,
		"rotationZ":      object.RotationZ,
		"spinX":          object.SpinX,
		"spinY":          object.SpinY,
		"spinZ":          object.SpinZ,
		"shiftX":         object.ShiftX,
		"shiftY":         object.ShiftY,
		"shiftZ":         object.ShiftZ,
		"driftSpeed":     object.DriftSpeed,
		"driftPhase":     object.DriftPhase,
	})
	validateNonNegativeNumericFields(report, object.ID, path, map[string]float64{
		"lineWidth":      object.LineWidth,
		"dashSize":       object.DashSize,
		"gapSize":        object.GapSize,
		"outlineWidth":   object.OutlineWidth,
		"lodMinDistance": object.LODMinDistance,
		"lodMaxDistance": object.LODMaxDistance,
	})
	if object.LODMaxDistance > 0 && object.LODMinDistance > object.LODMaxDistance {
		report.add(Error, "scene.lod.invalid_range", "LOD minimum distance must not exceed maximum distance", path+".lodMinDistance", object.ID, map[string]any{"lodMinDistance": object.LODMinDistance, "lodMaxDistance": object.LODMaxDistance})
	}
	if object.Opacity != nil {
		validateNonNegativeFloat(report, object.ID, path+".opacity", *object.Opacity)
	}
	if object.Emissive != nil {
		validateNonNegativeFloat(report, object.ID, path+".emissive", *object.Emissive)
	}
	for i, point := range object.Points {
		validateVector3(report, object.ID, fmt.Sprintf("%s.points[%d]", path, i), point)
	}
	for i, segment := range object.LineSegments {
		if segment[0] < 0 || segment[1] < 0 {
			report.add(Error, "scene.line.invalid_segment", "Line segment indices must not be negative", fmt.Sprintf("%s.lineSegments[%d]", path, i), object.ID, map[string]any{"from": segment[0], "to": segment[1]})
			continue
		}
		if len(object.Points) > 0 && (segment[0] >= len(object.Points) || segment[1] >= len(object.Points)) {
			report.add(Error, "scene.line.invalid_segment", "Line segment index is outside the points array", fmt.Sprintf("%s.lineSegments[%d]", path, i), object.ID, map[string]any{"from": segment[0], "to": segment[1], "points": len(object.Points)})
		}
	}
	validateLive(report, object.ID, path, object.Live)
}

func validateModel(report *Report, model scene.ModelIR, path string) {
	validateNumericFields(report, model.ID, path, map[string]float64{
		"scaleX": model.ScaleX,
		"scaleY": model.ScaleY,
		"scaleZ": model.ScaleZ,
	})
	validateNonNegativeFloat(report, model.ID, path+".bounds", model.Bounds)
	if model.AnimationSpeed != nil {
		validateNonNegativeFloat(report, model.ID, path+".animationSpeed", *model.AnimationSpeed)
	}
	if model.AnimationWeight != nil {
		validateNonNegativeFloat(report, model.ID, path+".animationWeight", *model.AnimationWeight)
	}
	if model.AnimationFadeInMS != nil && *model.AnimationFadeInMS < 0 {
		report.add(Error, "scene.animation.invalid_fade", "Model animation fade duration must not be negative", path+".animationFadeInMS", model.ID, nil)
	}
	if model.AnimationFadeOutMS != nil && *model.AnimationFadeOutMS < 0 {
		report.add(Error, "scene.animation.invalid_fade", "Model animation fade duration must not be negative", path+".animationFadeOutMS", model.ID, nil)
	}
}

func validatePoints(report *Report, points scene.PointsIR, path string) {
	if points.Count < 0 {
		report.add(Error, "scene.points.invalid_count", "Point layer count must not be negative", path+".count", points.ID, nil)
	}
	if points.PositionStride != 0 && points.PositionStride != 3 {
		report.add(Error, "scene.points.invalid_stride", "Point positions must use stride 3", path+".positionStride", points.ID, map[string]any{"positionStride": points.PositionStride})
	}
	for i, value := range points.Positions {
		validateFiniteFloat(report, points.ID, fmt.Sprintf("%s.positions[%d]", path, i), value)
	}
	if len(points.Positions) > 0 {
		if len(points.Positions)%3 != 0 {
			report.add(Error, "scene.points.invalid_positions", "Point positions must contain x/y/z triples", path+".positions", points.ID, map[string]any{"values": len(points.Positions)})
		} else if points.Count >= 0 && len(points.Positions)/3 != points.Count {
			report.add(Error, "scene.points.count_mismatch", "Point count does not match positions", path+".positions", points.ID, map[string]any{"count": points.Count, "positions": len(points.Positions) / 3})
		}
	}
	for i, value := range points.Sizes {
		validateNonNegativeFloat(report, points.ID, fmt.Sprintf("%s.sizes[%d]", path, i), value)
	}
	if len(points.Sizes) > 0 && points.Count >= 0 && len(points.Sizes) != points.Count {
		report.add(Error, "scene.points.count_mismatch", "Point count does not match sizes", path+".sizes", points.ID, map[string]any{"count": points.Count, "sizes": len(points.Sizes)})
	}
	if len(points.Colors) > 0 && points.Count >= 0 && len(points.Colors) != points.Count {
		report.add(Error, "scene.points.count_mismatch", "Point count does not match colors", path+".colors", points.ID, map[string]any{"count": points.Count, "colors": len(points.Colors)})
	}
	validateNumericFields(report, points.ID, path, map[string]float64{
		"size":         points.Size,
		"minPixelSize": points.MinPixelSize,
		"maxPixelSize": points.MaxPixelSize,
		"opacity":      points.Opacity,
		"x":            points.X,
		"y":            points.Y,
		"z":            points.Z,
		"rotationX":    points.RotationX,
		"rotationY":    points.RotationY,
		"rotationZ":    points.RotationZ,
		"spinX":        points.SpinX,
		"spinY":        points.SpinY,
		"spinZ":        points.SpinZ,
	})
	validateNonNegativeNumericFields(report, points.ID, path, map[string]float64{
		"size":         points.Size,
		"minPixelSize": points.MinPixelSize,
		"maxPixelSize": points.MaxPixelSize,
	})
	if points.MaxPixelSize > 0 && points.MinPixelSize > points.MaxPixelSize {
		report.add(Error, "scene.points.invalid_pixel_size", "Point minPixelSize must not exceed maxPixelSize", path+".minPixelSize", points.ID, map[string]any{"minPixelSize": points.MinPixelSize, "maxPixelSize": points.MaxPixelSize})
	}
	if total, ok := validateCompressedArrays(report, points.ID, path+".compressedPositions", points.CompressedPositions); ok && total > 0 && points.Count >= 0 && total != points.Count*3 {
		report.add(Error, "scene.points.count_mismatch", "Point count does not match compressed positions", path+".compressedPositions", points.ID, map[string]any{"count": points.Count, "values": total})
	}
	if total, ok := validateCompressedArrays(report, points.ID, path+".compressedSizes", points.CompressedSizes); ok && total > 0 && points.Count >= 0 && total != points.Count {
		report.add(Error, "scene.points.count_mismatch", "Point count does not match compressed sizes", path+".compressedSizes", points.ID, map[string]any{"count": points.Count, "values": total})
	}
	validateCompressedArrays(report, points.ID, path+".previewPositions", points.PreviewPositions)
	validateCompressedArrays(report, points.ID, path+".previewSizes", points.PreviewSizes)
	validateLive(report, points.ID, path, points.Live)
}

func validateInstancedMesh(report *Report, mesh scene.InstancedMeshIR, path string) {
	if mesh.Count < 0 {
		report.add(Error, "scene.instances.invalid_count", "Instanced mesh count must not be negative", path+".count", mesh.ID, nil)
	}
	if strings.TrimSpace(mesh.Kind) == "" {
		report.add(Error, "scene.instances.kind_missing", "Instanced mesh requires kind", path+".kind", mesh.ID, nil)
	}
	for i, value := range mesh.Transforms {
		validateFiniteFloat(report, mesh.ID, fmt.Sprintf("%s.transforms[%d]", path, i), value)
	}
	if len(mesh.Transforms) > 0 && len(mesh.Transforms)%16 != 0 {
		report.add(Error, "scene.instances.invalid_transforms", "Instanced mesh transforms must be 4x4 matrices", path+".transforms", mesh.ID, map[string]any{"values": len(mesh.Transforms)})
	}
	if mesh.Count > 0 && len(mesh.Transforms) > 0 && len(mesh.Transforms)/16 != mesh.Count {
		report.add(Error, "scene.instances.count_mismatch", "Instanced mesh count does not match transform matrix count", path+".transforms", mesh.ID, map[string]any{"count": mesh.Count, "matrices": len(mesh.Transforms) / 16})
	}
	if len(mesh.Colors) > 0 && mesh.Count >= 0 && len(mesh.Colors) != mesh.Count {
		report.add(Error, "scene.instances.count_mismatch", "Instanced mesh count does not match colors", path+".colors", mesh.ID, map[string]any{"count": mesh.Count, "colors": len(mesh.Colors)})
	}
	for name, values := range mesh.Attributes {
		for i, value := range values {
			validateFiniteFloat(report, mesh.ID, fmt.Sprintf("%s.attributes.%s[%d]", path, name, i), value)
		}
		if mesh.Count > 0 && len(values)%mesh.Count != 0 {
			report.add(Error, "scene.instances.count_mismatch", "Instanced mesh attribute length is not aligned to instance count", path+".attributes."+name, mesh.ID, map[string]any{"count": mesh.Count, "values": len(values)})
		}
	}
	if total, ok := validateCompressedArrays(report, mesh.ID, path+".compressedTransforms", mesh.CompressedTransforms); ok && total > 0 && mesh.Count >= 0 && total != mesh.Count*16 {
		report.add(Error, "scene.instances.count_mismatch", "Instanced mesh count does not match compressed transforms", path+".compressedTransforms", mesh.ID, map[string]any{"count": mesh.Count, "values": total})
	}
	validateCompressedArrays(report, mesh.ID, path+".previewTransforms", mesh.PreviewTransforms)
	validateMaterialScalars(report, mesh.ID, path, mesh.Roughness, mesh.Metalness)
	validatePrimitiveParameters(report, mesh.Kind, mesh.ID, path, mesh.Size, mesh.Width, mesh.Height, mesh.Depth, mesh.Radius, mesh.RadiusTop, mesh.RadiusBottom, mesh.Tube, mesh.Segments, mesh.RadialSegments, mesh.TubularSegments)
	validateLive(report, mesh.ID, path, mesh.Live)
}

func validateMeshInstance(report *Report, instance scene.MeshInstanceIR, path, parentID string) {
	validateNumericFields(report, parentID, path, map[string]float64{
		"x":         instance.X,
		"y":         instance.Y,
		"z":         instance.Z,
		"scaleX":    instance.ScaleX,
		"scaleY":    instance.ScaleY,
		"scaleZ":    instance.ScaleZ,
		"rotationX": instance.RotationX,
		"rotationY": instance.RotationY,
		"rotationZ": instance.RotationZ,
	})
}

func validateComputeParticles(report *Report, particles scene.ComputeParticlesIR, path string) {
	if particles.Count < 0 {
		report.add(Error, "scene.particles.invalid_count", "Compute particle count must not be negative", path+".count", particles.ID, nil)
	}
	validateNonNegativeFloat(report, particles.ID, path+".bounds", particles.Bounds)
	validateNumericFields(report, particles.ID, path+".emitter", map[string]float64{
		"x":         particles.Emitter.X,
		"y":         particles.Emitter.Y,
		"z":         particles.Emitter.Z,
		"rotationX": particles.Emitter.RotationX,
		"rotationY": particles.Emitter.RotationY,
		"rotationZ": particles.Emitter.RotationZ,
		"spinX":     particles.Emitter.SpinX,
		"spinY":     particles.Emitter.SpinY,
		"spinZ":     particles.Emitter.SpinZ,
		"radius":    particles.Emitter.Radius,
		"rate":      particles.Emitter.Rate,
		"lifetime":  particles.Emitter.Lifetime,
		"wind":      particles.Emitter.Wind,
		"scatter":   particles.Emitter.Scatter,
	})
	validateNonNegativeNumericFields(report, particles.ID, path+".emitter", map[string]float64{
		"radius":   particles.Emitter.Radius,
		"rate":     particles.Emitter.Rate,
		"lifetime": particles.Emitter.Lifetime,
		"scatter":  particles.Emitter.Scatter,
	})
	if particles.Emitter.Arms < 0 {
		report.add(Error, "scene.particles.invalid_emitter", "Compute particle emitter arms must not be negative", path+".emitter.arms", particles.ID, nil)
	}
	for i, force := range particles.Forces {
		validateNumericFields(report, particles.ID, fmt.Sprintf("%s.forces[%d]", path, i), map[string]float64{
			"strength":  force.Strength,
			"x":         force.X,
			"y":         force.Y,
			"z":         force.Z,
			"frequency": force.Frequency,
		})
	}
	validateNumericFields(report, particles.ID, path+".material", map[string]float64{
		"size":       particles.Material.Size,
		"sizeEnd":    particles.Material.SizeEnd,
		"opacity":    particles.Material.Opacity,
		"opacityEnd": particles.Material.OpacityEnd,
	})
	validateNonNegativeNumericFields(report, particles.ID, path+".material", map[string]float64{
		"size":       particles.Material.Size,
		"sizeEnd":    particles.Material.SizeEnd,
		"opacity":    particles.Material.Opacity,
		"opacityEnd": particles.Material.OpacityEnd,
	})
	validateLive(report, particles.ID, path, particles.Live)
}

func validateWaterSystem(report *Report, water scene.WaterSystemIR, path string) {
	switch strings.TrimSpace(water.InteractionProfile) {
	case "", "water-object-drop-orbit":
	default:
		report.add(Warn, "scene.water.unknown_interaction_profile", "Water simulation interactionProfile is not recognized", path+".interactionProfile", water.ID, map[string]any{"profile": water.InteractionProfile})
	}
	if water.Resolution < 0 {
		report.add(Error, "scene.water.invalid_resolution", "Water simulation resolution must not be negative", path+".resolution", water.ID, nil)
	}
	if water.SeedDrops < 0 {
		report.add(Error, "scene.water.invalid_seed_drops", "Water simulation seedDrops must not be negative", path+".seedDrops", water.ID, nil)
	}
	if water.DropEventID < 0 {
		report.add(Error, "scene.water.invalid_drop_event_id", "Water simulation dropEventID must not be negative", path+".dropEventID", water.ID, nil)
	}
	for field, value := range map[string]int{
		"surfaceResolution":        water.SurfaceResolution,
		"causticsResolution":       water.CausticsResolution,
		"objectTextureResolution":  water.ObjectTextureResolution,
		"objectTexturePixelBudget": water.ObjectTexturePixelBudget,
		"objectShadowResolution":   water.ObjectShadowResolution,
	} {
		if value < 0 {
			report.add(Error, "scene.water.invalid_texture_resolution", "Water texture target resolution must not be negative", path+"."+field, water.ID, nil)
		}
	}
	switch strings.ToLower(strings.TrimSpace(water.ObjectTextureResolutionMode)) {
	case "", "fixed", "viewport", "auto", "upstream":
	default:
		report.add(Warn, "scene.water.unknown_object_texture_resolution_mode", "Water object texture resolution mode is not recognized", path+".objectTextureResolutionMode", water.ID, map[string]any{"mode": water.ObjectTextureResolutionMode})
	}
	validateNumericFields(report, water.ID, path, map[string]float64{
		"poolWidth":         water.PoolWidth,
		"poolHeight":        water.PoolHeight,
		"poolLength":        water.PoolLength,
		"cornerRadius":      water.CornerRadius,
		"waveSpeed":         water.WaveSpeed,
		"damping":           water.Damping,
		"normalScale":       water.NormalScale,
		"dropRadius":        water.DropRadius,
		"dropStrength":      water.DropStrength,
		"dropX":             water.DropX,
		"dropZ":             water.DropZ,
		"dropEventRadius":   water.DropEventRadius,
		"dropEventStrength": water.DropEventStrength,
		"lightDirectionX":   water.LightDirectionX,
		"lightDirectionY":   water.LightDirectionY,
		"lightDirectionZ":   water.LightDirectionZ,
		"aboveWaterColorR":  water.AboveWaterColorR,
		"aboveWaterColorG":  water.AboveWaterColorG,
		"aboveWaterColorB":  water.AboveWaterColorB,
		"objectX":           water.ObjectX,
		"objectY":           water.ObjectY,
		"objectZ":           water.ObjectZ,
		"objectPreviousX":   water.ObjectPreviousX,
		"objectPreviousY":   water.ObjectPreviousY,
		"objectPreviousZ":   water.ObjectPreviousZ,
		"objectDriftX":      water.ObjectDriftX,
		"objectDriftY":      water.ObjectDriftY,
		"objectDriftZ":      water.ObjectDriftZ,
	})
	validateNonNegativeNumericFields(report, water.ID, path, map[string]float64{
		"poolWidth":               water.PoolWidth,
		"poolHeight":              water.PoolHeight,
		"poolLength":              water.PoolLength,
		"cornerRadius":            water.CornerRadius,
		"normalScale":             water.NormalScale,
		"aboveWaterColorR":        water.AboveWaterColorR,
		"aboveWaterColorG":        water.AboveWaterColorG,
		"aboveWaterColorB":        water.AboveWaterColorB,
		"dropRadius":              water.DropRadius,
		"dropEventRadius":         water.DropEventRadius,
		"objectRadius":            water.ObjectRadius,
		"objectHalfSizeX":         water.ObjectHalfSizeX,
		"objectHalfSizeY":         water.ObjectHalfSizeY,
		"objectHalfSizeZ":         water.ObjectHalfSizeZ,
		"objectBobAmplitude":      water.ObjectBobAmplitude,
		"objectBobSpeed":          water.ObjectBobSpeed,
		"objectDisplacementScale": water.ObjectDisplacementScale,
	})
	for i, sphere := range water.ObjectDisplacementSpheres {
		spherePath := fmt.Sprintf("%s.objectDisplacementSpheres[%d]", path, i)
		validateNumericFields(report, water.ID, spherePath, map[string]float64{
			"offsetX": sphere.OffsetX,
			"offsetY": sphere.OffsetY,
			"offsetZ": sphere.OffsetZ,
		})
		validateNonNegativeNumericFields(report, water.ID, spherePath, map[string]float64{
			"radius": sphere.Radius,
		})
	}
}

func validatePrimitiveParameters(report *Report, kind, id, path string, size, width, height, depth, radius, radiusTop, radiusBottom, tube float64, segments, radialSegments, tubularSegments int) {
	for _, field := range []struct {
		name  string
		value float64
	}{
		{"size", size},
		{"width", width},
		{"height", height},
		{"depth", depth},
		{"radius", radius},
		{"radiusTop", radiusTop},
		{"radiusBottom", radiusBottom},
		{"tube", tube},
	} {
		if !finite(field.value) {
			report.add(Error, "scene.primitive.non_finite", "Primitive parameter must be finite", path+"."+field.name, id, nil)
		}
		if field.value < 0 {
			report.add(Error, "scene.primitive.invalid_parameter", "Primitive parameter must not be negative", path+"."+field.name, id, map[string]any{"value": field.value})
		}
	}
	for _, field := range []struct {
		name  string
		value int
	}{
		{"segments", segments},
		{"radialSegments", radialSegments},
		{"tubularSegments", tubularSegments},
	} {
		if field.value < 0 {
			report.add(Error, "scene.primitive.invalid_segments", "Primitive segment count must not be negative", path+"."+field.name, id, map[string]any{"value": field.value})
		}
	}
	if strings.Contains(strings.ToLower(kind), "torus") && tube > 0 && radius > 0 && tube >= radius {
		report.add(Warn, "scene.primitive.torus_tube_large", "Torus tube is greater than or equal to radius; mesh may self-intersect", path+".tube", id, map[string]any{"radius": radius, "tube": tube})
	}
}

func validateLabel(report *Report, label scene.LabelIR, path string) {
	if strings.TrimSpace(label.Text) == "" {
		report.add(Error, "scene.label.text_missing", "Label scene record requires text", path+".text", label.ID, nil)
	}
	validateNumericFields(report, label.ID, path, map[string]float64{
		"x":          label.X,
		"y":          label.Y,
		"z":          label.Z,
		"priority":   label.Priority,
		"shiftX":     label.ShiftX,
		"shiftY":     label.ShiftY,
		"shiftZ":     label.ShiftZ,
		"driftSpeed": label.DriftSpeed,
		"driftPhase": label.DriftPhase,
		"maxWidth":   label.MaxWidth,
		"lineHeight": label.LineHeight,
		"offsetX":    label.OffsetX,
		"offsetY":    label.OffsetY,
		"anchorX":    label.AnchorX,
		"anchorY":    label.AnchorY,
	})
	validateNonNegativeNumericFields(report, label.ID, path, map[string]float64{
		"maxWidth":   label.MaxWidth,
		"lineHeight": label.LineHeight,
	})
	if label.MaxLines < 0 {
		report.add(Error, "scene.label.invalid_layout", "Label maxLines must not be negative", path+".maxLines", label.ID, nil)
	}
	validateLive(report, label.ID, path, label.Live)
}

func validateSprite(report *Report, sprite scene.SpriteIR, path string) {
	validateNumericFields(report, sprite.ID, path, map[string]float64{
		"x":          sprite.X,
		"y":          sprite.Y,
		"z":          sprite.Z,
		"priority":   sprite.Priority,
		"shiftX":     sprite.ShiftX,
		"shiftY":     sprite.ShiftY,
		"shiftZ":     sprite.ShiftZ,
		"driftSpeed": sprite.DriftSpeed,
		"driftPhase": sprite.DriftPhase,
		"width":      sprite.Width,
		"height":     sprite.Height,
		"scale":      sprite.Scale,
		"opacity":    sprite.Opacity,
		"offsetX":    sprite.OffsetX,
		"offsetY":    sprite.OffsetY,
		"anchorX":    sprite.AnchorX,
		"anchorY":    sprite.AnchorY,
	})
	validateNonNegativeNumericFields(report, sprite.ID, path, map[string]float64{
		"width":   sprite.Width,
		"height":  sprite.Height,
		"scale":   sprite.Scale,
		"opacity": sprite.Opacity,
	})
	validateLive(report, sprite.ID, path, sprite.Live)
}

func validateHTML(report *Report, html scene.HTMLIR, path string, opts Options, targetIDs map[string]struct{}) {
	mode := strings.ToLower(strings.TrimSpace(html.Mode))
	if mode == "" {
		mode = "dom"
	}
	if mode != "dom" && mode != "texture" && mode != "portal" && mode != "world" && mode != "screen" {
		report.add(Warn, "scene.html.unknown_mode", "HTML surface mode is not part of the formal mode set", path+".mode", html.ID, map[string]any{"mode": html.Mode})
	}
	if mode == "texture" {
		severity := Warn
		if opts.Strict {
			severity = Error
		}
		if strings.TrimSpace(html.Fallback) == "" {
			report.add(severity, "scene.html.texture_fallback", "Texture-backed HTML must include accessible fallback DOM", path+".fallback", html.ID, nil)
		}
		if html.TextureWidth <= 0 || html.TextureHeight <= 0 {
			report.add(severity, "scene.html.texture_size_missing", "Texture-backed HTML should declare texture dimensions", path, html.ID, nil)
		}
	}
	if html.TextureWidth < 0 || html.TextureHeight < 0 || html.MaxTexturePixels < 0 {
		report.add(Error, "scene.html.invalid_texture_size", "HTML texture dimensions and caps must not be negative", path, html.ID, nil)
	}
	validateNumericFields(report, html.ID, path, map[string]float64{
		"surfaceWidth":  html.SurfaceWidth,
		"surfaceHeight": html.SurfaceHeight,
		"x":             html.X,
		"y":             html.Y,
		"z":             html.Z,
		"priority":      html.Priority,
		"shiftX":        html.ShiftX,
		"shiftY":        html.ShiftY,
		"shiftZ":        html.ShiftZ,
		"driftSpeed":    html.DriftSpeed,
		"driftPhase":    html.DriftPhase,
		"width":         html.Width,
		"height":        html.Height,
		"scale":         html.Scale,
		"opacity":       html.Opacity,
		"offsetX":       html.OffsetX,
		"offsetY":       html.OffsetY,
		"anchorX":       html.AnchorX,
		"anchorY":       html.AnchorY,
	})
	validateNonNegativeNumericFields(report, html.ID, path, map[string]float64{
		"surfaceWidth":  html.SurfaceWidth,
		"surfaceHeight": html.SurfaceHeight,
		"width":         html.Width,
		"height":        html.Height,
		"scale":         html.Scale,
		"opacity":       html.Opacity,
	})
	if target := strings.TrimSpace(html.Target); target != "" {
		if _, ok := targetIDs[target]; !ok {
			report.add(Error, "scene.html.invalid_target", "HTML target must resolve to an object, model, instanced mesh, or instanced GLB mesh", path+".target", html.ID, map[string]any{"target": target})
		}
	}
	pixels := html.TextureWidth * html.TextureHeight
	capPixels := opts.MaxTexturePixels
	if html.MaxTexturePixels > 0 {
		capPixels = html.MaxTexturePixels
	}
	if capPixels > 0 && pixels > capPixels {
		report.add(Error, "scene.texture.over_budget", "HTML texture dimensions exceed pixel budget", path, html.ID, map[string]any{"pixels": pixels, "maxTexturePixels": capPixels})
	}
	validateLive(report, html.ID, path, html.Live)
}

func validateLight(report *Report, light scene.LightIR, path string) {
	if strings.TrimSpace(light.Kind) == "" {
		report.add(Error, "scene.light.kind_missing", "Light scene record requires kind", path+".kind", light.ID, nil)
	}
	validateNumericFields(report, light.ID, path, map[string]float64{
		"intensity":      light.Intensity,
		"x":              light.X,
		"y":              light.Y,
		"z":              light.Z,
		"directionX":     light.DirectionX,
		"directionY":     light.DirectionY,
		"directionZ":     light.DirectionZ,
		"angle":          light.Angle,
		"penumbra":       light.Penumbra,
		"range":          light.Range,
		"decay":          light.Decay,
		"width":          light.Width,
		"height":         light.Height,
		"shadowBias":     light.ShadowBias,
		"shadowSoftness": light.ShadowSoftness,
	})
	validateNonNegativeNumericFields(report, light.ID, path, map[string]float64{
		"intensity":      light.Intensity,
		"angle":          light.Angle,
		"penumbra":       light.Penumbra,
		"range":          light.Range,
		"decay":          light.Decay,
		"width":          light.Width,
		"height":         light.Height,
		"shadowSoftness": light.ShadowSoftness,
	})
	if light.ShadowSize < 0 {
		report.add(Error, "scene.shadow.invalid_size", "Light shadowSize must not be negative", path+".shadowSize", light.ID, nil)
	}
	if light.ShadowCascades < 0 {
		report.add(Error, "scene.shadow.invalid_size", "Light shadowCascades must not be negative", path+".shadowCascades", light.ID, nil)
	}
	for i, coefficient := range light.Coefficients {
		validateVector3(report, light.ID, fmt.Sprintf("%s.coefficients[%d]", path, i), coefficient)
	}
	validateLive(report, light.ID, path, light.Live)
}

func validateAnimation(report *Report, animation scene.AnimationClipIR, path string, nodeCount int) {
	validateNonNegativeFloat(report, animation.Name, path+".duration", animation.Duration)
	if len(animation.Channels) == 0 {
		report.add(Error, "scene.animation.channels_missing", "Animation clip requires at least one channel", path+".channels", animation.Name, nil)
	}
	for i, channel := range animation.Channels {
		validateAnimationChannel(report, animation.Name, channel, fmt.Sprintf("%s.channels[%d]", path, i), nodeCount)
	}
}

func validateAnimationChannel(report *Report, id string, channel scene.AnimationChannelIR, path string, nodeCount int) {
	if channel.TargetNode < 0 {
		report.add(Error, "scene.animation.invalid_target", "Animation targetNode must not be negative", path+".targetNode", id, nil)
	} else if channel.TargetNode >= nodeCount {
		report.add(Error, "scene.animation.invalid_target", "Animation targetNode is outside the known scene node range", path+".targetNode", id, map[string]any{"targetNode": channel.TargetNode, "nodeCount": nodeCount})
	}
	if strings.TrimSpace(channel.Property) == "" {
		report.add(Error, "scene.animation.property_missing", "Animation channel requires property", path+".property", id, nil)
	}
	for i, value := range channel.Times {
		validateFiniteFloat(report, id, fmt.Sprintf("%s.times[%d]", path, i), value)
		if value < 0 {
			report.add(Error, "scene.animation.invalid_time", "Animation keyframe times must not be negative", fmt.Sprintf("%s.times[%d]", path, i), id, map[string]any{"value": value})
		}
		if i > 0 && value < channel.Times[i-1] {
			report.add(Error, "scene.animation.invalid_time", "Animation keyframe times must be non-decreasing", fmt.Sprintf("%s.times[%d]", path, i), id, map[string]any{"previous": channel.Times[i-1], "value": value})
		}
	}
	for i, value := range channel.Values {
		validateFiniteFloat(report, id, fmt.Sprintf("%s.values[%d]", path, i), value)
	}
	hasTimes := len(channel.Times) > 0 || len(channel.CompressedTimes) > 0
	hasValues := len(channel.Values) > 0 || len(channel.CompressedValues) > 0
	if !hasTimes {
		report.add(Error, "scene.animation.times_missing", "Animation channel requires times or compressedTimes", path+".times", id, nil)
	}
	if !hasValues {
		report.add(Error, "scene.animation.values_missing", "Animation channel requires values or compressedValues", path+".values", id, nil)
	}
	if len(channel.Times) > 0 && len(channel.Values) > 0 && len(channel.Values)%len(channel.Times) != 0 {
		report.add(Error, "scene.animation.values_mismatch", "Animation values must be aligned to keyframe times", path+".values", id, map[string]any{"times": len(channel.Times), "values": len(channel.Values)})
	}
	validateCompressedArrays(report, id, path+".compressedTimes", channel.CompressedTimes)
	validateCompressedArrays(report, id, path+".compressedValues", channel.CompressedValues)
	validateCompressedArrays(report, id, path+".previewTimes", channel.PreviewTimes)
	validateCompressedArrays(report, id, path+".previewValues", channel.PreviewValues)
}

func validatePostEffect(report *Report, raw json.RawMessage, path string) {
	var record map[string]any
	if err := json.Unmarshal(raw, &record); err != nil {
		report.add(Error, "scene.post_effect.invalid", "Post effect record must be a JSON object", path, "", map[string]any{"error": err.Error()})
		return
	}
	kind, hasKind := stringField(record, "kind")
	typ, hasType := stringField(record, "type")
	if (!hasKind || strings.TrimSpace(kind) == "") && (!hasType || strings.TrimSpace(typ) == "") {
		report.add(Error, "scene.post_effect.kind_missing", "Post effect record requires kind or type", path, "", nil)
	}
}

func validateMaterialScalars(report *Report, id, path string, values ...float64) {
	names := []string{"roughness", "metalness", "clearcoat", "sheen", "transmission", "iridescence", "anisotropy"}
	for i, value := range values {
		name := "scalar"
		if i < len(names) {
			name = names[i]
		}
		if !finite(value) {
			report.add(Error, "scene.material.non_finite", "Material scalar must be finite", path+"."+name, id, nil)
		}
	}
}

func validateNumericFields(report *Report, id, path string, fields map[string]float64) {
	for name, value := range fields {
		validateFiniteFloat(report, id, path+"."+name, value)
	}
}

func validateNonNegativeNumericFields(report *Report, id, path string, fields map[string]float64) {
	for name, value := range fields {
		validateNonNegativeFloat(report, id, path+"."+name, value)
	}
}

func validateFiniteFloat(report *Report, id, path string, value float64) {
	if !finite(value) {
		report.add(Error, "scene.numeric.non_finite", "Scene numeric field must be finite", path, id, nil)
	}
}

func validateNonNegativeFloat(report *Report, id, path string, value float64) {
	validateFiniteFloat(report, id, path, value)
	if value < 0 {
		report.add(Error, "scene.numeric.negative", "Scene numeric field must not be negative", path, id, map[string]any{"value": value})
	}
}

func validateVector3(report *Report, id, path string, value scene.Vector3) {
	validateNumericFields(report, id, path, map[string]float64{
		"x": value.X,
		"y": value.Y,
		"z": value.Z,
	})
}

func validateLive(report *Report, id, path string, liveFields []string) {
	for i, live := range liveFields {
		if strings.TrimSpace(live) == "" {
			report.add(Error, "scene.live.invalid", "Live field name must not be empty", fmt.Sprintf("%s.live[%d]", path, i), id, nil)
		}
	}
}

func validateCompressedArrays(report *Report, id, path string, chunks []scene.CompressedArray) (int, bool) {
	total := 0
	validCounts := true
	for i, chunk := range chunks {
		chunkPath := fmt.Sprintf("%s[%d]", path, i)
		if math.IsNaN(float64(chunk.Norm)) || math.IsInf(float64(chunk.Norm), 0) {
			report.add(Error, "scene.compressed.non_finite", "Compressed array norm must be finite", chunkPath+".norm", id, nil)
		}
		if math.IsNaN(float64(chunk.MaxVal)) || math.IsInf(float64(chunk.MaxVal), 0) {
			report.add(Error, "scene.compressed.non_finite", "Compressed array maxVal must be finite", chunkPath+".maxVal", id, nil)
		}
		if chunk.Dim < 0 {
			report.add(Error, "scene.compressed.invalid_metadata", "Compressed array dim must not be negative", chunkPath+".dim", id, map[string]any{"dim": chunk.Dim})
			validCounts = false
		}
		if chunk.Count < 0 {
			report.add(Error, "scene.compressed.invalid_metadata", "Compressed array count must not be negative", chunkPath+".count", id, map[string]any{"count": chunk.Count})
			validCounts = false
		} else {
			total += chunk.Count
		}
		if chunk.BitWidth < 0 {
			report.add(Error, "scene.compressed.invalid_metadata", "Compressed array bitWidth must not be negative", chunkPath+".bitWidth", id, map[string]any{"bitWidth": chunk.BitWidth})
		}
	}
	return total, validCounts
}

func stringField(record map[string]any, name string) (string, bool) {
	value, ok := record[name]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

func (r *Report) add(severity Severity, code, message, path, id string, data map[string]any) {
	r.Diagnostics = append(r.Diagnostics, Diagnostic{
		Severity: severity,
		Code:     code,
		Message:  message,
		Path:     path,
		ID:       id,
		Data:     data,
	})
}

func hasError(diags []Diagnostic) bool {
	for _, diag := range diags {
		if diag.Severity == Error || diag.Severity == Fatal {
			return true
		}
	}
	return false
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
