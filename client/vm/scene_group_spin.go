package vm

import (
	"m31labs.dev/gosx/internal/scenehierarchy"
)

func sceneGroupWorldMatrices(props map[string]any, seconds float64) (map[string][16]float64, error) {
	raw := sceneValue(props, "groups")
	if raw == nil {
		raw = propValue(props, "groups")
	}
	records := sceneLightList(raw)
	if len(records) == 0 {
		return nil, nil
	}
	groups := make([]scenehierarchy.Group, len(records))
	for i, r := range records {
		g := &groups[i]
		g.ID = stringFromAny(r["id"], "")
		g.ParentNode = stringFromAny(r["parentNode"], "")
		g.X = numberFromAny(r["x"], 0)
		g.Y = numberFromAny(r["y"], 0)
		g.Z = numberFromAny(r["z"], 0)
		g.RotationX = numberFromAny(r["rotationX"], 0)
		g.RotationY = numberFromAny(r["rotationY"], 0)
		g.RotationZ = numberFromAny(r["rotationZ"], 0)
		g.SpinX = numberFromAny(r["spinX"], 0)
		g.SpinY = numberFromAny(r["spinY"], 0)
		g.SpinZ = numberFromAny(r["spinZ"], 0)
		g.ScaleX = numberFromAny(r["scaleX"], 0)
		g.ScaleY = numberFromAny(r["scaleY"], 0)
		g.ScaleZ = numberFromAny(r["scaleZ"], 0)
		g.ParentMatrix = numberSliceFromAny(r["parentMatrix"])
	}
	return scenehierarchy.WorldMatrices(groups, seconds)
}

func sceneGroupNode(node resolvedNode, matrices map[string][16]float64) (resolvedNode, bool) {
	matrix, ok := matrices[stringFromAny(node.Props["parentNode"], "")]
	if !ok {
		return node, false
	}
	props := make(map[string]any, len(node.Props)+1)
	for key, value := range node.Props {
		props[key] = value
	}
	switch node.Kind {
	case "mesh":
		if prefix, valid := sceneParentMatrixFromAny(props["parentMatrix"]); valid {
			matrix = scenehierarchy.Multiply(matrix, prefix)
		}
		props["parentMatrix"] = matrix[:]
	default:
		x, y, z := numberFromAny(props["x"], 0), numberFromAny(props["y"], 0), numberFromAny(props["z"], 0)
		props["x"] = matrix[0]*x + matrix[4]*y + matrix[8]*z + matrix[12]
		props["y"] = matrix[1]*x + matrix[5]*y + matrix[9]*z + matrix[13]
		props["z"] = matrix[2]*x + matrix[6]*y + matrix[10]*z + matrix[14]
		if node.Kind == "light" {
			x, y, z = numberFromAny(props["directionX"], 0), numberFromAny(props["directionY"], 0), numberFromAny(props["directionZ"], 0)
			props["directionX"] = matrix[0]*x + matrix[4]*y + matrix[8]*z
			props["directionY"] = matrix[1]*x + matrix[5]*y + matrix[9]*z
			props["directionZ"] = matrix[2]*x + matrix[6]*y + matrix[10]*z
		}
	}
	node.Props = props
	return node, true
}

func sceneGroupProps(props map[string]any, matrices map[string][16]float64) map[string]any {
	if len(matrices) == 0 {
		return props
	}
	result := make(map[string]any, len(props))
	for k, v := range props {
		result[k] = v
	}
	source, _ := props["scene"].(map[string]any)
	scene := make(map[string]any, len(source))
	for k, v := range source {
		scene[k] = v
	}
	result["scene"] = scene
	for _, kind := range []string{"lights", "sprites"} {
		raw := sceneLightList(source[kind])
		if len(raw) == 0 {
			continue
		}
		records := make([]map[string]any, len(raw))
		nodeKind := "light"
		if kind == "sprites" {
			nodeKind = "sprite"
		}
		for i, r := range raw {
			node, _ := sceneGroupNode(resolvedNode{Kind: nodeKind, Props: r}, matrices)
			records[i] = node.Props
		}
		scene[kind] = records
	}
	return result
}
