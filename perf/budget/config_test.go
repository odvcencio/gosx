package budget

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func putConfigInput(t *testing.T, root, name string, value any) Ref {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
		t.Fatal(err)
	}
	return Ref{File: name, SHA256: inputDigest(data)}
}

// Each mutation gets a confined, self-contained input tree with fresh hashes.
func configFixture(t *testing.T, edit func(*File, *Profile, *Coefficients, *Toolchain, map[string]any)) string {
	t.Helper()
	root := t.TempDir()
	var f File
	var p Profile
	var c Coefficients
	var tc Toolchain
	var catalog map[string]any
	for _, input := range []struct {
		name string
		out  any
	}{
		{"budget.v2.json", &f}, {"profile.v1.json", &p}, {"coefficients.v1.json", &c},
		{"toolchain.v1.json", &tc}, {"catalog.v1.json", &catalog},
	} {
		data, err := os.ReadFile(filepath.Join("testdata", input.name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, input.out); err != nil {
			t.Fatal(err)
		}
	}
	tc.Fonts = []Ref{}
	interaction := putConfigInput(t, root, "interaction.json", map[string]any{"sequence": "counter-input"})
	catalog["interactionContract"] = interaction
	catalog["routes"].([]any)[0].(map[string]any)["sourcePath"] = "source.gsx"
	if err := os.WriteFile(filepath.Join(root, "source.gsx"), []byte("<button>increment</button>\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(&f, &p, &c, &tc, catalog)
	}
	f.Profile = putConfigInput(t, root, "profile.json", p)
	c.ProfileSHA256 = f.Profile.SHA256
	f.Coefficients = putConfigInput(t, root, "coefficients.json", c)
	f.Toolchain = putConfigInput(t, root, "toolchain.json", tc)
	f.Fixtures = putConfigInput(t, root, "catalog.json", catalog)
	putConfigInput(t, root, "budget.json", f)
	return filepath.Join(root, "budget.json")
}

func TestConfigLoad(t *testing.T) {
	f, err := Load("testdata/budget.v2.json", LoadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	page := f.PageTypes["island"]
	if page.Allocation.Status != "illustrative" || page.Allocation.MinAppBytes != 193536 || page.Backend != "none" {
		t.Fatalf("wrong allocation: %+v", page)
	}
	if _, err := Load("testdata/invalid-budget.v2.json", LoadOptions{}); err == nil {
		t.Fatal("invalid budget input accepted")
	}
	path := configFixture(t, nil)
	t.Chdir(t.TempDir())
	if _, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)}); err != nil {
		t.Fatal(err)
	}
}

func TestConfigDerivationInputsPreserveValidation(t *testing.T) {
	for name, edit := range map[string]func(*File){
		"reference-hash": func(f *File) { f.Profile.SHA256 = strings.Repeat("a", 64) },
		"allocation-schema": func(f *File) {
			page := f.PageTypes["island"]
			page.Allocation.FrameworkBytes = -1
			f.PageTypes["island"] = page
		},
		"mix": func(f *File) {
			page := f.PageTypes["island"]
			page.Mix.OtherPPM++
			f.PageTypes["island"] = page
		},
		"goal-unit": func(f *File) {
			page := f.PageTypes["island"]
			page.Goals[0].Unit = "B"
			f.PageTypes["island"] = page
		},
		"registered-route": func(f *File) { f.Routes[0].RouteTemplate = "/other/" },
		"guardrail":        func(f *File) { f.Guardrails[0].Limit++ },
	} {
		t.Run(name, func(t *testing.T) {
			path := configFixture(t, func(f *File, _ *Profile, _ *Coefficients, _ *Toolchain, _ map[string]any) {
				page := f.PageTypes["island"]
				page.AppReserveBytes += 1024
				f.PageTypes["island"] = page
			})
			root := filepath.Dir(path)
			body, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var file File
			if err := json.Unmarshal(body, &file); err != nil {
				t.Fatal(err)
			}
			edit(&file)
			putConfigInput(t, root, "budget.json", file)
			if _, err := LoadDerivationInputs(path, LoadOptions{RootDir: root}); err == nil {
				t.Fatal("derivation loading accepted invalid inputs")
			}
		})
	}
	path := configFixture(t, nil)
	root := filepath.Dir(path)
	body, err := os.ReadFile(filepath.Join(root, "profile.json"))
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "profile.json")
	if err := os.WriteFile(outside, body, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "profile.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "profile.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadDerivationInputs(path, LoadOptions{RootDir: root}); err == nil {
		t.Fatal("derivation loading accepted a reference outside its root")
	}
}

