package ir_test

import (
	"strings"
	"testing"
)

func TestLowerValidatesTypedLegacyEachExpressionAndAttributeSelectors(t *testing.T) {
	lowerTypedLegacySource(t, `package app

type Player struct {
	Name        string
	DisplayName string
}

type RosterProps struct {
	Players []Player
}

func Roster(props RosterProps) Node {
	return <ul>
		<Each of={props.Players} as="player">
			<li data-name={player.DisplayName}>{player.Name}</li>
		</Each>
	</ul>
}
`)
}

func TestLowerValidatesTypedLegacyEachDefaultItemBinding(t *testing.T) {
	_, err := parse(t, []byte(`package app

type Player struct {
	Name string
}

type RosterProps struct {
	Players []Player
}

func Roster(props RosterProps) Node {
	return <Each of={props.Players}><span>{item.name}</span></Each>
}
`))
	if err == nil {
		t.Fatal("Lower accepted an invalid selector through Each's default item binding")
	}
	if want := "typed legacy component Roster cannot resolve item.name"; !strings.Contains(err.Error(), want) {
		t.Fatalf("Lower error = %v, want substring %q", err, want)
	}
}

func TestLowerValidatesTypedLegacyEachFilterFormatting(t *testing.T) {
	for _, tc := range []struct {
		name      string
		separator string
	}{
		{name: "space before call", separator: " "},
		{name: "comment before call", separator: " /* keep receiver */ "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lowerTypedLegacySource(t, `package app

type Player struct {
	Name string
}

type RosterProps struct {
	Players []Player
}

func Roster(props RosterProps) Node {
	return <ul>
		<Each of={props.Players.filter`+tc.separator+`(func(player Player) bool { return player.Name != "" })} as="player">
			<li>{player.Name}</li>
		</Each>
	</ul>
}
`)
		})
	}
}

func TestLowerValidatesTypedLegacyEachFilteredReceiverWithMultipleRoots(t *testing.T) {
	lowerTypedLegacySource(t, `package app

type Player struct {
	Name string
}

type RosterProps struct {
	Players []Player
	Include bool
}

func Roster(props RosterProps) Node {
	return <ul>
		<Each of={props.Players.filter(func(player Player) bool { return props.Include && player.Name != "" })} as="player">
			<li>{player.Name}</li>
		</Each>
	</ul>
}
`)
}

func TestLowerRejectsTypedLegacyEachFilteredReceiverAliasWithMultipleRoots(t *testing.T) {
	_, err := parse(t, []byte(`package app

type Player struct {
	Name string
}

type RosterProps struct {
	Players []Player
	Include bool
}

func Roster(props RosterProps) Node {
	return <ul>
		<Each of={props.Players.filter(func(player Player) bool { return props.Include && player.Name != "" })} as="player">
			<li>{player.name}</li>
		</Each>
	</ul>
}
`))
	if err == nil {
		t.Fatal("Lower accepted an invalid selector after a multi-root filter predicate")
	}
	if want := "typed legacy component Roster cannot resolve player.name"; !strings.Contains(err.Error(), want) {
		t.Fatalf("Lower error = %v, want substring %q", err, want)
	}
}

func TestLowerRejectsTypedLegacyEachAttributeAlias(t *testing.T) {
	_, err := parse(t, []byte(`package app

type Player struct {
	Name string
}

type RosterProps struct {
	Players []Player
}

func Roster(props RosterProps) Node {
	return <ul>
		<Each of={props.Players} as="player">
			<li class={player.name}>invalid</li>
		</Each>
	</ul>
}
`))
	if err == nil {
		t.Fatal("Lower accepted an invalid typed legacy attribute selector")
	}
	for _, want := range []string{
		"typed legacy component Roster cannot resolve player.name",
		"struct Player declares no visible field name",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Lower error = %v, want substring %q", err, want)
		}
	}
}

