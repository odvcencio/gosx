package budgetci

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/perf/budget"
)

func TestSourceLocalRequiresCleanTreeAndRealAncestor(t *testing.T) {
	opts, source := approvalFixture(t)
	t.Setenv("GITHUB_ACTIONS", "false")
	before := time.Now().UTC()
	identity, err := LoadIdentity(context.Background(), opts.Root, source.reviews[0].CommitID)
	if err != nil || identity.Head != opts.HeadSHA || identity.Base != source.reviews[0].CommitID || identity.Started.Before(before) || identity.Started.After(time.Now().UTC()) {
		t.Fatal("local source snapshot differs", err)
	}
	data, _ := json.Marshal(identity)
	if string(data) != "{}" {
		t.Fatal("private source metadata became a public root")
	}
	if err := os.WriteFile(filepath.Join(opts.Root, "budget.json"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadIdentity(context.Background(), opts.Root, source.reviews[0].CommitID); err == nil {
		t.Fatal("dirty tracked source accepted")
	}
}

func TestSourceCIUsesExactEventHeadBaseAndRepository(t *testing.T) {
	for _, cause := range []string{"valid-pr", "valid-push", "merge-head", "base-repository", "repository", "zero-base", "missing-object", "other-branch", "override", "unknown-event", "invalid-utf8"} {
		t.Run(cause, func(t *testing.T) {
			opts, source := approvalFixture(t)
			base := source.reviews[0].CommitID
			event := map[string]any{"repository": map[string]any{"full_name": opts.Repository}, "pull_request": map[string]any{"number": 1, "head": map[string]any{"sha": opts.HeadSHA}, "base": map[string]any{"sha": base, "repo": map[string]any{"full_name": opts.Repository}}}}
			eventName, localBase := "pull_request", ""
			switch cause {
			case "valid-push", "other-branch":
				delete(event, "pull_request")
				eventName = "push"
				event["after"], event["before"], event["ref"] = opts.HeadSHA, base, "refs/heads/main"
				if cause == "other-branch" {
					event["ref"] = "refs/heads/other"
				}
			case "merge-head":
				event["pull_request"].(map[string]any)["head"] = map[string]any{"sha": base}
			case "base-repository":
				event["pull_request"].(map[string]any)["base"].(map[string]any)["repo"] = map[string]any{"full_name": "fixture/other"}
			case "repository":
				event["repository"] = map[string]any{"full_name": "fixture/other"}
			case "zero-base":
				event["pull_request"].(map[string]any)["base"].(map[string]any)["sha"] = strings.Repeat("0", 40)
			case "missing-object":
				event["pull_request"].(map[string]any)["base"].(map[string]any)["sha"] = strings.Repeat("f", 40)
			case "override":
				localBase = base
			case "unknown-event":
				eventName = "workflow_dispatch"
			}
			data, _ := json.Marshal(event)
			if cause == "invalid-utf8" {
				data = append(data, 0xff)
			}
			path := filepath.Join(t.TempDir(), "event.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GITHUB_ACTIONS", "true")
			t.Setenv("GITHUB_REPOSITORY", opts.Repository)
			t.Setenv("GITHUB_EVENT_NAME", eventName)
			t.Setenv("GITHUB_EVENT_PATH", path)
			identity, err := LoadIdentity(context.Background(), opts.Root, localBase)
			if cause == "valid-pr" || cause == "valid-push" {
				if err != nil || identity.Head != opts.HeadSHA || identity.Base != base || identity.Repository != opts.Repository || (identity.PullRequest == 1) != (cause == "valid-pr") {
					t.Fatal("trusted event did not bind source", err)
				}
			} else {
				var typed *budget.InputError
				if !errors.As(err, &typed) || typed.Reference != "ci" || strings.Contains(err.Error(), "fixture/") {
					t.Fatal("bad source was accepted or leaked metadata", err)
				}
			}
		})
	}
}