func TestConfigRejectSemanticErrors(t *testing.T) {
	for name, edit := range map[string]func(*PageType, *CoefficientSet){
		"mix-total":      func(p *PageType, _ *CoefficientSet) { p.Mix.OtherPPM++ },
		"share-fraction": func(p *PageType, _ *CoefficientSet) { p.MinAppPPM = 499999 },
		"rounded-pool": func(p *PageType, _ *CoefficientSet) {
			p.Allocation.TotalBytes += 1024
			p.Allocation.FrameworkBytes += 1024
		},
		"share-bytes": func(p *PageType, _ *CoefficientSet) {
			p.Allocation.MinAppBytes -= 1024
			p.Allocation.FrameworkBytes += 1024
		},
		"after-ready-share": func(p *PageType, _ *CoefficientSet) {
			p.AfterReadyAllocation.MinAppBytes -= 1024
			p.AfterReadyAllocation.FrameworkBytes += 1024
		},
		"reserve": func(p *PageType, _ *CoefficientSet) { p.AppReserveBytes++ },
		"after-ready-reserve": func(p *PageType, _ *CoefficientSet) {
			p.AfterReadyAllocation.AppCriticalReserveBytes = 1024
			p.AfterReadyAllocation.FrameworkBytes -= 1024
		},
		"totals":            func(p *PageType, _ *CoefficientSet) { p.Allocation.TotalBytes++ },
		"missing-primary":   func(p *PageType, _ *CoefficientSet) { p.PrimaryMetric = "fif" },
		"goal-unit":         func(p *PageType, _ *CoefficientSet) { p.Goals[0].Unit = "B" },
		"duplicate-goal":    func(p *PageType, _ *CoefficientSet) { p.Goals = append(p.Goals, p.Goals[0]) },
		"missing-set":       func(p *PageType, _ *CoefficientSet) { p.CoefficientSet = "missing" },
		"warm-set":          func(_ *PageType, s *CoefficientSet) { s.Scenario = "hard-warm" },
		"backend-set":       func(p *PageType, _ *CoefficientSet) { p.Backend = "webgpu" },
		"prior-certificate": func(p *PageType, _ *CoefficientSet) { p.Allocation.Status = "proxy-measured" },
		"unused-work": func(_ *PageType, s *CoefficientSet) {
			e := &s.Entries[3]
			zero := int64(0)
			e.Value = &zero
			e.Status = "unused"
			e.Method = "unused"
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := configFixture(t, func(f *File, _ *Profile, c *Coefficients, _ *Toolchain, _ map[string]any) {
				p := f.PageTypes["island"]
				edit(&p, &c.Sets[0])
				f.PageTypes["island"] = p
			})
			if _, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
				t.Fatal("invalid allocation accepted")
			}
		})
	}
	for name, edit := range map[string]func(*File, map[string]any){
		"unconfigured-type":  func(f *File, _ map[string]any) { f.Routes[0].PageTypes = []string{"static"} },
		"unregistered-route": func(f *File, _ map[string]any) { f.Routes[0].RouteTemplate = "/other/" },
		"duplicate-route":    func(f *File, _ map[string]any) { f.Routes = append(f.Routes, f.Routes[0]) },
		"source-traversal": func(_ *File, c map[string]any) {
			c["routes"].([]any)[0].(map[string]any)["sourcePath"] = "../source.gsx"
		},
		"route-traversal": func(_ *File, c map[string]any) {
			c["routes"].([]any)[0].(map[string]any)["routeTemplate"] = "/../counter/"
		},
		"duplicate-catalog":   func(_ *File, c map[string]any) { r := c["routes"].([]any); c["routes"] = append(r, r[0]) },
		"guardrail-duplicate": func(f *File, _ map[string]any) { f.Guardrails[0] = f.Guardrails[1] },
		"guardrail-limit":     func(f *File, _ map[string]any) { f.Guardrails[0].Limit++ },
		"guardrail-unit":      func(f *File, _ map[string]any) { f.Guardrails[0].Unit = "B" },
		"guardrail-mode":      func(f *File, _ map[string]any) { f.Guardrails[0].Mode = "gate" },
		"framework-exception": func(f *File, _ map[string]any) {
			f.Exceptions = []Exception{{ID: "EX-2026-001", Scope: "type:island", Metric: "frameworkBytes", Extra: 1024, ReasonCode: "temporary", OwnerRole: "runtime", ApprovedRole: "owner", Issue: 1, ApprovalReview: 1, Expires: "2026-11-01"}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := configFixture(t, func(f *File, _ *Profile, _ *Coefficients, _ *Toolchain, c map[string]any) { edit(f, c) })
			if _, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
}

func TestConfigBoundReferences(t *testing.T) {
	for _, name := range []string{"profile.json", "coefficients.json", "toolchain.json", "catalog.json", "interaction.json"} {
		t.Run(name, func(t *testing.T) {
			path := configFixture(t, nil)
			root := filepath.Dir(path)
			file, err := os.OpenFile(filepath.Join(root, name), os.O_APPEND|os.O_WRONLY, 0600)
			if err != nil {
				t.Fatal(err)
			}
			_, writeErr := file.WriteString("\n")
			closeErr := file.Close()
			if writeErr != nil || closeErr != nil {
				t.Fatal(writeErr, closeErr)
			}
			if _, err := Load(path, LoadOptions{RootDir: root}); err == nil {
				t.Fatal("changed reference accepted")
			}
		})
	}
	path := configFixture(t, func(_ *File, _ *Profile, c *Coefficients, _ *Toolchain, _ map[string]any) { c.Reference = "phone-4gb" })
	if _, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
		t.Fatal("mismatched coefficient reference accepted")
	}
	path = configFixture(t, nil)
	root := filepath.Dir(path)
	out := filepath.Join(t.TempDir(), "source.gsx")
	if err := os.WriteFile(out, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "source.gsx")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(out, filepath.Join(root, "source.gsx")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, LoadOptions{RootDir: root}); err == nil {
		t.Fatal("fixture source symlink escape accepted")
	}
}

func TestConfigClosedTypes(t *testing.T) {
	for _, key := range []string{"root", "memory", "exception", "hub", "page-map"} {
		t.Run(key, func(t *testing.T) {
			path := configFixture(t, nil)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var v map[string]any
			if err := json.Unmarshal(data, &v); err != nil {
				t.Fatal(err)
			}
			switch key {
			case "root":
				v["unknown"] = true
			case "memory":
				v["pageTypes"].(map[string]any)["island"].(map[string]any)["memory"] = map[string]any{"unknown": 0}
			case "exception":
				v["exceptions"] = []any{map[string]any{"unknown": 0}}
			case "hub":
				v["hubBudgets"] = []any{map[string]any{"unknown": 0}}
			case "page-map":
				v["pageTypes"].(map[string]any)["unregistered"] = v["pageTypes"].(map[string]any)["island"]
			}
			putConfigInput(t, filepath.Dir(path), "budget.json", v)
			if _, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
				t.Fatal("unknown nested input accepted")
			}
		})
	}
}

