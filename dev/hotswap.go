package dev

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/island/program"
)

// islandProgram is a freshly recompiled island ready to ship over the dev
// socket. Component is the island component name (the stable handle the client
// uses to find every live island built from this component); ProgramJSON is the
// program.EncodeJSON wire form the client feeds to __gosx_reload_program.
type islandProgram struct {
	Component   string
	ProgramJSON []byte
}

// emitChange is the dev-socket delivery seam: given the set of source files that
// changed, it decides between a hot program swap and a full page reload, runs
// the heavy rebuild/restart hook only when a reload is actually required, and
// broadcasts the matching SSE events.
//
// Classification (scoped to the Phase-0 hot-swap seam):
//   - If every changed path is an island .gsx that recompiles to one or more
//     island programs, broadcast one "program" event per island component and
//     do NOT reload, and do NOT run OnChange (no app rebuild/restart). The
//     client routes each "program" event to __gosx_reload_program, which
//     hot-swaps the live island in place (signal state intact) — no
//     window.location.reload(). Skipping the restart is what keeps the swap
//     sub-100ms and preserves running state.
//   - Any other change (.go / .css / .js, a deleted/unreadable .gsx, or a .gsx
//     that declares no islands) runs OnChange (rebuild assets + restart app)
//     and then broadcasts a full "reload". Server, route, and Go changes need
//     the app rebuilt/restarted, which only a reload picks up.
//   - A .gsx that fails to compile broadcasts "build-error" and reloads, so the
//     browser surfaces the failure rather than silently hot-swapping stale code.
//
// Phase-1 follow-up: emitChange keys program events by component name and lets
// the client fan out across window.__gosx.islands (which maps runtime islandID
// -> component). A fully-general changed-file -> runtime-islandID mapping (and a
// paired "patch"-only path) is broader than the Phase-0 seam and is deferred.
func (s *Server) emitChange(paths []string) {
	programs, fullReload, compileErr := s.classifyChange(paths)

	if compileErr != nil {
		s.recordBuildError(compileErr)
		s.logf("island recompile failed, falling back to reload: %v", compileErr)
		s.broadcast("build-error", map[string]any{
			"error": compileErr.Error(),
			"time":  time.Now().Format(time.RFC3339Nano),
		})
		s.broadcastReload("build_error")
		return
	}

	if fullReload || len(programs) == 0 {
		if err := s.runOnChange(); err != nil {
			s.recordBuildError(err)
			s.logf("change handling failed: %v", err)
			s.broadcast("build-error", map[string]any{
				"error": err.Error(),
				"time":  time.Now().Format(time.RFC3339Nano),
			})
			return
		}
		s.markBuilt()
		s.logf("change detected, reloading clients")
		s.broadcastReload("file_change")
		return
	}

	// Island-only change: hot-swap in place, no rebuild/restart, no reload.
	s.markBuilt()
	for _, prog := range programs {
		// Refresh the staged asset so a later hard refresh (or a freshly
		// mounted island) fetches the same fresh bytecode the live page just
		// hot-swapped to. Cheap (one file write) and keeps disk in step with
		// the socket without paying for a full rebuild/restart.
		s.restageIslandProgram(prog)
		s.logf("island %s changed, hot-swapping program (%d bytes)", prog.Component, len(prog.ProgramJSON))
		s.broadcast("program", map[string]any{
			"component": prog.Component,
			"format":    "json",
			"program":   string(prog.ProgramJSON),
			"time":      time.Now().Format(time.RFC3339Nano),
		})
	}
}

// restageIslandProgram rewrites the staged build/islands/<Component>.json so the
// on-disk dev asset matches the bytecode just pushed over the socket. It is a
// no-op when no BuildDir is configured (e.g. unit tests) or when the write
// fails — a stale-on-disk asset only affects a manual hard refresh, never the
// live hot-swap, so a write error is logged rather than surfaced as a reload.
func (s *Server) restageIslandProgram(prog islandProgram) {
	if strings.TrimSpace(s.BuildDir) == "" {
		return
	}
	islandDir := filepath.Join(s.BuildDir, "islands")
	if err := os.MkdirAll(islandDir, 0o755); err != nil {
		s.logf("restage island %s: %v", prog.Component, err)
		return
	}
	path := filepath.Join(islandDir, prog.Component+".json")
	if err := os.WriteFile(path, prog.ProgramJSON, 0o644); err != nil {
		s.logf("restage island %s: %v", prog.Component, err)
	}
}

// runOnChange invokes the rebuild/restart hook when one is configured. A nil
// hook (as in unit tests, or a server wired only for asset serving) is a no-op.
func (s *Server) runOnChange() error {
	if s.OnChange == nil {
		return nil
	}
	return s.OnChange()
}

func (s *Server) recordBuildError(err error) {
	s.mu.Lock()
	s.lastError = err.Error()
	s.mu.Unlock()
}

func (s *Server) markBuilt() {
	s.mu.Lock()
	s.lastBuild = time.Now()
	s.lastError = ""
	s.mu.Unlock()
}

func (s *Server) broadcastReload(reason string) {
	s.broadcast("reload", map[string]any{
		"reason": reason,
		"time":   time.Now().Format(time.RFC3339Nano),
	})
}

// classifyChange inspects the changed paths and returns the island programs to
// hot-swap, whether a full reload is required instead, and any compile error.
//
// fullReload is set the moment a non-island change is seen (or an island .gsx
// yields no island components) — a single Go/CSS/JS edit reloads the whole page
// because those affect server render, routing, or non-island assets.
func (s *Server) classifyChange(paths []string) (programs []islandProgram, fullReload bool, err error) {
	for _, path := range paths {
		if !strings.EqualFold(filepath.Ext(path), ".gsx") {
			// Non-island source: server/route/Go/CSS/JS change → full reload.
			return nil, true, nil
		}
		if _, statErr := os.Stat(path); statErr != nil {
			// A removed/renamed .gsx can't be hot-swapped — the island it
			// declared is gone. Reload so the page reflects its removal.
			return nil, true, nil
		}
		compiled, compileErr := compileIslandPrograms(path)
		if compileErr != nil {
			return nil, false, compileErr
		}
		if len(compiled) == 0 {
			// A .gsx with no island components (e.g. a server page template)
			// still needs a full reload — there is nothing to hot-swap.
			return nil, true, nil
		}
		programs = append(programs, compiled...)
	}
	return programs, false, nil
}

// compileIslandPrograms recompiles a single .gsx file and returns the JSON wire
// program for every island component it declares. It mirrors the per-island
// lowering compileDevIslands performs at full-build time, so a hot-swapped
// program is byte-identical to what a fresh page load would fetch.
func compileIslandPrograms(path string) ([]islandProgram, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	irProg, err := gosx.Compile(source)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", path, err)
	}

	var out []islandProgram
	for i, comp := range irProg.Components {
		if !comp.IsIsland {
			continue
		}
		isl, err := ir.LowerIsland(irProg, i)
		if err != nil {
			return nil, fmt.Errorf("lower island %s in %s: %w", comp.Name, path, err)
		}
		data, err := program.EncodeJSON(isl)
		if err != nil {
			return nil, fmt.Errorf("encode island %s: %w", comp.Name, err)
		}
		out = append(out, islandProgram{Component: comp.Name, ProgramJSON: data})
	}
	return out, nil
}
