package budget

import (
	"bytes"
	"encoding/json"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/internal/pagecaps"
)

func fixtureAppAssets(assets []buildmanifest.PerfAssetUse, app string) []buildmanifest.PerfAssetUse {
	uses := []buildmanifest.PerfAssetUse{}
	for _, use := range assets {
		if use.Owner != "app" || strings.HasPrefix(use.ID, "app/"+app+"/") {
			uses = append(uses, use)
		}
	}
	return uses
}

// A serving URL is one physical identity. Compatible aliases share kind and
// hash; a framework declaration supplies the complete reporting metadata.
func fixturePhysicalAssets(uses []buildmanifest.PerfAssetUse) ([]buildmanifest.PerfAssetUse, error) {
	physical := []buildmanifest.PerfAssetUse{}
	byURL := map[string]int{}
	for _, use := range uses {
		if i, ok := byURL[use.URL]; ok {
			if physical[i].SHA256 != use.SHA256 || physical[i].Kind != use.Kind {
				return nil, measureFailure("wrong-fixture", "/manifest/assets")
			}
			phase := earlierPhase(physical[i].Phase, use.Phase)
			if use.Owner == "framework" && physical[i].Owner == "app" || use.Owner == physical[i].Owner && use.ID < physical[i].ID {
				physical[i] = use
			}
			physical[i].Phase = phase
			continue
		}
		byURL[use.URL] = len(physical)
		physical = append(physical, use)
	}
	return physical, nil
}

// Production validates staged bytes with the same route and graph checks as
// measurement. Measurement supplies fresh HTTP document bytes here as well.
func validateFixtureRoute(route FixtureRoute, uses []buildmanifest.PerfAssetUse, bodies map[string][]byte, document []byte, backend string) (ResourcePlan, pagecaps.Capabilities, error) {
	caps, err := pagecaps.FromHTML(document)
	if err != nil {
		return ResourcePlan{}, caps, measureFailure("capability", "/routes/capabilities")
	}
	if err := validateFixtureCapabilities(route, caps); err != nil {
		return ResourcePlan{}, caps, err
	}
	plan, err := ResolveReachability(ReachabilityOptions{Graph: &buildmanifest.PerfAssetUses{Version: 1, Assets: fixtureAppAssets(uses, route.App)}, Bodies: bodies, Route: route, Backend: backend})
	return plan, caps, err
}

func validateFixtureCapabilities(route FixtureRoute, caps pagecaps.Capabilities) error {
	observed, _ := json.Marshal(caps)
	declared, _ := json.Marshal(route.Capabilities)
	if !bytes.Equal(observed, declared) {
		return measureFailure("capability", "/routes/capabilities")
	}
	detected, err := pagecaps.Classify(caps, false)
	if err != nil || !fixtureCoversTypes(route.PageTypes, detected) {
		return measureFailure("capability", "/routes/pageTypes")
	}
	return nil
}