func TestConfigMeasuredEvidence(t *testing.T) {
	for _, phone := range []bool{false, true} {
		path := configFixture(t, func(f *File, p *Profile, c *Coefficients, _ *Toolchain, _ map[string]any) {
			zero := int64(0)
			c.Sets[0].PredictionErrorPPM = &zero
			for i := range c.Sets[0].Entries {
				e := &c.Sets[0].Entries[i]
				e.Status = "measured"
				e.Method = "isolated-fit"
				e.CI95 = [2]*int64{e.Value, e.Value}
				e.NVisits = 30
				e.NBlocks = 2
			}
			page := f.PageTypes["island"]
			page.Allocation.Status = "proxy-measured"
			if phone {
				p.Reference = "phone-4gb"
				target := int64(100)
				p.BenchmarkIndexTarget = &target
				c.Reference = p.Reference
				page.Allocation.Status = "phone-measured"
			}
			f.PageTypes["island"] = page
		})
		_, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)})
		if (err == nil) == phone {
			t.Fatalf("wrong evidence result: %v", err)
		}
	}
	for metric, unit := range map[string]string{"cls": "ratio", "dropped_frame_rate": "ratio", "long_tasks": "count", "gpu_tracked_peak": "B", "fif": "ms"} {
		if metricUnit(metric) != unit {
			t.Fatal("wrong metric unit", metric)
		}
	}
	if validRoute("/a/../b/") || !validRoute("/items/{id}/") || !strings.Contains(metricUnit("js_heap_peak"), "B") {
		t.Fatal("invalid semantic vocabulary")
	}
}

