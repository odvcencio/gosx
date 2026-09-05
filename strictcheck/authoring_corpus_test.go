package strictcheck

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"m31labs.dev/gosx/internal/gsxcorpus"
)

func TestAuthoringCorpusStrictCheckMatrix(t *testing.T) {
	cases, err := gsxcorpus.Load()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, tc := range cases {
		if tc.StrictCheck == gsxcorpus.Skip {
			continue
		}
		t.Run(tc.Name, func(t *testing.T) {
			root := t.TempDir()
			if err := tc.WriteStrictCheckModule(root); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := strictCheckCorpusContext(t)
			defer cancel()
			err := CheckFileWithOptions(ctx, tc.MainPath(root), Options{
				Env:     append(os.Environ(), "GOPROXY=off", "GOSUMDB=off"),
				GOWORK:  "off",
				GOFLAGS: "-mod=readonly",
			})
			assertCorpusExpectation(t, "strictCheck", tc.StrictCheck, tc.ErrorContains("strictCheck"), err)
			checked++
		})
	}
	if checked == 0 {
		t.Fatal("no strictcheck corpus cases ran")
	}
}

func strictCheckCorpusContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	timeout := 30 * time.Second
	if deadline, ok := t.Deadline(); ok {
		remaining := time.Until(deadline)
		if remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	return context.WithTimeout(context.Background(), timeout)
}

func assertCorpusExpectation(t *testing.T, target string, want gsxcorpus.Expectation, contains string, err error) {
	t.Helper()
	switch want {
	case gsxcorpus.Pass:
		if err != nil {
			t.Fatalf("%s error = %v, want pass", target, err)
		}
	case gsxcorpus.Reject:
		if err == nil {
			t.Fatalf("%s succeeded, want reject", target)
		}
		if contains != "" && !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(contains)) {
			t.Fatalf("%s error = %v, want substring %q", target, err, contains)
		}
	case gsxcorpus.Skip:
	default:
		t.Fatalf("unknown expectation %q for %s", want, target)
	}
}
