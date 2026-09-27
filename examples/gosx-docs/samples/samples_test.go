package samples

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

var goImportLine = regexp.MustCompile(`(?m)^import\s+[^\n]+\n`)
var gosxPackageDeclaration = regexp.MustCompile(`(?m)^package\s+[A-Za-z_][A-Za-z0-9_]*\s*$`)
var goPackageDeclaration = regexp.MustCompile(`(?m)^package\s+[A-Za-z_][A-Za-z0-9_]*\s*$`)
var goSampleImportSpec = regexp.MustCompile(`(?m)^\s*import\s+(?:(\w+)\s+)?"([^"]+)"\s*$`)

var goSampleImportPaths = map[string]string{
	"action":        "m31labs.dev/gosx/action",
	"auth":          "m31labs.dev/gosx/auth",
	"authn":         "m31labs.dev/gosx/auth",
	"authredis":     "m31labs.dev/gosx/auth/redis",
	"context":       "context",
	"crdt":          "m31labs.dev/gosx/crdt",
	"crdtsync":      "m31labs.dev/gosx/crdt/sync",
	"engine":        "m31labs.dev/gosx/engine",
	"fmt":           "fmt",
	"game":          "m31labs.dev/gosx/game",
	"gosx":          "m31labs.dev/gosx",
	"harness":       "m31labs.dev/gosx/scene/harness",
	"http":          "net/http",
	"hub":           "m31labs.dev/gosx/hub",
	"islandprogram": "m31labs.dev/gosx/island/program",
	"json":          "encoding/json",
	"log":           "log",
	"math":          "math",
	"os":            "os",
	"preview":       "m31labs.dev/gosx/scene/preview",
	"rand":          "math/rand",
	"redis":         "github.com/redis/go-redis/v9",
	"route":         "m31labs.dev/gosx/route",
	"scene":         "m31labs.dev/gosx/scene",
	"server":        "m31labs.dev/gosx/server",
	"session":       "m31labs.dev/gosx/session",
	"signal":        "m31labs.dev/gosx/signal",
	"slices":        "slices",
	"strings":       "strings",
	"textlayout":    "m31labs.dev/gosx/textlayout",
	"time":          "time",
	"testing":       "testing",
	"goredis":       "github.com/redis/go-redis/v9",
}

