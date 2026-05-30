package vm

import (
	"encoding/json"
	"hash/fnv"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	rootengine "m31labs.dev/gosx/engine"
)

func buildRenderBundle(props map[string]any, nodes []resolvedNode, width, height int, timeSeconds float64) rootengine.RenderBundle {
	if width <= 0 {
		width = 720
	}
	if height <= 0 {
		height = 420
	}
	postEffects, diagnostics := nativeRenderPostEffects(props)
	animations := nativeRenderAnimations(props)

	bundle := rootengine.RenderBundle{
		Background:      sceneBackground(props),
		Materials:       []rootengine.RenderMaterial{},
		Objects:         []rootengine.RenderObject{},
		Surfaces:        []rootengine.RenderSurface{},
		Lights:          []rootengine.RenderLight{},
		Lines:           []rootengine.RenderLine{},
		Labels:          []rootengine.RenderLabel{},
		Sprites:         []rootengine.RenderSprite{},
		Positions:       []float64{},
		Colors:          []float64{},
		WorldPositions:  []float64{},
		WorldColors:     []float64{},
		Animations:      animations,
		PostEffects:     postEffects,
		PostFXMaxPixels: int(math.Max(0, math.Floor(numberFromAny(sceneValue(props, "postFXMaxPixels"), numberFromAny(propValue(props, "postFXMaxPixels"), 0))))),
		Diagnostics:     diagnostics,
	}

	camera := sceneCameraFromProps(props)
	lights := sceneLightsFromProps(props)
	sprites := sceneSpritesFromProps(props)
	objects := make([]sceneObject, 0, len(nodes))
	labels := make([]sceneLabel, 0, len(nodes))
	for index, node := range nodes {
		switch strings.TrimSpace(strings.ToLower(node.Kind)) {
		case "camera":
			camera = normalizeSceneCameraMap(node.Props, camera)
		case "light":
			if light, ok := sceneLightFromResolvedNode(index, node); ok {
				lights = append(lights, light)
			}
		case "mesh":
			objects = append(objects, sceneObjectFromResolvedNode(index, node))
		case "label":
			if label, ok := sceneLabelFromResolvedNode(index, node); ok {
				labels = append(labels, label)
			}
		case "sprite":
			if sprite, ok := sceneSpriteFromResolvedNode(index, node); ok {
				sprites = append(sprites, sprite)
			}
		}
	}
	environment := resolveSceneEnvironment(props, len(lights) > 0)
	bundle.Camera = rootengine.RenderCamera{
		X:         camera.X,
		Y:         camera.Y,
		Z:         camera.Z,
		RotationX: camera.RotationX,
		RotationY: camera.RotationY,
		RotationZ: camera.RotationZ,
		FOV:       camera.FOV,
		Near:      camera.Near,
		Far:       camera.Far,
	}
	bundle.Environment = renderSceneEnvironment(environment)
	if len(lights) > 0 {
		bundle.Lights = renderSceneLights(lights)
	}
	appendSceneGrid(&bundle, width, height)
	for _, object := range objects {
		vertexOffset := len(bundle.WorldPositions) / 3
		materialIndex := ensureRenderMaterial(&bundle, object)
		material := bundle.Materials[materialIndex]
		appendResult := appendSceneObject(&bundle, camera, width, height, object, material, lights, environment, timeSeconds)
		vertexCount := (len(bundle.WorldPositions) / 3) - vertexOffset
		if vertexCount > 0 || appendResult.HasBounds || appendResult.ViewCulled {
			bounds := appendResult.Bounds
			if !appendResult.HasBounds && vertexCount > 0 {
				bounds = renderObjectBounds(bundle.WorldPositions, vertexOffset, vertexCount)
			}
			depthNear, depthFar, depthCenter := renderBoundsDepthMetrics(bounds, camera)
			bundle.Objects = append(bundle.Objects, rootengine.RenderObject{
				ID:            object.ID,
				Kind:          object.Kind,
				Pickable:      object.Pickable,
				MaterialIndex: materialIndex,
				RenderPass:    bundle.Materials[materialIndex].RenderPass,
				VertexOffset:  vertexOffset,
				VertexCount:   vertexCount,
				Static:        object.Static,
				Bounds:        bounds,
				DepthNear:     depthNear,
				DepthFar:      depthFar,
				DepthCenter:   depthCenter,
				ViewCulled:    appendResult.ViewCulled,
			})
			appendSceneSurface(&bundle, camera, width, height, object, materialIndex, material, bounds, timeSeconds)
		}
	}
	for _, label := range labels {
		appendSceneLabel(&bundle, camera, width, height, label, timeSeconds)
	}
	for _, sprite := range sprites {
		appendSceneSprite(&bundle, camera, width, height, sprite, timeSeconds)
	}
	bundle.ObjectCount = len(bundle.Objects)
	bundle.VertexCount = len(bundle.Positions) / 2
	bundle.WorldVertexCount = len(bundle.WorldPositions) / 3
	bundle.Passes = buildRenderPassBundles(bundle)
	return bundle
}

func appendSceneGrid(bundle *rootengine.RenderBundle, width, height int) {
	for x := 0; x <= width; x += 48 {
		appendSceneLine(bundle, width, height, rootengine.RenderPoint{X: float64(x), Y: 0}, rootengine.RenderPoint{X: float64(x), Y: float64(height)}, "rgba(141, 225, 255, 0.14)", 1)
	}
	for y := 0; y <= height; y += 48 {
		appendSceneLine(bundle, width, height, rootengine.RenderPoint{X: 0, Y: float64(y)}, rootengine.RenderPoint{X: float64(width), Y: float64(y)}, "rgba(141, 225, 255, 0.14)", 1)
	}
}

func appendSceneObject(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, object sceneObject, material rootengine.RenderMaterial, lights []sceneLight, environment sceneEnvironment, timeSeconds float64) sceneAppendResult {
	aspect := math.Max(0.0001, float64(width)/math.Max(1, float64(height)))
	result := sceneAppendResult{}
	if !sceneObjectUsesLineGeometry(object, material) && sceneObjectHasTexturedSurface(object, material) {
		for _, corner := range scenePlaneSurfaceCorners(object, timeSeconds) {
			result.Bounds, result.HasBounds = expandRenderBounds(result.Bounds, result.HasBounds, corner)
		}
		if result.HasBounds {
			result.ViewCulled = renderBoundsOutsideFrustum(result.Bounds, camera, width, height)
		}
		return result
	}
	for _, segment := range sceneObjectSegments(object) {
		worldFrom := translatePoint(segment[0], object, timeSeconds)
		worldTo := translatePoint(segment[1], object, timeSeconds)
		fromNormal := sceneObjectWorldNormal(object, segment[0], timeSeconds)
		toNormal := sceneObjectWorldNormal(object, segment[1], timeSeconds)
		fromRGBA := sceneLitColorRGBA(material, worldFrom, fromNormal, lights, environment)
		toRGBA := sceneLitColorRGBA(material, worldTo, toNormal, lights, environment)
		result.Bounds, result.HasBounds = expandRenderBounds(result.Bounds, result.HasBounds, worldFrom)
		result.Bounds, result.HasBounds = expandRenderBounds(result.Bounds, result.HasBounds, worldTo)
		clippedFrom, clippedTo, ok := clipWorldSegmentForCamera(worldFrom, worldTo, camera, aspect)
		if !ok {
			continue
		}
		appendWorldSceneLine(bundle, clippedFrom, clippedTo, fromRGBA, toRGBA)
		from := projectPoint(clippedFrom, camera, width, height)
		to := projectPoint(clippedTo, camera, width, height)
		if from == nil || to == nil {
			continue
		}
		stroke := mixRGBA(fromRGBA, toRGBA)
		stroke[3] = clamp(stroke[3]*material.Opacity, 0, 1)
		appendSceneLine(bundle, width, height, *from, *to, sceneRGBAString(stroke), 1.8)
	}
	if result.HasBounds {
		result.ViewCulled = renderBoundsOutsideFrustum(result.Bounds, camera, width, height)
	}
	return result
}

