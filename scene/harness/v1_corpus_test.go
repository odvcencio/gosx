package harness_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

type v1CorpusManifest struct {
	Schema         string         `json:"schema"`
	Contract       string         `json:"contract"`
	ContractCI     v1CIOwner      `json:"contractCI"`
	CompletionRule string         `json:"completionRule"`
	NonGoals       []string       `json:"nonGoals"`
	Cases          []v1CorpusCase `json:"cases"`
}

type v1CorpusCase struct {
	ID             string             `json:"id"`
	State          string             `json:"state"`
	Requirement    string             `json:"requirement"`
	TargetBackends []string           `json:"targetBackends"`
	CIOwners       []v1CIOwner        `json:"ciOwners"`
	Evidence       []v1CorpusEvidence `json:"evidence"`
}

type v1CIOwner struct {
	Label   string `json:"label"`
	Job     string `json:"job"`
	Step    string `json:"step"`
	Command string `json:"command"`
}

type v1CorpusEvidence struct {
	Kind     string   `json:"kind"`
	Path     string   `json:"path"`
	Contains string   `json:"contains"`
	Backends []string `json:"backends"`
	CILabel  string   `json:"ciLabel"`
}

func TestV1CorpusContract(t *testing.T) {
	manifest, repoRoot, docs, workflow := loadV1Corpus(t)
	errs := validateV1Corpus(manifest, repoRoot, docs, workflow)
	if len(errs) != 0 {
		t.Fatalf("Scene3D v1 corpus contract failed:\n- %s", strings.Join(errs, "\n- "))
	}
	blocked := make([]string, 0)
	for _, entry := range manifest.Cases {
		if entry.State == "blocked" {
			blocked = append(blocked, entry.ID)
		}
	}
	sort.Strings(blocked)
	t.Logf("Scene3D v1 contract is coherent; blocked completion cases: %s", strings.Join(blocked, ", "))
}

func TestV1CorpusContractRejectsFalseClaims(t *testing.T) {
	base, repoRoot, docs, workflow := loadV1Corpus(t)
	tests := []struct {
		name string
		want string
		edit func(*v1CorpusManifest, *string)
	}{
		{
			name: "false native backend claim",
			want: `gltf-cubic-trs-morph does not cover target backend "native-preview"`,
			edit: func(manifest *v1CorpusManifest, _ *string) {
				entry := findV1Case(manifest, "gltf-cubic-trs-morph")
				entry.TargetBackends = append(entry.TargetBackends, "native-preview")
			},
		},
		{
			name: "false CI label claim",
			want: `workflow has no job "native-tests"`,
			edit: func(manifest *v1CorpusManifest, _ *string) {
				entry := findV1Case(manifest, "gltf-cubic-trs-morph")
				entry.CIOwners[0].Label = "native-tests"
				entry.CIOwners[0].Job = "native-tests"
				entry.Evidence[0].CILabel = "native-tests"
			},
		},
		{
			name: "false evidence backend claim",
			want: `covers unclaimed backend "headless"`,
			edit: func(manifest *v1CorpusManifest, _ *string) {
				entry := findV1Case(manifest, "gltf-cubic-trs-morph")
				entry.Evidence[0].Backends = append(entry.Evidence[0].Backends, "headless")
			},
		},
		{
			name: "false CI command claim",
			want: `does not run exact command "make imaginary-native-proof"`,
			edit: func(manifest *v1CorpusManifest, _ *string) {
				findV1Case(manifest, "gltf-cubic-trs-morph").CIOwners[0].Command = "make imaginary-native-proof"
			},
		},
		{
			name: "non-test evidence path",
			want: "is not a Node test path",
			edit: func(manifest *v1CorpusManifest, _ *string) {
				entry := findV1Case(manifest, "gltf-cubic-trs-morph")
				entry.Evidence[0].Path = "docs/scene3d-v1-support.md"
				entry.Evidence[0].Contains = "Scene3D v1 support contract"
			},
		},
		{
			name: "documentation state drift",
			want: `docs state "blocked" does not match manifest state "enforced"`,
			edit: func(_ *v1CorpusManifest, docs *string) {
				*docs = strings.Replace(*docs,
					"| `gltf-cubic-trs-morph` | enforced |",
					"| `gltf-cubic-trs-morph` | blocked |", 1)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest := cloneV1Manifest(t, base)
			changedDocs := docs
			tc.edit(&manifest, &changedDocs)
			got := strings.Join(validateV1Corpus(manifest, repoRoot, changedDocs, workflow), "\n")
			if !strings.Contains(got, tc.want) {
				t.Fatalf("validation did not reject claim with %q; errors:\n%s", tc.want, got)
			}
		})
	}
}

