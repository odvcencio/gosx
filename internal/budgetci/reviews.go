// Package budgetci binds performance checks to trusted CI metadata.
package budgetci

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"m31labs.dev/gosx/perf/budget"
)

const nativeLimit = 2 << 20

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*/[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var accountPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var filePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*$`)

// Review and ReviewSource are private API records, never public artifacts.
type Review struct {
	ID       int64  `json:"id"`
	CommitID string `json:"commit_id"`
	State    string `json:"state"`
	User     struct {
		Login string `json:"login"`
	} `json:"user"`
}

type ReviewSource interface {
	Reviews(context.Context, string, int64) ([]Review, error)
	Permission(context.Context, string, string) (string, error)
}

type ApprovalOptions struct {
	Root, BudgetPath, HeadSHA, Repository string             `json:"-"`
	PullRequest                           int64              `json:"-"`
	Exceptions                            []budget.Exception `json:"-"`
	Source                                ReviewSource       `json:"-"`
}

func failure(code, pointer string) error {
	return &budget.InputError{Code: code, Reference: "ci", Pointer: pointer}
}

// Approvals reads review state and repository permission from a trusted source,
// then binds each approval to the budget blob at the reviewed ancestor commit.
// An unavailable review service returns no proofs; the gate retains exception
// failures and uses report-only mode through EvaluateExceptions.
func Approvals(ctx context.Context, opts ApprovalOptions) ([]budget.TrustedApproval, error) {
	if ctx == nil || !commitPattern.MatchString(opts.HeadSHA) || !repositoryPattern.MatchString(opts.Repository) || !validFile(opts.BudgetPath) || len(opts.Exceptions) > 64 || opts.PullRequest < 0 {
		return nil, failure("invalid-input", "/approvals")
	}
	if ctx.Err() != nil {
		return nil, failure("timeout", "/approvals")
	}
	head, err := commandOutput(ctx, "git", "-C", opts.Root, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != opts.HeadSHA {
		return nil, failure("wrong-fixture", "/head")
	}
	proofs := []budget.TrustedApproval{}
	if len(opts.Exceptions) == 0 {
		return proofs, nil
	}
	current, err := commandOutput(ctx, "git", "-C", opts.Root, "show", opts.HeadSHA+":"+opts.BudgetPath)
	if err != nil {
		return nil, failure("wrong-fixture", "/head/blob")
	}
	currentDigests, err := budget.ReviewExceptionDigests(bytes.NewReader(current))
	if err != nil {
		return nil, failure("invalid-input", "/head/blob")
	}
	if len(currentDigests) != len(opts.Exceptions) {
		return nil, failure("wrong-fixture", "/exceptions")
	}
	currentIDs := map[string]bool{}
	for _, exception := range opts.Exceptions {
		digest, err := budget.ExceptionSHA256(exception)
		if err != nil || currentDigests[exception.ID] != digest || currentIDs[exception.ID] {
			return nil, failure("wrong-fixture", "/exceptions")
		}
		currentIDs[exception.ID] = true
	}
	if opts.PullRequest <= 0 || opts.Source == nil {
		return proofs, nil
	}
	reviews, err := opts.Source.Reviews(ctx, opts.Repository, opts.PullRequest)
	if err != nil {
		if ctx.Err() != nil {
			return nil, failure("timeout", "/approvals")
		}
		var typed *budget.InputError
		if errors.As(err, &typed) && typed.Code == "invalid-input" {
			return nil, failure("invalid-input", "/reviews")
		}
		return proofs, nil
	}
	if len(reviews) > 1000 {
		return nil, failure("invalid-input", "/reviews")
	}
	latest := map[string]Review{}
	seen := map[int64]bool{}
	for _, review := range reviews {
		if review.ID <= 0 || seen[review.ID] || !commitPattern.MatchString(review.CommitID) {
			return nil, failure("invalid-input", "/reviews")
		}
		seen[review.ID] = true
		if !accountPattern.MatchString(review.User.Login) {
			continue // App and deleted-account records cannot establish a role.
		}
		switch review.State {
		case "COMMENTED", "PENDING":
			continue
		case "APPROVED", "CHANGES_REQUESTED", "DISMISSED":
		default:
			return nil, failure("invalid-input", "/reviews/state")
		}
		if review.ID > latest[review.User.Login].ID {
			latest[review.User.Login] = review
		}
	}
	byID := map[int64]Review{}
	for _, review := range latest {
		if review.State == "APPROVED" {
			byID[review.ID] = review
		}
	}
	permissions := map[string]string{}
	blobs := map[string]map[string]string{}
	for _, exception := range opts.Exceptions {
		digest, err := budget.ExceptionSHA256(exception)
		if err != nil {
			return nil, failure("invalid-input", "/exceptions")
		}
		review, ok := byID[exception.ApprovalReview]
		if !ok {
			continue
		}
		permission, cached := permissions[review.User.Login]
		if !cached {
			permission, err = opts.Source.Permission(ctx, opts.Repository, review.User.Login)
			if err != nil {
				if ctx.Err() != nil {
					return nil, failure("timeout", "/approvals")
				}
				var typed *budget.InputError
				if errors.As(err, &typed) && typed.Code == "invalid-input" {
					return nil, failure("invalid-input", "/permission")
				}
				continue
			}
			permissions[review.User.Login] = permission
		}
		if exception.ApprovedRole == "owner" && permission != "admin" || exception.ApprovedRole == "reviewer" && permission != "admin" && permission != "maintain" && permission != "write" {
			continue
		}
		if exception.ApprovedRole != "owner" && exception.ApprovedRole != "reviewer" {
			return nil, failure("invalid-input", "/exceptions/approvedRole")
		}
		bindings, cached := blobs[review.CommitID]
		if !cached {
			if exec.CommandContext(ctx, "git", "-C", opts.Root, "merge-base", "--is-ancestor", review.CommitID, opts.HeadSHA).Run() != nil {
				continue
			}
			data, err := commandOutput(ctx, "git", "-C", opts.Root, "show", review.CommitID+":"+opts.BudgetPath)
			if err != nil {
				return nil, failure("wrong-fixture", "/reviews/blob")
			}
			bindings, err = budget.ReviewExceptionDigests(bytes.NewReader(data))
			if err != nil {
				return nil, failure("invalid-input", "/reviews/blob")
			}
			blobs[review.CommitID] = bindings
		}
		if bindings[exception.ID] == digest {
			proofs = append(proofs, budget.TrustedApproval{ReviewID: review.ID, Role: exception.ApprovedRole, ExceptionSHA256: digest})
		}
	}
	if ctx.Err() != nil {
		return nil, failure("timeout", "/approvals")
	}
	sort.Slice(proofs, func(i, j int) bool {
		if proofs[i].ReviewID != proofs[j].ReviewID {
			return proofs[i].ReviewID < proofs[j].ReviewID
		}
		return proofs[i].ExceptionSHA256 < proofs[j].ExceptionSHA256
	})
	return proofs, nil
}

func validFile(file string) bool {
	if len(file) > 240 || !filePattern.MatchString(file) {
		return false
	}
	for _, component := range strings.Split(file, "/") {
		if component == "." || component == ".." {
			return false
		}
	}
	return true
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(data []byte) (int, error) {
	if b.Len()+len(data) > nativeLimit {
		return 0, failure("invalid-input", "/response")
	}
	return b.Buffer.Write(data)
}

func commandOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	var out boundedOutput
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &out, io.Discard
	if err := cmd.Run(); err != nil {
		return nil, failure("environment", "/command")
	}
	return out.Bytes(), nil
}

// GitHubReviews uses the runner's authenticated gh client. Repository and
// account values are validated before they enter an API path.
type GitHubReviews struct{}

func (GitHubReviews) Reviews(ctx context.Context, repository string, number int64) ([]Review, error) {
	if ctx == nil || !repositoryPattern.MatchString(repository) || number <= 0 {
		return nil, failure("invalid-input", "/reviews")
	}
	data, err := commandOutput(ctx, "gh", "api", "--paginate", "--slurp", "repos/"+repository+"/pulls/"+strconv.FormatInt(number, 10)+"/reviews?per_page=100")
	if err != nil {
		return nil, err
	}
	var pages [][]Review
	if json.Unmarshal(data, &pages) != nil {
		return nil, failure("invalid-input", "/reviews")
	}
	reviews := []Review{}
	for _, page := range pages {
		reviews = append(reviews, page...)
	}
	return reviews, nil
}

func (GitHubReviews) Permission(ctx context.Context, repository, account string) (string, error) {
	if ctx == nil || !repositoryPattern.MatchString(repository) || !accountPattern.MatchString(account) {
		return "", failure("invalid-input", "/permission")
	}
	data, err := commandOutput(ctx, "gh", "api", "repos/"+repository+"/collaborators/"+account+"/permission")
	if err != nil {
		return "", err
	}
	var response struct {
		Permission string `json:"permission"`
	}
	if json.Unmarshal(data, &response) != nil {
		return "", failure("invalid-input", "/permission")
	}
	return response.Permission, nil
}