func TestLowerKeepsDynamicNestedEachBindingOpaqueWhenItShadowsOuterBinding(t *testing.T) {
	lowerTypedLegacySource(t, `package app

type Player struct {
	Name string
}

type Group struct {
	Players []*Player
}

type RosterProps struct {
	Groups []Group
}

func Roster(props RosterProps) Node {
	return <section>
		<Each of={props.Groups} as="item">
			<Each of={item.Players} as="item">
				<span>{item.Name}</span>
			</Each>
		</Each>
	</section>
}
`)
}

func TestLowerKeepsDynamicNestedEachDefaultItemBindingOpaque(t *testing.T) {
	lowerTypedLegacySource(t, `package app

type Player struct {
	Name string
}

type Group struct {
	Players []*Player
}

type RosterProps struct {
	Groups []Group
}

func Roster(props RosterProps) Node {
	return <section>
		<Each of={props.Groups} as="item">
			<Each of={item.Players}>
				<span>{item.Name}</span>
			</Each>
		</Each>
	</section>
}
`)
}

func TestLowerLeavesOpaqueTypedLegacyEachSelectorsDynamic(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{
			name: "map-backed elements",
			source: `package app

type RosterProps struct {
	Rows []map[string]any
}

func Roster(props RosterProps) Node {
	return <Each of={props.Rows} as="row"><span>{row.display_name}</span></Each>
}
`,
		},
		{
			name: "map transform",
			source: `package app

type Player struct {
	Name string
}

type RosterProps struct {
	Players []Player
}

func Roster(props RosterProps) Node {
	return <Each of={props.Players.map(func(player Player) any { return player.Name })} as="row"><span>{row.display_name}</span></Each>
}
`,
		},
		{
			name: "flatMap transform",
			source: `package app

type Group struct {
	Names []string
}

type RosterProps struct {
	Groups []Group
}

func Roster(props RosterProps) Node {
	return <Each of={props.Groups.flatMap(func(group Group) any { return group.Names })} as="row"><span>{row.display_name}</span></Each>
}
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lowerTypedLegacySource(t, tc.source)
		})
	}
}

func TestLowerValidatesTypedLegacyNestedEachScopes(t *testing.T) {
	lowerTypedLegacySource(t, `package app

type Player struct {
	DisplayName string
}

type Group struct {
	Name    string
	Players []Player
}

type RosterProps struct {
	Groups []Group
}

func Roster(props RosterProps) Node {
	return <section>
		<Each of={props.Groups} as="group">
			<Each of={group.Players} as="player" index="i">
				<span data-group={group.Name} data-player={player.DisplayName}>{i}</span>
			</Each>
		</Each>
	</section>
}
`)
}

func TestLowerRejectsTypedLegacyNestedIndexSelector(t *testing.T) {
	_, err := parse(t, []byte(`package app

type Player struct {
	Name string
}

type Group struct {
	Players []Player
}

type RosterProps struct {
	Groups []Group
}

func Roster(props RosterProps) Node {
	return <section>
		<Each of={props.Groups} as="group">
			<Each of={group.Players} as="player" index="i">
				<span>{i.Name}</span>
			</Each>
		</Each>
	</section>
}
`))
	if err == nil {
		t.Fatal("Lower accepted a selector rooted at a typed legacy index binding")
	}
	if want := "typed legacy component Roster cannot use index binding i in a selector"; !strings.Contains(err.Error(), want) {
		t.Fatalf("Lower error = %v, want substring %q", err, want)
	}
}

func TestLowerLeavesUntypedLegacyEachDynamic(t *testing.T) {
	lowerTypedLegacySource(t, `package app

func Roster(props any) Node {
	return <ul><Each of={props.Players} as="player"><li>{player.name}</li></Each></ul>
}
`)
}

func TestLowerLeavesShadowedLegacyEachDynamic(t *testing.T) {
	lowerTypedLegacySource(t, `package app

func Each(props any) Node {
	return <span>{props.label}</span>
}

type RosterProps struct {
	Players []struct{ Name string }
}

func Roster(props RosterProps) Node {
	return <ul><Each of={props.Players} as="player"><li>{player.name}</li></Each></ul>
}
`)
}