func loadV1Corpus(t *testing.T) (v1CorpusManifest, string, string, string) {
	t.Helper()
	const manifestPath = "testdata/v1-corpus.json"
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest v1CorpusManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		t.Fatalf("decode %s: %v", manifestPath, err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		t.Fatalf("%s has trailing JSON: %v", manifestPath, err)
	}
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	docBytes, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(manifest.Contract)))
	if err != nil {
		t.Fatal(err)
	}
	workflowBytes, err := os.ReadFile(filepath.Join(repoRoot, ".github", "workflows", "ci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return manifest, repoRoot, string(docBytes), string(workflowBytes)
}

func validateV1Corpus(manifest v1CorpusManifest, repoRoot, docs, workflow string) []string {
	errs := make([]string, 0)
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }
	if manifest.Schema != "gosx.scene3d.corpus/v1" || manifest.Contract != "docs/scene3d-v1-support.md" {
		add("unexpected manifest identity: schema=%q contract=%q", manifest.Schema, manifest.Contract)
	}
	if manifest.CompletionRule == "" || manifest.ContractCI.Label != "scene3d-v1-corpus" {
		add("contract metadata is incomplete or carries the wrong CI label")
	}
	if err := validateWorkflowOwner(workflow, manifest.ContractCI); err != "" {
		add("contract CI owner: %s", err)
	}

	requiredNonGoals := []string{
		"advanced-taa-ssr-motion-blur", "alpha-sh-custom-attributes-spot-and-point-shadows-as-v1-gates",
		"full-native-visual-and-platform-parity", "general-mobile-touch-pinch-gamepad-controls", "generic-threejs-parity",
		"ik-retargeting-csg-nurbs-modeling-studio", "runtime-meshopt-draco-basis-transcoders", "webxr",
	}
	gotNonGoals := append([]string(nil), manifest.NonGoals...)
	sort.Strings(gotNonGoals)
	if strings.Join(gotNonGoals, "\n") != strings.Join(requiredNonGoals, "\n") {
		add("Scene3D v1 non-goals changed: got %v", gotNonGoals)
	}

	requiredIDs := []string{
		"desktop-controls-picking", "desktop-gizmo-commit", "generic-adapter-command-envelope",
		"glb-bin-plus-external", "gltf-basis-ktx2-policy", "gltf-cubic-trs-morph",
		"gltf-draco-rejection", "gltf-meshopt-rejection", "gltf-multi-buffer-external",
		"gltf-single-buffer-textured", "gltf-sparse-embedded-image", "hub-command-diff",
		"hub-remount-atomic-reject", "native-preview-degradation", "nested-group-scale-pick",
		"ordered-post-custom-uniforms", "scene-p95-budget-route",
	}
	allowedBackends := map[string]bool{
		"webgpu": true, "webgl2": true, "native-preview": true,
		"headless": true, "hub": true,
	}
	docStates := v1DocStates(docs)
	seenIDs := make(map[string]bool, len(manifest.Cases))

	for _, entry := range manifest.Cases {
		if entry.ID == "" || seenIDs[entry.ID] {
			add("empty or duplicate corpus id %q", entry.ID)
			continue
		}
		seenIDs[entry.ID] = true
		if entry.State != "enforced" && entry.State != "blocked" {
			add("%s has invalid state %q", entry.ID, entry.State)
		}
		if entry.Requirement == "" || len(entry.TargetBackends) == 0 {
			add("%s must name a requirement and target backends", entry.ID)
		}
		if docStates[entry.ID] != entry.State {
			add("%s docs state %q does not match manifest state %q", entry.ID, docStates[entry.ID], entry.State)
		}
		for _, backend := range entry.TargetBackends {
			if !allowedBackends[backend] {
				add("%s has unknown target backend %q", entry.ID, backend)
			}
		}
		if entry.State == "blocked" {
			if len(entry.CIOwners) != 0 || len(entry.Evidence) != 0 {
				add("%s is blocked but claims CI-owned evidence", entry.ID)
			}
			continue
		}
		if len(entry.CIOwners) == 0 || len(entry.Evidence) == 0 {
			add("%s is enforced without CI owners and evidence", entry.ID)
			continue
		}

		owners := make(map[string]v1CIOwner, len(entry.CIOwners))
		usedLabels := make(map[string]bool)
		coveredBackends := make(map[string]bool)
		for _, owner := range entry.CIOwners {
			if owner.Label == "" || owners[owner.Label].Label != "" {
				add("%s has empty or duplicate CI label %q", entry.ID, owner.Label)
				continue
			}
			if owner.Label != owner.Job {
				add("%s CI label %q must equal its workflow job %q", entry.ID, owner.Label, owner.Job)
			}
			owners[owner.Label] = owner
			if err := validateWorkflowOwner(workflow, owner); err != "" {
				add("%s CI owner %q: %s", entry.ID, owner.Label, err)
			}
		}

		for _, evidence := range entry.Evidence {
			if err := validateEvidencePath(evidence); err != "" {
				add("%s evidence %s: %s", entry.ID, evidence.Path, err)
			}
			body, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(evidence.Path)))
			if err != nil {
				add("%s evidence %s: %v", entry.ID, evidence.Path, err)
			} else if evidence.Contains == "" || !strings.Contains(string(body), evidence.Contains) {
				add("%s evidence %s is missing marker %q", entry.ID, evidence.Path, evidence.Contains)
			}
			if owners[evidence.CILabel].Label == "" {
				add("%s evidence %s uses undeclared CI label %q", entry.ID, evidence.Path, evidence.CILabel)
			} else {
				usedLabels[evidence.CILabel] = true
			}
			if len(evidence.Backends) == 0 {
				add("%s evidence %s names no backends", entry.ID, evidence.Path)
			}
			for _, backend := range evidence.Backends {
				if !containsV1(entry.TargetBackends, backend) {
					add("%s evidence %s covers unclaimed backend %q", entry.ID, evidence.Path, backend)
					continue
				}
				coveredBackends[backend] = true
			}
		}
		for _, backend := range entry.TargetBackends {
			if !coveredBackends[backend] {
				add("%s does not cover target backend %q", entry.ID, backend)
			}
		}
		for label := range owners {
			if !usedLabels[label] {
				add("%s CI label %q owns no evidence", entry.ID, label)
			}
		}
	}

	actualIDs := make([]string, 0, len(seenIDs))
	for id := range seenIDs {
		actualIDs = append(actualIDs, id)
	}
	sort.Strings(actualIDs)
	if strings.Join(actualIDs, "\n") != strings.Join(requiredIDs, "\n") {
		add("corpus case set changed: got %v", actualIDs)
	}
	for id := range docStates {
		if !seenIDs[id] {
			add("docs table has corpus row %q absent from manifest", id)
		}
	}
	return errs
}

