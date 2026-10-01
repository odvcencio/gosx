package docs

import (
	"strings"

	"m31labs.dev/gosx/scene"
)

// Keep the embedded basalt and sand maps on phones, but avoid three shadow
// traversals and optional detail/reflection passes on their slower CPUs.
func blackglassBeachRequestProgram(view, period, userAgent string) scene.Props {
	props := BlackglassBeachProgram(view, period)
	ua := strings.ToLower(userAgent)
	if !strings.Contains(ua, "mobile") && !strings.Contains(ua, "android") && !strings.Contains(ua, "iphone") {
		return props
	}
	props.MaxDevicePixelRatio = 1.5
	props.AdaptiveTargetFrameMS = 33.4
	props.Shadows.MaxPixels = scene.ShadowMaxPixels1024
	props.Environment.Haze = nil
	props.Environment.Sky.Clouds = nil
	props.Environment.Ocean.Reflections = nil
	// Reuse the existing ship LODs; sail deformation otherwise dominates the
	// four-times-slower phone profile even while it is moored offshore.
	props.Vessel.LODs = []scene.VesselLOD{{NodeID: "clipper-low", Distance: 25}}
	for i, node := range props.Graph.Nodes {
		switch n := node.(type) {
		case scene.DirectionalLight:
			n.ShadowCascades, n.ShadowSize = 1, 1024
			props.Graph.Nodes[i] = n
		case scene.Model:
			n.Detail = nil
			if n.ID == "clipper" {
				n.Src = blackglassBeachModelRoot + "clipper-mid.glb"
			}
			props.Graph.Nodes[i] = n
		}
	}
	return props
}
