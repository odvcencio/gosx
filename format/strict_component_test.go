package format

import (
	"bytes"
	"strings"
	"testing"

	"m31labs.dev/gosx"
)

func TestSourceFormatsStrictComponentIdempotently(t *testing.T) {
	source := []byte(`package app
type CardProps struct {
	Title string
	Body string
}

component Card(props: CardProps) {
	return <article><h2>{props.Title}</h2><p>{props.Body}</p></article>
}
`)
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	if !strings.Contains(string(formatted), "component Card(props: CardProps)") {
		t.Fatalf("strict declaration was not preserved:\n%s", formatted)
	}
	if _, err := gosx.Compile(formatted); err != nil {
		t.Fatalf("formatted strict source does not compile: %v\n%s", err, formatted)
	}
	again, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source second pass: %v", err)
	}
	if !bytes.Equal(formatted, again) {
		t.Fatalf("format is not idempotent\nfirst:\n%s\nsecond:\n%s", formatted, again)
	}
}

// TestSourceFormatsStrictConcatAndCondIdempotently covers the v0.42
// extensions: a concat hole in an attribute and text child, and a strict
// <If cond> element, must format idempotently and keep compiling.
func TestSourceFormatsStrictConcatAndCondIdempotently(t *testing.T) {
	source := []byte(`package app
type CardProps struct {
	Ready bool
	Tone string
}

component Card(props: CardProps) {
	return <div class={"tone-" + props.Tone}>
		<span>{"Rank " + props.Tone}</span>
		<If cond={props.Ready}>ready</If>
		<If cond={props.Ready == false}>not ready</If>
	</div>
}
`)
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	for _, want := range []string{
		`class={"tone-" + props.Tone}`,
		`{"Rank " + props.Tone}`,
		`<If cond={props.Ready}>`,
		`<If cond={props.Ready == false}>`,
	} {
		if !strings.Contains(string(formatted), want) {
			t.Fatalf("formatting changed %q:\n%s", want, formatted)
		}
	}
	if _, err := gosx.Compile(formatted); err != nil {
		t.Fatalf("formatted strict source does not compile: %v\n%s", err, formatted)
	}
	again, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source second pass: %v", err)
	}
	if !bytes.Equal(formatted, again) {
		t.Fatalf("format is not idempotent\nfirst:\n%s\nsecond:\n%s", formatted, again)
	}
}

// TestSourceFormatsStrictEachIdempotently covers #182: a strict <Each
// of={...} as="row" index="i"> block, including binding-rooted selector,
// concat, and cond holes inside its body, formats idempotently and keeps
// compiling.
func TestSourceFormatsStrictEachIdempotently(t *testing.T) {
	source := []byte(`package app
type BreakdownRow struct {
	Scored bool
	Label string
}

type RowProps struct {
	Breakdown []BreakdownRow
}

component Row(props: RowProps) {
	return <div>
		<Each of={props.Breakdown} as="row" index="i">
			<div class={"tone-" + row.Label} data-scored={row.Scored}>
				<span>{row.Label}</span>
				<span>{i}</span>
				<If cond={row.Scored}>scored</If>
			</div>
		</Each>
	</div>
}
`)
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	for _, want := range []string{
		`<Each of={props.Breakdown} as="row" index="i">`,
		`class={"tone-" + row.Label}`,
		`data-scored={row.Scored}`,
		`{row.Label}`,
		`{i}`,
		`<If cond={row.Scored}>`,
	} {
		if !strings.Contains(string(formatted), want) {
			t.Fatalf("formatting changed %q:\n%s", want, formatted)
		}
	}
	if _, err := gosx.Compile(formatted); err != nil {
		t.Fatalf("formatted strict source does not compile: %v\n%s", err, formatted)
	}
	again, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source second pass: %v", err)
	}
	if !bytes.Equal(formatted, again) {
		t.Fatalf("format is not idempotent\nfirst:\n%s\nsecond:\n%s", formatted, again)
	}
}

// TestSourceFormatsStrictSpreadCallSitesIdempotently covers #184: a tier-1
// spread at a strict call site and a tier-2 spread from a legacy body both
// format idempotently and keep compiling.
func TestSourceFormatsStrictSpreadCallSitesIdempotently(t *testing.T) {
	source := []byte(`package app
type TeamMarkProps struct {
	Tone string
}

component TeamMark(props: TeamMarkProps) {
	return <span>{props.Tone}</span>
}

component Wrap(props: TeamMarkProps) {
	return <div><TeamMark {...props}></TeamMark></div>
}

func Page() Node {
	team := map[string]any{"Tone": "red"}
	return <div><TeamMark {...team}></TeamMark></div>
}
`)
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	for _, want := range []string{
		`<TeamMark {...props}>`,
		`<TeamMark {...team}>`,
	} {
		if !strings.Contains(string(formatted), want) {
			t.Fatalf("formatting changed %q:\n%s", want, formatted)
		}
	}
	if _, err := gosx.Compile(formatted); err != nil {
		t.Fatalf("formatted strict source does not compile: %v\n%s", err, formatted)
	}
	again, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source second pass: %v", err)
	}
	if !bytes.Equal(formatted, again) {
		t.Fatalf("format is not idempotent\nfirst:\n%s\nsecond:\n%s", formatted, again)
	}
}

// TestSourceFormatsStrictNestedSelectorIdempotently covers section 2.b: a
// nested selector text hole, a nested concat operand, and a nested <If
// cond> selector all format idempotently and keep compiling.
func TestSourceFormatsStrictNestedSelectorIdempotently(t *testing.T) {
	source := []byte(`package app
type Team struct {
	City string
}

type Player struct {
	Name  string
	Ready bool
	Team  Team
}

type RowProps struct {
	Player Player
}

component Row(props: RowProps) {
	return <div class={"player-" + props.Player.Name}>
		<span>{props.Player.Team.City}</span>
		<If cond={props.Player.Ready}>ready</If>
	</div>
}
`)
	formatted, err := Source(source)
	if err != nil {
		t.Fatalf("Source: %v", err)
	}
	for _, want := range []string{
		`class={"player-" + props.Player.Name}`,
		`{props.Player.Team.City}`,
		`<If cond={props.Player.Ready}>`,
	} {
		if !strings.Contains(string(formatted), want) {
			t.Fatalf("formatting changed %q:\n%s", want, formatted)
		}
	}
	if _, err := gosx.Compile(formatted); err != nil {
		t.Fatalf("formatted strict source does not compile: %v\n%s", err, formatted)
	}
	again, err := Source(formatted)
	if err != nil {
		t.Fatalf("Source second pass: %v", err)
	}
	if !bytes.Equal(formatted, again) {
		t.Fatalf("format is not idempotent\nfirst:\n%s\nsecond:\n%s", formatted, again)
	}
}
