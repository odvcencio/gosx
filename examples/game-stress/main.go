// Command game-stress serves a GoSX game stress scene: 22 skinned figures
// driven by a synthetic 30 Hz simulation with prediction-style replays, a
// 40,000-figure crowd animated on the GPU, and optional live music. Open the
// page and read window.__stress, or run the browser benchmark in
// e2e/game_stress_e2e_test.go. See README.md.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/examples/game-stress/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8095", "listen address")
	root := flag.String("root", ".", "GoSX checkout (for client/js)")
	engineWASM := flag.String("engine", "", "prebuilt engine .wasm (default: build ./examples/game-stress/engine)")
	cicada := flag.String("cicada", "", "directory with a Cicada fixture.json and kernel.wasm (see README.md)")
	flag.Parse()

	absRoot, err := filepath.Abs(*root)
	if err != nil {
		log.Fatal(err)
	}
	if *engineWASM == "" {
		dir, err := os.MkdirTemp("", "game-stress")
		if err != nil {
			log.Fatal(err)
		}
		defer os.RemoveAll(dir)
		*engineWASM = filepath.Join(dir, "stress.wasm")
		cmd := exec.Command("go", "build", "-o", *engineWASM, "./examples/game-stress/engine")
		cmd.Dir = absRoot
		cmd.Env = append(os.Environ(), "GOOS=js", "GOARCH=wasm")
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Fatalf("build the engine: %v\n%s", err, out)
		}
	}
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		log.Fatal(err)
	}
	h, err := web.Handler(web.Options{
		Root: absRoot, EngineWASM: *engineWASM, CicadaDir: *cicada,
		WASMExec: filepath.Join(strings.TrimSpace(string(goroot)), "lib", "wasm", "wasm_exec.js"),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("game-stress on http://%s/ (try ?thread=worker, ?backend=webgl, ?crowd=10000, ?audio=cicada)\n", *addr)
	log.Fatal(http.ListenAndServe(*addr, h))
}