func sceneObjectUsesLineGeometry(object sceneObject, material rootengine.RenderMaterial) bool {
	return !(sceneObjectHasTexturedSurface(object, material) && !material.Wireframe)
}

func appendSceneSurface(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, object sceneObject, materialIndex int, material rootengine.RenderMaterial, bounds rootengine.RenderBounds, timeSeconds float64) {
	if !sceneObjectHasTexturedSurface(object, material) {
		return
	}
	corners := scenePlaneSurfaceCorners(object, timeSeconds)
	if len(corners) != 4 {
		return
	}
	depthNear, depthFar, depthCenter := renderBoundsDepthMetrics(bounds, camera)
	bundle.Surfaces = append(bundle.Surfaces, rootengine.RenderSurface{
		ID:            object.ID,
		Kind:          object.Kind,
		MaterialIndex: materialIndex,
		RenderPass:    material.RenderPass,
		Static:        object.Static,
		Positions:     scenePlaneSurfacePositions(corners),
		UV:            scenePlaneSurfaceUVs(),
		VertexCount:   6,
		Bounds:        bounds,
		DepthNear:     depthNear,
		DepthFar:      depthFar,
		DepthCenter:   depthCenter,
		ViewCulled:    renderBoundsOutsideFrustum(bounds, camera, width, height),
	})
}

func sceneObjectHasTexturedSurface(object sceneObject, material rootengine.RenderMaterial) bool {
	return object.Kind == "plane" && strings.TrimSpace(material.Texture) != ""
}

func scenePlaneSurfaceCorners(object sceneObject, timeSeconds float64) []point3 {
	vertices := boxVertices(object.Width, 0, object.Depth)
	if len(vertices) < 4 {
		return nil
	}
	return []point3{
		translatePoint(vertices[0], object, timeSeconds),
		translatePoint(vertices[1], object, timeSeconds),
		translatePoint(vertices[2], object, timeSeconds),
		translatePoint(vertices[3], object, timeSeconds),
	}
}

func scenePlaneSurfacePositions(corners []point3) []float64 {
	if len(corners) < 4 {
		return nil
	}
	return []float64{
		corners[0].X, corners[0].Y, corners[0].Z,
		corners[1].X, corners[1].Y, corners[1].Z,
		corners[2].X, corners[2].Y, corners[2].Z,
		corners[0].X, corners[0].Y, corners[0].Z,
		corners[2].X, corners[2].Y, corners[2].Z,
		corners[3].X, corners[3].Y, corners[3].Z,
	}
}

func scenePlaneSurfaceUVs() []float64 {
	return []float64{
		0, 1,
		1, 1,
		1, 0,
		0, 1,
		1, 0,
		0, 0,
	}
}

func appendSceneLabel(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, label sceneLabel, timeSeconds float64) {
	world := sceneLabelPoint(label, timeSeconds)
	position := projectPoint(world, camera, width, height)
	if position == nil {
		return
	}
	marginX := math.Max(24, label.MaxWidth)
	marginY := math.Max(24, label.LineHeight*2)
	if position.X < -marginX || position.X > float64(width)+marginX || position.Y < -marginY || position.Y > float64(height)+marginY {
		return
	}
	bundle.Labels = append(bundle.Labels, rootengine.RenderLabel{
		ID:          label.ID,
		Text:        label.Text,
		ClassName:   label.ClassName,
		Position:    *position,
		Depth:       cameraLocalPoint(world, camera).Z,
		Priority:    label.Priority,
		MaxWidth:    label.MaxWidth,
		MaxLines:    label.MaxLines,
		Overflow:    label.Overflow,
		Font:        label.Font,
		LineHeight:  label.LineHeight,
		Color:       label.Color,
		Background:  label.Background,
		BorderColor: label.BorderColor,
		OffsetX:     label.OffsetX,
		OffsetY:     label.OffsetY,
		AnchorX:     label.AnchorX,
		AnchorY:     label.AnchorY,
		Collision:   label.Collision,
		Occlude:     label.Occlude,
		WhiteSpace:  label.WhiteSpace,
		TextAlign:   label.TextAlign,
	})
}

func appendSceneSprite(bundle *rootengine.RenderBundle, camera sceneCamera, width, height int, sprite sceneSprite, timeSeconds float64) {
	point := sceneSpritePoint(sprite, timeSeconds)
	position := projectPoint(point, camera, width, height)
	if position == nil {
		return
	}
	depth := cameraLocalPoint(point, camera).Z
	screenWidth, screenHeight := projectedSceneSpriteSize(camera, width, height, sprite, depth)
	if screenWidth <= 0 || screenHeight <= 0 {
		return
	}
	marginX := math.Max(24, screenWidth)
	marginY := math.Max(24, screenHeight)
	if position.X < -marginX || position.X > float64(width)+marginX || position.Y < -marginY || position.Y > float64(height)+marginY {
		return
	}
	bundle.Sprites = append(bundle.Sprites, rootengine.RenderSprite{
		ID:        sprite.ID,
		Src:       sprite.Src,
		ClassName: sprite.ClassName,
		Position:  *position,
		Depth:     depth,
		Priority:  sprite.Priority,
		Width:     screenWidth,
		Height:    screenHeight,
		Opacity:   sprite.Opacity,
		OffsetX:   sprite.OffsetX,
		OffsetY:   sprite.OffsetY,
		AnchorX:   sprite.AnchorX,
		AnchorY:   sprite.AnchorY,
		Occlude:   sprite.Occlude,
		Fit:       normalizeSceneSpriteFit(sprite.Fit),
	})
}

func appendWorldSceneLine(bundle *rootengine.RenderBundle, from, to point3, fromRGBA, toRGBA [4]float64) {
	bundle.WorldPositions = append(bundle.WorldPositions,
		from.X, from.Y, from.Z,
		to.X, to.Y, to.Z,
	)
	bundle.WorldColors = append(bundle.WorldColors,
		fromRGBA[0], fromRGBA[1], fromRGBA[2], fromRGBA[3],
		toRGBA[0], toRGBA[1], toRGBA[2], toRGBA[3],
	)
}

func sceneLabelPoint(label sceneLabel, timeSeconds float64) point3 {
	offset := sceneLabelOffset(label, timeSeconds)
	return point3{
		X: label.X + offset.X,
		Y: label.Y + offset.Y,
		Z: label.Z + offset.Z,
	}
}

func sceneLabelOffset(label sceneLabel, timeSeconds float64) point3 {
	if label.ShiftX == 0 && label.ShiftY == 0 && label.ShiftZ == 0 {
		return point3{}
	}
	angle := label.DriftPhase + timeSeconds*label.DriftSpeed
	return point3{
		X: math.Cos(angle) * label.ShiftX,
		Y: math.Sin(angle*0.82+label.DriftPhase*0.35) * label.ShiftY,
		Z: math.Sin(angle) * label.ShiftZ,
	}
}

