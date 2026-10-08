package budgetci

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"m31labs.dev/gosx/buildmanifest"
)

// Compiler owns only its scratch binary and log, not the source checkout.
type Compiler struct {
	Path, SourceRoot, SourceSHA string `json:"-"`
	owned                       string
}

func cleanSource(ctx context.Context, root, sha string) error {
	if ctx == nil || ctx.Err() != nil || !commitPattern.MatchString(sha) {
		return failure("invalid-input", "/production/source")
	}
	actual, err := commandOutput(ctx, "git", "-C", root, "rev-parse", "HEAD")
	if err != nil || strings.TrimSpace(string(actual)) != sha || exec.CommandContext(ctx, "git", "-C", root, "diff", "--quiet", "HEAD", "--").Run() != nil {
		return failure("wrong-fixture", "/production/source")
	}
	return nil
}

// BuildCompiler compiles the CLI from the exact source revision being measured.
// It retains the supplied cache and temporary-directory environment.
func BuildCompiler(ctx context.Context, sourceRoot, sha, scratch string) (*Compiler, error) {
	if !productionSupported() {
		return nil, failure("environment", "/production/platform")
	}
	if err := cleanSource(ctx, sourceRoot, sha); err != nil {
		return nil, err
	}
	owned, err := os.MkdirTemp(scratch, "budget-compiler-")
	if err != nil {
		return nil, failure("environment", "/production/scratch")
	}
	compiler := &Compiler{SourceRoot: sourceRoot, SourceSHA: sha, owned: owned, Path: filepath.Join(owned, "gosx")}
	if runtime.GOOS == "windows" {
		compiler.Path += ".exe"
	}
	if err := productionCommand(ctx, sourceRoot, filepath.Join(owned, "compiler.log"), "go", "build", "-o", compiler.Path, "./cmd/gosx"); err != nil {
		compiler.Close()
		return nil, err
	}
	info, err := os.Stat(compiler.Path)
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		compiler.Close()
		return nil, failure("wrong-fixture", "/production/compiler")
	}
	return compiler, nil
}

func (c *Compiler) Close() error {
	if c == nil || c.owned == "" {
		return nil
	}
	if err := os.RemoveAll(c.owned); err != nil {
		return failure("cleanup", "/production/compiler")
	}
	return nil
}

// Application contains a completed production build. The caller owns the
// disposable source checkout; Close removes only newly created app outputs.
type Application struct {
	App, Root, DistDir, SourceSHA string                  `json:"-"`
	Manifest                      *buildmanifest.Manifest `json:"-"`
	owned                         string
}

func (c *Compiler) BuildApplication(ctx context.Context, app, scratch string) (*Application, error) {
	if c == nil || c.Path == "" {
		return nil, failure("invalid-input", "/production/compiler")
	}
	if err := cleanSource(ctx, c.SourceRoot, c.SourceSHA); err != nil {
		return nil, err
	}
	a := &Application{App: app, SourceSHA: c.SourceSHA}
	var err error
	switch app {
	case "docs":
		a.Root = filepath.Join(c.SourceRoot, "examples/gosx-docs")
		a.owned = filepath.Join(a.Root, "dist")
		if _, err := os.Lstat(a.owned); !os.IsNotExist(err) {
			return nil, failure("wrong-fixture", "/production/dist")
		}
	case "scaffold":
		a.owned, err = os.MkdirTemp(scratch, "budget-scaffold-")
		if err != nil {
			return nil, failure("environment", "/production/scratch")
		}
		a.Root = filepath.Join(a.owned, "app")
		if err := productionCommand(ctx, c.SourceRoot, filepath.Join(a.owned, "init.log"), c.Path, "init", a.Root, "--module", "example.com/budget-fixture"); err != nil {
			a.Close()
			return nil, err
		}
		counter := filepath.Join(a.Root, "app/counter")
		if err := os.MkdirAll(counter, 0700); err != nil {
			a.Close()
			return nil, failure("environment", "/production/scaffold")
		}
		for _, name := range []string{"page.gsx", "page.server.go"} {
			data, err := os.ReadFile(filepath.Join(c.SourceRoot, "perf/wire/testdata/counter", name))
			if err != nil || len(data) > nativeLimit || os.WriteFile(filepath.Join(counter, name), data, 0600) != nil {
				a.Close()
				return nil, failure("wrong-fixture", "/production/scaffold")
			}
		}
		if err := os.WriteFile(filepath.Join(a.Root, "app/route.config.json"), []byte("{\"prerender\":true}\n"), 0600); err != nil {
			a.Close()
			return nil, failure("environment", "/production/scaffold")
		}
	default:
		return nil, failure("wrong-fixture", "/production/app")
	}
	a.DistDir = filepath.Join(a.Root, "dist")
	log := filepath.Join(c.owned, app+"-build.log")
	if err := productionCommand(ctx, c.SourceRoot, log, c.Path, "build", "--prod", "--perf-app", app, a.Root); err != nil {
		a.Close()
		return nil, err
	}
	// The builder manifest is a private input. Bound it before decoding and keep
	// filesystem details out of diagnostics from the compatibility loader.
	root, err := os.OpenRoot(a.DistDir)
	if err != nil {
		a.Close()
		return nil, failure("wrong-fixture", "/production/manifest")
	}
	f, err := root.Open("build.json")
	root.Close()
	if err != nil {
		a.Close()
		return nil, failure("wrong-fixture", "/production/manifest")
	}
	info, statErr := f.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		f.Close()
		a.Close()
		return nil, failure("wrong-fixture", "/production/manifest")
	}
	data, readErr := io.ReadAll(io.LimitReader(f, nativeLimit+1))
	closeErr := f.Close()
	var manifest buildmanifest.Manifest
	if readErr != nil || closeErr != nil || len(data) > nativeLimit || json.Unmarshal(data, &manifest) != nil || manifest.PerfAssetUses == nil || manifest.ValidateIslandAssets() != nil || manifest.ValidatePerfAssetUses() != nil {
		a.Close()
		return nil, failure("wrong-fixture", "/production/manifest")
	}
	a.Manifest = &manifest
	return a, nil
}