// goSamplePreamble records the Go context omitted from each displayed
// fragment: imports and values supplied by the enclosing route, action, test,
// or scene. Package declarations in the tutorial and standalone program
// samples are compiled as complete app sources instead.
func goSamplePreamble(path string) (string, map[string]string) {
	imports := map[string]string{}
	var declarations string
	switch {
	case path == "auth/sessionSample.go.sample":
		imports["session"] = goSampleImportPaths["session"]
		imports["auth"] = goSampleImportPaths["auth"]
		imports["server"] = goSampleImportPaths["server"]
		declarations = "var app = server.New()\n"
		return declarations, imports
	case strings.HasPrefix(path, "auth/"):
		imports["auth"] = goSampleImportPaths["auth"]
		declarations = "var authn *auth.Manager\n"
		if strings.HasSuffix(path, "guardSample.go.sample") {
			imports["http"] = goSampleImportPaths["http"]
			declarations += "var adminHandler http.Handler\n"
		}
		if strings.HasSuffix(path, "magicSample.go.sample") || strings.HasSuffix(path, "passkeySample.go.sample") {
			imports["authredis"] = goSampleImportPaths["authredis"]
			imports["context"] = goSampleImportPaths["context"]
			imports["redis"] = goSampleImportPaths["redis"]
			declarations += `var redisClient *redis.Client
func lookupUser(context.Context, string) (auth.User, error) { return auth.User{}, nil }
`
		}
		if strings.HasSuffix(path, "magicSample.go.sample") {
			declarations += "var mailer auth.MagicLinkSender\n"
		}
		if strings.HasSuffix(path, "oauthSample.go.sample") {
			imports["server"] = goSampleImportPaths["server"]
			declarations += "var clientID, clientSecret string\n"
		}
		if !strings.HasSuffix(path, "guardSample.go.sample") {
			imports["server"] = goSampleImportPaths["server"]
			declarations += "var app = server.New()\n"
		}
	case strings.HasPrefix(path, "debugging-scene3d/"):
		imports["scene"] = goSampleImportPaths["scene"]
		imports["testing"] = goSampleImportPaths["testing"]
		declarations = "var props scene.Props\nvar t *testing.T\n"
	case strings.HasPrefix(path, "deployment/"):
		imports["goredis"] = goSampleImportPaths["goredis"]
		declarations = "var redisClient goredis.UniversalClient\n"
	case strings.HasPrefix(path, "engines/"):
		imports["route"] = goSampleImportPaths["route"]
		declarations = `type ChartProps struct { Series []float64 }
var ctx *route.RouteContext
var values []float64
`
	case strings.HasPrefix(path, "forms/"):
		imports["action"] = goSampleImportPaths["action"]
		declarations = "var ctx *action.Context\n"
		if strings.HasSuffix(path, "code-006.go.sample") {
			delete(imports, "action")
			imports["server"] = goSampleImportPaths["server"]
			imports["session"] = goSampleImportPaths["session"]
			declarations = "var app = server.New()\nvar sessions *session.Manager\n"
		}
	case strings.HasPrefix(path, "hubs/"):
		imports["crdt"] = goSampleImportPaths["crdt"]
		imports["hub"] = goSampleImportPaths["hub"]
		imports["http"] = goSampleImportPaths["http"]
		declarations = `var doc = crdt.NewDoc()
var left, right = crdt.NewDoc(), crdt.NewDoc()
var room = hub.New("docs")
var mux = http.NewServeMux()
`
	case strings.HasPrefix(path, "images/"):
		// Image samples only need the GoSX server image helpers they call.
	case strings.HasPrefix(path, "islands/"):
		imports["route"] = goSampleImportPaths["route"]
		imports["islandprogram"] = goSampleImportPaths["islandprogram"]
		declarations = `var ctx *route.RouteContext
var counterProgram *islandprogram.Program
var counterHash string
`
	case strings.HasPrefix(path, "motion/"):
		imports["route"] = goSampleImportPaths["route"]
		imports["gosx"] = goSampleImportPaths["gosx"]
		declarations = "var ctx *route.RouteContext\nvar content gosx.Node\n"
	case strings.HasPrefix(path, "runtime/"):
		imports["route"] = goSampleImportPaths["route"]
		imports["gosx"] = goSampleImportPaths["gosx"]
		imports["time"] = goSampleImportPaths["time"]
		declarations = `var ctx *route.RouteContext
var appName string
var body gosx.Node
var launchAt time.Time
var router = route.NewRouter()
var currentValue = func() string { return "0" }
`
	case strings.HasPrefix(path, "scene3d/"):
		imports["scene"] = goSampleImportPaths["scene"]
		imports["game"] = goSampleImportPaths["game"]
		declarations = `var props scene.Props
var mesh scene.InstancedMesh
var accel *scene.SceneAccelerator
var ray scene.Ray
var previous, next scene.Props
var starPositions []scene.Vector3
var city []scene.Node
var joints []scene.Vector3
var smallScale = scene.Vec3(0.6, 0.6, 0.6)
var assets *game.Assets
var vertexWGSL, fragmentWGSL, vertexGLSL, fragmentGLSL string
func ProductScene(*game.Assets) scene.Props { return scene.Props{} }
func MyScene() scene.Props { return scene.Props{} }
`
	case strings.HasPrefix(path, "signals/"):
		imports["signal"] = goSampleImportPaths["signal"]
		declarations = `var first = signal.New("Ada")
var last = signal.New("Lovelace")
var total = signal.New(0)
`
	case strings.HasPrefix(path, "streaming/"):
		imports["context"] = goSampleImportPaths["context"]
		imports["route"] = goSampleImportPaths["route"]
		imports["server"] = goSampleImportPaths["server"]
		imports["gosx"] = goSampleImportPaths["gosx"]
		declarations = `var ctx *route.RouteContext
var fallback gosx.Node
var resolve server.DeferredResolver
var loadActivity = func(context.Context) ([]string, error) { return nil, nil }
func ActivityList([]string) gosx.Node { return gosx.Text("activity") }
`
	case strings.HasPrefix(path, "text-layout/"):
		imports["route"] = goSampleImportPaths["route"]
		imports["textlayout"] = goSampleImportPaths["textlayout"]
		declarations = `var article struct { Summary string }
var ctx *route.RouteContext
var text string
var measurer textlayout.BatchMeasurer = textlayout.ApproximateMeasurer{}
`
	case strings.HasPrefix(path, "compiler/"):
		imports["ir"] = "m31labs.dev/gosx/ir"
		declarations = `type Import = ir.Import
type Node = ir.Node
type NodeID = ir.NodeID
type ComponentSyntax = ir.ComponentSyntax
`
	}
	return declarations, imports
}

