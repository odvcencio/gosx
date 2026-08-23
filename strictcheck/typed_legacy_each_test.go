package strictcheck

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckFileAcceptsTypedLegacyEachExportedSelectors(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main

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

component Page() {
	return <main>ok</main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile rejected exported typed legacy Each selectors: %v", err)
	}
}

func TestCheckFileRejectsTypedLegacyEachSelectorAlias(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main

type Player struct {
	Name    string
	NFLTeam string
}

type RosterProps struct {
	Players []Player
}

func Roster(props RosterProps) Node {
	return <ul>
		<Each of={props.Players} as="player">
			<li class={player.nfl_team}>{player.Name}</li>
		</Each>
	</ul>
}

component Page() {
	return <main>ok</main>
}
`)
	err := CheckFile(context.Background(), path)
	if err == nil {
		t.Fatal("CheckFile accepted a non-exported/snake_case typed legacy Each selector")
	}
	for _, want := range []string{
		"typed legacy component Roster cannot resolve player.nfl_team",
		"struct Player declares no visible field nfl_team",
		"page.gsx:",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("CheckFile error = %v, want substring %q", err, want)
		}
	}
}