func sceneSpritePoint(sprite sceneSprite, timeSeconds float64) point3 {
	offset := sceneSpriteOffset(sprite, timeSeconds)
	return point3{
		X: sprite.X + offset.X,
		Y: sprite.Y + offset.Y,
		Z: sprite.Z + offset.Z,
	}
}

func sceneSpriteOffset(sprite sceneSprite, timeSeconds float64) point3 {
	if sprite.ShiftX == 0 && sprite.ShiftY == 0 && sprite.ShiftZ == 0 {
		return point3{}
	}
	angle := sprite.DriftPhase + timeSeconds*sprite.DriftSpeed
	return point3{
		X: math.Cos(angle) * sprite.ShiftX,
		Y: math.Sin(angle*0.82+sprite.DriftPhase*0.35) * sprite.ShiftY,
		Z: math.Sin(angle) * sprite.ShiftZ,
	}
}

func projectedSceneSpriteSize(camera sceneCamera, width, height int, sprite sceneSprite, depth float64) (float64, float64) {
	if depth <= 0 {
		return 0, 0
	}
	focal := (math.Min(float64(width), float64(height)) / 2) / math.Tan((camera.FOV*math.Pi)/360)
	scale := sprite.Scale
	if scale <= 0 {
		scale = 1
	}
	worldWidth := sprite.Width
	if worldWidth <= 0 {
		worldWidth = 1.25
	}
	worldHeight := sprite.Height
	if worldHeight <= 0 {
		worldHeight = worldWidth
	}
	return math.Max(1, (worldWidth*scale*focal)/depth), math.Max(1, (worldHeight*scale*focal)/depth)
}

func ensureRenderMaterial(bundle *rootengine.RenderBundle, object sceneObject) int {
	profile := resolveRenderMaterial(object)
	for index, existing := range bundle.Materials {
		if renderMaterialEqual(existing, profile) {
			return index
		}
	}
	bundle.Materials = append(bundle.Materials, profile)
	return len(bundle.Materials) - 1
}

func renderMaterialEqual(left, right rootengine.RenderMaterial) bool {
	if left.Key != right.Key ||
		left.Kind != right.Kind ||
		left.Color != right.Color ||
		left.Texture != right.Texture ||
		left.Opacity != right.Opacity ||
		left.Wireframe != right.Wireframe ||
		left.BlendMode != right.BlendMode ||
		left.RenderPass != right.RenderPass ||
		left.Emissive != right.Emissive ||
		left.Roughness != right.Roughness ||
		left.Metalness != right.Metalness ||
		left.Clearcoat != right.Clearcoat ||
		left.Sheen != right.Sheen ||
		left.Transmission != right.Transmission ||
		left.Iridescence != right.Iridescence ||
		left.Anisotropy != right.Anisotropy ||
		left.NormalMap != right.NormalMap ||
		left.RoughnessMap != right.RoughnessMap ||
		left.MetalnessMap != right.MetalnessMap ||
		left.EmissiveMap != right.EmissiveMap ||
		left.CustomVertex != right.CustomVertex ||
		left.CustomFragment != right.CustomFragment ||
		left.CustomVertexWGSL != right.CustomVertexWGSL ||
		left.CustomFragmentWGSL != right.CustomFragmentWGSL ||
		!reflect.DeepEqual(left.CustomUniforms, right.CustomUniforms) {
		return false
	}
	if len(left.ShaderData) != len(right.ShaderData) {
		return false
	}
	for i := range left.ShaderData {
		if left.ShaderData[i] != right.ShaderData[i] {
			return false
		}
	}
	return true
}

func resolveRenderMaterial(object sceneObject) rootengine.RenderMaterial {
	profile := rootengine.RenderMaterial{
		Kind:      stringFromAny(object.Material, "flat"),
		Color:     object.Color,
		Texture:   strings.TrimSpace(object.Texture),
		Opacity:   1,
		Wireframe: true,
		BlendMode: "opaque",
		Emissive:  0,
		Roughness: 0.55,
		Metalness: 0,
	}
	profile.CustomVertex = object.CustomVertex
	profile.CustomFragment = object.CustomFragment
	profile.CustomVertexWGSL = object.CustomVertexWGSL
	profile.CustomFragmentWGSL = object.CustomFragmentWGSL
	profile.CustomUniforms = cloneAnyMap(object.CustomUniforms)

	kindKey := strings.ToLower(strings.TrimSpace(profile.Kind))
	customProfile, hasCustomProfile := materialProfileForKind(kindKey)
	if hasCustomProfile {
		profile.Kind = kindKey
		if customProfile.HasOpacity {
			profile.Opacity = customProfile.Opacity
		}
		if customProfile.HasWireframe {
			profile.Wireframe = customProfile.Wireframe
		}
		if customProfile.HasBlendMode && strings.TrimSpace(customProfile.BlendMode) != "" {
			profile.BlendMode = customProfile.BlendMode
		}
		if customProfile.HasEmissive {
			profile.Emissive = customProfile.Emissive
		}
		if customProfile.HasRoughness {
			profile.Roughness = clamp(customProfile.Roughness, 0, 1)
		}
		if customProfile.HasMetalness {
			profile.Metalness = clamp(customProfile.Metalness, 0, 1)
		}
		if customProfile.HasClearcoat {
			profile.Clearcoat = clamp(customProfile.Clearcoat, 0, 1)
		}
		if customProfile.HasSheen {
			profile.Sheen = clamp(customProfile.Sheen, 0, 1)
		}
		if customProfile.HasTransmission {
			profile.Transmission = clamp(customProfile.Transmission, 0, 1)
		}
		if customProfile.HasIridescence {
			profile.Iridescence = clamp(customProfile.Iridescence, 0, 1)
		}
		if customProfile.HasAnisotropy {
			profile.Anisotropy = clamp(customProfile.Anisotropy, -1, 1)
		}
		if customProfile.HasNormalMap {
			profile.NormalMap = strings.TrimSpace(customProfile.NormalMap)
		}
		if customProfile.HasRoughnessMap {
			profile.RoughnessMap = strings.TrimSpace(customProfile.RoughnessMap)
		}
		if customProfile.HasMetalnessMap {
			profile.MetalnessMap = strings.TrimSpace(customProfile.MetalnessMap)
		}
		if customProfile.HasEmissiveMap {
			profile.EmissiveMap = strings.TrimSpace(customProfile.EmissiveMap)
		}
	}

	switch kindKey {
	case "ghost":
		if !hasCustomProfile {
			profile.Opacity = 0.42
			profile.BlendMode = "alpha"
			profile.Emissive = 0.12
		}
	case "glass":
		if !hasCustomProfile {
			profile.Opacity = 0.28
			profile.BlendMode = "alpha"
			profile.Emissive = 0.08
		}
	case "glow":
		if !hasCustomProfile {
			profile.Opacity = 0.92
			profile.BlendMode = "additive"
			profile.Emissive = 0.42
		}
	case "matte":
		if !hasCustomProfile {
			profile.Wireframe = true
		}
	case "flat":
	}

	if object.HasOpacity {
		profile.Opacity = object.Opacity
	}
	if object.HasWireframe {
		profile.Wireframe = object.Wireframe
	}
	if object.HasBlendMode && object.BlendMode != "" {
		profile.BlendMode = object.BlendMode
	}
	if object.HasEmissive {
		profile.Emissive = object.Emissive
	}
	if object.HasRoughness {
		profile.Roughness = object.Roughness
	}
	if object.HasMetalness {
		profile.Metalness = object.Metalness
	}
	if object.HasClearcoat {
		profile.Clearcoat = object.Clearcoat
	}
	if object.HasSheen {
		profile.Sheen = object.Sheen
	}
	if object.HasTransmission {
		profile.Transmission = object.Transmission
	}
	if object.HasIridescence {
		profile.Iridescence = object.Iridescence
	}
	if object.HasAnisotropy {
		profile.Anisotropy = object.Anisotropy
	}
	if object.HasTexture {
		profile.Texture = strings.TrimSpace(object.Texture)
	}
	if object.HasNormalMap {
		profile.NormalMap = strings.TrimSpace(object.NormalMap)
	}
	if object.HasRoughnessMap {
		profile.RoughnessMap = strings.TrimSpace(object.RoughnessMap)
	}
	if object.HasMetalnessMap {
		profile.MetalnessMap = strings.TrimSpace(object.MetalnessMap)
	}
	if object.HasEmissiveMap {
		profile.EmissiveMap = strings.TrimSpace(object.EmissiveMap)
	}
	if profile.Opacity < 0.999 && profile.BlendMode == "opaque" {
		profile.BlendMode = "alpha"
	}
	profile.RenderPass = renderPassFromMaterialProfile(profile)
	profile.Key = renderMaterialKey(profile)
	if hasCustomProfile && len(customProfile.ShaderData) >= 3 {
		profile.ShaderData = cloneMaterialShaderData(customProfile.ShaderData)
	} else {
		profile.ShaderData = renderMaterialShaderData(profile)
	}
	return profile
}

