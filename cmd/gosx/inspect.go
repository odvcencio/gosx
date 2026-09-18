package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

// inspectReport versions the transport envelope. Program, when requested, is
// the current release's IR; consumers must pin their GoSX version to decode it.
type inspectReport struct {
	SchemaVersion int                  `json:"schemaVersion"`
	GoSXVersion   string               `json:"gosxVersion"`
	SourceHash    string               `json:"sourceHash"`
	File          string               `json:"file"`
	Valid         bool                 `json:"valid"`
	Phase         gosx.AnalysisPhase   `json:"phase"`
	Stages        []gosx.AnalysisEvent `json:"stages"`
	Diagnostics   []ir.Diagnostic      `json:"diagnostics"`
	Components    []inspectComponent   `json:"components"`
	NodeCount     int                  `json:"nodeCount"`
	Error         string               `json:"error,omitempty"`
	Program       *ir.Program          `json:"program,omitempty"`
}

type inspectComponent struct {
	Name      string  `json:"name"`
	PropsType string  `json:"propsType,omitempty"`
	IsIsland  bool    `json:"isIsland"`
	Span      ir.Span `json:"span"`
}

func cmdInspect() {
	if err := runInspect(os.Args[2:], os.Stdout); err != nil {
		fatal("inspect: %v", err)
	}
}

func runInspect(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	includeIR := flags.Bool("ir", false, "include current-release component IR")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		return fmt.Errorf("usage: gosx inspect [--ir] <file.gsx>")
	}
	file := flags.Arg(0)
	source, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("read %s: %w", file, err)
	}
	report := inspectReport{
		SchemaVersion: 1, GoSXVersion: gosx.Version, SourceHash: fmt.Sprintf("%x", sha256.Sum256(source)), File: file,
		Stages: []gosx.AnalysisEvent{}, Diagnostics: []ir.Diagnostic{}, Components: []inspectComponent{},
	}
	analysis, analysisErr := gosx.Analyze(source, gosx.AnalysisOptions{
		IncludeWarnings: true,
		Observe:         func(event gosx.AnalysisEvent) { report.Stages = append(report.Stages, event) },
	})
	report.Valid = analysisErr == nil
	report.Phase = analysis.Phase
	report.Diagnostics = append(report.Diagnostics, analysis.Diagnostics...)
	if analysisErr != nil {
		report.Error = analysisErr.Error()
		// ParseError is not an IR diagnostic; project it into the same source
		// location shape so consumers never have to parse human error strings.
		if parseErr, ok := analysisErr.(*gosx.ParseError); ok {
			report.Diagnostics = append(report.Diagnostics, ir.Diagnostic{
				Span: ir.Span{StartLine: parseErr.Line, StartCol: parseErr.Column,
					EndLine: parseErr.Line, EndCol: parseErr.Column + 1},
				Message: parseErr.Message,
			})
		}
	}
	for i := range report.Diagnostics {
		report.Diagnostics[i].Span.File = file
	}
	if analysis.Program != nil {
		report.NodeCount = len(analysis.Program.Nodes)
		for _, component := range analysis.Program.Components {
			span := component.Span
			span.File = file
			report.Components = append(report.Components, inspectComponent{
				Name: component.Name, PropsType: component.PropsType, IsIsland: component.IsIsland, Span: span,
			})
		}
		if *includeIR {
			report.Program = analysis.Program
		}
	}
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		return fmt.Errorf("write inspection: %w", err)
	}
	return analysisErr
}