var goSamplePackageAliases = map[string]string{
	"action":        "m31labs.dev/gosx/action",
	"auth":          "m31labs.dev/gosx/auth",
	"authredis":     "m31labs.dev/gosx/auth/redis",
	"context":       "context",
	"crdt":          "m31labs.dev/gosx/crdt",
	"crdtsync":      "m31labs.dev/gosx/crdt/sync",
	"engine":        "m31labs.dev/gosx/engine",
	"fmt":           "fmt",
	"game":          "m31labs.dev/gosx/game",
	"gosx":          "m31labs.dev/gosx",
	"goredis":       "github.com/redis/go-redis/v9",
	"harness":       "m31labs.dev/gosx/scene/harness",
	"http":          "net/http",
	"hub":           "m31labs.dev/gosx/hub",
	"islandprogram": "m31labs.dev/gosx/island/program",
	"island":        "m31labs.dev/gosx/island",
	"json":          "encoding/json",
	"log":           "log",
	"math":          "math",
	"os":            "os",
	"preview":       "m31labs.dev/gosx/scene/preview",
	"rand":          "math/rand",
	"redis":         "github.com/redis/go-redis/v9",
	"route":         "m31labs.dev/gosx/route",
	"scene":         "m31labs.dev/gosx/scene",
	"server":        "m31labs.dev/gosx/server",
	"signal":        "m31labs.dev/gosx/signal",
	"slices":        "slices",
	"strings":       "strings",
	"textlayout":    "m31labs.dev/gosx/textlayout",
	"time":          "time",
	"testing":       "testing",
}