func renderPassFromMaterialProfile(profile rootengine.RenderMaterial) string {
	switch strings.ToLower(strings.TrimSpace(profile.BlendMode)) {
	case "additive":
		return "additive"
	case "alpha":
		return "alpha"
	}
	if profile.Opacity < 0.999 {
		return "alpha"
	}
	return "opaque"
}

func renderMaterialKey(profile rootengine.RenderMaterial) string {
	// Direct Builder writes avoid the fmt.Sprintf boxing/reflection walk
	// that this runs on every material profile (one per scene element).
	kind := strings.ToLower(strings.TrimSpace(profile.Kind))
	color := strings.TrimSpace(profile.Color)
	texture := strings.TrimSpace(profile.Texture)
	normalMap := strings.TrimSpace(profile.NormalMap)
	roughnessMap := strings.TrimSpace(profile.RoughnessMap)
	metalnessMap := strings.TrimSpace(profile.MetalnessMap)
	emissiveMap := strings.TrimSpace(profile.EmissiveMap)
	blendMode := strings.ToLower(strings.TrimSpace(profile.BlendMode))
	var b strings.Builder
	b.Grow(len(kind) + len(color) + len(texture) + len(normalMap) + len(roughnessMap) + len(metalnessMap) + len(emissiveMap) + len(blendMode) + len(profile.RenderPass) + 120)
	b.WriteString(kind)
	b.WriteByte('|')
	b.WriteString(color)
	b.WriteByte('|')
	b.WriteString(texture)
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Roughness, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Metalness, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Clearcoat, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Sheen, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Transmission, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Iridescence, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Anisotropy, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(normalMap)
	b.WriteByte('|')
	b.WriteString(roughnessMap)
	b.WriteByte('|')
	b.WriteString(metalnessMap)
	b.WriteByte('|')
	b.WriteString(emissiveMap)
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Opacity, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strconv.FormatBool(profile.Wireframe))
	b.WriteByte('|')
	b.WriteString(blendMode)
	b.WriteByte('|')
	b.WriteString(profile.RenderPass)
	b.WriteByte('|')
	b.WriteString(strconv.FormatFloat(profile.Emissive, 'f', 3, 64))
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(profile.CustomVertex))
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(profile.CustomFragment))
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(profile.CustomVertexWGSL))
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(profile.CustomFragmentWGSL))
	if len(profile.CustomUniforms) > 0 {
		b.WriteByte('|')
		b.WriteString(renderMaterialCustomUniformKey(profile.CustomUniforms))
	}
	return b.String()
}

func renderMaterialCustomUniformKey(values map[string]any) string {
	if len(values) == 0 {
		return ""
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return ""
	}
	return string(encoded)
}

func renderMaterialShaderData(profile rootengine.RenderMaterial) []float64 {
	kind := strings.ToLower(strings.TrimSpace(profile.Kind))
	emissive := profile.Emissive
	switch kind {
	case "ghost":
		return []float64{1, emissive, 0.3}
	case "glass":
		return []float64{2, emissive, 0.7}
	case "glow":
		return []float64{3, emissive, 1}
	case "matte":
		return []float64{4, emissive, 0.2}
	default:
		return []float64{0, emissive, 1}
	}
}

func buildRenderPassBundles(bundle rootengine.RenderBundle) []rootengine.RenderPassBundle {
	passes := map[string]*rootengine.RenderPassBundle{
		"staticOpaque": {
			Name:      "staticOpaque",
			Blend:     "opaque",
			Depth:     "opaque",
			Static:    true,
			CacheKey:  renderStaticPassKey(bundle),
			Positions: []float64{},
			Colors:    []float64{},
			Materials: []float64{},
		},
		"dynamicOpaque": {
			Name:      "dynamicOpaque",
			Blend:     "opaque",
			Depth:     "opaque",
			Positions: []float64{},
			Colors:    []float64{},
			Materials: []float64{},
		},
		"alpha": {
			Name:      "alpha",
			Blend:     "alpha",
			Depth:     "translucent",
			Positions: []float64{},
			Colors:    []float64{},
			Materials: []float64{},
		},
		"additive": {
			Name:      "additive",
			Blend:     "additive",
			Depth:     "translucent",
			Positions: []float64{},
			Colors:    []float64{},
			Materials: []float64{},
		},
	}
	opaqueObjects := []rootengine.RenderObject{}
	alphaObjects := []rootengine.RenderObject{}
	additiveObjects := []rootengine.RenderObject{}

	for _, object := range bundle.Objects {
		switch renderPassBucketName(object) {
		case "alpha":
			alphaObjects = append(alphaObjects, object)
		case "additive":
			additiveObjects = append(additiveObjects, object)
		default:
			opaqueObjects = append(opaqueObjects, object)
		}
	}

	sort.SliceStable(alphaObjects, func(i, j int) bool {
		if alphaObjects[i].DepthCenter != alphaObjects[j].DepthCenter {
			return alphaObjects[i].DepthCenter > alphaObjects[j].DepthCenter
		}
		return alphaObjects[i].VertexOffset < alphaObjects[j].VertexOffset
	})
	sort.SliceStable(additiveObjects, func(i, j int) bool {
		if additiveObjects[i].DepthCenter != additiveObjects[j].DepthCenter {
			return additiveObjects[i].DepthCenter > additiveObjects[j].DepthCenter
		}
		return additiveObjects[i].VertexOffset < additiveObjects[j].VertexOffset
	})

	for _, object := range opaqueObjects {
		appendRenderPassObject(passes, bundle, object)
	}
	for _, object := range alphaObjects {
		appendRenderPassObject(passes, bundle, object)
	}
	for _, object := range additiveObjects {
		appendRenderPassObject(passes, bundle, object)
	}

	ordered := []rootengine.RenderPassBundle{
		finalizeRenderPassBundle(passes["staticOpaque"]),
		finalizeRenderPassBundle(passes["dynamicOpaque"]),
	}
	if passes["alpha"].VertexCount > 0 {
		ordered = append(ordered, finalizeRenderPassBundle(passes["alpha"]))
	}
	if passes["additive"].VertexCount > 0 {
		ordered = append(ordered, finalizeRenderPassBundle(passes["additive"]))
	}
	return ordered
}