func (a *Application) Close() error {
	if a == nil || a.owned == "" {
		return nil
	}
	if err := os.RemoveAll(a.owned); err != nil {
		return failure("cleanup", "/production/app")
	}
	return nil
}

func productionEnvironment(values map[string]string) []string {
	values["GOWORK"] = "off"
	env := []string{}
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if _, replaced := values[key]; !replaced {
			env = append(env, item)
		}
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env
}

func productionCommand(ctx context.Context, dir, log, name string, args ...string) error {
	f, err := os.OpenFile(log, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return failure("environment", "/production/log")
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir, cmd.Env, cmd.Stdout, cmd.Stderr = dir, productionEnvironment(map[string]string{}), f, f
	runErr, closeErr := runProduction(cmd), f.Close()
	if runErr != nil || closeErr != nil {
		return failure("environment", "/production/build")
	}
	return nil
}

// Server owns one process, one log and a local HTTP/1 client. No browser or
// media player is launched. Close waits for that exact process and closes its
// idle connections before returning.
type Server struct {
	BaseURL   string       `json:"-"`
	Client    *http.Client `json:"-"`
	cmd       *exec.Cmd
	done      chan error
	log       *os.File
	transport *http.Transport
	once      sync.Once
	closeErr  error
}

func (a *Application) Serve(ctx context.Context, log string) (*Server, error) {
	if ctx == nil || ctx.Err() != nil || a == nil || a.Manifest == nil {
		return nil, failure("invalid-input", "/production/server")
	}
	if !productionSupported() {
		return nil, failure("environment", "/production/platform")
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, failure("environment", "/production/listener")
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return nil, failure("cleanup", "/production/listener")
	}
	f, err := os.OpenFile(log, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, failure("environment", "/production/log")
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		f.Close()
		return nil, failure("environment", "/production/secret")
	}
	transport := &http.Transport{ForceAttemptHTTP2: false}
	s := &Server{BaseURL: "http://" + address, done: make(chan error, 1), log: f, transport: transport}
	s.Client = &http.Client{Transport: transport, Timeout: 30 * time.Second}
	executable := filepath.Join(a.DistDir, "server/app")
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	s.cmd = exec.CommandContext(ctx, executable)
	prepareProduction(s.cmd)
	s.cmd.Dir = a.DistDir
	s.cmd.Env = productionEnvironment(map[string]string{"PORT": address, "GOSX_LISTEN_ADDR": address, "PUBLIC_URL": s.BaseURL, "GOSX_APP_ROOT": a.DistDir, "SESSION_SECRET": hex.EncodeToString(secret[:])})
	s.cmd.Stdout, s.cmd.Stderr = f, f
	if err := s.cmd.Start(); err != nil {
		s.Close()
		return nil, failure("environment", "/production/server")
	}
	go func() { s.done <- s.cmd.Wait(); close(s.done) }()
	ready := "/readyz"
	if a.App == "scaffold" {
		ready = "/api/health"
	}
	deadline, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			s.Close()
			return nil, failure("environment", "/production/readiness")
		case <-deadline.Done():
			s.Close()
			return nil, failure("timeout", "/production/readiness")
		case <-ticker.C:
			req, _ := http.NewRequestWithContext(deadline, http.MethodGet, s.BaseURL+ready, nil)
			response, err := s.Client.Do(req)
			if err != nil {
				continue
			}
			_, readErr := io.Copy(io.Discard, io.LimitReader(response.Body, 8192))
			closeErr := response.Body.Close()
			if response.StatusCode == http.StatusOK && readErr == nil && closeErr == nil {
				return s, nil
			}
		}
	}
}

func (s *Server) Close() error {
	if s == nil {
		return nil
	}
	s.once.Do(func() {
		if s.transport != nil {
			s.transport.CloseIdleConnections()
		}
		if s.cmd != nil && s.cmd.Process != nil {
			select {
			case <-s.done:
			default:
				_ = s.cmd.Process.Signal(os.Interrupt)
				timer := time.NewTimer(5 * time.Second)
				select {
				case <-s.done:
				case <-timer.C:
					if err := s.cmd.Process.Kill(); err != nil && err != os.ErrProcessDone {
						s.closeErr = failure("cleanup", "/production/server")
					}
					<-s.done
				}
				timer.Stop()
			}
			if err := stopProduction(s.cmd.Process.Pid); err != nil {
				s.closeErr = failure("cleanup", "/production/server")
			}
		}
		if s.log != nil && s.log.Close() != nil {
			s.closeErr = failure("cleanup", "/production/log")
		}
	})
	return s.closeErr
}
