package docs

import "m31labs.dev/gosx/scene"

// BlackglassCoastContract is the typed runtime projection of the Blackglass
// Coast SceneDoc authored in GoSX 3D Studio. It deliberately carries the
// water volume and gameplay anchors as first-class values: the showcase must
// bind authoring semantics, not quietly recreate them as presentation-only
// coordinates.
//
// Source: gosx3d-studio / BlackglassCoastDocument, SceneDoc revision 1.
type BlackglassCoastContract struct {
	Schema       string
	DocumentID   string
	Revision     uint64
	ArtDirection string
	Water        BlackglassWaterZone
	Markers      []BlackglassMarker
}

type BlackglassWaterZone struct {
	ID             string
	Name           string
	Center         scene.Vector3
	Size           scene.Vector3
	SurfaceY       float64
	Current        scene.Vector3
	BuoyancyScale  float64
	LinearDrag     float64
	RuntimeProfile string
}

type BlackglassMarker struct {
	ID       string
	Name     string
	Kind     string
	EntityID string
	Position scene.Vector3
}

var blackglassCoastContract = BlackglassCoastContract{
	Schema:       "gosx.scene3d.world/v1",
	DocumentID:   "blackglass-coast",
	Revision:     1,
	ArtDirection: "sunlit volcanic naturalism",
	Water: BlackglassWaterZone{
		ID: "blackglass-cove", Name: "Blackglass Cove",
		Center: scene.Vec3(-12, -3, -6), Size: scene.Vec3(34, 10, 24), SurfaceY: 0,
		Current: scene.Vec3(0.16, 0, -0.05), BuoyancyScale: 1.15, LinearDrag: 0.35,
		RuntimeProfile: "blackglass-coast",
	},
	Markers: []BlackglassMarker{
		{ID: "arrival", Name: "Arrival Beach", Kind: "player-spawn", EntityID: "arrival-marker", Position: scene.Vec3(-1.8, 0.2, 9.2)},
		{ID: "opening-camera", Name: "Opening Overlook", Kind: "camera-start", EntityID: "coast-cliff-overlook", Position: scene.Vec3(-2, 7.2, 22)},
		{ID: "beacon-terrace", Name: "Beacon Terrace", Kind: "checkpoint", EntityID: "beacon-plinth", Position: scene.Vec3(8, 1.6, -1.5)},
		{ID: "beacon-lens", Name: "Beacon Lens", Kind: "interactable", EntityID: "beacon-lens", Position: scene.Vec3(8, 8.1, -4)},
		{ID: "cinematic-beacon", Name: "Beacon Hero Frame", Kind: "cinematic-target", EntityID: "blackglass-beacon", Position: scene.Vec3(8, 4.7, -4)},
	},
}

func BlackglassCoastRuntimeContract() BlackglassCoastContract { return blackglassCoastContract }

// Local maps an authored world position into the water-centered coordinate
// system used by WaterSystem. WaterSystem owns its horizontal origin, so every
// surrounding mesh and camera anchor takes this same path.
func (c BlackglassCoastContract) Local(position scene.Vector3) scene.Vector3 {
	return scene.Vec3(position.X-c.Water.Center.X, position.Y, position.Z-c.Water.Center.Z)
}

func (c BlackglassCoastContract) Marker(id string) (BlackglassMarker, bool) {
	for _, marker := range c.Markers {
		if marker.ID == id {
			return marker, true
		}
	}
	return BlackglassMarker{}, false
}
