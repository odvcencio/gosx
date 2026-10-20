package budgetci

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"m31labs.dev/gosx/internal/regularfile"
)

// Identity is a private source snapshot. The UTC clock is frozen once when the
// check starts; pull request arguments cannot override CI identity or time.
type Identity struct {
	Head, Base, Repository string    `json:"-"`
	PullRequest            int64     `json:"-"`
	Started                time.Time `json:"-"`
}

// LoadIdentity requires a clean tracked source tree and exact commit objects.
// CI uses only the runner's event record. Local replay supplies an ancestor SHA.
func LoadIdentity(ctx context.Context, root, localBase string) (Identity, error) {
	var out Identity
	if ctx == nil || ctx.Err() != nil {
		return out, failure("invalid-input", "/source")
	}
	out.Started = time.Now().UTC()
	head, err := commandOutput(ctx, "git", "-C", root, "rev-parse", "HEAD")
	if err != nil {
		return Identity{}, failure("wrong-fixture", "/head")
	}
	out.Head = strings.TrimSpace(string(head))
	if !commitPattern.MatchString(out.Head) || exec.CommandContext(ctx, "git", "-C", root, "diff", "--quiet", "HEAD", "--").Run() != nil {
		return Identity{}, failure("wrong-fixture", "/source")
	}
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		if !commitPattern.MatchString(localBase) || exec.CommandContext(ctx, "git", "-C", root, "merge-base", "--is-ancestor", localBase, out.Head).Run() != nil {
			return Identity{}, failure("wrong-fixture", "/base")
		}
		out.Base = localBase
		return out, nil
	}
	if localBase != "" || !repositoryPattern.MatchString(os.Getenv("GITHUB_REPOSITORY")) {
		return Identity{}, failure("invalid-input", "/event")
	}
	path := os.Getenv("GITHUB_EVENT_PATH")
	rootDir, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return Identity{}, failure("environment", "/event")
	}
	defer rootDir.Close()
	f, err := regularfile.Open(rootDir, filepath.Base(path))
	if err != nil {
		if errors.Is(err, regularfile.ErrUnsafe) {
			return Identity{}, failure("invalid-input", "/event")
		}
		return Identity{}, failure("environment", "/event")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return Identity{}, failure("invalid-input", "/event")
	}
	data, err := io.ReadAll(io.LimitReader(f, nativeLimit+1))
	if err != nil || len(data) > nativeLimit || !utf8.Valid(data) {
		return Identity{}, failure("invalid-input", "/event")
	}
	var event struct {
		After, Before, Ref string
		Repository         struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		PullRequest *struct {
			Number int64 `json:"number"`
			Head   struct {
				SHA string `json:"sha"`
			} `json:"head"`
			Base struct {
				SHA  string `json:"sha"`
				Repo struct {
					FullName string `json:"full_name"`
				} `json:"repo"`
			} `json:"base"`
		} `json:"pull_request"`
	}
	if json.Unmarshal(data, &event) != nil {
		return Identity{}, failure("invalid-input", "/event")
	}
	out.Repository = os.Getenv("GITHUB_REPOSITORY")
	if event.Repository.FullName != out.Repository {
		return Identity{}, failure("wrong-fixture", "/event/repository")
	}
	if event.PullRequest != nil && os.Getenv("GITHUB_EVENT_NAME") == "pull_request" {
		pr := event.PullRequest
		if pr.Head.SHA != out.Head || pr.Base.Repo.FullName != out.Repository || pr.Number <= 0 {
			return Identity{}, failure("wrong-fixture", "/event/pull_request")
		}
		out.Base, out.PullRequest = pr.Base.SHA, pr.Number
	} else if event.PullRequest == nil && os.Getenv("GITHUB_EVENT_NAME") == "push" {
		if event.After != out.Head || event.Ref != "refs/heads/main" {
			return Identity{}, failure("wrong-fixture", "/event/push")
		}
		out.Base = event.Before
	} else {
		return Identity{}, failure("invalid-input", "/event/type")
	}
	if !commitPattern.MatchString(out.Base) || strings.Trim(out.Base, "0") == "" || exec.CommandContext(ctx, "git", "-C", root, "cat-file", "-e", out.Base+"^{commit}").Run() != nil {
		return Identity{}, failure("wrong-fixture", "/base")
	}
	return out, nil
}
