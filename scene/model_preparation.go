package scene

import (
	"encoding/json"
	"errors"
	"strings"
)

// ModelPreparation selects a model asset to prepare for a mounted Surface.
// ID identifies failures for the caller; it does not create a scene node.
type ModelPreparation struct {
	ID  string `json:"id"`
	Src string `json:"src"`
}

// ModelPreparationResult counts selected models and distinct native texture records.
// Success covers decoded geometry/animations and uploaded material maps. GPU
// geometry buffers, shader compilation and scene-generation commit are separate.
type ModelPreparationResult struct {
	Models   int `json:"models"`
	Textures int `json:"textures"`
}

func modelPreparationSnapshot(requests []ModelPreparation) ([]byte, error) {
	if len(requests) < 1 || len(requests) > 32 {
		return nil, errors.New("scene: preparation requires 1–32 models")
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