func appendRenderPassObject(passes map[string]*rootengine.RenderPassBundle, bundle rootengine.RenderBundle, object rootengine.RenderObject) {
	if object.ViewCulled || object.VertexCount <= 0 {
		return
	}
	material := bundle.Materials[object.MaterialIndex]
	if !renderObjectUsesLinePass(object, material) {
		return
	}
	passName := renderPassBucketName(object)
	pass := passes[passName]
	if pass == nil {
		return
	}
	pass.Positions = appendPassSlice(pass.Positions, bundle.WorldPositions, object.VertexOffset*3, object.VertexCount*3)
	appendPassColors(pass, bundle.WorldColors, object, material)
	appendPassMaterials(pass, material, object.VertexCount)
}

func renderObjectUsesLinePass(object rootengine.RenderObject, material rootengine.RenderMaterial) bool {
	return !(object.Kind == "plane" && strings.TrimSpace(material.Texture) != "" && !material.Wireframe)
}

func appendPassSlice(target []float64, source []float64, start, length int) []float64 {
	end := start + length
	if start < 0 || end > len(source) || start > end {
		return target
	}
	return append(target, source[start:end]...)
}

func appendPassColors(pass *rootengine.RenderPassBundle, source []float64, object rootengine.RenderObject, material rootengine.RenderMaterial) {
	start := object.VertexOffset * 4
	end := start + object.VertexCount*4
	if start < 0 || end > len(source) || start > end {
		return
	}
	opacity := material.Opacity
	for i := start; i < end; i += 4 {
		pass.Colors = append(pass.Colors,
			source[i],
			source[i+1],
			source[i+2],
			source[i+3]*opacity,
		)
	}
}

func appendPassMaterials(pass *rootengine.RenderPassBundle, material rootengine.RenderMaterial, vertexCount int) {
	if len(material.ShaderData) < 3 || vertexCount <= 0 {
		return
	}
	for i := 0; i < vertexCount; i++ {
		pass.Materials = append(pass.Materials, material.ShaderData[0], material.ShaderData[1], material.ShaderData[2])
	}
}

func finalizeRenderPassBundle(pass *rootengine.RenderPassBundle) rootengine.RenderPassBundle {
	if pass == nil {
		return rootengine.RenderPassBundle{}
	}
	pass.VertexCount = len(pass.Positions) / 3
	return *pass
}

func renderPassBucketName(object rootengine.RenderObject) string {
	switch object.RenderPass {
	case "additive":
		return "additive"
	case "alpha":
		return "alpha"
	case "opaque":
		if object.Static {
			return "staticOpaque"
		}
		return "dynamicOpaque"
	default:
		if object.Static {
			return "staticOpaque"
		}
		return "dynamicOpaque"
	}
}

func renderStaticPassKey(bundle rootengine.RenderBundle) string {
	hasher := fnv.New64a()
	writeStaticPassFloat := func(value float64) {
		_, _ = hasher.Write([]byte(strconv.FormatFloat(value, 'f', 3, 64)))
		_, _ = hasher.Write([]byte{'|'})
	}
	writeStaticPassString := func(value string) {
		_, _ = hasher.Write([]byte(value))
		_, _ = hasher.Write([]byte{'|'})
	}

	writeStaticPassFloat(bundle.Camera.X)
	writeStaticPassFloat(bundle.Camera.Y)
	writeStaticPassFloat(bundle.Camera.Z)
	writeStaticPassFloat(bundle.Camera.RotationX)
	writeStaticPassFloat(bundle.Camera.RotationY)
	writeStaticPassFloat(bundle.Camera.RotationZ)
	writeStaticPassFloat(bundle.Camera.FOV)
	writeStaticPassFloat(bundle.Camera.Near)
	writeStaticPassFloat(bundle.Camera.Far)

	for _, object := range bundle.Objects {
		if object.ViewCulled || object.VertexCount <= 0 || object.RenderPass != "opaque" || !object.Static {
			continue
		}
		if object.MaterialIndex >= 0 && object.MaterialIndex < len(bundle.Materials) {
			if !renderObjectUsesLinePass(object, bundle.Materials[object.MaterialIndex]) {
				continue
			}
		}
		writeStaticPassString(object.ID)
		writeStaticPassString(object.Kind)
		writeStaticPassFloat(float64(object.MaterialIndex))
		writeStaticPassFloat(float64(object.VertexOffset))
		writeStaticPassFloat(float64(object.VertexCount))
		writeStaticPassFloat(object.DepthNear)
		writeStaticPassFloat(object.DepthFar)
		writeStaticPassFloat(object.DepthCenter)
		writeStaticPassFloat(object.Bounds.MinX)
		writeStaticPassFloat(object.Bounds.MinY)
		writeStaticPassFloat(object.Bounds.MinZ)
		writeStaticPassFloat(object.Bounds.MaxX)
		writeStaticPassFloat(object.Bounds.MaxY)
		writeStaticPassFloat(object.Bounds.MaxZ)
		if object.MaterialIndex >= 0 && object.MaterialIndex < len(bundle.Materials) {
			writeStaticPassString(bundle.Materials[object.MaterialIndex].Key)
		}
		start := object.VertexOffset * 4
		end := start + object.VertexCount*4
		if start >= 0 && end <= len(bundle.WorldColors) && start <= end {
			for _, value := range bundle.WorldColors[start:end] {
				writeStaticPassFloat(value)
			}
		}
	}
	return strconv.FormatUint(hasher.Sum64(), 16)
}

func appendSceneLine(bundle *rootengine.RenderBundle, width, height int, from, to rootengine.RenderPoint, color string, lineWidth float64) {
	rgba := sceneColorRGBA(color, [4]float64{0.55, 0.88, 1, 1})
	bundle.Lines = append(bundle.Lines, rootengine.RenderLine{
		From:      from,
		To:        to,
		Color:     color,
		LineWidth: lineWidth,
	})
	fromX, fromY := sceneClipPoint(from, width, height)
	toX, toY := sceneClipPoint(to, width, height)
	bundle.Positions = append(bundle.Positions, fromX, fromY, toX, toY)
	bundle.Colors = append(bundle.Colors,
		rgba[0], rgba[1], rgba[2], rgba[3],
		rgba[0], rgba[1], rgba[2], rgba[3],
	)
}

func sceneRGBAString(rgba [4]float64) string {
	r := int(math.Round(clamp(rgba[0], 0, 1) * 255))
	g := int(math.Round(clamp(rgba[1], 0, 1) * 255))
	bb := int(math.Round(clamp(rgba[2], 0, 1) * 255))
	a := clamp(rgba[3], 0, 1)
	var b strings.Builder
	b.Grow(24)
	b.WriteString("rgba(")
	b.WriteString(strconv.Itoa(r))
	b.WriteString(", ")
	b.WriteString(strconv.Itoa(g))
	b.WriteString(", ")
	b.WriteString(strconv.Itoa(bb))
	b.WriteString(", ")
	b.WriteString(strconv.FormatFloat(a, 'f', 3, 64))
	b.WriteByte(')')
	return b.String()
}

