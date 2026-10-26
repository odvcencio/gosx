package scene

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ModelPreparation selects a model asset to prepare for a mounted Surface.
// ID identifies failures for the caller; it does not create a scene node.
type ModelPreparation struct {
	ID  string `json:"id"`
	Src string `json:"src"`
	// RigidGeometry prepares shared CPU primitive streams without scene nodes.
	// Only immutable rigid meshes are supported; skins, morphs and node animation
	// are rejected. GPU buffers and shaders remain first-render work.
	RigidGeometry bool `json:"rigidGeometry,omitempty"`
}

// ModelPreparationResult counts selected models and distinct native texture records.
// Success covers decoded geometry/animations and uploaded material maps. GPU
// geometry buffers, shader compilation and scene-generation commit are separate.
type ModelPreparationResult struct {
	Models   int `json:"models"`
	Textures int `json:"textures"`
}

func modelPreparationSnapshot(requests []ModelPreparation) ([]byte, error) {
	return modelPreparationSnapshotLimit(requests, 32)
}

func modelPreparationSnapshotLimit(requests []ModelPreparation, maximum int) ([]byte, error) {
	if len(requests) < 1 || len(requests) > maximum {
		return nil, fmt.Errorf("scene: preparation requires 1–%d models", maximum)
	}
	ids := make(map[string]bool, len(requests))
	for _, request := range requests {
		if strings.TrimSpace(request.ID) == "" || strings.TrimSpace(request.Src) == "" || ids[request.ID] {
			return nil, errors.New("scene: invalid preparation model")
		}
		ids[request.ID] = true
	}
	return json.Marshal(requests)
}
