// strings.* intrinsics — Slice X.B. Covers Split, Join, TrimSpace,
// Replace, ToLower, ToUpper, Contains, HasPrefix, HasSuffix.
//
// Some of these duplicate string-method opcodes (OpToLower, OpSplit,
// etc.) that already exist. The duplication is intentional: the X.C
// lowerer emits qualified intrinsic calls for explicit
// strings.ToLower(s) call expressions and the string-method opcodes
// for Go's method-syntax sugar s.ToLower() — but Go doesn't have
// method syntax on strings, so the lowerer always reaches the
// intrinsics path for strings.* selector calls. Keeping the opcodes
// avoids breaking pre-existing programs that already encoded
// strings.ToLower as OpToLower.

package vm

import (
	"errors"
	"strings"
)

func init() {
	RegisterIntrinsic("strings.Split", intrinsicStringsSplit)
	RegisterIntrinsic("strings.Join", intrinsicStringsJoin)
	RegisterIntrinsic("strings.TrimSpace", stringsUnary(strings.TrimSpace))
	RegisterIntrinsic("strings.ToLower", stringsUnary(strings.ToLower))
	RegisterIntrinsic("strings.ToUpper", stringsUnary(strings.ToUpper))
	RegisterIntrinsic("strings.Replace", intrinsicStringsReplace)
	RegisterIntrinsic("strings.Contains", stringsPredicate(strings.Contains))
	RegisterIntrinsic("strings.HasPrefix", stringsPredicate(strings.HasPrefix))
	RegisterIntrinsic("strings.HasSuffix", stringsPredicate(strings.HasSuffix))
}

// stringsUnary lifts a string→string function. Argument count is
// guarded once at the call site.
func stringsUnary(fn func(string) string) Intrinsic {
	return func(args []Value) (Value, error) {
		if len(args) != 1 {
			return Value{}, errors.New("strings: function expects 1 argument")
		}
		return StringVal(fn(args[0].Str)), nil
	}
}

// stringsPredicate lifts a (string, string)→bool predicate.
func stringsPredicate(fn func(string, string) bool) Intrinsic {
	return func(args []Value) (Value, error) {
		if len(args) != 2 {
			return Value{}, errors.New("strings: predicate expects 2 arguments")
		}
		return BoolVal(fn(args[0].Str, args[1].Str)), nil
	}
}

func intrinsicStringsSplit(args []Value) (Value, error) {
	if len(args) != 2 {
		return Value{}, errors.New("strings.Split expects 2 arguments")
	}
	parts := strings.Split(args[0].Str, args[1].Str)
	items := make([]Value, len(parts))
	for i, p := range parts {
		items[i] = StringVal(p)
	}
	return ArrayVal(items), nil
}

func intrinsicStringsJoin(args []Value) (Value, error) {
	if len(args) != 2 {
		return Value{}, errors.New("strings.Join expects 2 arguments")
	}
	src := args[0]
	if src.Items == nil {
		return StringVal(""), nil
	}
	parts := make([]string, len(src.Items))
	for i, v := range src.Items {
		parts[i] = v.String()
	}
	return StringVal(strings.Join(parts, args[1].Str)), nil
}

func intrinsicStringsReplace(args []Value) (Value, error) {
	// Lowered from strings.ReplaceAll OR strings.Replace; for Replace
	// the Go signature takes (s, old, new, n). We accept both 3 and 4
	// args; with 3 args we treat it as ReplaceAll (n = -1).
	switch len(args) {
	case 3:
		return StringVal(strings.ReplaceAll(args[0].Str, args[1].Str, args[2].Str)), nil
	case 4:
		return StringVal(strings.Replace(args[0].Str, args[1].Str, args[2].Str, int(args[3].Num))), nil
	default:
		return Value{}, errors.New("strings.Replace expects 3 or 4 arguments")
	}
}
