package docs

import "testing"

func TestCMSBlocksFromFormPreservesSubmittedContent(t *testing.T) {
	blocks, errs := cmsBlocksFromForm(map[string]string{
		"block_count":      "3",
		"block_0_kind":     "hero",
		"block_0_title":    "A real title",
		"block_0_subtitle": "A real subtitle",
		"block_1_kind":     "feature",
		"block_1_title":    "Typed actions",
		"block_1_body":     "Published through GoSX.",
		"block_2_kind":     "quote",
		"block_2_text":     "Ship what the user wrote.",
		"block_2_author":   "GoSX",
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if got := blocks[0]["title"]; got != "A real title" {
		t.Fatalf("hero title = %q", got)
	}
	if got := blocks[1]["body"]; got != "Published through GoSX." {
		t.Fatalf("feature body = %q", got)
	}
	if got := blocks[2]["text"]; got != "Ship what the user wrote." {
		t.Fatalf("quote text = %q", got)
	}
}

func TestCMSBlocksFromFormRejectsMissingRequiredContent(t *testing.T) {
	_, errs := cmsBlocksFromForm(map[string]string{
		"block_count":  "3",
		"block_0_kind": "hero",
		"block_1_kind": "feature",
		"block_2_kind": "quote",
	})
	for _, name := range []string{"block_0_title", "block_1_title", "block_2_text"} {
		if errs[name] == "" {
			t.Errorf("missing validation error for %s", name)
		}
	}
}

func TestCMSBlocksFromFormRequiresAtLeastOneBlock(t *testing.T) {
	blocks, errs := cmsBlocksFromForm(map[string]string{})
	if blocks != nil {
		t.Fatalf("expected no blocks, got %v", blocks)
	}
	if errs["block_count"] == "" {
		t.Fatal("expected a block_count validation error when no blocks are submitted")
	}
}

// TestCMSBlocksFromFormSupportsMultipleBlocksOfSameKind is the core new
// capability this heat pass adds: the palette can append any number of
// blocks of any kind, not just the original one-of-each fixed layout.
func TestCMSBlocksFromFormSupportsMultipleBlocksOfSameKind(t *testing.T) {
	blocks, errs := cmsBlocksFromForm(map[string]string{
		"block_count":   "4",
		"block_0_kind":  "hero",
		"block_0_title": "Welcome",
		"block_1_kind":  "feature",
		"block_1_title": "First feature",
		"block_2_kind":  "feature",
		"block_2_title": "Second feature",
		"block_3_kind":  "quote",
		"block_3_text":  "Two features, one hero.",
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	if len(blocks) != 4 {
		t.Fatalf("expected 4 blocks, got %d", len(blocks))
	}
	if blocks[1]["kind"] != "feature" || blocks[1]["title"] != "First feature" {
		t.Fatalf("unexpected block[1] = %v", blocks[1])
	}
	if blocks[2]["kind"] != "feature" || blocks[2]["title"] != "Second feature" {
		t.Fatalf("unexpected block[2] = %v", blocks[2])
	}
	summary := cmsSummarizeBlocks(blocks)
	if summary["feature"] != 2 || summary["hero"] != 1 || summary["quote"] != 1 {
		t.Fatalf("unexpected summary: %v", summary)
	}
}

func TestCMSBlocksFromFormEnforcesLengthLimits(t *testing.T) {
	over := make([]byte, 121)
	for i := range over {
		over[i] = 'x'
	}
	_, errs := cmsBlocksFromForm(map[string]string{
		"block_count":   "1",
		"block_0_kind":  "hero",
		"block_0_title": string(over),
	})
	if errs["block_0_title"] == "" {
		t.Fatal("expected a length validation error for an over-limit hero title")
	}
}

func TestCMSBlocksFromFormRejectsUnknownKind(t *testing.T) {
	blocks, errs := cmsBlocksFromForm(map[string]string{
		"block_count":  "1",
		"block_0_kind": "carousel",
	})
	if len(blocks) != 0 {
		t.Fatalf("expected no blocks for an unknown kind, got %v", blocks)
	}
	if errs["block_0_kind"] == "" {
		t.Fatal("expected a validation error for an unknown block kind")
	}
}

func TestCMSBlocksFromFormClampsExcessiveBlockCount(t *testing.T) {
	blocks, errs := cmsBlocksFromForm(map[string]string{
		"block_count": "999999",
	})
	if len(blocks) != 0 {
		t.Fatalf("expected no valid blocks, got %v", blocks)
	}
	if len(errs) != cmsMaxBlocks {
		t.Fatalf("expected block_count to clamp to cmsMaxBlocks (%d) missing-kind errors, got %d", cmsMaxBlocks, len(errs))
	}
}

func TestCMSSummarizeBlocksCountsByKind(t *testing.T) {
	summary := cmsSummarizeBlocks([]map[string]string{
		{"kind": "hero"},
		{"kind": "quote"},
		{"kind": "quote"},
	})
	if summary["hero"] != 1 || summary["quote"] != 2 || summary["feature"] != 0 {
		t.Fatalf("unexpected summary: %v", summary)
	}
}

func TestCMSStorePublishesDefensiveSnapshot(t *testing.T) {
	var store publishStore
	blocks, errs := cmsBlocksFromForm(map[string]string{
		"block_count":   "3",
		"block_0_kind":  "hero",
		"block_0_title": "Published",
		"block_1_kind":  "feature",
		"block_1_title": "Feature",
		"block_2_kind":  "quote",
		"block_2_text":  "Quote",
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected validation errors: %v", errs)
	}
	store.save(blocks)
	got, count, status := store.snapshot()
	if count != 3 || status == "Not published yet" {
		t.Fatalf("snapshot count/status = %d/%q", count, status)
	}
	got[0]["title"] = "mutated"
	again, _, _ := store.snapshot()
	if again[0]["title"] != "Published" {
		t.Fatal("snapshot mutation escaped into store")
	}
}
