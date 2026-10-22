//gosx:deterministic

// Package det provides floating-point math that gives the same bits on every
// platform GoSX targets: js/wasm in the browser, amd64 on a server, and arm64
// on Apple Silicon.
//
// # Why the standard library is not enough
//
// Two things make a Go float program differ between platforms:
//
//  1. Fused multiply-add. The Go spec lets the compiler evaluate x*y + z with
//     one rounding instead of two. arm64 always fuses where it can. amd64 at
//     the default GOAMD64=v1 does not fuse, and GOAMD64=v3 does. js/wasm has no
//     scalar fused multiply-add instruction, so it never fuses. An explicit
//     conversion, as in float64(x*y) + z, forces the product to round and
//     prevents fusion.
//  2. Platform-specific math. On amd64, [math.Exp] uses assembly that uses FMA
//     when the CPU has it, and [math.Log] has assembly too. js/wasm uses the
//     portable Go code. The results can differ in the last bit.
//
// # What this package does
//
// Every function here is portable Go with no assembly. The portable
// algorithms from the Go standard library are ported with an explicit float64
// conversion on every product that feeds an addition or subtraction, so no
// compiler can fuse them. Functions that are already exact on every platform
// (Sqrt, Abs, Floor, Ceil, Trunc, Copysign, and the like) are re-exported so a
// deterministic package needs to import only det.
//
// The results match [math] to within 1 unit in the last place on tested
// inputs. They are not always bit-equal to [math], because the standard
// library's own result depends on the platform. det results are the same on all
// platforms. The golden tests in this package record exact bit patterns so any
// platform can check them.
//
// # Writing deterministic code
//
// Use [MulAdd] where you want x*y + z without fusion. Use [math.FMA] where you
// want the fused result: it is correctly rounded and identical on every
// platform, but it is slow on platforms without a hardware instruction. Never
// write a bare x*y + z in code that must be deterministic.
//
// The gosx deterministic linter (see the det/lint package and the
// "gosx detlint" command) checks packages marked with a //gosx:deterministic
// package comment for these hazards.
//
// # License
//
// The algorithms are ported from the Go standard library and, through it,
// from FreeBSD msun and the Cephes library. Go's BSD license is in
// LICENSE-GO in this directory.
package det
