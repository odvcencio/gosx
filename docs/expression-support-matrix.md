# GoSX expression support matrix

Generated file. Do not edit by hand. Regenerate it with:

```
go test ./internal/evalparity/... -run TestSupportMatrixUpToDate -update
```

This matrix compares GoSX's three expression evaluators:

- **transpile**: real Go source, compiled by the Go compiler (`m31labs.dev/gosx/transpile`).
- **route**: the file router's per-request reflect interpreter (`m31labs.dev/gosx/route`). This is what `router.AddDir` serves.
- **client-vm**: the same compiled IR, lowered to island bytecode and walked by the client VM (`m31labs.dev/gosx/client/vm`) — the code that runs in the browser under WASM, run natively here.

Each row is one case from `internal/evalparity`'s differential test table (`cases_table.go`). "Agrees" means the backend produces the pinned expected value. "Diverges" means the backend runs without error but produces a different, pinned value. "Unsupported" means the backend cannot run the case at all (a compile or lower error). See each case's notes for why.

## Summary

| Backend | Agrees | Diverges | Unsupported | Total |
|---|---|---|---|---|
| transpile | 46 | 0 | 14 | 60 |
| route | 54 | 6 | 0 | 60 |
| client-vm | 59 | 1 | 0 | 60 |

## numeric

| Case | Expression | transpile | route | client-vm |
|---|---|---|---|---|
| `numeric_int_add_literal` | `3 + 4` | agrees: `7` | agrees: `7` | agrees: `7` |
| `numeric_int_add_props` | `props.A + props.B` | agrees: `7` | agrees: `7` | agrees: `7` |
| `numeric_int_sub_props` | `props.A - props.B` | agrees: `-6` | agrees: `-6` | agrees: `-6` |
| `numeric_int_mul_props` | `props.A * props.B` | agrees: `42` | agrees: `42` | agrees: `42` |
| `numeric_int_div_literal` | `7 / 2` | agrees: `3` | agrees: `3` | agrees: `3` |
| `numeric_int_div_props` | `props.A / props.B` | agrees: `3` | agrees: `3` | agrees: `3` |
| `numeric_int_mod_props` | `props.A % props.B` | agrees: `1` | agrees: `1` | agrees: `1` |
| `numeric_float_div_props` | `props.A / props.B` | agrees: `3.75` | agrees: `3.75` | agrees: `3.75` |
| `numeric_mixed_literal_add` | `3 + 4.5` | agrees: `7.5` | agrees: `7.5` | agrees: `7.5` |
| `numeric_mixed_typed_add_props` | `props.A + props.F` | unsupported | agrees: `7.5` | agrees: `7.5` |
| `numeric_float_format_large` | `100000000.0` | agrees: `1e+08` | agrees: `1e+08` | agrees: `1e+08` |
| `numeric_float_format_small` | `0.0001` | agrees: `0.0001` | agrees: `0.0001` | agrees: `0.0001` |
| `numeric_float_negative` | `-3.5` | agrees: `-3.5` | agrees: `-3.5` | agrees: `-3.5` |
| `numeric_float_repeating` | `1.0 / 3.0` | agrees: `0.3333333333333333` | agrees: `0.3333333333333333` | agrees: `0.3333333333333333` |
| `numeric_precision_large_int` | `props.A + 0` | agrees: `9007199254740993` | diverges: `9.007199254740992e+15` | diverges: `9007199254740992` |
| `numeric_unary_neg_props` | `-props.A` | agrees: `-5` | agrees: `-5` | agrees: `-5` |
| `numeric_negative_literal_div` | `-7 / 2` | agrees: `-3` | diverges: `-3.5` | agrees: `-3` |

Notes:

