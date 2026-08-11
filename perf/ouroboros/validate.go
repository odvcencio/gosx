package ouroboros

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

func ValidateInventory(inv *Inventory) error {
	if inv == nil {
		return fmt.Errorf("inventory is nil")
	}
	if inv.SchemaVersion != SchemaVersion {
		return fmt.Errorf("schemaVersion = %q, want %q", inv.SchemaVersion, SchemaVersion)
	}
	if inv.BaseRevision == "" || inv.BaseRevision == "unknown" {
		return fmt.Errorf("baseRevision is required")
	}
	if inv.OverlayHash == "" {
		return fmt.Errorf("overlayHash is required")
	}
	if inv.Overlay.BaseRevision == "" {
		return fmt.Errorf("overlay baseRevision is required")
	}
	if inv.Overlay.Hash == "" {
		return fmt.Errorf("overlay hash is required")
	}
	if inv.Manifest.BaseRevision == "" || inv.Manifest.OverlayHash == "" {
		return fmt.Errorf("manifest baseRevision and overlayHash are required")
	}
	if inv.Manifest.BaseRevision != inv.BaseRevision || inv.Overlay.BaseRevision != inv.BaseRevision {
		return fmt.Errorf("baseRevision mismatch across root, manifest, and overlay")
	}
	if inv.Manifest.OverlayHash != inv.OverlayHash {
		return fmt.Errorf("manifest overlayHash mismatch: %q != %q", inv.Manifest.OverlayHash, inv.OverlayHash)
	}
	if inv.Overlay.Hash != inv.OverlayHash {
		return fmt.Errorf("overlay hash mismatch: evidence=%q root=%q", inv.Overlay.Hash, inv.OverlayHash)
	}
	if inv.Files.Included == nil || inv.Files.Sidecars == nil || inv.Files.Embedded == nil || inv.Files.Excluded == nil || inv.Files.Audit == nil {
		return fmt.Errorf("file inventory arrays must be present")
	}
	if inv.Scope.Included == nil || inv.Scope.Excluded == nil {
		return fmt.Errorf("scope include and exclude rules are required")
	}
	if inv.Totals.IncludedFiles != len(inv.Files.Included) {
		return fmt.Errorf("included file total mismatch")
	}
	if inv.Totals.SidecarFiles != len(inv.Files.Sidecars) {
		return fmt.Errorf("sidecar file total mismatch")
	}
	if inv.Totals.EmbeddedFiles != len(inv.Files.Embedded) {
		return fmt.Errorf("embedded file total mismatch")
	}
	if inv.Totals.ExcludedFiles != len(inv.Files.Excluded) {
		return fmt.Errorf("excluded file total mismatch")
	}
	if inv.Totals.AuditFiles != len(inv.Files.Audit) {
		return fmt.Errorf("audit file total mismatch")
	}
	if inv.Structural.Gotreesitter.Language == "" {
		return fmt.Errorf("gotreesitter parse summary is required")
	}
	if inv.Surface.GosxNames == nil || inv.Surface.BroaderBrowserGosxNames == nil || inv.Surface.SerializationSites == nil {
		return fmt.Errorf("surface arrays are required")
	}
	if inv.Surface.GosxNameCount != len(inv.Surface.GosxNames) {
		return fmt.Errorf("gosxNameCount mismatch")
	}
	if inv.Surface.BroaderBrowserNameCount != len(inv.Surface.BroaderBrowserGosxNames) {
		return fmt.Errorf("broaderBrowserNameCount mismatch")
	}
	if inv.Surface.SerializationSiteCount != len(inv.Surface.SerializationSites) {
		return fmt.Errorf("serializationSiteCount mismatch")
	}
	if err := validateGosxNameRecords("gosxNames", inv.Surface.GosxNames); err != nil {
		return err
	}
	if err := validateGosxNameRecords("broaderBrowserGosxNames", inv.Surface.BroaderBrowserGosxNames); err != nil {
		return err
	}
	if err := validateSerializationSites(inv.Surface.SerializationSites); err != nil {
		return err
	}
	if err := validateCompatibilityAudit(inv); err != nil {
		return err
	}
	if len(inv.Ratchets) == 0 {
		return fmt.Errorf("at least one scope ratchet is required")
	}
	seenRatchets := map[string]bool{}
	for _, ratchet := range inv.Ratchets {
		if ratchet.ID == "" || ratchet.Scope == "" || ratchet.Status == "" || ratchet.Definition == "" {
			return fmt.Errorf("malformed ratchet: %+v", ratchet)
		}
		if seenRatchets[ratchet.ID] {
			return fmt.Errorf("duplicate ratchet %s", ratchet.ID)
		}
		seenRatchets[ratchet.ID] = true
	}
	if inv.Manifest.FixtureRoutes == nil || inv.Manifest.Variants == nil {
		return fmt.Errorf("manifest routes and variants are required")
	}
	if err := validateManifestVariants(inv); err != nil {
		return err
	}
	if inv.Pixels != nil {
		if inv.Pixels.SchemaVersion != "gosx.ouroboros.pixels.v1" {
			return fmt.Errorf("pixels schemaVersion = %q, want gosx.ouroboros.pixels.v1", inv.Pixels.SchemaVersion)
		}
		if inv.Pixels.ManifestPath == "" || inv.Pixels.Initial == nil || inv.Pixels.Settled == nil {
			return fmt.Errorf("pixels manifestPath, initial, and settled are required")
		}
		for _, capture := range append(append([]PixelCaptureRef{}, inv.Pixels.Initial...), inv.Pixels.Settled...) {
			if capture.RouteID == "" || capture.Backend == "" || capture.Path == "" {
				return fmt.Errorf("malformed pixel capture reference: %+v", capture)
			}
		}
	}
	return nil
}