func inputDigest(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

func testException() Exception {
	return Exception{ID: "EX-2026-001", Scope: "type:island", Metric: "totalBytes", Extra: 1024, ReasonCode: "temporary", OwnerRole: "runtime", ApprovedRole: "owner", Issue: 1, ApprovalReview: 1, Expires: "2026-11-01"}
}

func TestConfigExceptionContract(t *testing.T) {
	for _, input := range []struct{ scope, metric, policy string }{{"type:island", "totalBytes", ""}, {"route:fixture:/counter/", "totalBytes", ""}, {"type:island", "policy", "html-compressed"}} {
		path := configFixture(t, func(f *File, _ *Profile, _ *Coefficients, _ *Toolchain, _ map[string]any) {
			e := testException()
			e.Scope, e.Metric, e.Policy = input.scope, input.metric, input.policy
			f.Exceptions = []Exception{e}
		})
		if _, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)}); err != nil {
			t.Fatal(err)
		}
	}
	for _, edit := range []func(*Exception){
		func(e *Exception) { e.Scope = "C:/private/file" }, func(e *Exception) { e.Scope = "route:fixture:/counter/?q=1" },
		func(e *Exception) { e.Scope = "route:fixture:/counter/#part" }, func(e *Exception) { e.Scope = "route:other:/counter/" },
		func(e *Exception) { e.Scope = "type:static" }, func(e *Exception) { e.Metric = "policy" },
		func(e *Exception) { e.Policy = "html-compressed" },
		func(e *Exception) { e.Metric = "policy"; e.Policy = "not-a-policy" },
		func(e *Exception) { e.ReasonCode = "ok" }, func(e *Exception) { e.Expires = "2026-02-30" },
		func(e *Exception) { e.Expires = "2026-11-01T00:00:00Z" },
	} {
		path := configFixture(t, func(f *File, _ *Profile, _ *Coefficients, _ *Toolchain, _ map[string]any) {
			e := testException()
			edit(&e)
			f.Exceptions = []Exception{e}
		})
		if _, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
			t.Fatal("invalid exception accepted")
		}
	}
	path := configFixture(t, func(f *File, _ *Profile, _ *Coefficients, _ *Toolchain, _ map[string]any) {
		f.Exceptions = []Exception{testException(), testException()}
	})
	if _, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
		t.Fatal("duplicate exception ID accepted")
	}
	path = configFixture(t, func(f *File, _ *Profile, _ *Coefficients, _ *Toolchain, _ map[string]any) {
		f.Exceptions = []Exception{testException()}
	})
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body = bytes.Replace(body, []byte("type:island"), []byte{'t', 'y', 'p', 'e', ':', 0xff}, 1)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = Load(path, LoadOptions{RootDir: filepath.Dir(path)})
	requireInputError(t, err, "budget", "")
}

func TestConfigStaticAndBackendRules(t *testing.T) {
	for _, family := range []string{"static", "enhanced", "island", "engine/js", "engine/shared", "go-wasm", "video", "preview", "scene3d/js", "scene3d/shared", "game/js", "game/shared"} {
		for _, backend := range []string{"webgpu", "webgl2"} {
			name := family + "-" + backend
			want := strings.HasPrefix(family, "scene3d/") || strings.HasPrefix(family, "game/")
			if knownPageType(name) != want {
				t.Fatalf("wrong backend family: %s", name)
			}
		}
	}
	for _, name := range []string{"static", "island-webgpu", "island", "scene3d/js-webgpu"} {
		path := configFixture(t, func(f *File, _ *Profile, c *Coefficients, _ *Toolchain, _ map[string]any) {
			page := f.PageTypes["island"]
			if name != "static" {
				page.Backend = "webgpu"
				c.Sets[0].Backend = "webgpu"
			}
			if name == "static" {
				page.Allocation.MinAppBytes += page.Allocation.FrameworkBytes - 1
				page.Allocation.FrameworkBytes = 1
				page.AfterReadyAllocation.MinAppBytes += page.AfterReadyAllocation.FrameworkBytes
				page.AfterReadyAllocation.FrameworkBytes = 0
			}
			f.PageTypes = map[string]PageType{name: page}
			f.Routes[0].PageTypes = []string{name}
		})
		_, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)})
		if (err == nil) != (name == "scene3d/js-webgpu") {
			t.Fatalf("backend/static rule %s: %v", name, err)
		}
	}
}

