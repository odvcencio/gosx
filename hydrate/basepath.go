package hydrate

import (
	"strings"

	"m31labs.dev/gosx/controller"
	"m31labs.dev/gosx/internal/urlpath"
)

// WithBasePath returns a manifest whose framework URL fields use the public
// prefix. It copies changed collections so rendering cannot alter shared state
// or application props. An empty prefix keeps the original manifest.
func (m *Manifest) WithBasePath(prefix string) *Manifest {
	if m == nil || prefix == "" || m.basePath == prefix {
		return m
	}
	if m.basePathSource != nil {
		m = m.basePathSource
	}
	out := *m
	out.basePath = prefix
	out.basePathSource = m
	out.Runtime.Path = urlpath.URL(prefix, out.Runtime.Path)
	out.Islands = append([]IslandEntry(nil), m.Islands...)
	for i := range out.Islands {
		out.Islands[i].ProgramRef = urlpath.URL(prefix, out.Islands[i].ProgramRef)
	}
	out.ComputeIslands = append([]ComputeIslandEntry(nil), m.ComputeIslands...)
	for i := range out.ComputeIslands {
		out.ComputeIslands[i].ProgramRef = urlpath.URL(prefix, out.ComputeIslands[i].ProgramRef)
	}
	out.Engines = append([]EngineEntry(nil), m.Engines...)
	for i := range out.Engines {
		out.Engines[i].ProgramRef = urlpath.URL(prefix, out.Engines[i].ProgramRef)
		out.Engines[i].Props = enginePropsWithBasePath(prefix, out.Engines[i])
	}
	out.Hubs = append([]HubEntry(nil), m.Hubs...)
	for i := range out.Hubs {
		out.Hubs[i].Path = urlpath.URL(prefix, out.Hubs[i].Path)
		if input := out.Hubs[i].Input; input != nil {
			copy := *input
			copy.FightPath = urlpath.URL(prefix, copy.FightPath)
			copy.CPUEndpoint = urlpath.URL(prefix, copy.CPUEndpoint)
			copy.LocalEndpoint = urlpath.URL(prefix, copy.LocalEndpoint)
			copy.FightCurrentEndpoint = urlpath.URL(prefix, copy.FightCurrentEndpoint)
			out.Hubs[i].Input = &copy
		}
	}
	if m.Bundles != nil {
		out.Bundles = make(map[string]BundleRef, len(m.Bundles))
		for key, ref := range m.Bundles {
			ref.Path = urlpath.URL(prefix, ref.Path)
			out.Bundles[key] = ref
		}
	}
	out.Controllers = append([]ControllerEntry(nil), m.Controllers...)
	for i := range out.Controllers {
		out.Controllers[i].Config.Resources = append([]controller.FetchResource(nil), m.Controllers[i].Config.Resources...)
		for j := range out.Controllers[i].Config.Resources {
			r := &out.Controllers[i].Config.Resources[j]
			r.URL = urlpath.URL(prefix, r.URL)
		}
	}
	if m.TextureVariants != nil {
		out.TextureVariants = make(map[string][]ManifestVariantRef, len(m.TextureVariants))
		for key, refs := range m.TextureVariants {
			copies := append([]ManifestVariantRef(nil), refs...)
			for i := range copies {
				copies[i].URI = urlpath.URL(prefix, copies[i].URI)
			}
			out.TextureVariants[key] = copies
		}
		for key := range m.TextureVariants {
			copies := out.TextureVariants[key]
			// glTF resolves relative images against the public model URL. Keep
			// authored keys and also index their public root-relative spelling.
			if len(key) > 0 && key[0] != '/' && !strings.Contains(key, ":") && !strings.HasPrefix(key, "../") {
				alias := urlpath.URL(prefix, "/"+key)
				if _, exists := m.TextureVariants[alias]; !exists {
					if _, rootKey := m.TextureVariants["/"+key]; !rootKey {
						out.TextureVariants[alias] = copies
					}
				}
			} else if strings.HasPrefix(key, "/") && !strings.HasPrefix(key, "//") {
				alias := urlpath.URL(prefix, key)
				if _, exists := m.TextureVariants[alias]; !exists {
					out.TextureVariants[alias] = copies
				}
			}
		}
	}
	return &out
}