func TestEveryGoSampleTypeChecks(t *testing.T) {
	_, testFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve sample test source")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(testFile), "../../.."))
	work := t.TempDir()
	mod := fmt.Sprintf("module docsamples\n\ngo 1.26\n\nrequire m31labs.dev/gosx v0.0.0\n\nreplace m31labs.dev/gosx => %s\n", filepath.ToSlash(repoRoot))
	if err := os.WriteFile(filepath.Join(work, "go.mod"), []byte(mod), 0o600); err != nil {
		t.Fatal(err)
	}
	checksums, err := os.ReadFile(filepath.Join(repoRoot, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "go.sum"), checksums, 0o600); err != nil {
		t.Fatal(err)
	}

	checked := make(map[string]string)
	fragmentCount := 0
	goSampleCount := 0
	wasmPath := ""
	for _, path := range Files() {
		if !strings.HasSuffix(path, ".go.sample") {
			continue
		}
		goSampleCount++
		source, err := Read(path)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case strings.HasPrefix(path, "tutorial/"):
			checked[path] = "compiled as part of the four tutorial app builds"
			continue
		case path == "engines/wasmModuleSample.go.sample":
			wasmPath = path
			if err := writeSampleCheckFile(work, "wasm/main.go", []byte(source)); err != nil {
				t.Fatal(err)
			}
			checked[path] = "built for GOOS=js GOARCH=wasm"
			continue
		case path == "routing/moduleSample.go.sample":
			if err := writeSampleCheckFile(work, "full/routing/module.go", []byte(source)); err != nil {
				t.Fatal(err)
			}
			checked[path] = "built as a complete package"
			continue
		}

		generated, err := typedGoSampleSource(path, source)
		if err != nil {
			t.Fatalf("prepare %s: %v", path, err)
		}
		name := fmt.Sprintf("sample%03d", fragmentCount)
		if err := writeSampleCheckFile(work, filepath.Join("samples", name, "sample.go"), generated); err != nil {
			t.Fatal(err)
		}
		checked[path] = "built with its declared fragment preamble"
		fragmentCount++
	}
	if len(checked) != goSampleCount {
		t.Fatalf("typed %d of %d Go samples; syntax-only samples are not accepted", len(checked), goSampleCount)
	}

	build := exec.Command("go", "build", "-mod=mod", "-p=2", "./samples/...", "./full/routing")
	build.Dir = work
	build.Env = goSampleCheckEnv(os.Environ())
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("type-check documentation Go fragments against module packages: %v\n%s", err, output)
	}
	if wasmPath == "" {
		t.Fatal("Go/WASM module sample is missing from the type-check set")
	}
	wasmBuild := exec.Command("go", "build", "-mod=mod", "-p=2", "-o", filepath.Join(work, "engine-sample.wasm"), "./wasm")
	wasmBuild.Dir = work
	wasmBuild.Env = goSampleCheckEnv(os.Environ(), "GOOS=js", "GOARCH=wasm")
	if output, err := wasmBuild.CombinedOutput(); err != nil {
		t.Fatalf("build Go/WASM documentation sample: %v\n%s", err, output)
	}

	negative := `package samplecheck
import "m31labs.dev/gosx/scene"
var _ = scene.Group{Scal: scene.Vec3(2, 2, 2)}
`
	if err := writeSampleCheckFile(work, "negative/negative.go", []byte(negative)); err != nil {
		t.Fatal(err)
	}
	negativeBuild := exec.Command("go", "build", "./negative")
	negativeBuild.Dir = work
	negativeBuild.Env = goSampleCheckEnv(os.Environ())
	output, err := negativeBuild.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "unknown field Scal") {
		t.Fatalf("expected misspelled Group.Scal field to fail type checking with an unknown-field diagnostic; err=%v output=%s", err, output)
	}
	t.Logf("type-checked %d Go samples: %d documentation fragments, %d complete or tutorial program files; syntax-only: 0", len(checked), fragmentCount, len(checked)-fragmentCount)
}

