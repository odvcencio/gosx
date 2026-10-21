# Deterministic simulation

A simulation is deterministic when the same inputs give the same bits on every
platform. Rollback netcode, server-authoritative games and replays all need
this. GoSX gives you three tools for it: the `det` math package, the
`det/parity` hash recorder, and a linter. The `physics` package uses all three.

## The three platforms

| Platform | Fuses `x*y + z`? | `math.Exp`, `math.Log` |
|---|---|---|
| js/wasm (browser) | Never. WebAssembly has no scalar fused multiply-add. | Portable Go code |
| amd64 (server) | No at the default `GOAMD64=v1`. Yes at `GOAMD64=v3`. | Assembly (`Exp` uses FMA when the CPU has it; `Log` has its own assembly) |
| arm64 (Apple Silicon) | Always, where it can. | Portable Go code, `Exp` has assembly |

Two things differ between these platforms:

1. **Fused multiply-add.** The Go spec lets the compiler compute `x*y + z`
   with one rounding instead of two. The result differs in the last bit.
2. **Platform math.** The assembly in `math` can return different bits than the
   portable code. One case is `math.Log(1e-310)`: on amd64 it returns about
   -709.09, and the correct value (which wasm returns) is about -713.80.

Go map iteration order is random on every platform. That breaks determinism too.

## The rules

1. Round every float product explicitly: write `float64(x*y) + z`, not
   `x*y + z`. An assignment, a function argument or a return value does not
   round. Inlining can put the product next to an addition elsewhere.
2. Call `det.Sin`, `det.Exp` and the other `det` functions, not `math`.
   `det.Sqrt`, `det.Abs`, `det.Floor` and similar are exact and are re-exported
   for convenience.
3. Use `det.MulAdd(x, y, z)` for a non-fused `x*y + z`. Use `math.FMA` if you
   want one rounding. `math.FMA` is correctly rounded on every platform, but it
   is slow where no hardware instruction exists.
4. Do not range over a map. Range over a slice of sorted keys.
5. Do not read the clock (`time.Now`, `time.Since`). Pass the simulation time in.
6. Do not use the global `math/rand` source. Build one with
   `rand.New(rand.NewSource(seed))`.
7. Do not start goroutines or use `select` inside the simulation step.

## The `det` package

```go
import "m31labs.dev/gosx/det"

angle := det.Atan2(dy, dx)
speed := det.Hypot(vx, vy)
decay := det.Exp(float64(-k * dt))
```

`Exp`, `Exp2`, `Log`, `Log2`, `Log10`, `Pow`, `Sin`, `Cos`, `Tan`, `Asin`,
`Acos`, `Atan`, `Atan2`, `Hypot`, `Sinh`, `Cosh` and `Tanh` are ported from the
portable Go algorithms with an explicit conversion on every product. They have
no assembly. Accuracy matches `math`: the tests compare against `math` within
1 ulp where `math` runs the same algorithm, and against a high-precision
reference for the functions built on `Exp`.

`det/testdata/golden.json` records the SHA-256 of the output bits over fixed
input sequences. Any platform can check it with `go test ./det`.

## The linter

Mark a package with a directive before its package clause:

```go
//gosx:deterministic

// Package sim ...
package sim
```

Then run:

```sh
gosx detlint ./sim/...
# or, as a go vet tool:
go build -o gosx-detlint ./cmd/gosx-detlint
go vet -vettool=$PWD/gosx-detlint ./sim/...
```

`-fix` wraps unrounded products in a conversion. The analyzer reports:

| Rule | What it flags |
|---|---|
| `fused-multiply-add` | A float or complex product used anywhere except as an operand of `*` or `/`, in a comparison, or under a conversion. Also `x *= y`. Products with a constant power of two are accepted. |
| `math-call` | `math` functions with a `det` replacement, or with none (`Cbrt`, `Expm1`, ...), and `math/cmplx` |
| `map-range` | `range` over a map, and `maps.Keys/Values/All` not wrapped in `slices.Sorted` |
| `wall-clock` | `time.Now`, `time.Since`, `time.Until`, timers and `time.Sleep` |
| `global-rand` | Package-level `math/rand` functions and `crypto/rand` |
| `goroutine` | `go` statements |
| `select` | `select` statements |

Silence one rule on one line, with a reason:

```go
//gosx:allow map-range summing integers is order independent
for _, v := range counts {
```

Test files are not checked. The analyzer needs type information to tell float
arithmetic from integer arithmetic, so it uses `go/types` through
`golang.org/x/tools/go/analysis`. In this repository, `make lint-deterministic`
runs it on `det` and `physics`. Add your package to `DET_LINT_PKGS`.

## Proving it: parity tests

`det/parity` records one SHA-256 hash of your state after each step:

```go
rec := parity.NewRecorder("my-sim", func(dst []byte) []byte {
	for _, b := range world.Bodies { // a slice, in fixed order
		dst = parity.AppendFloat64(dst, b.X)
	}
	return dst
})
for i := 0; i < 600; i++ {
	world.Step()
	rec.Step()
}
if d := parity.Compare(golden, rec.Trace()); d != nil {
	t.Fatal(d) // names the first step that differs
}
```

Make targets in this repository:

| Target | What it runs |
|---|---|
| `make test-det-parity` | `det` and `det/parity` natively, at `GOAMD64=v3` (when the CPU supports it), as js/wasm under Node, and as linux/arm64 under `qemu-aarch64` (skipped with a message if it is not installed) |
| `make test-physics-parity` | The physics golden corpus natively, at `GOAMD64=v3`, and as js/wasm |
| `make test-det-parity-arm64-remote PARITY_ARM64_HOST=<ssh alias>` | Cross-compiles for darwin/arm64, runs the `det`, `det/parity` and `physics` parity tests on an Apple Silicon Mac over ssh, and deletes its files from the host. Skips when the variable is unset. |
| `make build-det-parity-darwin-arm64` | Writes darwin/arm64 test binaries to `build/det-parity/` so you can copy them to a Mac yourself. The golden data is embedded. |
| `make lint-deterministic` | The linter |
