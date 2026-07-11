package docs

import "testing"

func TestCMSBlocksFromFormPreservesSubmittedContent(t *testing.T) {
	blocks, errs := cmsBlocksFromForm(map[string]string{
		"hero_title":    "A real title",
		"hero_subtitle": "A real subtitle",
		"feature_title": "Typed actions",
		"feature_body":  "Published through GoSX.",
		"quote_text":    "Ship what the user wrote.",
		"quote_author":  "GoSX",
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
	_, errs := cmsBlocksFromForm(map[string]string{})
	for _, name := range []string{"hero_title", "feature_title", "quote_text"} {
		if errs[name] == "" {
			t.Errorf("missing validation error for %s", name)
		}
	}
}

func TestCMSStorePublishesDefensiveSnapshot(t *testing.T) {
	var store publishStore
	blocks, _ := cmsBlocksFromForm(map[string]string{
		"hero_title": "Published", "feature_title": "Feature", "quote_text": "Quote",
	})
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
