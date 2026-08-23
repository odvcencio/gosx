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
