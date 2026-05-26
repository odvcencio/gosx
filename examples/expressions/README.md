# expressions — Phase 4 expression-language samples

Demonstrates the `.gsx` expression forms surfaced by Phase 4
(see `gosx/docs/expressions.md` for the full subset specification).

The samples here are illustrative: each `.gsx` snippet shows a form that
the IR parser now accepts. Executable proof lives in the parser and
round-trip tests:

- `ir/exprparse_test.go` — `TestParseFilterClosureWithReturn`,
  `TestParseMapClosure`, `TestParseFindClosure`,
  `TestParseChainedMethodCall`, `TestParseClosureRejectsMultiStatementBody`,
  `TestParseChainDepthCap`, plus the existing
  `TestParseCollectionMethod`, `TestParseSplitMethodStoresLiteralSeparator`,
  `TestParseStringMethodChain`, `TestParsePredicateMethod`.
- `ir/closure_eval_test.go` — end-to-end VM evaluation through a real
  `program.Program` built from the parser output.

## What each form lowers to

| `.gsx` source                                                | Opcode (`island/program/program.go`) |
|--------------------------------------------------------------|--------------------------------------|
| `items.filter(func(i){ return i.active })`                   | `OpFilter`                           |
| `items.map(func(i){ return i.name })`                        | `OpMap`                              |
| `items.find(func(i){ return i.id == 3 })`                    | `OpFind`                             |
| `items.slice(0, 5)`                                          | `OpSlice`                            |
| `items.append(x)`                                            | `OpAppend`                           |
| `items.len()` / `items.length`                               | `OpLen`                              |
| `title.toLower()` / `title.toUpper()` / `title.trim()`       | `OpToLower` / `OpToUpper` / `OpTrim` |
| `title.split(", ")`                                          | `OpSplit`                            |
| `title.contains("foo")`                                      | `OpContains`                         |
| `title.startsWith("Go")` / `title.endsWith("X")`             | `OpStartsWith` / `OpEndsWith`        |
| `title.replace("a", "b")`                                    | `OpReplace`                          |
| `title.substring(0, 4)`                                      | `OpSubstring`                        |

Closure-only forms (Phase 4 additions) bind the parameter name to the
existing `_item` magic prop so the lowered ops are identical to the
legacy bare-expression form `items.map(_item * 2)`.

## Sample composition

`expressions.gsx` shows a single component using several forms together.
It is **not** wired into a runnable `main.go` — running the IR parser
against any of the snippets verifies the grammar acceptance. To exercise
the runtime end-to-end, see `ir/closure_eval_test.go`.

## Non-goals (will fail at parse time)

- Multi-param closures: `func(a, b){ ... }`
- Multi-statement closure bodies: `func(i){ x := 1; return x }`
- Closures outside `.map`/`.filter`/`.find` arguments
- Method chains deeper than 4 levels