func mixRGBA(left, right [4]float64) [4]float64 {
	return [4]float64{
		(left[0] + right[0]) / 2,
		(left[1] + right[1]) / 2,
		(left[2] + right[2]) / 2,
		(left[3] + right[3]) / 2,
	}
}

func sceneLitColorRGBA(material rootengine.RenderMaterial, worldPoint, normal point3, lights []sceneLight, environment sceneEnvironment) [4]float64 {
	base := sceneColorRGBA(material.Color, [4]float64{0.55, 0.88, 1, 1})
	if !sceneLightingActive(lights, environment) {
		return base
	}

	normal = normalizePoint3(normal)
	if normal == (point3{}) {
		normal = point3{Y: 1}
	}
	baseColor := point3{X: base[0], Y: base[1], Z: base[2]}
	emissive := clamp(material.Emissive, 0, 1)
	lighting := point3{}
	if environment.AmbientIntensity > 0 {
		lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(sceneColorPoint(environment.AmbientColor, point3{X: 1, Y: 1, Z: 1}), environment.AmbientIntensity)))
	}
	if environment.SkyIntensity > 0 || environment.GroundIntensity > 0 {
		hemi := clamp((normal.Y*0.5)+0.5, 0, 1)
		sky := scalePoint3(sceneColorPoint(environment.SkyColor, point3{X: 0.88, Y: 0.94, Z: 1}), environment.SkyIntensity*hemi)
		ground := scalePoint3(sceneColorPoint(environment.GroundColor, point3{X: 0.12, Y: 0.16, Z: 0.22}), environment.GroundIntensity*(1-hemi))
		lighting = addPoint3(lighting, multiplyPoint3(baseColor, addPoint3(sky, ground)))
	}
	for _, light := range lights {
		switch light.Kind {
		case "ambient":
			lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(sceneColorPoint(light.Color, point3{X: 1, Y: 1, Z: 1}), light.Intensity)))
		case "directional":
			direction := normalizePoint3(point3{X: -light.DirectionX, Y: -light.DirectionY, Z: -light.DirectionZ})
			diffuse := clamp(dotPoint3(normal, direction), 0, 1)
			if diffuse > 0 {
				lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(sceneColorPoint(light.Color, point3{X: 1, Y: 1, Z: 1}), light.Intensity*diffuse)))
			}
		case "point":
			offset := point3{X: light.X - worldPoint.X, Y: light.Y - worldPoint.Y, Z: light.Z - worldPoint.Z}
			distance := point3Length(offset)
			if distance == 0 {
				distance = 0.0001
			}
			diffuse := clamp(dotPoint3(normal, scalePoint3(offset, 1/distance)), 0, 1)
			if diffuse <= 0 {
				continue
			}
			attenuation := scenePointLightAttenuation(light, distance)
			if attenuation <= 0 {
				continue
			}
			lighting = addPoint3(lighting, multiplyPoint3(baseColor, scalePoint3(sceneColorPoint(light.Color, point3{X: 1, Y: 1, Z: 1}), light.Intensity*diffuse*attenuation)))
		}
	}
	lit := addPoint3(scalePoint3(baseColor, emissive), scalePoint3(lighting, environment.Exposure))
	lit = addPoint3(lit, scalePoint3(baseColor, 0.06))
	return [4]float64{
		clamp(lit.X, 0, 1),
		clamp(lit.Y, 0, 1),
		clamp(lit.Z, 0, 1),
		base[3],
	}
}

func sceneLightingActive(lights []sceneLight, environment sceneEnvironment) bool {
	return len(lights) > 0 ||
		environment.AmbientIntensity > 0 ||
		environment.SkyIntensity > 0 ||
		environment.GroundIntensity > 0
}

func sceneObjectWorldNormal(object sceneObject, point point3, timeSeconds float64) point3 {
	normal := sceneObjectLocalNormal(object, point)
	return normalizePoint3(rotatePoint(normal,
		object.RotationX+object.SpinX*timeSeconds,
		object.RotationY+object.SpinY*timeSeconds,
		object.RotationZ+object.SpinZ*timeSeconds,
	))
}

func sceneObjectLocalNormal(object sceneObject, point point3) point3 {
	switch object.Kind {
	case "lines":
		width := math.Max(object.Width/2, 0.0001)
		height := math.Max(object.Height/2, 0.0001)
		depth := math.Max(object.Depth/2, 0.0001)
		ax := math.Abs(point.X / width)
		ay := math.Abs(point.Y / height)
		az := math.Abs(point.Z / depth)
		switch {
		case ax >= ay && ax >= az:
			return point3{X: math.Copysign(1, point.X)}
		case ay >= az:
			return point3{Y: math.Copysign(1, point.Y)}
		default:
			return point3{Z: math.Copysign(1, point.Z)}
		}
	case "plane":
		return point3{Y: 1}
	case "sphere":
		return normalizePoint3(point)
	case "pyramid":
		width := math.Max(object.Width/2, 0.0001)
		height := math.Max(object.Height/2, 0.0001)
		depth := math.Max(object.Depth/2, 0.0001)
		return normalizePoint3(point3{
			X: point.X / width,
			Y: (point.Y / height) + 0.35,
			Z: point.Z / depth,
		})
	default:
		width := math.Max(object.Width/2, 0.0001)
		height := math.Max(object.Height/2, 0.0001)
		depth := math.Max(object.Depth/2, 0.0001)
		ax := math.Abs(point.X / width)
		ay := math.Abs(point.Y / height)
		az := math.Abs(point.Z / depth)
		switch {
		case ax >= ay && ax >= az:
			return point3{X: math.Copysign(1, point.X)}
		case ay >= az:
			return point3{Y: math.Copysign(1, point.Y)}
		default:
			return point3{Z: math.Copysign(1, point.Z)}
		}
	}
}

func scenePointLightAttenuation(light sceneLight, distance float64) float64 {
	if light.Range > 0 {
		falloff := clamp(1-(distance/light.Range), 0, 1)
		return math.Pow(falloff, light.Decay)
	}
	return 1 / (1 + math.Pow(distance*0.35, math.Max(light.Decay, 1)))
}

func sceneColorPoint(value string, fallback point3) point3 {
	rgba := sceneColorRGBA(value, [4]float64{fallback.X, fallback.Y, fallback.Z, 1})
	return point3{X: rgba[0], Y: rgba[1], Z: rgba[2]}
}

func addPoint3(left, right point3) point3 {
	return point3{X: left.X + right.X, Y: left.Y + right.Y, Z: left.Z + right.Z}
}

func scalePoint3(point point3, scale float64) point3 {
	return point3{X: point.X * scale, Y: point.Y * scale, Z: point.Z * scale}
}

func multiplyPoint3(left, right point3) point3 {
	return point3{X: left.X * right.X, Y: left.Y * right.Y, Z: left.Z * right.Z}
}

func point3Length(point point3) float64 {
	return math.Sqrt((point.X * point.X) + (point.Y * point.Y) + (point.Z * point.Z))
}

