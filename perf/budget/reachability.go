package budget

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/pagecaps"
	"m31labs.dev/gosx/perf/wire"
)

// ReachabilityOptions contains private, already produced body inventory.
// Inventory is used only when the producer lacks a typed Graph. Bodies bind
// logical IDs to full raw hashes; no filesystem or network access occurs here.
type ReachabilityOptions struct {
	Graph     *buildmanifest.PerfAssetUses
	Inventory []buildmanifest.PerfAssetUse
	Bodies    map[string][]byte
	Route     FixtureRoute
	Backend   string
}
type PlannedAsset struct{ ID, Phase string }
type ResourcePlan struct {
	Reachability    string
	Assets          []PlannedAsset
	documents       *pagecaps.DocumentTree
	executingAssets map[string]bool
}

// ResolveReachability binds extracted references to producer dependencies.
// "known" describes declared closure, not a certified browser observation.
// An old graph or unresolved syntax retains all potential startup bytes.
func ResolveReachability(opts ReachabilityOptions) (ResourcePlan, error) {
	result := ResourcePlan{Reachability: "unknown", Assets: []PlannedAsset{}, executingAssets: map[string]bool{}}
	assets := opts.Inventory
	if opts.Graph != nil {
		assets = opts.Graph.Assets
	}
	graph := &buildmanifest.PerfAssetUses{Version: 1, Assets: assets}
	if opts.Graph != nil {
		graph = opts.Graph
	}
	if err := (&buildmanifest.Manifest{PerfAssetUses: graph}).ValidatePerfAssetUses(); err != nil {
		return result, measureFailure("wrong-fixture", "/graph")
	}
	if !validRoute(opts.Route.RouteTemplate) || validateInput(opts.Backend, inputDefinitions["Backend"]) != nil {
		return result, measureFailure("invalid-input", "/route")
	}
	byID := map[string][]int{}
	byURL := map[string][]string{}
	phases := map[string]string{}
	for i, asset := range assets {
		body, ok := opts.Bodies[asset.ID]
		sum := sha256.Sum256(body)
		if !ok || len(body) > maxMeasureBody || hex.EncodeToString(sum[:]) != asset.SHA256 {
			return result, measureFailure("wrong-fixture", "/assets/"+strconv.Itoa(i)+"/body")
		}
		if prior := byURL[asset.URL]; len(prior) > 0 {
			other := assets[byID[prior[0]][0]]
			if other.SHA256 != asset.SHA256 || other.Kind != asset.Kind {
				return result, measureFailure("wrong-fixture", "/assets/"+strconv.Itoa(i))
			}
		}
		if len(byID[asset.ID]) == 0 {
			byURL[asset.URL] = append(byURL[asset.URL], asset.ID)
		}
		byID[asset.ID] = append(byID[asset.ID], i)
		phases[asset.ID] = "dormant"
	}
	docIDs := byURL[opts.Route.RouteTemplate]
	if len(docIDs) == 0 || assets[byID[docIDs[0]][0]].Kind != "html" {
		return result, measureFailure("wrong-fixture", "/route/document")
	}
	known := opts.Graph != nil && !(opts.Route.Capabilities.Scene3D && opts.Backend == "none")
	type pending struct{ id, phase string }
	queue := []pending{}
	enabled := func(asset buildmanifest.PerfAssetUse) bool {
		switch asset.Condition {
		case "webgpu":
			return opts.Route.Capabilities.Scene3D && opts.Backend == "webgpu"
		case "webgl":
			return opts.Route.Capabilities.Scene3D && opts.Backend == "webgl2"
		case "device-loss", "pipeline-recovery":
			return opts.Route.Capabilities.Scene3D
		case "hls-required":
			return opts.Route.Capabilities.Video
		default:
			return true
		}
	}
	// Document edges are resolved before resource phases. Each embedding keeps
	// its inherited permission, including multiple uses of the same fetched body.
	tree, err := pagecaps.ParseDocumentTree(opts.Bodies[docIDs[0]], opts.Route.RouteTemplate, func(base, reference string) ([]byte, string, bool, error) {
		resolved, ok := resolveReference(base, reference)
		if !ok {
			return nil, "", false, nil
		}
		ids := byURL[resolved]
		if len(ids) == 0 {
			return nil, "", false, measureFailure("undeclared-fetch", "/graph/documents")
		}
		for _, id := range ids {
			for _, i := range byID[id] {
				if enabled(assets[i]) && assets[i].Kind == "html" {
					return opts.Bodies[id], resolved, true, nil
				}
			}
		}
		return nil, resolved, false, nil
	})
	if err != nil {
		if _, ok := err.(*InputError); ok {
			return result, err
		}
		return result, measureFailure("capability", "/html")
	}
	result.documents = tree
	known = known && tree.Complete
	documentContexts := map[string][]*pagecaps.Document{}
	for _, doc := range tree.Documents {
		documentContexts[doc.URL] = append(documentContexts[doc.URL], doc)
	}
	mark := func(id, phase string, executing bool) error {
		indexes, ok := byID[id]
		if !ok {
			return measureFailure("undeclared-fetch", "/graph/dependencies")
		}
		if assets[indexes[0]].Kind == "html" {
			executing = false
			for _, doc := range documentContexts[assets[indexes[0]].URL] {
				executing = executing || doc.ScriptsAllowed
			}
		}
		available := false
		for _, i := range indexes {
			available = available || enabled(assets[i])
		}
		if !available {
			return measureFailure("wrong-backend", "/graph/condition")
		}
		for _, alias := range byURL[assets[indexes[0]].URL] {
			active := false
			for _, i := range byID[alias] {
				active = active || enabled(assets[i])
			}
			if active {
				promoted := phaseRank(phase) < phaseRank(phases[alias])
				executionChanged := executing && !result.executingAssets[alias]
				result.executingAssets[alias] = result.executingAssets[alias] || executing
				if promoted {
					phases[alias] = phase
				}
				if promoted || executionChanged {
					queue = append(queue, pending{alias, phases[alias]})
				}
			}
		}
		return nil
	}
	for _, id := range docIDs {
		if err := mark(id, "critical", true); err != nil {
			return result, err
		}
	}
	for _, id := range opts.Route.CriticalAssetIDs {
		if err := mark(id, "critical", true); err != nil {
			return result, err
		}
	}
	for _, asset := range assets {
		if asset.Phase != "dormant" && asset.Kind != "html" && enabled(asset) {
			if err := mark(asset.ID, asset.Phase, true); err != nil {
				return result, err
			}
		}
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if phases[current.id] != current.phase {
			continue
		}
		indexes := byID[current.id]
		use := assets[indexes[0]]
		dependencies := map[string]bool{}
		for _, i := range indexes {
			asset := assets[i]
			if !enabled(asset) {
				continue
			}
			for _, dep := range asset.Dependencies {
				// A sandbox blocks script-driven dependency edges. Explicit benign
				// references such as preloads are still scanned and charged below.
				if use.Kind == "html" && !result.executingAssets[current.id] && len(documentContexts[use.URL]) > 0 && len(byID[dep]) > 0 {
					kind := assets[byID[dep][0]].Kind
					if kind == "js" || kind == "wasm" || kind == "program" {
						continue
					}
				}
				dependencies[dep] = true
				childPhase := current.phase
				for _, j := range byID[dep] {
					if enabled(assets[j]) {
						childPhase = earlierPhase(childPhase, assets[j].Phase)
					}
				}
				if err := mark(dep, childPhase, result.executingAssets[current.id]); err != nil {
					return result, err
				}
			}
		}
		if use.Kind != "html" && use.Kind != "css" && use.Kind != "js" {
			continue
		}
		type contextReferences struct {
			refs      wire.ReferenceSet
			base      string
			executing bool
		}
		scans := []contextReferences{}
		if use.Kind == "html" && len(documentContexts[use.URL]) > 0 {
			for _, doc := range documentContexts[use.URL] {
				refs, err := wire.ScanDocumentReferences(doc)
				if err != nil {
					return result, measureFailure("wrong-fixture", "/assets/"+strconv.Itoa(indexes[0])+"/references")
				}
				scans = append(scans, contextReferences{refs, doc.URL, doc.ScriptsAllowed})
			}
		} else {
			if use.Kind == "html" {
				known = false
			}
			refs, err := wire.ScanReferences(opts.Bodies[use.ID], use.Kind)
			if err != nil {
				return result, measureFailure("wrong-fixture", "/assets/"+strconv.Itoa(indexes[0])+"/references")
			}
			scans = append(scans, contextReferences{refs, use.URL, result.executingAssets[current.id]})
		}
		for _, scan := range scans {
			refs := scan.refs
			known = known && refs.Complete
			for _, ref := range refs.Resources {
				resolved, ok := resolveReference(scan.base, ref.URL)
				if !ok {
					known = false
					continue
				}
				targets := byURL[resolved]
				if len(targets) == 0 {
					return result, measureFailure("undeclared-fetch", "/assets/"+strconv.Itoa(indexes[0])+"/references")
				}
				if ref.Potential {
					continue
				}
				if use.Kind != "html" {
					declared := false
					for _, id := range targets {
						declared = declared || dependencies[id]
					}
					if !declared {
						return result, measureFailure("undeclared-fetch", "/assets/"+strconv.Itoa(indexes[0])+"/dependencies")
					}
				}
				phase := current.phase
				if use.Kind == "html" {
					phase = laterPhase("startup", current.phase)
				}
				for _, id := range targets {
					if err := mark(id, phase, scan.executing); err != nil {
						return result, err
					}
				}
			}
		}

	}
	if !known {
		// Missing producer evidence and computed references cannot defer or
		// exclude any potential body, even one labeled dormant.
		for _, asset := range assets {
			if asset.Kind != "html" {
				phases[asset.ID] = earlierPhase(phases[asset.ID], "startup")
				result.executingAssets[asset.ID] = true
			}
		}
	} else {
		result.Reachability = "known"
	}
	// Aliases share a physical response and therefore one earliest phase.
	for _, ids := range byURL {
		phase := "dormant"
		for _, id := range ids {
			phase = earlierPhase(phase, phases[id])
		}
		for _, id := range ids {
			phases[id] = phase
		}
	}
	for _, id := range slices.Sorted(maps.Keys(phases)) {
		result.Assets = append(result.Assets, PlannedAsset{ID: id, Phase: phases[id]})
	}
	return result, nil
}

func resolveReference(base, reference string) (string, bool) {
	parent, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	ref, err := url.Parse(reference)
	if err != nil || ref.Scheme != "" || ref.Host != "" || ref.User != nil || ref.RawQuery != "" || ref.RawPath != "" {
		return "", false
	}
	resolved := parent.ResolveReference(ref)
	resolved.Fragment = ""
	if resolved.Path == "" || !strings.HasPrefix(resolved.Path, "/") || strings.Contains(resolved.Path, "\\") {
		return "", false
	}
	return resolved.Path, true
}

func phaseRank(phase string) int {
	switch phase {
	case "critical":
		return 0
	case "startup":
		return 1
	case "after-ready":
		return 2
	case "dormant":
		return 3
	default:
		return 4
	}
}
func earlierPhase(a, b string) string {
	if phaseRank(a) < phaseRank(b) {
		return a
	}
	return b
}

func laterPhase(a, b string) string {
	if phaseRank(a) > phaseRank(b) {
		return a
	}
	return b
}
