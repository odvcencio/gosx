package hydrate

import (
	"encoding/json"

	"m31labs.dev/gosx/internal/urlpath"
)

// URL fields are part of the built-in engine contracts, not a heuristic over
// arbitrary props. Raw messages keep unknown fields and numeric values intact.
type engineURLShape struct {
	urls     []string
	children map[string]engineURLShape
	elements *engineURLShape
}

func engineURLCollection(shape engineURLShape) engineURLShape {
	return engineURLShape{elements: &shape}
}

var videoEngineURLs = engineURLShape{
	urls: []string{"src", "Src", "poster", "Poster", "sync", "subtitleBase", "subtitle_base"},
	children: map[string]engineURLShape{
		"sources":          engineURLCollection(engineURLShape{urls: []string{"src", "source", "url"}}),
		"subtitleTracks":   engineURLCollection(engineURLShape{urls: []string{"src", "url", "uri"}}),
		"subtitle_tracks":  engineURLCollection(engineURLShape{urls: []string{"src", "url", "uri"}}),
		"subtitles":        {urls: []string{"refreshEndpoint", "refresh_endpoint"}},
		"subtitleOptions":  {urls: []string{"refreshEndpoint", "refresh_endpoint"}},
		"subtitle_options": {urls: []string{"refreshEndpoint", "refresh_endpoint"}},
		"telemetry":        {urls: []string{"endpoint"}},
		"videoTelemetry":   {urls: []string{"endpoint"}},
		"video_telemetry":  {urls: []string{"endpoint"}},
	},
}

var sceneEngineURLs = sceneEngineURLShape()

func sceneEngineURLShape() engineURLShape {
	descriptor := engineURLShape{urls: []string{"uri"}}
	detailLayer := engineURLShape{urls: []string{"albedo", "normal", "roughness"}}
	detail := engineURLShape{children: map[string]engineURLShape{"ground": detailLayer, "steep": detailLayer}}
	descriptors := engineURLShape{children: map[string]engineURLShape{
		"baseColor": descriptor, "normal": descriptor, "roughness": descriptor,
		"metalness": descriptor, "occlusion": descriptor, "emissive": descriptor,
		"data": engineURLCollection(descriptor),
	}}
	appearance := engineURLShape{
		urls:     []string{"texture", "normalMap", "roughnessMap", "metalnessMap", "occlusionMap", "emissiveMap"},
		children: map[string]engineURLShape{"detail": detail, "textureDescriptors": descriptors},
	}
	material := engineURLShape{urls: appearance.urls, children: map[string]engineURLShape{
		"detail": detail, "textureDescriptors": descriptors, "variants": engineURLCollection(appearance),
	}}
	state := engineURLShape{urls: append([]string{"src"}, appearance.urls...), children: appearance.children}
	model := engineURLShape{
		urls: append([]string{"src", "previewSrc", "fullSrc"}, appearance.urls...),
		children: map[string]engineURLShape{
			"detail": detail, "textureDescriptors": descriptors, "material": material,
			"inState": state, "outState": state,
		},
	}
	environment := engineURLShape{urls: []string{"envMap"}, children: map[string]engineURLShape{
		"inState": {urls: []string{"envMap"}}, "outState": {urls: []string{"envMap"}},
		"ocean": {children: map[string]engineURLShape{"normalMap": {urls: []string{"src"}}}},
		"ibl": {urls: []string{"source"}, children: map[string]engineURLShape{
			"radiance": descriptor, "irradiance": descriptor, "brdfLUT": descriptor,
		}},
	}}
	scene := engineURLShape{children: map[string]engineURLShape{
		"models": engineURLCollection(model), "objects": engineURLCollection(model),
		"instancedMeshes": engineURLCollection(model), "instancedGLBMeshes": engineURLCollection(model),
		"sprites":   engineURLCollection(engineURLShape{urls: []string{"src"}}),
		"materials": engineURLCollection(material), "environment": environment,
		"nodes": engineURLCollection(engineURLShape{children: map[string]engineURLShape{
			"mesh": model, "sprite": {urls: []string{"src"}}, "material": material,
		}}),
	}}
	props := engineURLShape{children: make(map[string]engineURLShape, len(scene.children)+1)}
	for key, shape := range scene.children {
		props.children[key] = shape
	}
	props.children["scene"] = scene
	return props
}

func enginePropsWithBasePath(prefix string, entry EngineEntry) json.RawMessage {
	var shape engineURLShape
	switch {
	case entry.Kind == "video":
		shape = videoEngineURLs
	case entry.Component == "GoSXScene3D":
		shape = sceneEngineURLs
	default:
		return entry.Props
	}
	props, _ := prefixEngineURLJSON(prefix, entry.Props, shape)
	return props
}

func prefixEngineURLJSON(prefix string, data json.RawMessage, shape engineURLShape) (json.RawMessage, bool) {
	changed := false
	if shape.elements != nil {
		var list []json.RawMessage
		if json.Unmarshal(data, &list) == nil {
			for i, value := range list {
				updated, modified := prefixEngineURLJSON(prefix, value, *shape.elements)
				list[i], changed = updated, changed || modified
			}
			if changed {
				encoded, err := json.Marshal(list)
				if err == nil {
					return encoded, true
				}
			}
			return data, false
		}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil {
		return data, false
	}
	for key, value := range object {
		if shape.elements != nil {
			updated, modified := prefixEngineURLJSON(prefix, value, *shape.elements)
			object[key], changed = updated, changed || modified
		} else if child, ok := shape.children[key]; ok {
			updated, modified := prefixEngineURLJSON(prefix, value, child)
			object[key], changed = updated, changed || modified
		}
	}
	for _, key := range shape.urls {
		var value string
		if json.Unmarshal(object[key], &value) != nil {
			continue
		}
		if public := urlpath.URL(prefix, value); public != value {
			object[key], _ = json.Marshal(public)
			changed = true
		}
	}
	if changed {
		encoded, err := json.Marshal(object)
		if err == nil {
			return encoded, true
		}
	}
	return data, false
}
