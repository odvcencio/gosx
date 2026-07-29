package docs

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"m31labs.dev/gosx/scene"
)

const blackglassCoastSceneDocSHA256 = "3f219e90d54b3291bfc5c15f8f7aa411c448898414a26517b1d0fd36a90da800"

// blackglassCoastSceneDoc is the lossless, canonical Studio export. The
// runtime reads its authored world semantics at startup; no presentation
// coordinates are duplicated in GoSX source.
//
//go:embed blackglass-coast.scene3d
var blackglassCoastSceneDoc []byte

type BlackglassCoastContract struct {
	Schema              string
	DocumentID          string
	Revision            uint64
	DocumentFingerprint string
	ArtDirection        string
	Water               BlackglassWaterZone
	Markers             []BlackglassMarker
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

type blackglassSceneDoc struct {
	Schema   string            `json:"schema"`
	ID       string            `json:"id"`
	Revision uint64            `json:"revision"`
	Metadata map[string]string `json:"metadata"`
	World    struct {
		WaterZones map[string]blackglassWaterZone `json:"waterZones"`
		Markers    map[string]blackglassMarker    `json:"markers"`
	} `json:"world"`
}

type blackglassVector struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

type blackglassWaterZone struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Center         blackglassVector `json:"center"`
	Size           blackglassVector `json:"size"`
	SurfaceY       float64          `json:"surfaceY"`
	Current        blackglassVector `json:"current"`
	BuoyancyScale  float64          `json:"buoyancyScale"`
	LinearDrag     float64          `json:"linearDrag"`
	RuntimeProfile string           `json:"runtimeProfile"`
}

type blackglassMarker struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	Kind     string           `json:"kind"`
	EntityID string           `json:"entity"`
	Position blackglassVector `json:"position"`
}

var (
	blackglassCoastContractOnce sync.Once
	blackglassCoastContractData BlackglassCoastContract
	blackglassCoastContractErr  error
)

func BlackglassCoastRuntimeContract() BlackglassCoastContract {
	blackglassCoastContractOnce.Do(func() {
		blackglassCoastContractData, blackglassCoastContractErr = loadBlackglassCoastRuntimeContract(blackglassCoastSceneDoc)
	})
	if blackglassCoastContractErr != nil {
		panic("invalid embedded Blackglass Coast Studio export: " + blackglassCoastContractErr.Error())
	}
	return cloneBlackglassCoastContract(blackglassCoastContractData)
}

func loadBlackglassCoastRuntimeContract(payload []byte) (BlackglassCoastContract, error) {
	sum := sha256.Sum256(payload)
	fingerprint := hex.EncodeToString(sum[:])
	if fingerprint != blackglassCoastSceneDocSHA256 {
		return BlackglassCoastContract{}, fmt.Errorf("fingerprint %s, want %s", fingerprint, blackglassCoastSceneDocSHA256)
	}
	var document blackglassSceneDoc
	if err := json.Unmarshal(payload, &document); err != nil {
		return BlackglassCoastContract{}, fmt.Errorf("decode SceneDoc: %w", err)
	}
	if document.Schema != "gosx.scene3d.document/v1" || document.ID != "blackglass-coast" || document.Revision != 1 {
		return BlackglassCoastContract{}, fmt.Errorf("unexpected source identity %q/%q@%d", document.Schema, document.ID, document.Revision)
	}
	zone, ok := document.World.WaterZones["blackglass-cove"]
	if !ok || zone.ID != "blackglass-cove" || zone.RuntimeProfile == "" || zone.Size.X <= 0 || zone.Size.Y <= 0 || zone.Size.Z <= 0 {
		return BlackglassCoastContract{}, fmt.Errorf("invalid blackglass-cove water zone")
	}
	markers := make([]BlackglassMarker, 0, len(document.World.Markers))
	for id, marker := range document.World.Markers {
		if marker.ID != id || marker.Kind == "" {
			return BlackglassCoastContract{}, fmt.Errorf("invalid marker %q", id)
		}
		markers = append(markers, BlackglassMarker{ID: marker.ID, Name: marker.Name, Kind: marker.Kind, EntityID: marker.EntityID, Position: scene.Vec3(marker.Position.X, marker.Position.Y, marker.Position.Z)})
	}
	sort.Slice(markers, func(i, j int) bool { return markers[i].ID < markers[j].ID })
	return BlackglassCoastContract{
		Schema: "gosx.scene3d.world/v1", DocumentID: document.ID, Revision: document.Revision, DocumentFingerprint: fingerprint,
		ArtDirection: document.Metadata["art-direction"],
		Water:        BlackglassWaterZone{ID: zone.ID, Name: zone.Name, Center: scene.Vec3(zone.Center.X, zone.Center.Y, zone.Center.Z), Size: scene.Vec3(zone.Size.X, zone.Size.Y, zone.Size.Z), SurfaceY: zone.SurfaceY, Current: scene.Vec3(zone.Current.X, zone.Current.Y, zone.Current.Z), BuoyancyScale: zone.BuoyancyScale, LinearDrag: zone.LinearDrag, RuntimeProfile: zone.RuntimeProfile},
		Markers:      markers,
	}, nil
}

func cloneBlackglassCoastContract(contract BlackglassCoastContract) BlackglassCoastContract {
	contract.Markers = append([]BlackglassMarker(nil), contract.Markers...)
	return contract
}

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
