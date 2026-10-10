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

// PlannedAsset retains its private declaration URL for traversal and accounting.
type PlannedAsset struct{ ID, URL, Phase string }
type assetUseIdentity struct{ id, url string }
type referenceEnvironment struct {
	document, worker string
	restricted       bool
}
type contextualUse struct {
	key         assetUseIdentity
	environment referenceEnvironment
}
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
	return resolveReachability(opts, nil)
}

// verify returns the confined, verified final response URL for a reached use.
// Offline callers retain declaration URLs; live collection verifies each use
// before scanning it, so redirects cannot leave a stale dependency closure.
func resolveReachability(opts ReachabilityOptions, verify func(PlannedAsset) (string, error)) (ResourcePlan, error) {
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
	byURL := map[string][]int{}
	byUse := map[assetUseIdentity][]int{}
	phases := map[assetUseIdentity]string{}
	for i, asset := range assets {
		body, ok := opts.Bodies[asset.ID]
		sum := sha256.Sum256(body)
		if !ok || len(body) > maxMeasureBody || hex.EncodeToString(sum[:]) != asset.SHA256 {
			return result, measureFailure("wrong-fixture", "/assets/"+strconv.Itoa(i)+"/body")
		}
		if prior := byURL[asset.URL]; len(prior) > 0 {
			other := assets[prior[0]]
			if other.SHA256 != asset.SHA256 || other.Kind != asset.Kind {
				return result, measureFailure("wrong-fixture", "/assets/"+strconv.Itoa(i))
			}
		}
		key := assetUseIdentity{asset.ID, asset.URL}
		byURL[asset.URL] = append(byURL[asset.URL], i)
		byID[asset.ID] = append(byID[asset.ID], i)
		byUse[key] = append(byUse[key], i)
		phases[key] = "dormant"
	}
	docIDs := byURL[opts.Route.RouteTemplate]
	if len(docIDs) == 0 || assets[docIDs[0]].Kind != "html" {
		return result, measureFailure("wrong-fixture", "/route/document")
	}
	known := opts.Graph != nil && !(opts.Route.Capabilities.Scene3D && opts.Backend == "none")
	type pending struct {
		key         assetUseIdentity
		phase       string
		environment referenceEnvironment
	}
	queue := []pending{}
	visits := map[contextualUse]string{}
	environments := map[assetUseIdentity][]referenceEnvironment{}
	finalURLs := map[string]string{}
	scanned := map[string]wire.ReferenceSet{}
	rootBase := ""
	type opaqueEdges struct {
		source, phase string
		dependencies  []string
		restricted    bool
	}
	edges := []opaqueEdges{}
	literalTargets := map[string]map[string]bool{}
	conservative := false
	enabled := func(asset buildmanifest.PerfAssetUse) bool {
		if conservative {
			return true
		}
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

	// Document construction resolves embeddings against the same verified final
	// response URLs as the resource closure. Permissions remain per embedding.
	result.documents = &pagecaps.DocumentTree{Complete: true}
	documentContexts := map[string][]*pagecaps.Document{}
	addDocuments := func(use buildmanifest.PerfAssetUse, final string, restricted bool) error {
		tree, err := pagecaps.ParseDocumentTree(opts.Bodies[use.ID], final, func(base, reference string) ([]byte, string, bool, error) {
			resolved, ok := resolveReferenceFrom(base, reference, final)
			if !ok {
				return nil, "", false, nil
			}
			targets := byURL[resolved]
			if len(targets) == 0 {
				return nil, "", false, measureFailure("undeclared-fetch", "/graph/documents")
			}
			for _, i := range targets {
				asset := assets[i]
				if !enabled(asset) || asset.Kind != "html" {
					continue
				}
				target := asset.URL
				if verify != nil {
					var err error
					target, err = verify(PlannedAsset{ID: asset.ID, URL: asset.URL, Phase: "startup"})
					if err != nil {
						return nil, "", false, err
					}
				}
				return opts.Bodies[asset.ID], target, true, nil
			}
			return nil, resolved, false, nil
		})
		if err != nil {
			if _, ok := err.(*InputError); ok {
				return err
			}
			return measureFailure("capability", "/html")
		}
		if result.documents.Root == nil {
			result.documents.Root = tree.Root
		}
		result.documents.Complete = result.documents.Complete && tree.Complete
		known = known && tree.Complete
		for _, doc := range tree.Documents {
			doc.ScriptsAllowed = doc.ScriptsAllowed && !restricted
			result.documents.Documents = append(result.documents.Documents, doc)
			documentContexts[doc.URL] = append(documentContexts[doc.URL], doc)
		}
		return nil
	}
	rootUse := assets[docIDs[0]]
	rootURL := rootUse.URL
	if verify != nil {
		var err error
		rootURL, err = verify(PlannedAsset{ID: rootUse.ID, URL: rootUse.URL, Phase: "critical"})
		if err != nil {
			return result, err
		}
	}
	if err := addDocuments(rootUse, rootURL, false); err != nil {
		return result, err
	}
	mark := func(key assetUseIdentity, phase string, environment referenceEnvironment) error {
		indexes, ok := byUse[key]
		if !ok {
			return measureFailure("undeclared-fetch", "/graph/dependencies")
		}
		available := false
		for _, i := range indexes {
			available = available || enabled(assets[i])
		}
		if !available {
			return measureFailure("wrong-backend", "/graph/condition")
		}
		for _, i := range byURL[key.url] {
			alias := assets[i]
			aliasKey := assetUseIdentity{alias.ID, alias.URL}
			state := contextualUse{aliasKey, environment}
			if enabled(alias) && phaseRank(phase) < phaseRank(visits[state]) {
				if visits[state] == "" && environment != (referenceEnvironment{}) && !environment.restricted {
					environments[aliasKey] = append(environments[aliasKey], environment)
				}
				phases[aliasKey] = earlierPhase(phases[aliasKey], phase)
				if !environment.restricted {
					result.executingAssets[alias.ID] = true
				}
				visits[state] = phase
				queue = append(queue, pending{aliasKey, phase, environment})
			}
		}
		return nil
	}
	markID := func(id, phase string, environment referenceEnvironment) error {
		indexes, ok := byID[id]
		if !ok {
			return measureFailure("undeclared-fetch", "/graph/dependencies")
		}
		available := false
		for _, i := range indexes {
			asset := assets[i]
			if !enabled(asset) {
				continue
			}
			available = true
			if err := mark(assetUseIdentity{asset.ID, asset.URL}, phase, environment); err != nil {
				return err
			}
		}
		if !available {
			return measureFailure("wrong-backend", "/graph/condition")
		}
		return nil
	}
	markURL := func(assetURL, phase string, environment referenceEnvironment) error {
		available := false
		for _, i := range byURL[assetURL] {
			asset := assets[i]
			if !enabled(asset) {
				continue
			}
			available = true
			if err := mark(assetUseIdentity{asset.ID, asset.URL}, phase, environment); err != nil {
				return err
			}
		}
		if !available {
			return measureFailure("wrong-backend", "/graph/condition")
		}
		return nil
	}
	if err := markURL(opts.Route.RouteTemplate, "critical", referenceEnvironment{}); err != nil {
		return result, err
	}
	for _, id := range opts.Route.CriticalAssetIDs {
		if err := markID(id, "critical", referenceEnvironment{}); err != nil {
			return result, err
		}
	}
	for _, asset := range assets {
		if asset.Phase != "dormant" && asset.Kind != "html" && enabled(asset) {
			if err := mark(assetUseIdentity{asset.ID, asset.URL}, asset.Phase, referenceEnvironment{}); err != nil {
				return result, err
			}
		}
	}
	for len(queue) > 0 || len(edges) > 0 || !known && !conservative {
		if len(queue) == 0 && len(edges) > 0 {
			// Resolve every established execution context before treating an
			// unrepresented typed edge as opaque. A shared script's document-
			// relative literals can select different dependencies in each frame.
			ready := edges
			edges = nil
			for _, edge := range ready {
				for _, dep := range edge.dependencies {
					if literalTargets[edge.source][dep] {
						continue
					}
					phase := edge.phase
					for _, i := range byID[dep] {
						if enabled(assets[i]) {
							phase = earlierPhase(phase, assets[i].Phase)
						}
					}
					// A typed edge without a loader literal does not establish a
					// window/worker realm. Relative environment APIs remain unknown.
					if err := markID(dep, phase, referenceEnvironment{restricted: edge.restricted}); err != nil {
						return result, err
					}
				}
			}
			continue
		}
		if len(queue) == 0 {
			// Missing producer evidence and computed references cannot defer or
			// exclude any potential body, even one labeled dormant. Traverse the
			// promoted bodies too: they may reach HTML and redirect-relative edges.
			conservative = true
			for _, asset := range assets {
				if asset.Kind != "html" {
					key := assetUseIdentity{asset.ID, asset.URL}
					if err := mark(key, "startup", referenceEnvironment{}); err != nil {
						return result, err
					}
				}
			}
			continue
		}
		// Establish document and worker environments through actual edges before
		// interpreting unanchored inventory scripts. A shared script is scanned in
		// every established environment, even when its body was already reached.
		next := 0
		for i, item := range queue {
			if item.environment != (referenceEnvironment{}) || assets[byUse[item.key][0]].Kind != "js" {
				next = i
				break
			}
		}
		current := queue[next]
		queue = append(queue[:next], queue[next+1:]...)
		if visits[contextualUse{current.key, current.environment}] != current.phase {
			continue
		}
		indexes := byUse[current.key]
		use := assets[indexes[0]]
		if use.Kind == "js" && current.environment == (referenceEnvironment{}) && len(environments[current.key]) > 0 {
			for _, environment := range environments[current.key] {
				if err := mark(current.key, current.phase, environment); err != nil {
					return result, err
				}
			}
			continue
		}
		referenceBase := use.URL
		missingFinalContract := false
		if verify != nil {
			final, err := verify(PlannedAsset{ID: use.ID, URL: use.URL, Phase: current.phase})
			if err != nil {
				return result, err
			}
			referenceBase = final
			resolved, err := url.Parse(final)
			if err != nil {
				return result, measureFailure("wrong-fixture", "/graph/finalURL")
			}
			// A redirect selects the final declaration's dependency contract.
			// The verified body still has to agree with the original use.
			if targets := byURL[resolved.Path]; len(targets) > 0 {
				for _, i := range targets {
					if assets[i].SHA256 != use.SHA256 || assets[i].Kind != use.Kind {
						return result, measureFailure("wrong-fixture", "/graph/finalURL")
					}
				}
				if err := markURL(resolved.Path, current.phase, current.environment); err != nil {
					return result, err
				}
				indexes = targets
			} else {
				missingFinalContract = resolved.Path != use.URL
			}
		}
		finalURLs[use.URL] = referenceBase

		if use.Kind == "html" {
			if len(documentContexts[referenceBase]) == 0 {
				// Fetching or prefetching HTML does not embed a browsing document.
				// Without an embedding context its execution and closure cannot
				// be certified; retain the conservative resource inventory.
				known = false
			}
			permitted := false
			for _, doc := range documentContexts[referenceBase] {
				permitted = permitted || doc.ScriptsAllowed
			}
			current.environment.restricted = !permitted
			result.executingAssets[use.ID] = permitted
		}
		dependencies := map[string]bool{}
		for _, i := range indexes {
			asset := assets[i]
			if !enabled(asset) {
				continue
			}
			for _, dep := range asset.Dependencies {
				if current.environment.restricted && len(byID[dep]) > 0 {
					kind := assets[byID[dep][0]].Kind
					if kind == "js" || kind == "wasm" || kind == "program" {
						continue
					}
				}
				dependencies[dep] = true
			}
		}

		type contextReferences struct {
			refs        wire.ReferenceSet
			environment referenceEnvironment
		}
		scans := []contextReferences{}
		if use.Kind == "html" {
			for _, doc := range documentContexts[referenceBase] {
				refs, err := wire.ScanDocumentReferences(doc)
				if err != nil {
					return result, measureFailure("wrong-fixture", "/assets/"+strconv.Itoa(indexes[0])+"/references")
				}
				environment := referenceEnvironment{document: doc.BaseURL, restricted: !doc.ScriptsAllowed}
				scans = append(scans, contextReferences{refs, environment})
				if use.URL == opts.Route.RouteTemplate && doc == result.documents.Root {
					rootBase = doc.BaseURL
				}
			}
		} else {
			refs := wire.ReferenceSet{Complete: true}
			if use.Kind == "css" || use.Kind == "js" && !current.environment.restricted {
				var found bool
				refs, found = scanned[use.ID]
				if !found {
					var err error
					refs, err = wire.ScanReferences(opts.Bodies[use.ID], use.Kind)
					if err != nil {
						return result, measureFailure("wrong-fixture", "/assets/"+strconv.Itoa(indexes[0])+"/references")
					}
					scanned[use.ID] = refs
				}
			}
			scans = append(scans, contextReferences{refs, current.environment})
		}
		for _, scan := range scans {
			refs := scan.refs
			current.environment = scan.environment
			known = known && refs.Complete
			if literalTargets[referenceBase] == nil {
				literalTargets[referenceBase] = map[string]bool{}
			}
			for _, ref := range refs.Resources {
				base := referenceURLBase(ref, referenceBase, rootBase, current.environment, finalURLs)
				resolved, ok := resolveReferenceFrom(base, ref.URL, referenceBase)
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
					for _, i := range targets {
						declared = declared || enabled(assets[i]) && dependencies[assets[i].ID]
					}
					if !declared {
						if !missingFinalContract {
							return result, measureFailure("undeclared-fetch", "/assets/"+strconv.Itoa(indexes[0])+"/dependencies")
						}
						// The original URL's edge list cannot certify a new
						// relative target without the final declaration.
						known = false
					}
				}
				phase := current.phase
				if use.Kind == "html" && phase == "critical" {
					phase = "startup"
				}
				for _, i := range targets {
					asset := assets[i]
					if enabled(asset) {
						literalTargets[referenceBase][asset.ID] = true
						if dependencies[asset.ID] {
							phase = earlierPhase(phase, earlierPhase(current.phase, asset.Phase))
						}
					}
				}
				environment := current.environment
				if ref.Worker {
					environment = referenceEnvironment{worker: resolved, restricted: current.environment.restricted}
				}
				if err := markURL(resolved, phase, environment); err != nil {
					return result, err
				}
			}
			edges = append(edges, opaqueEdges{referenceBase, current.phase, slices.Sorted(maps.Keys(dependencies)), current.environment.restricted})
		}
	}
	if known {
		result.Reachability = "known"
	}
	// Aliases share a physical response and therefore one earliest phase.
	for _, indexes := range byURL {
		phase := "dormant"
		for _, i := range indexes {
			asset := assets[i]
			phase = earlierPhase(phase, phases[assetUseIdentity{asset.ID, asset.URL}])
		}
		for _, i := range indexes {
			asset := assets[i]
			phases[assetUseIdentity{asset.ID, asset.URL}] = phase
		}
	}
	for _, key := range slices.SortedFunc(maps.Keys(phases), func(a, b assetUseIdentity) int {
		if a.id != b.id {
			return strings.Compare(a.id, b.id)
		}
		return strings.Compare(a.url, b.url)
	}) {
		result.Assets = append(result.Assets, PlannedAsset{ID: key.id, URL: key.url, Phase: phases[key]})
	}
	return result, nil
}