func normalizePoint3(point point3) point3 {
	length := point3Length(point)
	if length == 0 {
		return point3{}
	}
	return scalePoint3(point, 1/length)
}

func dotPoint3(left, right point3) float64 {
	return (left.X * right.X) + (left.Y * right.Y) + (left.Z * right.Z)
}

func sceneClipPoint(point rootengine.RenderPoint, width, height int) (float64, float64) {
	return (point.X/float64(width))*2 - 1, 1 - (point.Y/float64(height))*2
}

func sceneObjectSegments(object sceneObject) [][2]point3 {
	switch object.Kind {
	case "box", "cube":
		return boxSegments(object)
	case "lines":
		return customLineSegments(object)
	case "plane":
		return planeSegments(object)
	case "pyramid":
		return pyramidSegments(object)
	case "sphere":
		return sphereSegments(object)
	default:
		return boxSegments(object)
	}
}

func customLineSegments(object sceneObject) [][2]point3 {
	out := make([][2]point3, 0, len(object.LineSegments))
	for _, edge := range object.LineSegments {
		if edge[0] < 0 || edge[1] < 0 || edge[0] >= len(object.Points) || edge[1] >= len(object.Points) || edge[0] == edge[1] {
			continue
		}
		out = append(out, [2]point3{object.Points[edge[0]], object.Points[edge[1]]})
	}
	return out
}

func boxSegments(object sceneObject) [][2]point3 {
	return indexSegments(boxVertices(object.Width, object.Height, object.Depth), [][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
		{4, 5}, {5, 6}, {6, 7}, {7, 4},
		{0, 4}, {1, 5}, {2, 6}, {3, 7},
	})
}

func planeSegments(object sceneObject) [][2]point3 {
	vertices := boxVertices(object.Width, 0, object.Depth)
	return indexSegments(vertices[:4], [][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
	})
}

func pyramidSegments(object sceneObject) [][2]point3 {
	halfWidth := object.Width / 2
	halfDepth := object.Depth / 2
	halfHeight := object.Height / 2
	vertices := []point3{
		{X: -halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: -halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: 0, Y: halfHeight, Z: 0},
	}
	return indexSegments(vertices, [][2]int{
		{0, 1}, {1, 2}, {2, 3}, {3, 0},
		{0, 4}, {1, 4}, {2, 4}, {3, 4},
	})
}

func sphereSegments(object sceneObject) [][2]point3 {
	out := circleSegments(object.Radius, "xy", object.Segments)
	out = append(out, circleSegments(object.Radius, "xz", object.Segments)...)
	out = append(out, circleSegments(object.Radius, "yz", object.Segments)...)
	return out
}

func boxVertices(width, height, depth float64) []point3 {
	halfWidth := width / 2
	halfHeight := height / 2
	halfDepth := depth / 2
	return []point3{
		{X: -halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: -halfDepth},
		{X: halfWidth, Y: halfHeight, Z: -halfDepth},
		{X: -halfWidth, Y: halfHeight, Z: -halfDepth},
		{X: -halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: halfWidth, Y: -halfHeight, Z: halfDepth},
		{X: halfWidth, Y: halfHeight, Z: halfDepth},
		{X: -halfWidth, Y: halfHeight, Z: halfDepth},
	}
}

func indexSegments(points []point3, edgePairs [][2]int) [][2]point3 {
	out := make([][2]point3, 0, len(edgePairs))
	for _, edge := range edgePairs {
		out = append(out, [2]point3{points[edge[0]], points[edge[1]]})
	}
	return out
}

func circleSegments(radius float64, axis string, segments int) [][2]point3 {
	points := make([]point3, 0, segments)
	for i := 0; i < segments; i++ {
		angle := (math.Pi * 2 * float64(i)) / float64(segments)
		points = append(points, circlePoint(radius, axis, angle))
	}
	out := make([][2]point3, 0, len(points))
	for i := range points {
		out = append(out, [2]point3{points[i], points[(i+1)%len(points)]})
	}
	return out
}

func circlePoint(radius float64, axis string, angle float64) point3 {
	sin := math.Sin(angle) * radius
	cos := math.Cos(angle) * radius
	switch axis {
	case "xy":
		return point3{X: cos, Y: sin, Z: 0}
	case "yz":
		return point3{X: 0, Y: cos, Z: sin}
	default:
		return point3{X: cos, Y: 0, Z: sin}
	}
}

func translatePoint(point point3, object sceneObject, timeSeconds float64) point3 {
	rotated := rotatePoint(point,
		object.RotationX+object.SpinX*timeSeconds,
		object.RotationY+object.SpinY*timeSeconds,
		object.RotationZ+object.SpinZ*timeSeconds,
	)
	offset := sceneMotionOffset(object, timeSeconds)
	return point3{
		X: rotated.X + object.X + offset.X,
		Y: rotated.Y + object.Y + offset.Y,
		Z: rotated.Z + object.Z + offset.Z,
	}
}

func sceneMotionOffset(object sceneObject, timeSeconds float64) point3 {
	if object.ShiftX == 0 && object.ShiftY == 0 && object.ShiftZ == 0 {
		return point3{}
	}
	angle := object.DriftPhase + timeSeconds*object.DriftSpeed
	return point3{
		X: math.Cos(angle) * object.ShiftX,
		Y: math.Sin(angle*0.82+object.DriftPhase*0.35) * object.ShiftY,
		Z: math.Sin(angle) * object.ShiftZ,
	}
}

func rotatePoint(point point3, rotationX, rotationY, rotationZ float64) point3 {
	x := point.X
	y := point.Y
	z := point.Z

	sinX, cosX := math.Sin(rotationX), math.Cos(rotationX)
	nextY := y*cosX - z*sinX
	nextZ := y*sinX + z*cosX
	y, z = nextY, nextZ

	sinY, cosY := math.Sin(rotationY), math.Cos(rotationY)
	nextX := x*cosY + z*sinY
	nextZ = -x*sinY + z*cosY
	x, z = nextX, nextZ

	sinZ, cosZ := math.Sin(rotationZ), math.Cos(rotationZ)
	nextX = x*cosZ - y*sinZ
	nextY = x*sinZ + y*cosZ

	return point3{X: nextX, Y: nextY, Z: z}
}

func projectPoint(point point3, camera sceneCamera, width, height int) *rootengine.RenderPoint {
	local := cameraLocalPoint(point, camera)
	depth := local.Z
	if depth <= camera.Near || depth >= camera.Far {
		return nil
	}
	focal := (math.Min(float64(width), float64(height)) / 2) / math.Tan((camera.FOV*math.Pi)/360)
	return &rootengine.RenderPoint{
		X: float64(width)/2 + (local.X*focal)/depth,
		Y: float64(height)/2 - (local.Y*focal)/depth,
	}
}

func clipWorldSegmentForCamera(from, to point3, camera sceneCamera, aspect float64) (point3, point3, bool) {
	localFrom := cameraLocalPoint(from, camera)
	localTo := cameraLocalPoint(to, camera)
	depthFrom := localFrom.Z
	depthTo := localTo.Z
	if depthFrom <= camera.Near && depthTo <= camera.Near {
		return point3{}, point3{}, false
	}
	clippedFrom := from
	clippedTo := to
	if depthFrom <= camera.Near || depthTo <= camera.Near {
		t := (camera.Near - depthFrom) / math.Max(0.000001, depthTo-depthFrom)
		if depthFrom <= camera.Near {
			clippedFrom = lerpPoint3(from, to, t)
		} else {
			clippedTo = lerpPoint3(from, to, t)
		}
	}
	depthFrom = cameraLocalPoint(clippedFrom, camera).Z
	depthTo = cameraLocalPoint(clippedTo, camera).Z
	if worldSegmentOutsideFrustum(clippedFrom, depthFrom, clippedTo, depthTo, camera, aspect) {
		return point3{}, point3{}, false
	}
	return clippedFrom, clippedTo, true
}

