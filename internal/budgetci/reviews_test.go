package budgetci

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx/perf/budget"
)

type reviewFixture struct {
	reviews    []Review
	permission string
	err        error
	calls      int
}

func (s *reviewFixture) Reviews(context.Context, string, int64) ([]Review, error) {
	s.calls++
	return append([]Review{}, s.reviews...), s.err
}
func (s *reviewFixture) Permission(context.Context, string, string) (string, error) {
	s.calls++
	return s.permission, nil
}

func gitFixture(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture Git command failed: %v", err)
	}
	return strings.TrimSpace(string(data))
}

func approvalFixture(t *testing.T) (ApprovalOptions, *reviewFixture) {
	t.Helper()
	root := t.TempDir()
	gitFixture(t, root, "init", "-q")
	gitFixture(t, root, "config", "user.name", "Fixture")
	gitFixture(t, root, "config", "user.email", "fixture")
	data, err := os.ReadFile("../../perf/budget/testdata/budget.v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var file budget.File
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	exception := budget.Exception{ID: "EX-2026-001", Scope: "route:fixture:/counter/", Metric: "totalBytes", Extra: 2000, ReasonCode: "feature", OwnerRole: "app", ApprovedRole: "reviewer", Issue: 7, ApprovalReview: 7, Expires: "2026-10-09"}
	file.Exceptions = []budget.Exception{exception}
	data, _ = json.Marshal(file)
	if err := os.WriteFile(filepath.Join(root, "budget.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	gitFixture(t, root, "add", "budget.json")
	gitFixture(t, root, "commit", "-qm", "Add fixture budget")
	reviewed := gitFixture(t, root, "rev-parse", "HEAD")
	gitFixture(t, root, "commit", "--allow-empty", "-qm", "Keep fixture budget")
	head := gitFixture(t, root, "rev-parse", "HEAD")
	review := Review{ID: 7, CommitID: reviewed, State: "APPROVED"}
	review.User.Login = "fixture-reviewer"
	source := &reviewFixture{reviews: []Review{review}, permission: "write"}
	return ApprovalOptions{Root: root, BudgetPath: "budget.json", HeadSHA: head, Repository: "fixture/project", PullRequest: 1, Exceptions: file.Exceptions, Source: source}, source
}

func TestReviewsBindAncestorBudgetAndRepositoryRole(t *testing.T) {
	opts, source := approvalFixture(t)
	before, _ := json.Marshal(opts.Exceptions)
	proofs, err := Approvals(context.Background(), opts)
	if err != nil || len(proofs) != 1 || proofs[0].ReviewID != 7 || proofs[0].Role != "reviewer" {
		t.Fatal("trusted approval missing", proofs, err)
	}
	digest, err := budget.ExceptionSHA256(opts.Exceptions[0])
	if err != nil || proofs[0].ExceptionSHA256 != digest || source.calls != 2 {
		t.Fatal("approval content or API binding changed", err)
	}
	after, _ := json.Marshal(opts.Exceptions)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("approval adapter mutated configuration")
	}
	private, _ := json.Marshal(opts)
	if string(private) != "{}" {
		t.Fatal("native execution bindings became public")
	}
}

func TestReviewsUnavailableAndSupersededGrantNothing(t *testing.T) {
	for _, cause := range []string{"unavailable", "read", "none", "changes", "dismissed", "new-review", "missing-review", "no-pr", "no-source", "app-account", "foreign-commit"} {
		t.Run(cause, func(t *testing.T) {
			opts, source := approvalFixture(t)
			switch cause {
			case "unavailable":
				source.err = errors.New("private service response")
			case "read", "none":
				source.permission = cause
			case "changes":
				source.reviews[0].State = "CHANGES_REQUESTED"
			case "dismissed":
				source.reviews[0].State = "DISMISSED"
			case "new-review":
				next := source.reviews[0]
				next.ID++
				source.reviews = append(source.reviews, next)
			case "missing-review":
				source.reviews[0].ID++
			case "no-pr":
				opts.PullRequest = 0
			case "no-source":
				opts.Source = nil
			case "app-account":
				source.reviews[0].User.Login = "fixture[bot]"
			case "foreign-commit":
				source.reviews[0].CommitID = strings.Repeat("f", 40)
			}
			proofs, err := Approvals(context.Background(), opts)
			if err != nil || len(proofs) != 0 {
				t.Fatal("untrusted or unavailable approval granted authority", err)
			}
		})
	}
}

func TestReviewsKeepApprovalAcrossCommentsAndCacheSharedProof(t *testing.T) {
	opts, source := approvalFixture(t)
	comment := source.reviews[0]
	comment.ID, comment.State = 9, "COMMENTED"
	source.reviews = append(source.reviews, comment)
	proofs, err := Approvals(context.Background(), opts)
	if err != nil || len(proofs) != 1 {
		t.Fatal("comment erased an approval", err)
	}
	second := opts.Exceptions[0]
	second.ID = "EX-2026-002"
	opts.Exceptions = append(opts.Exceptions, second)
	data, _ := os.ReadFile(filepath.Join(opts.Root, opts.BudgetPath))
	var file budget.File
	json.Unmarshal(data, &file)
	file.Exceptions = opts.Exceptions
	data, _ = json.Marshal(file)
	os.WriteFile(filepath.Join(opts.Root, opts.BudgetPath), data, 0600)
	gitFixture(t, opts.Root, "add", opts.BudgetPath)
	gitFixture(t, opts.Root, "commit", "-qm", "Add second fixture exception")
	opts.HeadSHA = gitFixture(t, opts.Root, "rev-parse", "HEAD")
	source.reviews[0].CommitID, source.calls = opts.HeadSHA, 0
	proofs, err = Approvals(context.Background(), opts)
	if err != nil || len(proofs) != 2 || source.calls != 2 || proofs[0].ExceptionSHA256 >= proofs[1].ExceptionSHA256 {
		t.Fatal("shared review proof was not cached and sorted", err)
	}
}

func TestReviewsRequireOwnerPermissionAndExactContent(t *testing.T) {
	for _, permission := range []string{"write", "maintain", "admin"} {
		t.Run(permission, func(t *testing.T) {
			opts, source := approvalFixture(t)
			opts.Exceptions[0].ApprovedRole = "owner"
			data, _ := os.ReadFile(filepath.Join(opts.Root, opts.BudgetPath))
			var file budget.File
			json.Unmarshal(data, &file)
			file.Exceptions = opts.Exceptions
			data, _ = json.Marshal(file)
			os.WriteFile(filepath.Join(opts.Root, opts.BudgetPath), data, 0600)
			gitFixture(t, opts.Root, "add", opts.BudgetPath)
			gitFixture(t, opts.Root, "commit", "-qm", "Update fixture approval role")
			opts.HeadSHA = gitFixture(t, opts.Root, "rev-parse", "HEAD")
			source.reviews[0].CommitID = opts.HeadSHA
			source.permission = permission
			proofs, err := Approvals(context.Background(), opts)
			if err != nil || (len(proofs) == 1) != (permission == "admin") {
				t.Fatal("owner role did not require repository authority", err)
			}
			// The reviewed ancestor cannot approve different exception content.
			source.reviews[0].CommitID = gitFixture(t, opts.Root, "rev-parse", "HEAD~2")
			proofs, err = Approvals(context.Background(), opts)
			if err != nil || len(proofs) != 0 {
				t.Fatal("older content approved a role change", err)
			}
		})
	}
}

func TestReviewsMalformedInputsRemainToolErrors(t *testing.T) {
	for _, cause := range []string{"head", "repository", "path", "content", "duplicate", "state", "api-json", "nil-context", "cancelled"} {
		t.Run(cause, func(t *testing.T) {
			opts, source := approvalFixture(t)
			ctx := context.Background()
			switch cause {
			case "head":
				opts.HeadSHA = strings.Repeat("a", 40)
			case "repository":
				opts.Repository = "private/value?secret"
			case "path":
				opts.BudgetPath = "../private"
			case "content":
				opts.Exceptions[0].Extra++
			case "duplicate":
				source.reviews = append(source.reviews, source.reviews[0])
			case "state":
				source.reviews[0].State = "private-state"
			case "api-json":
				source.err = failure("invalid-input", "/private")
			case "nil-context":
				ctx = nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			proofs, err := Approvals(ctx, opts)
			var typed *budget.InputError
			if len(proofs) != 0 || !errors.As(err, &typed) || typed.Reference != "ci" || strings.Contains(err.Error(), "private") {
				t.Fatal("tool failure was waived or leaked values", err)
			}
		})
	}
}

func TestReviewsNoExceptionsAvoidNetworkAndBoundNativeOutput(t *testing.T) {
	opts, source := approvalFixture(t)
	opts.Exceptions = nil
	proofs, err := Approvals(context.Background(), opts)
	if err != nil || len(proofs) != 0 || source.calls != 0 {
		t.Fatal("empty exception set queried review metadata", err)
	}
	var output boundedOutput
	if _, err := output.Write(make([]byte, nativeLimit+1)); err == nil || output.Len() != 0 {
		t.Fatal("native response limit was bypassed")
	}
	if _, err := (GitHubReviews{}).Reviews(context.Background(), "bad?repository", 1); err == nil {
		t.Fatal("API repository escaped its grammar")
	}
	if _, err := (GitHubReviews{}).Permission(context.Background(), "fixture/project", "bad/account"); err == nil {
		t.Fatal("API account escaped its grammar")
	}
}
