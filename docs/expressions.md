# GoSX expression language

The `.gsx` expression language is the subset of Go-flavoured syntax
that may appear inside `{ ... }` interpolations and attribute values.
It compiles to the unified VM's opcode set (`island/program/program.go`)
and is evaluated by `client/vm/vm.go`.

This document specifies the **supported subset as of Phase 4** and the
**explicit non-goals**. Anything not on the supported list is rejected
at parse time. Authors who hit the wall should
[file a follow-up](#when-you-hit-the-wall) rather than work around it.

---

## Supported array methods

Each form lowers to an existing opcode — no new opcodes were added in
Phase 4 (per ADR 0002: stay on v1).

| Form                                | Opcode      | Notes                                          |
|-------------------------------------|-------------|------------------------------------------------|
| `xs.filter(fn)`                     | `OpFilter`  | `fn` is a single-param closure or expression   |
| `xs.map(fn)`                        | `OpMap`     | Body may be any single expression              |
| `xs.find(fn)`                       | `OpFind`    | Returns the zero value when nothing matches    |
| `xs.slice(start, end)`              | `OpSlice`   | Both bounds required (positive ints)           |
| `xs.append(x)`                      | `OpAppend`  | Returns a new array; no mutation               |
| `xs.len()` or `xs.length`           | `OpLen`     | Equivalent forms                               |
| `xs.contains(x)`                    | `OpContains`| Returns bool                                   |

### Predicate / transformer arguments

The `xs.map(fn)`, `xs.filter(fn)`, and `xs.find(fn)` methods accept
either of two equivalent forms:

```gsx
// Bare-expression form (legacy; still supported).
items.map(_item * 2)

// Single-param closure form (Phase 4 addition).
items.map(func(i){ return i * 2 })
items.map(func(i){ i * 2 })       // 'return' is optional for expression bodies
```

Both lower to the same opcode shape. The closure form binds its
parameter name to the magic `_item` prop for the duration of the body,
so authors may pick whichever reads better.

The closure body also has access to `_index` (the current iteration
position):

```gsx
items.map(func(i){ _index + 1 })  // 1, 2, 3, ...
```

---

## Supported string methods

| Form                            | Opcode         |
|---------------------------------|----------------|
| `s.toLower()`                   | `OpToLower`    |
| `s.toUpper()`                   | `OpToUpper`    |
| `s.trim()`                      | `OpTrim`       |
| `s.split(sep)`                  | `OpSplit`      |
| `s.contains(sub)`               | `OpContains`   |
| `s.startsWith(prefix)`          | `OpStartsWith` |
| `s.endsWith(suffix)`            | `OpEndsWith`   |
| `s.replace(old, new)`           | `OpReplace`    |
| `s.substring(start, end)`       | `OpSubstring`  |
| `xs.join(sep)`                  | `OpJoin`       |

Method names are matched case-insensitively, so `s.ToUpper()`,
`s.toUpper()`, and `s.TOUPPER()` all map to `OpToUpper`.

---

## Closures

Phase 4 introduces a tight subset of closure syntax for predicates and
transformers passed to `.map`, `.filter`, and `.find`.

### Allowed

- **Single parameter** only: `func(x){ ... }`.
- **Single expression body**: `func(x){ x.name }` or `func(x){ return x.name }`.
- The `return` keyword is optional for a one-expression body. Both
  forms produce identical IR.
- The parameter name is local to the closure body. Outer props,
  signals, and the magic `_item` / `_index` props remain visible.

### Not allowed (rejected at parse time with a clear error)

- **Multi-param closures**: `func(a, b){ ... }` —
  there is no second iteration variable in the runtime; if you need
  one, pre-shape the data Go-side or file a follow-up.
- **Zero-param closures**: `func(){ ... }` —
  ambiguous and not needed by any supported method.
- **Multi-statement bodies**: `func(i){ x := 1; return x }` —
  the VM has no statement evaluator inside expression context.
- **Closures outside `.map` / `.filter` / `.find` arguments** —
  rejected to keep the grammar unambiguous with Go's anonymous
  function syntax.
- **Captures by reference** — the closure body only reads props /
  signals / the magic iteration prop. There is no mutable shared
  state captured from the enclosing scope at parse time.

### Method-chain depth cap

Chains deeper than **four** `.foo(...)` levels are rejected with a
helpful error message that points the author to refactor the
expression or file a follow-up:

```gsx
// OK — four levels.
items.filter(func(i){ return i.a }).map(func(i){ return i.b }).filter(func(i){ return i.c }).map(func(i){ return i.d })

// Rejected — exceeds the 4-level cap.
items.filter(...).map(...).filter(...).map(...).filter(...)
```

This is a sanity cap, not a hard architectural limit. If a real-world
case needs deeper chains, file a follow-up plan that documents the
case and the desired new cap.

---

## Non-goals

These are intentionally out of scope for the expression language.
Implementing any of them requires its own plan and (typically) an ADR.

- **Multi-param lambdas** — `func(a, b){ ... }`.
- **Map / dict comprehensions** — `{k: v for k, v in m}`.
- **Generic functions** — `func[T any](x T){ ... }`.
- **Operator overloading** — defining `+` for custom types.
- **Method definitions** — `func (r Receiver) Name() { ... }`.
- **Control-flow keywords** beyond `return` — no `if`, `for`,
  `switch`, `defer`, `go`, `select`, channels.
- **Method chains** deeper than four levels.

If the grammar accepts a form, the runtime handles it. If the runtime
needs a form the grammar rejects, the grammar change is the work —
not a runtime hack.

---

## When you hit the wall

If a real `.gsx` use case demands something outside the supported
subset:

1. **Stop**. Don't silently expand the parser.
2. **File a Phase 4.x follow-up plan** under
   `~/.hyphae/spaces/m31labs-gosx/plans/` describing:
   - The use case (concrete `.gsx` snippet you wanted to write).
   - Whether the runtime would need a new opcode (if yes, the
     follow-up also triggers an ADR per ADR 0002).
   - Whether the grammar change is local or risks breaking existing
     valid `.gsx` files.
3. **Workaround until then**: pre-shape the data in the Go handler
   file companion to your `.gsx`. The "Go-side pre-shaping" pattern
   was the only option pre–Phase 4; it remains the safe fallback.

The expression language is deliberately small. Keeping it small keeps
the VM small, keeps the WASM bundle small, and keeps the contract
between `.gsx` authors and runtime engineers narrow enough to fit in
this document.