func validateCompatibilityAudit(inv *Inventory) error {
	audit := inv.Surface.CompatibilityAudit
	if audit.SchemaVersion != compatibilityAuditSchemaVersion {
		return fmt.Errorf("compatibilityAudit schemaVersion = %q", audit.SchemaVersion)
	}
	if audit.Status != "pass" && audit.Status != "fail-closed" {
		return fmt.Errorf("compatibilityAudit status = %q", audit.Status)
	}
	if err := validateCompatibilityNameSet("receipt", audit.Receipt, compatibilityAuditScope, compatibilityReceiptMethod, compatibilityReceiptClassifier, false); err != nil {
		return err
	}
	if audit.Receipt.Count != canonicalGosx || audit.Receipt.NameSetHash != compatibilityReceiptHash {
		return fmt.Errorf("compatibilityAudit receipt does not match pinned 209-name hash")
	}
	if err := validateCompatibilityNameSet("anchor", audit.Anchor, compatibilityFullRuntimeScope, compatibilityFullMethod, compatibilityFullClassifier, true); err != nil {
		return err
	}
	if err := validateCompatibilityNameSet("current", audit.Current, compatibilityFullRuntimeScope, compatibilityFullMethod, compatibilityFullClassifier, true); err != nil {
		return err
	}
	if audit.Receipt.SourceIdentity.Kind != "pinned-receipt" || audit.Receipt.SourceIdentity.ArtifactPath == "" {
		return fmt.Errorf("compatibilityAudit receipt identity mismatch")
	}
	if audit.Anchor.SourceIdentity.Kind != "clean-anchor" || audit.Anchor.SourceIdentity.Revision != inv.BaseRevision || audit.Anchor.SourceIdentity.OverlayHash != OverlayClean {
		return fmt.Errorf("compatibilityAudit anchor identity mismatch")
	}
	if audit.Current.SourceIdentity.Kind != "current-overlay" || audit.Current.SourceIdentity.Revision != inv.BaseRevision || audit.Current.SourceIdentity.OverlayHash != inv.OverlayHash {
		return fmt.Errorf("compatibilityAudit current identity mismatch")
	}
	recovered := differenceStrings(audit.Anchor.Names, audit.Receipt.Names)
	added := differenceStrings(audit.Current.Names, audit.Anchor.Names)
	removed := differenceStrings(audit.Anchor.Names, audit.Current.Names)
	missing := differenceStrings(audit.Receipt.Names, audit.Anchor.Names)
	if !equalStrings(recovered, audit.Reconciliation.RecoveredPreexisting) ||
		!equalStrings(added, audit.Reconciliation.AddedSinceAnchor) ||
		!equalStrings(removed, audit.Reconciliation.RemovedSinceAnchor) ||
		!equalStrings(missing, audit.Reconciliation.MissingFromAnchor) {
		return fmt.Errorf("compatibilityAudit reconciliation mismatch")
	}
	available := len(added) == 0 && len(removed) == 0
	if audit.CanonicalAvailable != available {
		return fmt.Errorf("compatibilityAudit canonical availability mismatch")
	}
	if available && audit.Status != "pass" {
		return fmt.Errorf("compatibilityAudit status = %q, want pass", audit.Status)
	}
	if !available && audit.Status != "fail-closed" {
		return fmt.Errorf("compatibilityAudit status = %q, want fail-closed", audit.Status)
	}
	return nil
}

