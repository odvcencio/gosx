// math/rand intrinsics — Slice X.B. Float64/Intn/Seed are exposed
// through a process-wide *rand.Rand so reproducible streams are
// possible when a surface seeds explicitly.
//
// The default seed is 1 (matching math/rand's pre-1.20 behavior); a
// Seed call replaces the source. Deterministic-by-default is preferred
// over time-seeded so test fixtures stay stable; surfaces that want
// time-varying randomness call rand.Seed(time.Now().UnixNano()) — and
// time intrinsics aren't in the X.B subset so this is honest friction
// pointing toward future plan work.

package vm

import (
	"errors"
	"math/rand"
	"sync"

	"m31labs.dev/gosx/island/program"
)

// randMu guards the package-scoped *rand.Rand. The math/rand package's
// default source is concurrency-safe but uses a global mutex; we keep
// a local source so seeding from one surface doesn't perturb another
// outside its own evaluation, and we surface our own mutex so the
// behavior stays predictable in tests.
var (
	randMu sync.Mutex
	randSrc = rand.New(rand.NewSource(1))
)

func init() {
	RegisterIntrinsic("rand.Float64", func(args []Value) (Value, error) {
		if len(args) != 0 {
			return Value{}, errors.New("rand.Float64 takes no arguments")
		}
		randMu.Lock()
		defer randMu.Unlock()
		return FloatVal(randSrc.Float64()), nil
	})
	RegisterIntrinsic("rand.Intn", func(args []Value) (Value, error) {
		if len(args) != 1 {
			return Value{}, errors.New("rand.Intn expects 1 argument")
		}
		n := int(args[0].Num)
		if n <= 0 {
			return Value{}, errors.New("rand.Intn: n must be > 0")
		}
		randMu.Lock()
		defer randMu.Unlock()
		return IntVal(randSrc.Intn(n)), nil
	})
	RegisterIntrinsic("rand.Seed", func(args []Value) (Value, error) {
		if len(args) != 1 {
			return Value{}, errors.New("rand.Seed expects 1 argument")
		}
		seed := int64(args[0].Num)
		randMu.Lock()
		defer randMu.Unlock()
		randSrc = rand.New(rand.NewSource(seed))
		return ZeroValue(program.TypeAny), nil // Seed returns nothing
	})
}