// HTML's first base href sets the document API base, including inline scripts
// and styles. Modules and external stylesheets retain their own final URL.
func documentReferenceBase(final string, refs wire.ReferenceSet) string {
	if !refs.HasBaseHref {
		return final
	}
	parent, err := url.Parse(final)
	if err != nil {
		return ""
	}
	href, err := url.Parse(strings.Trim(refs.BaseHref, " \t\r\n\f"))
	if err != nil {
		return ""
	}
	return parent.ResolveReference(href).String()
}

func referenceURLBase(ref wire.Reference, source, root string, environment referenceEnvironment, finals map[string]string) string {
	switch ref.Base {
	case wire.ReferenceBaseSource:
		return source
	case wire.ReferenceBaseDocument:
		return environment.document
	case wire.ReferenceBaseEnvironment, wire.ReferenceBaseWorker:
		if environment.worker != "" {
			if final, ok := finals[environment.worker]; ok {
				return final
			}
			return ""
		}
		if ref.Base == wire.ReferenceBaseWorker {
			return ""
		}
		if environment.document != "" {
			return environment.document
		}
		// A root-relative URL is independent of the script's unresolved realm
		// only when both possible environments retain the verified origin.
		if strings.HasPrefix(ref.URL, "/") && !strings.HasPrefix(ref.URL, "//") {
			return root
		}
	}
	return ""
}

func resolveReferenceFrom(base, reference, origin string) (string, bool) {
	if base == "" {
		return "", false
	}
	trusted, err := url.Parse(origin)
	if err != nil {
		return "", false
	}
	parent, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	parent = trusted.ResolveReference(parent)
	ref, err := url.Parse(reference)
	if err != nil || ref.User != nil || ref.RawQuery != "" || ref.ForceQuery || ref.RawPath != "" {
		return "", false
	}
	resolved := parent.ResolveReference(ref)
	resolved.Fragment = ""
	if resolved.Scheme != trusted.Scheme || resolved.Host != trusted.Host || resolved.User != nil || resolved.Path == "" || !strings.HasPrefix(resolved.Path, "/") || strings.ContainsAny(resolved.Path, "\\ \t\r\n") {
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
