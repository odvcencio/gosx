# Compiler inspection and integration hooks

GoSX exposes one host-side parse → lower → validate pipeline through
`gosx.Analyze`. `gosx.Compile` and the language server's source index use that
same pipeline. This lets studio inspectors, language tools, and profilers use
compiler artifacts without reproducing validation rules.

## Go API

```go
analysis, err := gosx.Analyze(source, gosx.AnalysisOptions{
    IncludeWarnings: true,
    Observe: func(event gosx.AnalysisEvent) {
        log.Printf("%s: %s (%d nodes)", event.Phase, event.Duration, event.Nodes)
    },
})
if err != nil {
    // The syntax tree survives recoverable parse errors. A successfully
    // lowered Program survives validation errors for editor inspection.
    // Never render that Program when err != nil.
    return err
}
for _, component := range analysis.Program.Components {
    fmt.Println(component.Name, component.PropsType, component.Span)
}
```

The result is non-nil on failure. `Phase` identifies the last attempted stage.
`Tree` and `Language` expose the CST after parsing; retain the original source
bytes unchanged while using tree text and ranges. `Program` exposes component
schemas, nodes, expression text, imports, and source spans after lowering.
Lowering errors currently do **not** return a partial program. `Diagnostics`
holds lower/validation findings; syntax errors retain the typed `ParseError`
in the returned error. Warnings are optional and never block compilation.

The observer receives one value-only event after each attempted stage, including
failure. Later stages do not run after an error. Durations exclude observer
execution and include cold grammar initialization in the parse stage. There is
no global registration, mutable IR callback, or runtime dependency injection.
Observers run synchronously; concurrent users synchronize shared observer state,
and observer panics propagate. Artifact inspection is supported; mutating the
returned CST or IR is not a compiler transformation protocol.

This is a source-file analysis seam. It does not load packages, prove imported
Go types, emit island bytecode, certify engine support, or execute code. Continue
using `gosx check` for package and island checks and the existing build pipeline
for executable output. The API is excluded from TinyGo production runtimes,
like `gosx.Parse`; it does not increase their compiler dependency surface.

## CLI protocol

```sh
gosx inspect app/page.gsx
gosx inspect --ir app/page.gsx
```

Both commands write a JSON object to stdout. The compact form includes:

| Field | Meaning |
| --- | --- |
| `schemaVersion` | Envelope version, currently `1`. |
| `gosxVersion` | Library release used by the inspecting executable. |
| `sourceHash` | Hex SHA-256 of the exact source bytes, for stale-result detection. |
| `file` | Input filename as supplied by the caller. |
| `valid` | Source-file analysis succeeded; this is not whole-project validity. |
| `phase`, `stages` | Last stage and ordered observations; durations are nanoseconds. |
| `diagnostics` | Errors and warnings using the existing `ir.Diagnostic` shape. |
| `components`, `nodeCount` | Declared component names, props types, island flags, spans, and IR size. |
| `error` | Human-readable failure, present only if analysis failed. |
| `program` | Current-release raw IR, only with `--ir` and successful lowering. |

Compiler failures still produce JSON and a nonzero process exit status. Argument,
file-read, and output-write failures may prevent a complete JSON report. Consumers
must check both the exit status and `valid`. Diagnostics and component spans carry
the filename, with one-based line and UTF-8 byte column coordinates; convert those
coordinates for an editor using UTF-16 positions. Diagnostic fields retain the
existing Go IR JSON spelling (`Span`, `Message`, `Severity`, etc.); error severity
is `0`, warning is `1`. Syntax diagnostics use the same shape in this CLI envelope.

The envelope version does not freeze the raw IR or CST layout. Pin the GoSX
release when consuming `--ir` or compiler structures. A locally modified binary
can share a release number; reproducible integrations must also pin the binary
or build revision. Timings vary between runs and should be excluded from golden
comparisons. A source hash detects staleness; it does not authorize a source edit.

## Next integration layers

The architecture roadmap proposes a versioned, projected component catalog,
package-aware analysis sessions, stable diagnostic codes and fix-its, and source
edits guarded by a source hash. Those are future APIs. Existing observers cannot
register syntax, disable a validator, change backend capability claims, or write
files. Studio commands should consume inspection results and submit an explicit
edit proposal through their own revision-safe command boundary.