func typedGoSampleSource(path, source string) ([]byte, error) {
	declarations, imports := goSamplePreamble(path)
	sourceImports := goSampleImportSpec.FindAllStringSubmatch(source, -1)
	for _, match := range sourceImports {
		alias := match[1]
		importPath := match[2]
		if alias == "" {
			alias = filepath.Base(importPath)
		}
		imports[alias] = importPath
	}
	body := goSampleImportSpec.ReplaceAllString(source, "")
	body = goPackageDeclaration.ReplaceAllString(body, "")
	aliasSource := regexp.MustCompile(`(?m)^\s*//.*$`).ReplaceAllString(body, "")
	for alias, importPath := range goSamplePackageAliases {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(alias) + `\s*\.`).MatchString(aliasSource + declarations) {
			if _, exists := imports[alias]; !exists {
				imports[alias] = importPath
			}
		}
	}
	if strings.HasPrefix(path, "deployment/") && strings.Contains(body, "redisClient") {
		imports["redis"] = "m31labs.dev/gosx/server/redis"
	}
	aliases := make([]string, 0, len(imports))
	for alias := range imports {
		aliases = append(aliases, alias)
	}
	sort.Strings(aliases)
	var importBlock strings.Builder
	importBlock.WriteString("package samplecheck\n\nimport (\n")
	for _, alias := range aliases {
		fmt.Fprintf(&importBlock, "\t%s %q\n", alias, imports[alias])
	}
	importBlock.WriteString(")\n\n")

	wrapped := strings.TrimSpace(body)
	if strings.TrimSpace(declarations) != "" {
		importBlock.WriteString(declarations)
		importBlock.WriteString("\n")
	}
	if _, err := parser.ParseFile(token.NewFileSet(), path, "package samplecheck\n"+wrapped, parser.AllErrors); err != nil {
		wrapped = strings.TrimSpace(assignSceneLiteralExpressions(wrapped))
		if isStructFieldFragment(wrapped) {
			wrapped = "func sample() any { _ = scene.Props{\n" + wrapped + "\n}; return nil }"
		} else {
			wrapped = "func sample() any {\n" + wrapped + "\n"
			for _, name := range topLevelSampleDeclarations(wrapped + "return nil\n}") {
				wrapped += "_ = " + name + "\n"
			}
			wrapped += "return nil\n}"
		}
	}
	importBlock.WriteString(wrapped)
	importBlock.WriteString("\n")
	return []byte(importBlock.String()), nil
}

func assignSceneLiteralExpressions(source string) string {
	lines := strings.Split(source, "\n")
	depth := 0
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if depth == 0 && strings.HasPrefix(trimmed, "scene.") && !regexp.MustCompile(`^scene\.\w+\s*:`).MatchString(trimmed) {
			indent := len(line) - len(strings.TrimLeft(line, " \t"))
			lines[i] = line[:indent] + "_ = " + strings.TrimLeft(line[indent:], " \t")
		}
		depth += goSampleDelimiterDelta(line)
		if depth < 0 {
			depth = 0
		}
	}
	return strings.Join(lines, "\n")
}

func topLevelSampleDeclarations(functionSource string) []string {
	file, err := parser.ParseFile(token.NewFileSet(), "sample.go", "package samplecheck\n"+functionSource, parser.AllErrors)
	if err != nil {
		return nil
	}
	var names []string
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "sample" || function.Body == nil {
			continue
		}
		for _, statement := range function.Body.List {
			assignment, ok := statement.(*ast.AssignStmt)
			if !ok || assignment.Tok != token.DEFINE {
				continue
			}
			for _, left := range assignment.Lhs {
				identifier, ok := left.(*ast.Ident)
				if ok && identifier.Name != "_" {
					names = append(names, identifier.Name)
				}
			}
		}
	}
	return names
}

func goSampleDelimiterDelta(line string) int {
	delta := 0
	for _, character := range line {
		switch character {
		case '{', '[', '(':
			delta++
		case '}', ']', ')':
			delta--
		}
	}
	return delta
}

func isStructFieldFragment(source string) bool {
	return regexp.MustCompile(`^(?:Camera|Controls):`).MatchString(strings.TrimSpace(source))
}

func writeSampleCheckFile(root, relative string, source []byte) error {
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, source, 0o600)
}

func goSampleCheckEnv(environment []string, overrides ...string) []string {
	result := make([]string, 0, len(environment)+len(overrides)+1)
	for _, entry := range environment {
		if strings.HasPrefix(entry, "GOWORK=") || strings.HasPrefix(entry, "GOOS=") || strings.HasPrefix(entry, "GOARCH=") {
			continue
		}
		result = append(result, entry)
	}
	result = append(result, "GOWORK=off")
	return append(result, overrides...)
}

func TestEveryGoSampleParses(t *testing.T) {
	for _, path := range Files() {
		if !strings.HasSuffix(path, ".go.sample") {
			continue
		}
		t.Run(path, func(t *testing.T) {
			source, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			wrapped := goSampleHarness(source)
			if _, err := parser.ParseFile(token.NewFileSet(), path, wrapped, parser.AllErrors); err != nil {
				t.Fatalf("parse Go sample: %v", err)
			}
		})
	}
}