func renderObjectBounds(worldPositions []float64, vertexOffset, vertexCount int) rootengine.RenderBounds {
	if vertexCount <= 0 {
		return rootengine.RenderBounds{}
	}
	start := vertexOffset * 3
	bounds := rootengine.RenderBounds{
		MinX: worldPositions[start],
		MinY: worldPositions[start+1],
		MinZ: worldPositions[start+2],
		MaxX: worldPositions[start],
		MaxY: worldPositions[start+1],
		MaxZ: worldPositions[start+2],
	}
	for i := start + 3; i < start+vertexCount*3; i += 3 {
		x := worldPositions[i]
		y := worldPositions[i+1]
		z := worldPositions[i+2]
		bounds.MinX = math.Min(bounds.MinX, x)
		bounds.MinY = math.Min(bounds.MinY, y)
		bounds.MinZ = math.Min(bounds.MinZ, z)
		bounds.MaxX = math.Max(bounds.MaxX, x)
		bounds.MaxY = math.Max(bounds.MaxY, y)
		bounds.MaxZ = math.Max(bounds.MaxZ, z)
	}
	return bounds
}

func renderBoundsOutsideFrustum(bounds rootengine.RenderBounds, camera sceneCamera, width, height int) bool {
	aspect := math.Max(0.0001, float64(width)/math.Max(1, float64(height)))
	corners := renderBoundsCorners(bounds)
	allLeft, allRight, allBottom, allTop := true, true, true, true
	allNear, allFar := true, true
	for _, corner := range corners {
		local := cameraLocalPoint(corner, camera)
		allNear = allNear && local.Z <= camera.Near
		allFar = allFar && local.Z >= camera.Far
		clipX, clipY := projectWorldClipPoint(corner, camera, aspect)
		allLeft = allLeft && clipX < -1
		allRight = allRight && clipX > 1
		allBottom = allBottom && clipY < -1
		allTop = allTop && clipY > 1
	}
	if allNear || allFar {
		return true
	}
	return allLeft || allRight || allBottom || allTop
}

func renderBoundsDepthMetrics(bounds rootengine.RenderBounds, camera sceneCamera) (near, far, center float64) {
	corners := renderBoundsCorners(bounds)
	if len(corners) == 0 {
		depth := cameraLocalPoint(point3{}, camera).Z
		return depth, depth, depth
	}
	near = cameraLocalPoint(corners[0], camera).Z
	far = near
	for _, corner := range corners[1:] {
		depth := cameraLocalPoint(corner, camera).Z
		near = math.Min(near, depth)
		far = math.Max(far, depth)
	}
	center = (near + far) / 2
	return near, far, center
}

func expandRenderBounds(bounds rootengine.RenderBounds, hasBounds bool, point point3) (rootengine.RenderBounds, bool) {
	if !hasBounds {
		return rootengine.RenderBounds{
			MinX: point.X,
			MinY: point.Y,
			MinZ: point.Z,
			MaxX: point.X,
			MaxY: point.Y,
			MaxZ: point.Z,
		}, true
	}
	bounds.MinX = math.Min(bounds.MinX, point.X)
	bounds.MinY = math.Min(bounds.MinY, point.Y)
	bounds.MinZ = math.Min(bounds.MinZ, point.Z)
	bounds.MaxX = math.Max(bounds.MaxX, point.X)
	bounds.MaxY = math.Max(bounds.MaxY, point.Y)
	bounds.MaxZ = math.Max(bounds.MaxZ, point.Z)
	return bounds, true
}

func renderBoundsCorners(bounds rootengine.RenderBounds) []point3 {
	return []point3{
		{X: bounds.MinX, Y: bounds.MinY, Z: bounds.MinZ},
		{X: bounds.MinX, Y: bounds.MinY, Z: bounds.MaxZ},
		{X: bounds.MinX, Y: bounds.MaxY, Z: bounds.MinZ},
		{X: bounds.MinX, Y: bounds.MaxY, Z: bounds.MaxZ},
		{X: bounds.MaxX, Y: bounds.MinY, Z: bounds.MinZ},
		{X: bounds.MaxX, Y: bounds.MinY, Z: bounds.MaxZ},
		{X: bounds.MaxX, Y: bounds.MaxY, Z: bounds.MinZ},
		{X: bounds.MaxX, Y: bounds.MaxY, Z: bounds.MaxZ},
	}
}

func projectWorldClipPoint(point point3, camera sceneCamera, aspect float64) (float64, float64) {
	local := cameraLocalPoint(point, camera)
	depth := local.Z
	focal := 1 / math.Max(0.0001, math.Tan((camera.FOV*math.Pi)/360))
	x := (local.X * focal / math.Max(depth, 0.0001)) / math.Max(aspect, 0.0001)
	y := local.Y * focal / math.Max(depth, 0.0001)
	return x, y
}

func worldSegmentOutsideFrustum(from point3, depthFrom float64, to point3, depthTo float64, camera sceneCamera, aspect float64) bool {
	if depthFrom >= camera.Far && depthTo >= camera.Far {
		return true
	}
	clipFromX, clipFromY := projectWorldClipPoint(from, camera, aspect)
	clipToX, clipToY := projectWorldClipPoint(to, camera, aspect)
	return (clipFromX < -1 && clipToX < -1) ||
		(clipFromX > 1 && clipToX > 1) ||
		(clipFromY < -1 && clipToY < -1) ||
		(clipFromY > 1 && clipToY > 1)
}

func cameraLocalPoint(point point3, camera sceneCamera) point3 {
	translated := point3{
		X: point.X - camera.X,
		Y: point.Y - camera.Y,
		Z: point.Z + camera.Z,
	}
	return inverseRotatePoint(translated, camera.RotationX, camera.RotationY, camera.RotationZ)
}

func inverseRotatePoint(point point3, rotationX, rotationY, rotationZ float64) point3 {
	x := point.X
	y := point.Y
	z := point.Z

	sinZ, cosZ := math.Sin(-rotationZ), math.Cos(-rotationZ)
	nextX := x*cosZ - y*sinZ
	nextY := x*sinZ + y*cosZ
	x, y = nextX, nextY

	sinY, cosY := math.Sin(-rotationY), math.Cos(-rotationY)
	nextX = x*cosY + z*sinY
	nextZ := -x*sinY + z*cosY
	x, z = nextX, nextZ

	sinX, cosX := math.Sin(-rotationX), math.Cos(-rotationX)
	nextY = y*cosX - z*sinX
	nextZ = y*sinX + z*cosX
	return point3{X: x, Y: nextY, Z: nextZ}
}

func lerpPoint3(from, to point3, t float64) point3 {
	t = clamp(t, 0, 1)
	return point3{
		X: from.X + (to.X-from.X)*t,
		Y: from.Y + (to.Y-from.Y)*t,
		Z: from.Z + (to.Z-from.Z)*t,
	}
}
