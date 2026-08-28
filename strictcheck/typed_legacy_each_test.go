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

func TestCheckFileAcceptsTypedLegacyMapProjection(t *testing.T) {
	dir := newTestModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, `package main

type DraftProps struct {
	Data map[string]any
}

func Draft(props DraftProps) Node {
	return <section>
		<Each of={props.Data.teams} as="team">
			<article data-seat={team.seat_id}>
				<If cond={team.ready}><strong>{team.name}</strong></If>
				<Each of={team.seat_controls} as="control">
					<button data-action={control.action}>{control.label}</button>
				</Each>
			</article>
		</Each>
		<Each of={props.Data.board} as="player"><span>{player.display_name}</span></Each>
		<Each of={props.Data.picks} as="pick"><span data-round={pick.round}>{pick.player_name}</span></Each>
	</section>
}

component Page() {
	return <main>ok</main>
}
`)
	if err := CheckFile(context.Background(), path); err != nil {
		t.Fatalf("CheckFile rejected a typed legacy map projection: %v", err)
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