func validateEvidencePath(evidence v1CorpusEvidence) string {
	clean := filepath.ToSlash(filepath.Clean(evidence.Path))
	if evidence.Path == "" || filepath.IsAbs(evidence.Path) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "path must be a repository-relative test"
	}
	switch evidence.Kind {
	case "go-test":
		if !strings.HasSuffix(clean, "_test.go") {
			return "is not a Go test path"
		}
	case "node-test":
		if !strings.HasSuffix(clean, ".test.js") && !strings.HasSuffix(clean, ".test.mjs") {
			return "is not a Node test path"
		}
	case "browser-test":
		if !strings.Contains(clean, "/testdata/") || !strings.HasSuffix(clean, "-browser.cjs") {
			return "is not a bounded browser test path"
		}
	default:
		return fmt.Sprintf("has unknown test kind %q", evidence.Kind)
	}
	return ""
}

func validateWorkflowOwner(workflow string, owner v1CIOwner) string {
	if owner.Job == "" || owner.Step == "" || owner.Command == "" {
		return "job, step, and command must all be exact and non-empty"
	}
	lines := strings.Split(workflow, "\n")
	jobStart := -1
	jobEnd := len(lines)
	for i, line := range lines {
		if line == "  "+owner.Job+":" {
			jobStart = i
			continue
		}
		if jobStart >= 0 && isWorkflowJobHeader(line) {
			jobEnd = i
			break
		}
	}
	if jobStart < 0 {
		return fmt.Sprintf("workflow has no job %q", owner.Job)
	}
	stepStart := -1
	stepEnd := jobEnd
	for i := jobStart + 1; i < jobEnd; i++ {
		if lines[i] == "      - name: "+owner.Step {
			stepStart = i
			continue
		}
		if stepStart >= 0 && strings.HasPrefix(lines[i], "      - name: ") {
			stepEnd = i
			break
		}
	}
	if stepStart < 0 {
		return fmt.Sprintf("job %q has no exact step %q", owner.Job, owner.Step)
	}
	for _, line := range lines[stepStart+1 : stepEnd] {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "run: ") && strings.TrimPrefix(trimmed, "run: ") == owner.Command {
			return ""
		}
		if trimmed == owner.Command {
			return ""
		}
	}
	return fmt.Sprintf("job %q step %q does not run exact command %q", owner.Job, owner.Step, owner.Command)
}

func isWorkflowJobHeader(line string) bool {
	return strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") &&
		strings.HasSuffix(line, ":") && strings.TrimSpace(line) != ""
}

func v1DocStates(docs string) map[string]string {
	states := make(map[string]string)
	for _, line := range strings.Split(docs, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) < 4 {
			continue
		}
		id := strings.Trim(strings.TrimSpace(fields[1]), "`")
		state := strings.TrimSpace(fields[2])
		if id != "" {
			states[id] = state
		}
	}
	return states
}

func cloneV1Manifest(t *testing.T, manifest v1CorpusManifest) v1CorpusManifest {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var clone v1CorpusManifest
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func findV1Case(manifest *v1CorpusManifest, id string) *v1CorpusCase {
	for i := range manifest.Cases {
		if manifest.Cases[i].ID == id {
			return &manifest.Cases[i]
		}
	}
	panic("missing corpus case " + id)
}

func containsV1(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
