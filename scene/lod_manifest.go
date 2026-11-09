package scene

import (
	"strconv"
	"strings"

	"m31labs.dev/gosx/assetpipe/manifest"
)

// ModelLODGroup turns one `gosx assets build` manifest entry into a LODGroup
// that swaps the entry's GLB files by camera distance.
//
// base supplies every Model field except ID and Src (position, scale, shadow
// flags, material, animation). Each level gets a copy of base with its own
// Src. group.ID names the group; level models take the ID
// "<group>-lod<level>". baseURL is prepended to each manifest URI, for example
// "/assets/". The group's Position and Rotation stay zero, so place the models
// through base.
func ModelLODGroup(entry manifest.Model, baseURL string, base Model) LODGroup {
	id := strings.TrimSpace(base.ID)
	if id == "" {
		id = entry.Name
	}
	group := LODGroup{ID: id}
	for _, lod := range entry.LODs {
		level := base
		level.ID = id + "-lod" + strconv.Itoa(lod.Level)
		level.Src = baseURL + lod.URI
		group.Levels = append(group.Levels, LODLevel{Distance: lod.Distance, Node: level})
	}
	return group
}
