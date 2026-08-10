package main

import (
	"crypto/sha256"
	"encoding/hex"
	"path"
	"sort"
	"strings"

	perfouroboros "m31labs.dev/gosx/perf/ouroboros"
)

const (
	ResourceManifestSchemaVersion = perfouroboros.ResourceManifestSchemaVersion
	CanonicalResourceManifestRef  = perfouroboros.CanonicalResourceManifestRef
)

type ouroborosResourceManifest = perfouroboros.ResourceManifest
type ouroborosManifestResource = perfouroboros.ResourceManifestResource
type ouroborosManifestRoute = perfouroboros.ResourceManifestRoute
type ouroborosDynamicEndpoint = perfouroboros.ResourceManifestDynamic
type ouroborosResourceExclusion = perfouroboros.ResourceManifestExclusion

func validateOuroborosResourceManifestWithPerf(distRoot string) error {
	_, err := perfouroboros.LoadAndValidateResourceManifest(distRoot, CanonicalResourceManifestRef, true)
	return err
}

func buildOuroborosResourceManifest(corpus ouroborosCorpusManifest, resources []ouroborosExportResourceRef, dynamics []ouroborosExportDynamicRef) ouroborosResourceManifest {
	routes := make([]ouroborosManifestRoute, 0, len(canonicalOuroborosExportIDs))
	routePaths := canonicalOuroborosRoutePathByID()
	byRoute := map[string][]string{}
	for _, resource := range resources {
		for _, routeID := range resource.Routes {
			byRoute[routeID] = append(byRoute[routeID], resource.ID)
		}
	}
	for _, id := range canonicalOuroborosExportIDs {
		ids := append([]string(nil), byRoute[id]...)
		sort.Strings(ids)
		routes = append(routes, ouroborosManifestRoute{ID: id, Route: routePaths[id], Resources: ids})
	}
	endpoints := []ouroborosDynamicEndpoint{}
	for _, dynamic := range dynamics {
		for _, routeID := range dynamic.Routes {
			endpoints = append(endpoints, ouroborosDynamicEndpoint{
				ID:       stableOuroborosResourceID("dynamic", routeID, dynamic.Ref),
				RouteID:  routeID,
				Route:    routePaths[routeID],
				Kind:     dynamic.Kind,
				URL:      dynamic.Ref,
				Producer: dynamic.Producer,
			})
		}
	}
	sort.Slice(endpoints, func(i, j int) bool { return endpoints[i].ID < endpoints[j].ID })
	manifestResources := make([]ouroborosManifestResource, 0, len(resources))
	for _, resource := range resources {
		manifestResources = append(manifestResources, ouroborosManifestResource{
			ID:           resource.ID,
			URL:          resource.Ref,
			OutputPath:   resource.File,
			Producer:     resource.Producer,
			Kind:         resource.Kind,
			Source:       resource.Source,
			ContentType:  resource.ContentType,
			SHA256:       resource.SHA256,
			Bytes:        resource.Bytes,
			GzipBytes:    resource.GzipBytes,
			BrotliBytes:  resource.BrotliBytes,
			UsedByRoutes: append([]string(nil), resource.Routes...),
			Parents:      append([]string(nil), resource.Parents...),
		})
	}
	sort.Slice(manifestResources, func(i, j int) bool {
		if manifestResources[i].URL != manifestResources[j].URL {
			return manifestResources[i].URL < manifestResources[j].URL
		}
		return manifestResources[i].ID < manifestResources[j].ID
	})
	return ouroborosResourceManifest{
		SchemaVersion:    ResourceManifestSchemaVersion,
		Contract:         corpus.ContractVersion,
		CorpusID:         corpus.CorpusID,
		Resources:        manifestResources,
		Routes:           routes,
		DynamicEndpoints: endpoints,
		Exclusions:       []ouroborosResourceExclusion{},
	}
}

func canonicalOuroborosRoutePathByID() map[string]string {
	return map[string]string{"R00": "/static", "R01": "/lite", "R02": "/island/counter", "R03": "/islands/kitchen", "R04": "/action/form", "R05": "/canvas-board", "R06": "/hub/echo", "R07": "/video-sync", "R08": "/scene/basic", "R09A": "/navigation/a", "R09B": "/navigation/b", "R10": "/demos/water"}
}

func stableOuroborosResourceID(parts ...string) string {
	if len(parts) == 2 && parts[0] == "resource" {
		return canonicalOuroborosResourceID(parts[1])
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "res-" + hex.EncodeToString(sum[:])[:16]
}

func canonicalOuroborosResourceID(ref string) string {
	clean := strings.Trim(path.Clean("/"+strings.TrimLeft(ref, "/")), "/")
	if clean == "" {
		clean = "root"
	}
	replacer := strings.NewReplacer("/", ":", "_", "-", ".", "-", " ", "-", "%", "-")
	id := "res:" + replacer.Replace(clean)
	if len(id) <= 96 {
		return id
	}
	sum := sha256.Sum256([]byte(ref))
	return id[:79] + ":" + hex.EncodeToString(sum[:])[:16]
}

func ouroborosResourceKind(ref string) string {
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(ref)), ".")
	if ext == "" {
		return "endpoint"
	}
	return ext
}