func validateCompatibilityNameSet(field string, set CompatibilityNameSetEvidence, scope, method, classifier string, requireRuntimeJSONHashes bool) error {
	if set.SourceIdentity.Kind == "" || set.Scope != scope || set.MethodVersion != method || set.ClassifierVersion != classifier || set.Names == nil || set.NameSetHash == "" || set.EvidenceHash == "" {
		return fmt.Errorf("malformed compatibilityAudit %s set", field)
	}
	if set.Count != len(set.Names) {
		return fmt.Errorf("compatibilityAudit %s count mismatch", field)
	}
	if !sort.StringsAreSorted(set.Names) {
		return fmt.Errorf("compatibilityAudit %s names are not sorted", field)
	}
	seen := map[string]bool{}
	for _, name := range set.Names {
		if !gosxExactRe.MatchString(name) || seen[name] {
			return fmt.Errorf("compatibilityAudit %s has invalid name %q", field, name)
		}
		seen[name] = true
	}
	if nameSetHash(set.Names) != set.NameSetHash {
		return fmt.Errorf("compatibilityAudit %s nameSetHash mismatch", field)
	}
	if requireRuntimeJSONHashes {
		for label, value := range map[string]string{
			"runtimeJSONSourceIdentityHash": set.RuntimeJSONSourceIdentityHash,
			"runtimeJSONSemanticHash":       set.RuntimeJSONSemanticHash,
			"runtimeJSONCountsHash":         set.RuntimeJSONCountsHash,
			"runtimeJSONGlobalNameHash":     set.RuntimeJSONGlobalNameHash,
		} {
			if !strings.HasPrefix(value, "sha256:") {
				return fmt.Errorf("compatibilityAudit %s %s missing", field, label)
			}
		}
		if got, want := set.RuntimeJSONGlobalNameHash, RuntimeJSONStaticGlobalNameHash(set.Names); got != want {
			return fmt.Errorf("compatibilityAudit %s runtimeJSONGlobalNameHash mismatch", field)
		}
	} else if set.RuntimeJSONSourceIdentityHash != "" || set.RuntimeJSONSemanticHash != "" || set.RuntimeJSONCountsHash != "" || set.RuntimeJSONGlobalNameHash != "" {
		return fmt.Errorf("compatibilityAudit %s has runtimeJSON hashes on non-runtime evidence", field)
	}
	if got, want := set.EvidenceHash, compatibilityEvidenceHash(set); got != want {
		return fmt.Errorf("compatibilityAudit %s evidenceHash mismatch", field)
	}
	return nil
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func validateGosxNameRecords(field string, records []GosxName) error {
	seen := map[string]bool{}
	validFamilies := map[string]bool{"bootstrap": true, "sidecar": true, "embedded": true, "wasm": true, "wasm-test": true}
	for _, record := range records {
		if record.Name == "" || len(record.Owners) == 0 || len(record.SourceFamilies) == 0 || record.CompatibilityClass == "" {
			return fmt.Errorf("malformed %s record: %+v", field, record)
		}
		if seen[record.Name] {
			return fmt.Errorf("duplicate %s record %s", field, record.Name)
		}
		seen[record.Name] = true
		for _, owner := range record.Owners {
			if owner == "" {
				return fmt.Errorf("%s record %s has empty owner", field, record.Name)
			}
		}
		for _, family := range record.SourceFamilies {
			if !validFamilies[family] {
				return fmt.Errorf("%s record %s has invalid source family %q", field, record.Name, family)
			}
		}
	}
	return nil
}

func validateSerializationSites(sites []SerializationSite) error {
	for _, site := range sites {
		if site.Path == "" || site.Line <= 0 || site.Kind == "" || site.Phase == "" || site.Text == "" {
			return fmt.Errorf("malformed serialization site: %+v", site)
		}
	}
	return nil
}

func validateManifestVariants(inv *Inventory) error {
	currentIDs := map[string]bool{"runtime": true, "islands": true}
	futureIDs := map[string]bool{"core": true, "engine": true, "collab": true, "full": true}
	currentRouteValues := map[string]bool{"runtime": true, "islands": true, "none": true}
	futureRouteValues := map[string]bool{"core": true, "engine": true, "collab": true, "full": true, "none": true}
	for _, route := range inv.Manifest.FixtureRoutes {
		if route.ID == "" || route.Route == "" || route.FixtureApp == "" {
			return fmt.Errorf("malformed fixture route: %+v", route)
		}
		if !currentRouteValues[route.ExpectedTinyGoCurrent] {
			return fmt.Errorf("route %s has invalid current TinyGo variant %q", route.ID, route.ExpectedTinyGoCurrent)
		}
		if !futureRouteValues[route.ExpectedTinyGoFuture] {
			return fmt.Errorf("route %s has invalid future TinyGo variant %q", route.ID, route.ExpectedTinyGoFuture)
		}
	}
	seen := map[string]bool{}
	for _, variant := range inv.Manifest.Variants {
		if variant.ID == "" || variant.Generation == "" || variant.Status == "" || variant.SelectedByRoutes == nil {
			return fmt.Errorf("malformed runtime variant: %+v", variant)
		}
		if seen[variant.ID] {
			return fmt.Errorf("duplicate runtime variant %s", variant.ID)
		}
		seen[variant.ID] = true
		switch variant.Generation {
		case "current":
			if !currentIDs[variant.ID] {
				return fmt.Errorf("invalid current runtime variant %q", variant.ID)
			}
			if variant.Status != "measured" {
				return fmt.Errorf("current runtime variant %s status = %q, want measured", variant.ID, variant.Status)
			}
			if variant.SizeArtifact == nil || variant.WASMArtifact == nil {
				return fmt.Errorf("current runtime variant %s requires size and WASM artifact refs", variant.ID)
			}
			if err := validateArtifactRef(inv, variant.ID, "size", variant.SizeArtifact); err != nil {
				return err
			}
			if err := validateArtifactRef(inv, variant.ID, "wasm", variant.WASMArtifact); err != nil {
				return err
			}
		case "future":
			if !futureIDs[variant.ID] {
				return fmt.Errorf("invalid future runtime variant %q", variant.ID)
			}
			if variant.Status != "planned" {
				return fmt.Errorf("future runtime variant %s status = %q, want planned", variant.ID, variant.Status)
			}
			if variant.SizeBytes != nil || variant.BudgetBytes != nil {
				return fmt.Errorf("planned runtime variant %s must use null sizeBytes and budgetBytes", variant.ID)
			}
			if variant.SizeArtifact != nil || variant.WASMArtifact != nil {
				return fmt.Errorf("planned runtime variant %s must not carry artifact refs", variant.ID)
			}
		default:
			return fmt.Errorf("runtime variant %s has invalid generation %q", variant.ID, variant.Generation)
		}
	}
	for id := range currentIDs {
		if !seen[id] {
			return fmt.Errorf("missing current runtime variant %s", id)
		}
	}
	for id := range futureIDs {
		if !seen[id] {
			return fmt.Errorf("missing future runtime variant %s", id)
		}
	}
	if seen["future-full"] {
		return fmt.Errorf("runtime variant future-full is not allowed")
	}
	return nil
}

func validateArtifactRef(inv *Inventory, variantID, kind string, ref *ArtifactRef) error {
	if ref.SchemaVersion != "gosx.ouroboros.artifact-ref.v1" || ref.Path == "" {
		return fmt.Errorf("%s artifact ref for %s is malformed", kind, variantID)
	}
	if ref.BaseRevision != inv.BaseRevision || ref.OverlayHash != inv.OverlayHash {
		return fmt.Errorf("%s artifact ref for %s has mismatched source anchor", kind, variantID)
	}
	return nil
}

func DecodeInventoryStrict(r io.Reader) (*Inventory, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	var inv Inventory
	if err := dec.Decode(&inv); err != nil {
		return nil, err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return nil, fmt.Errorf("trailing JSON content")
	}
	if err := ValidateInventory(&inv); err != nil {
		return nil, err
	}
	return &inv, nil
}

func DecodeInventoryStrictBytes(body []byte) (*Inventory, error) {
	return DecodeInventoryStrict(bytes.NewReader(body))
}

func ValidateInventoryFresh(ctx context.Context, repoRoot string, inv *Inventory) error {
	if err := ValidateInventory(inv); err != nil {
		return err
	}
	current, err := BuildOverlayEvidence(ctx, repoRoot, inv.BaseRevision)
	if err != nil {
		return err
	}
	if current.Hash != inv.OverlayHash {
		return fmt.Errorf("stale overlay: artifact=%s current=%s", inv.OverlayHash, current.Hash)
	}
	if err := validateCompatibilityAuditFresh(ctx, repoRoot, inv); err != nil {
		return err
	}
	return nil
}

func validateCompatibilityAuditFresh(ctx context.Context, repoRoot string, inv *Inventory) error {
	fresh, err := Collect(ctx, CollectOptions{RepoRoot: repoRoot, Git: true, Canopy: false})
	if err != nil {
		return fmt.Errorf("fresh compatibility collect: %w", err)
	}
	if fresh.BaseRevision != inv.BaseRevision {
		return fmt.Errorf("fresh compatibility baseRevision = %q, want %q", fresh.BaseRevision, inv.BaseRevision)
	}
	if fresh.OverlayHash != inv.OverlayHash {
		return fmt.Errorf("fresh compatibility overlayHash = %q, want %q", fresh.OverlayHash, inv.OverlayHash)
	}
	if err := ValidateInventory(fresh); err != nil {
		return fmt.Errorf("fresh compatibility inventory invalid: %w", err)
	}
	freshAudit := fresh.Surface.CompatibilityAudit
	if err := compareCompatibilityEvidence("anchor", inv.Surface.CompatibilityAudit.Anchor, freshAudit.Anchor); err != nil {
		return err
	}
	if err := compareCompatibilityEvidence("current", inv.Surface.CompatibilityAudit.Current, freshAudit.Current); err != nil {
		return err
	}
	if !equalStrings(freshAudit.Reconciliation.RecoveredPreexisting, inv.Surface.CompatibilityAudit.Reconciliation.RecoveredPreexisting) ||
		!equalStrings(freshAudit.Reconciliation.AddedSinceAnchor, inv.Surface.CompatibilityAudit.Reconciliation.AddedSinceAnchor) ||
		!equalStrings(freshAudit.Reconciliation.RemovedSinceAnchor, inv.Surface.CompatibilityAudit.Reconciliation.RemovedSinceAnchor) ||
		!equalStrings(freshAudit.Reconciliation.MissingFromAnchor, inv.Surface.CompatibilityAudit.Reconciliation.MissingFromAnchor) {
		return fmt.Errorf("fresh compatibility reconciliation mismatch")
	}
	if inv.Surface.CompatibilityAudit.CanonicalAvailable != freshAudit.CanonicalAvailable {
		return fmt.Errorf("fresh compatibility canonical availability mismatch")
	}
	if inv.Surface.CompatibilityAudit.Status != freshAudit.Status {
		return fmt.Errorf("fresh compatibility status = %q, want %q", inv.Surface.CompatibilityAudit.Status, freshAudit.Status)
	}
	return nil
}

func compareCompatibilityEvidence(field string, stored, fresh CompatibilityNameSetEvidence) error {
	checks := []struct {
		name string
		got  string
		want string
	}{
		{"sourceIdentity.kind", stored.SourceIdentity.Kind, fresh.SourceIdentity.Kind},
		{"sourceIdentity.revision", stored.SourceIdentity.Revision, fresh.SourceIdentity.Revision},
		{"sourceIdentity.overlayHash", stored.SourceIdentity.OverlayHash, fresh.SourceIdentity.OverlayHash},
		{"scope", stored.Scope, fresh.Scope},
		{"methodVersion", stored.MethodVersion, fresh.MethodVersion},
		{"classifierVersion", stored.ClassifierVersion, fresh.ClassifierVersion},
		{"nameSetHash", stored.NameSetHash, fresh.NameSetHash},
		{"runtimeJSONSourceIdentityHash", stored.RuntimeJSONSourceIdentityHash, fresh.RuntimeJSONSourceIdentityHash},
		{"runtimeJSONSemanticHash", stored.RuntimeJSONSemanticHash, fresh.RuntimeJSONSemanticHash},
		{"runtimeJSONCountsHash", stored.RuntimeJSONCountsHash, fresh.RuntimeJSONCountsHash},
		{"runtimeJSONGlobalNameHash", stored.RuntimeJSONGlobalNameHash, fresh.RuntimeJSONGlobalNameHash},
		{"evidenceHash", stored.EvidenceHash, fresh.EvidenceHash},
	}
	for _, check := range checks {
		if check.got != check.want {
			return fmt.Errorf("fresh compatibility %s %s = %q, want %q", field, check.name, check.got, check.want)
		}
	}
	if stored.Count != fresh.Count {
		return fmt.Errorf("fresh compatibility %s count = %d, want %d", field, stored.Count, fresh.Count)
	}
	if !equalStrings(stored.Names, fresh.Names) {
		return fmt.Errorf("fresh compatibility %s names mismatch", field)
	}
	return nil
}