- `numeric_int_add_literal`: two int literals add the same everywhere
- `numeric_int_add_props`: int props fields add the same everywhere
- `numeric_int_sub_props`: int subtraction, including a negative result
- `numeric_int_mul_props`: int multiplication
- `numeric_int_div_literal`: literal int division truncates like Go
- `numeric_int_div_props`: int / int truncates like Go, not float division (see route/exprlower.go's QUO fix)
- `numeric_int_mod_props`: int modulo
- `numeric_float_div_props`: float / float keeps the fractional result
- `numeric_mixed_literal_add`: untyped int + untyped float constants promote to float, same everywhere
- `numeric_mixed_typed_add_props`: a declared int field + a declared float64 field is invalid Go (mismatched types); route/VM both coerce dynamically to float and agree
  - transpile unsupported: real Go rejects "invalid operation: props.A + props.F (mismatched types int and float64)"; route/VM erase the Go static type at props-transport time and add as float64 either way
- `numeric_float_format_large`: large float literal formats in scientific notation the same way in Go's fmt and strconv.FormatFloat('g', -1, 64)
- `numeric_float_format_small`: small float literal keeps decimal notation
- `numeric_float_negative`: negative float literal
- `numeric_float_repeating`: a repeating-decimal float division formats identically (shortest round-trip representation, both strconv and fmt)
- `numeric_precision_large_int`: a bare props.A read (no arithmetic) preserves full int64 precision on every backend — reflect and JSON round-trips both pass the exact value through untouched. Arithmetic is what loses precision: route's numericValue and the VM's Value both fold every number through a float64 field, which cannot represent 2^53+1 exactly; transpile keeps the real Go int64
  - route diverges: route/fileeval.go's applyFileBinaryOp ADD always returns numericValue(left)+numericValue(right) as a bare float64, which cannot represent 2^53+1 exactly (rounds to 9007199254740992) and then formats through fmt.Sprint's large-magnitude float path as scientific notation
  - client-vm diverges: client/vm/value.go's Value packs every number into a float64 num field; Add keeps the TypeInt tag when both operands are int-kind, but the value it carries already rounded to the nearest representable float64 (9007199254740992) before Add ever ran
- `numeric_unary_neg_props`: unary minus on a direct int prop read (not a nested sub-expression) agrees everywhere
- `numeric_negative_literal_div`: -7 / 2: real Go and the VM truncate toward zero (-3); route's applyFileUnaryOp SUB case converts through numericValue (float64) before QUO ever sees the operand, discarding the int-ness the QUO fix (route/exprlower.go) depends on — a known, narrower-than-full-fix gap: QUO's fix only recovers int-ness from a DIRECT int operand (a struct field, a bare positive literal), not one already erased by a prior unary/binary op. Fixing that fully means every arithmetic op preserving Go's static type through the interpreter's untyped `any` pipeline, a materially bigger change than this harness's targeted QUO fix
  - route diverges: applyFileUnaryOp's SUB case returns -numericValue(value) (always float64); QUO's isIntegerKind check then sees a float64 left operand and falls back to float division

## string

| Case | Expression | transpile | route | client-vm |
|---|---|---|---|---|
| `string_concat_literal` | `"foo" + "bar"` | agrees: `foobar` | agrees: `foobar` | agrees: `foobar` |
| `string_concat_props_and_literal` | `props.S + "!"` | agrees: `hello!` | agrees: `hello!` | agrees: `hello!` |
| `string_eq_literal_true` | `"a" == "a"` | agrees: `true` | agrees: `true` | agrees: `true` |
| `string_eq_props_false` | `props.S == "z"` | agrees: `false` | agrees: `false` | agrees: `false` |
| `string_lt_lexicographic` | `"apple" < "banana"` | agrees: `true` | agrees: `true` | agrees: `true` |
| `string_neq_literal` | `"a" != "b"` | agrees: `true` | agrees: `true` | agrees: `true` |
| `string_empty_concat` | `"" + "x"` | agrees: `x` | agrees: `x` | agrees: `x` |
| `string_len_builtin` | `len(props.S)` | agrees: `5` | diverges: `(empty)` | agrees: `5` |
| `string_concat_three_literals` | `"a" + "b" + "c"` | agrees: `abc` | agrees: `abc` | agrees: `abc` |

Notes:

- `string_concat_literal`: plain string concatenation
- `string_concat_props_and_literal`: string prop concatenated with a literal
- `string_eq_literal_true`: string equality, literal true case
- `string_eq_props_false`: string equality, props false case
- `string_lt_lexicographic`: string < is lexicographic byte comparison
- `string_neq_literal`: string inequality
- `string_empty_concat`: concatenating with an empty string literal is a no-op
- `string_len_builtin`: len(string): the island DSL has a builtin OpLen opcode; route's CallExpr path only resolves bound Funcs/component identifiers, so the unbound "len" identifier evaluates to nil and the call silently renders empty
  - route diverges: route/exprlower.go's CallExpr case lowers node.Fun (the identifier "len") the same way any other identifier lowers; nothing in route/fileeval.go registers a "len" builtin, so it resolves to a nil function and callValue silently returns nil
- `string_concat_three_literals`: chained + associates left to right the same way everywhere

## comparison

| Case | Expression | transpile | route | client-vm |
|---|---|---|---|---|
| `comparison_int_eq_true` | `props.A == props.B` | agrees: `true` | agrees: `true` | agrees: `true` |
| `comparison_int_neq_true` | `props.A != props.B` | agrees: `true` | agrees: `true` | agrees: `true` |
| `comparison_int_lt_true` | `props.A < props.B` | agrees: `true` | agrees: `true` | agrees: `true` |
| `comparison_int_gt_false` | `props.A > props.B` | agrees: `false` | agrees: `false` | agrees: `false` |
| `comparison_int_gte_equal` | `props.A >= props.B` | agrees: `true` | agrees: `true` | agrees: `true` |
| `comparison_int_lte_equal` | `props.A <= props.B` | agrees: `true` | agrees: `true` | agrees: `true` |
| `comparison_float_eq_literal` | `7.0 == 7.0` | agrees: `true` | agrees: `true` | agrees: `true` |
| `comparison_bool_eq_props` | `props.Flag == true` | agrees: `true` | agrees: `true` | agrees: `true` |
| `comparison_string_case_sensitive` | `"Apple" == "apple"` | agrees: `false` | agrees: `false` | agrees: `false` |
| `comparison_float_neq_literal` | `7.0 != 8.0` | agrees: `true` | agrees: `true` | agrees: `true` |

Notes:

- `comparison_int_eq_true`: int equality
- `comparison_int_neq_true`: int inequality
- `comparison_int_lt_true`: int less-than
- `comparison_int_gt_false`: int greater-than, false case
- `comparison_int_gte_equal`: >= at the equal boundary
- `comparison_int_lte_equal`: <= at the equal boundary
- `comparison_float_eq_literal`: float equality
- `comparison_bool_eq_props`: bool equality
- `comparison_string_case_sensitive`: string equality is case-sensitive (byte comparison, not locale-aware) everywhere
- `comparison_float_neq_literal`: float inequality

## ternary

| Case | Expression | transpile | route | client-vm |
|---|---|---|---|---|
| `ternary_plain_value` | `props.Flag ? "yes" : "no"` | unsupported | diverges: `(empty)` | agrees: `yes` |
| `ternary_jsx_both_branches` | `props.Flag ? <span>yes</span> : <b>no</b>` | unsupported | agrees: `<span>yes</span>` | agrees: `<span>yes</span>` |
| `and_jsx_conditional_true` | `props.Flag && <span>yes</span>` | unsupported | agrees: `<span>yes</span>` | agrees: `<span>yes</span>` |
| `and_jsx_conditional_false` | `props.Flag && <span>yes</span>` | unsupported | agrees: `(empty)` | agrees: `(empty)` |

Notes:

- `ternary_plain_value`: GSX-only ternary (Go has none) on a plain (non-JSX) value
  - transpile unsupported: GSX ternary "?:" is not valid Go syntax; transpile.go has no lowering for gsx_ternary_expression, so Transpile fails with "illegal character U+003F '?'"
  - route diverges: go/parser.ParseExpr rejects the "?" token the same way transpile does, but compiledFileExpr (route/exprcache.go) treats any parse failure as "render empty" instead of surfacing an error, so the hole silently renders nothing instead of "yes"
- `ternary_jsx_both_branches`: ternary with a JSX element on both branches lowers to two <If> subtrees (ir/lower.go's lowerConditionalExprContainer); route/VM agree
  - transpile unsupported: GSX ternary "?:" is not valid Go syntax and transpile.go has no JSX-conditional lowering either; author <If cond={...}>...</If> instead for a transpile-safe conditional
- `and_jsx_conditional_true`: GSX's `cond && <jsx>` sugar (not real Go's &&) mounts the element when true; route/VM agree
  - transpile unsupported: transpile.go has no lowering for the "cond && <jsx>" structural sugar; it emits the JSX element's "<" byte-for-byte into a Go expression position, which fails to parse ("expected operand, found '<'")
- `and_jsx_conditional_false`: same sugar, false case renders nothing
  - transpile unsupported: same as and_jsx_conditional_true: transpile has no lowering for this sugar

## indexing

| Case | Expression | transpile | route | client-vm |
|---|---|---|---|---|
| `index_slice_inbounds` | `props.Items[1]` | agrees: `20` | agrees: `20` | agrees: `20` |
| `index_map_present` | `props.M["k"]` | agrees: `42` | agrees: `42` | agrees: `42` |
| `index_slice_out_of_range` | `props.Items[9]` | unsupported | diverges: `(empty)` | agrees: `0` |
| `index_map_missing_key` | `props.M["missing"]` | agrees: `0` | diverges: `(empty)` | agrees: `0` |
| `index_nested_struct_field` | `props.Nested.X` | agrees: `5` | agrees: `5` | agrees: `5` |

Notes:

- `index_slice_inbounds`: slice indexing in bounds
- `index_map_present`: map indexing, present key
- `index_slice_out_of_range`: out-of-range slice index: real Go panics at runtime (recovered by the harness — see caseCallLine); route fails soft to "", VM fails soft to the element type's zero value. Documented, not fixed: ir/lower.go's strict-renderer validator already refuses index expressions for exactly this reason ("out-of-range behavior differs from Go")
  - transpile unsupported: real Go panics at runtime: "index out of range [9] with length 3"
  - route diverges: route/fileeval.go's indexSequentialValue returns the zero any (nil) past the slice bound instead of erroring, which renders as empty text rather than a zero-valued element
- `index_map_missing_key`: missing map key: real Go returns the value type's zero value at compile-checked type, matching VM; route fails soft to "" the same way index_slice_out_of_range does
  - route diverges: route/fileeval.go's indexMapValue (via mapLookup) returns nil for a missing key instead of the value type's zero value, which renders as empty text instead of "0"
- `index_nested_struct_field`: field access on a nested named-struct props field

## bool-coercion

| Case | Expression | transpile | route | client-vm |
|---|---|---|---|---|
| `bool_and_nonbool_int_truthy` | `props.A && props.B` | unsupported | agrees: `true` | agrees: `true` |
| `bool_and_nonbool_int_falsy` | `props.A && props.B` | unsupported | agrees: `false` | agrees: `false` |
| `bool_not_nonbool_string_empty` | `!props.S` | unsupported | agrees: `true` | agrees: `true` |
| `bool_not_nonbool_string_nonempty` | `!props.S` | unsupported | agrees: `false` | agrees: `false` |
| `bool_or_nonbool_string_fallback` | `props.S || "fallback"` | unsupported | agrees: `true` | agrees: `true` |
| `bool_literal_true` | `true` | agrees: `true` | agrees: `true` | agrees: `true` |
| `bool_not_literal_false` | `!false` | agrees: `true` | agrees: `true` | agrees: `true` |

Notes:

- `bool_and_nonbool_int_truthy`: && on two nonzero int props: Go's && requires bool operands, but the GSX grammar does not. route coerces via truthy(); VM's Value.truth() used to answer false for every non-bool kind (only BoolVal(true) ever set the tag bit) — fixed in client/vm/value.go, locked by TestValueTruthNonBoolKinds
  - transpile unsupported: real Go rejects "invalid operation: operator && not defined on props.A (variable of type int)"
- `bool_and_nonbool_int_falsy`: && with one zero int operand
  - transpile unsupported: same as bool_and_nonbool_int_truthy: real Go rejects && on int operands
- `bool_not_nonbool_string_empty`: ! on an empty string prop
  - transpile unsupported: real Go rejects "invalid operation: operator ! not defined on props.S (variable of type string)"
- `bool_not_nonbool_string_nonempty`: ! on a non-empty string prop
  - transpile unsupported: same as bool_not_nonbool_string_empty: real Go rejects ! on a string operand
- `bool_or_nonbool_string_fallback`: || on a non-empty string prop short-circuits to that prop, JS-template style
  - transpile unsupported: real Go rejects "invalid operation: operator || not defined on props.S (variable of type string)"
- `bool_literal_true`: bare bool literal
- `bool_not_literal_false`: ! on a literal bool is ordinary Go, agrees everywhere

## nil

| Case | Expression | transpile | route | client-vm |
|---|---|---|---|---|
| `nil_eq_empty_string` | `props.S == nil` | unsupported | agrees: `true` | agrees: `true` |
| `nil_eq_nonempty_string` | `props.S == nil` | unsupported | agrees: `false` | agrees: `false` |
| `nil_neq_string` | `props.S != nil` | unsupported | agrees: `true` | agrees: `true` |

Notes:

- `nil_eq_empty_string`: route treats nil as == "" for a string comparison (route/fileeval.go's equalValues doc comment); the VM's island DSL parser accepts a bare "nil" identifier and evaluates it the same way in practice
  - transpile unsupported: real Go rejects "invalid operation: props.S == nil (mismatched types string and untyped nil)"
- `nil_eq_nonempty_string`: same rule, false case
  - transpile unsupported: same as nil_eq_empty_string: real Go rejects comparing a string to untyped nil
- `nil_neq_string`: != nil is the complement of == nil
  - transpile unsupported: same as nil_eq_empty_string: real Go rejects comparing a string to untyped nil

## html-escape

| Case | Expression | transpile | route | client-vm |
|---|---|---|---|---|
| `escape_angle_brackets` | `props.S` | agrees: `&lt;script&gt;` | agrees: `&lt;script&gt;` | agrees: `&lt;script&gt;` |
| `escape_ampersand` | `props.S` | agrees: `Q&amp;A` | agrees: `Q&amp;A` | agrees: `Q&amp;A` |
| `escape_double_quote` | `props.S` | agrees: `say &#34;hi&#34;` | agrees: `say &#34;hi&#34;` | agrees: `say &#34;hi&#34;` |
| `escape_apostrophe` | `props.S` | agrees: `it&#39;s` | agrees: `it&#39;s` | agrees: `it&#39;s` |
| `escape_concat_result` | `"<" + "b" + ">"` | agrees: `&lt;b&gt;` | agrees: `&lt;b&gt;` | agrees: `&lt;b&gt;` |

Notes:

- `escape_angle_brackets`: < and > escape identically everywhere (gosx.RenderHTML, route's renderFileEvaluatedExpr, and island.RenderResolvedHTML all call html.EscapeString on scalar expression text)
- `escape_ampersand`: & escapes to &amp;
- `escape_double_quote`: double quote escapes to &#34;
- `escape_apostrophe`: apostrophe escapes to &#39;
- `escape_concat_result`: an expression's computed (not just a bare prop's) result still escapes
