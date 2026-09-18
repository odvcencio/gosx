//go:build !tinygo

package gosx_test

import (
	"reflect"
	"testing"

	"m31labs.dev/gosx"
)

func TestAnalyzeCompilerContract(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		phases       int
		program      bool
		failure      bool
	}{
		{"valid", `package app
component Page() {
return <main>Ready</main>
}`, 3, true, false},
		{"syntax", `package app
component Page() {
return <main>{</main>
}`, 1, false, true},
		{"package", `not a valid gsx file`, 1, false, true},
		{"lower", `package app
component Page(props: Missing) {
return <main>{props.Name}</main>
}`, 2, false, true},
		{"validation", `package app
func broken() Node {
return <main>Ready</main>
}`, 3, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var events []gosx.AnalysisEvent
			analysis, err := gosx.Analyze([]byte(tc.source+"\n"), gosx.AnalysisOptions{
				Observe: func(event gosx.AnalysisEvent) { events = append(events, event) },
			})
			if (err != nil) != tc.failure || (analysis.Program != nil) != tc.program {
				t.Fatalf("unexpected artifacts/error: %+v, %v", analysis, err)
			}
			if analysis.Tree == nil || analysis.Language == nil || len(events) != tc.phases {
				t.Fatalf("missing inspection artifacts or phase events: %+v, %+v", analysis, events)
			}
			order := []gosx.AnalysisPhase{gosx.AnalysisParse, gosx.AnalysisLower, gosx.AnalysisValidate}
			for i, event := range events {
				if event.Phase != order[i] || event.Duration < 0 || (event.Error != "") != (tc.failure && i == len(events)-1) {
					t.Fatalf("unexpected event: %+v", event)
				}
			}
			if analysis.Phase != order[tc.phases-1] {
				t.Fatalf("unexpected terminal phase %s", analysis.Phase)
			}
			program, compileErr := gosx.Compile([]byte(tc.source + "\n"))
			if tc.failure {
				if program != nil || compileErr == nil || compileErr.Error() != err.Error() {
					t.Fatalf("Compile diverged: %v / %v", compileErr, err)
				}
			} else if compileErr != nil || !reflect.DeepEqual(program, analysis.Program) {
				t.Fatalf("Compile diverged from Analyze: %v", compileErr)
			}
		})
	}
}

func TestAnalyzeWarningsAreOptionalAndAdvisory(t *testing.T) {
	source := []byte(`package app
func Card(props any) Node {
return <main>{props.Name}</main>
}
`)
	without, err := gosx.Analyze(source, gosx.AnalysisOptions{})
	if err != nil || len(without.Diagnostics) != 0 {
		t.Fatalf("default inspection changed Compile behavior: %v, %+v", err, without.Diagnostics)
	}
	with, err := gosx.Analyze(source, gosx.AnalysisOptions{IncludeWarnings: true})
	if err != nil || len(with.Diagnostics) == 0 || !reflect.DeepEqual(with.Program, without.Program) {
		t.Fatalf("warnings must be advisory and preserve IR: %v, %+v", err, with.Diagnostics)
	}
}