func TestEveryGoSXSampleCompiles(t *testing.T) {
	for _, path := range Files() {
		if !strings.HasSuffix(path, ".gosx.sample") && !strings.HasSuffix(path, ".gsx.sample") {
			continue
		}
		t.Run(path, func(t *testing.T) {
			source, err := Read(path)
			if err != nil {
				t.Fatal(err)
			}
			if !gosxPackageDeclaration.MatchString(source) {
				if strings.Contains(source, "component ") || strings.Contains(source, "type ") {
					source = "package sample\n" + source
				} else {
					source = "package sample\nfunc Example() Node {\nreturn <div>\n" + source + "\n</div>\n}\n"
				}
			}
			if _, err := gosx.Compile([]byte(source)); err != nil {
				t.Fatalf("compile GoSX sample: %v", err)
			}
		})
	}
}

func TestExecutableSamplesHaveValidSyntax(t *testing.T) {
	for _, path := range Files() {
		switch filepath.Ext(strings.TrimSuffix(path, ".sample")) {
		case ".bash":
			t.Run(path, func(t *testing.T) {
				source, err := Read(path)
				if err != nil {
					t.Fatal(err)
				}
				command := exec.Command("bash", "-n")
				command.Stdin = strings.NewReader(source)
				if output, err := command.CombinedOutput(); err != nil {
					t.Fatalf("parse shell sample: %v\n%s", err, output)
				}
			})
		case ".js":
			t.Run(path, func(t *testing.T) {
				source, err := Read(path)
				if err != nil {
					t.Fatal(err)
				}
				file := filepath.Join(t.TempDir(), "sample.mjs")
				if err := os.WriteFile(file, []byte(source), 0o600); err != nil {
					t.Fatal(err)
				}
				if output, err := exec.Command("node", "--check", file).CombinedOutput(); err != nil {
					t.Fatalf("parse JavaScript sample: %v\n%s", err, output)
				}
			})
		case ".json":
			t.Run(path, func(t *testing.T) {
				source, err := Read(path)
				if err != nil {
					t.Fatal(err)
				}
				if !json.Valid([]byte(source)) {
					t.Fatal("invalid JSON sample")
				}
			})
		}
	}
}

func goSampleHarness(source string) string {
	source = strings.TrimSpace(source)
	if goPackageDeclaration.MatchString(source) || strings.HasPrefix(source, "//go:build ") {
		return source
	}
	if regexp.MustCompile(`^[A-Z][A-Za-z0-9_]*\s*:`).MatchString(source) {
		return "package sample\nimport \"m31labs.dev/gosx/scene\"\nfunc Example() { _ = scene.Props{\n" + source + "\n} }\n"
	}
	prefix := "package sample\n"
	if _, err := parser.ParseFile(token.NewFileSet(), "sample.go", prefix+source, parser.AllErrors); err == nil {
		return prefix + source
	}
	if match := goImportLine.FindStringIndex(source); match != nil {
		imports := source[:match[1]]
		body := strings.TrimSpace(source[match[1]:])
		if body == "" {
			return prefix + imports
		}
		return prefix + imports + "func Example() {\n" + body + "\n}\n"
	}
	_, parseErr := parser.ParseFile(token.NewFileSet(), "sample.go", prefix+source, parser.AllErrors)
	if errors, ok := parseErr.(scanner.ErrorList); ok && len(errors) > 0 {
		offset := errors[0].Pos.Offset - len(prefix)
		if offset > 0 && offset < len(source) {
			declarations := strings.TrimSpace(source[:offset])
			body := strings.TrimSpace(source[offset:])
			return prefix + declarations + "\nfunc Example() {\n" + body + "\n}\n"
		}
	}
	return prefix + "func Example() {\n" + source + "\n}\n"
}