func TestConfigAllPathsAndProfileBinding(t *testing.T) {
	for _, field := range []string{"criticalAssetIDs", "id", "dependencies"} {
		path := configFixture(t, func(_ *File, _ *Profile, _ *Coefficients, _ *Toolchain, c map[string]any) {
			if field == "criticalAssetIDs" {
				c["routes"].([]any)[0].(map[string]any)[field] = []any{"nested/../asset.js"}
				return
			}
			rule := map[string]any{"id": "asset.js", "owner": "app", "kind": "js", "phase": "startup", "condition": "always", "dependencies": []any{}}
			if field == "id" {
				rule[field] = "nested/../asset.js"
			} else {
				rule[field] = []any{"nested/./asset.js"}
			}
			c["assetRules"] = []any{rule}
		})
		if _, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
			t.Fatal("unsafe Path accepted", field)
		}
	}
	path := configFixture(t, nil)
	root := filepath.Dir(path)
	f, err := Load(path, LoadOptions{RootDir: root})
	if err != nil {
		t.Fatal(err)
	}
	var c Coefficients
	body, err := os.ReadFile(filepath.Join(root, "coefficients.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatal(err)
	}
	c.ProfileSHA256 = strings.Repeat("a", 64)
	f.Coefficients = putConfigInput(t, root, "coefficients.json", c)
	putConfigInput(t, root, "budget.json", f)
	_, err = Load(path, LoadOptions{RootDir: root})
	requireInputError(t, err, "coefficients", "/profileSHA256")
}

func TestConfigErrorOrderAndIntegralLimits(t *testing.T) {
	path := configFixture(t, func(f *File, _ *Profile, _ *Coefficients, _ *Toolchain, _ map[string]any) {
		page := f.PageTypes["island"]
		page.Backend = "webgpu"
		f.PageTypes["static"] = page
		page = f.PageTypes["island"]
		page.Mix.OtherPPM++
		f.PageTypes["island"] = page
	})
	for i := 0; i < 32; i++ {
		_, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)})
		requireInputError(t, err, "budget", "/pageTypes/island/mix")
	}
	for _, goal := range []Goal{{Metric: "long_tasks", Max: "1.5", Unit: "count"}, {Metric: "js_heap_peak", Max: "1.5", Unit: "B"}} {
		path := configFixture(t, func(f *File, _ *Profile, _ *Coefficients, _ *Toolchain, _ map[string]any) {
			page := f.PageTypes["island"]
			page.Goals = append(page.Goals, goal)
			f.PageTypes["island"] = page
		})
		if _, err := Load(path, LoadOptions{RootDir: filepath.Dir(path)}); err == nil {
			t.Fatal("fractional discrete limit accepted")
		}
	}
}

func TestConfigNativeInputsReuseVerifiedSnapshots(t *testing.T) {
	path := configFixture(t, nil)
	inputs, err := LoadInputs(path, LoadOptions{RootDir: filepath.Dir(path)})
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if inputs.BudgetSHA256 != inputDigest(original) {
		t.Fatal("budget snapshot hash is not its exact bytes")
	}
	data, err := json.Marshal(inputs)
	if err != nil || string(data) != "{}" {
		t.Fatal("native input snapshot gained a JSON surface", err)
	}
	coefficientPath := filepath.Join(inputs.RootDir(), inputs.File.Coefficients.File)
	if err := os.WriteFile(coefficientPath, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Derive(inputs.File, inputs.Profile, inputs.Coefficients); err != nil {
		t.Fatal("derivation reread snapshot sources", err)
	}
	if _, err := LoadInputs(path, LoadOptions{RootDir: inputs.RootDir()}); err == nil {
		t.Fatal("fresh loader accepted changed referenced bytes")
	}
}
