package claimcheck

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// minRepoClaims is the floor on the number of claims in the tree.
//
// The floor exists so the walk cannot pass by finding nothing. A checker that
// parses zero markers and reports success is an inert oracle, and this
// repository has already shipped three of those. Raise the floor when a batch of
// claims lands. Lower it only with a note that says which claims went away and
// why.
//
// The tree carried 38 claims on the day this floor was set to 25.
const minRepoClaims = 25

// TestRepoClaimsHold parses every claim marker in the tree and checks it.
//
// A PASS PROVES: every marker parses, every target file exists, and every claim
// holds against the file it names.
//
// A PASS DOES NOT PROVE: that the prose beside a marker is true. The marker pins
// the one fact a regular expression can state. Choose the fact that the prose
// depends on, so the prose cannot go stale without the marker going red.
func TestRepoClaimsHold(t *testing.T) {
	root := repoRoot(t)

	claims, malformed, err := Scan(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}

	for _, m := range malformed {
		t.Errorf("%s: %s\n  line: %s", m.Where(), m.Reason, m.Text)
	}

	if len(claims) < minRepoClaims {
		t.Fatalf("the walk parsed %d claim(s) and the floor is %d.\n"+
			"Either the markers were deleted or the walk stopped reading the tree. "+
			"A checker that finds nothing must never report success.",
			len(claims), minRepoClaims)
	}

	results := VerifyAll(root, claims)
	if len(results) != len(claims) {
		t.Fatalf("the checker returned %d result(s) for %d claim(s); every claim must be evaluated",
			len(results), len(claims))
	}

	evaluated := map[string]bool{}
	failures := 0
	for _, r := range results {
		evaluated[r.Claim.Where()+" "+r.Claim.Pattern] = true
		if r.OK {
			continue
		}
		failures++
		t.Error(r.Problem)
	}
	for _, c := range claims {
		if !evaluated[c.Where()+" "+c.Pattern] {
			t.Errorf("%s was scanned and never evaluated: %s", c.Where(), c)
		}
	}

	if failures > 0 {
		t.Logf("%d of %d claim(s) in the tree are false", failures, len(claims))
	}
}

// TestRepoUsesEveryVerb keeps a verb from rotting unused.
//
// A verb that no real claim uses is only covered by the fixture corpus. The
// corpus proves the code path runs; this test proves the tree relies on it, so a
// change to that path breaks a real guard and not only a fixture.
func TestRepoUsesEveryVerb(t *testing.T) {
	root := repoRoot(t)
	claims, _, err := Scan(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	used := map[Verb]int{}
	for _, c := range claims {
		used[c.Verb]++
	}
	for _, v := range []Verb{VerbHas, VerbLacks, VerbCount} {
		if used[v] == 0 {
			t.Errorf("no claim in the tree uses the %q verb; only the fixture corpus covers it", v)
		}
	}
	t.Logf("claims by verb: has=%d lacks=%d count=%d", used[VerbHas], used[VerbLacks], used[VerbCount])
}

// TestRepoClaimTargetsAreTracked keeps a claim from pointing at generated output.
//
// A claim against a build artefact passes until somebody cleans the tree, and
// then it fails for a reason that has nothing to do with the comment. Point a
// claim at source.
func TestRepoClaimTargetsAreTracked(t *testing.T) {
	root := repoRoot(t)
	claims, _, err := Scan(root)
	if err != nil {
		t.Fatalf("scan %s: %v", root, err)
	}
	for _, c := range claims {
		for dir := range skipDirs {
			if strings.HasPrefix(c.Target, dir+"/") {
				t.Errorf("%s targets %s, which sits under the skipped directory %q. Point the claim at source.",
					c.Where(), c.Target, dir)
			}
		}
	}
}

// repoRoot returns the directory that holds go.mod above the package directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	root, err := FindRoot(wd)
	if err != nil {
		t.Fatalf("%v", err)
	}
	return root
}

// TestFindRootStopsAtGoMod pins the only piece of path logic the walk depends
// on. A wrong root turns every claim into a missing target.
func TestFindRootStopsAtGoMod(t *testing.T) {
	root := repoRoot(t)
	if _, err := os.Stat(fmt.Sprintf("%s/go.mod", root)); err != nil {
		t.Fatalf("FindRoot returned %s, which holds no go.mod: %v", root, err)
	}
	if _, err := FindRoot(os.TempDir()); err == nil {
		t.Skip("the temporary directory sits inside a Go module on this machine")
	}
}
