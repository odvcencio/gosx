package dev

import (
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

// SelenaUpdate is one rebuilt Selena source for the browser. Materials holds
// one entry per material in the source: "material" is its name and the rest
// are the Scene3D material fields that carry shader code and layout
// (customVertex, customFragment, customVertexWGSL, customFragmentWGSL,
// specularFragmentGLSL, specularFragmentWGSL, shaderLayout).
type SelenaUpdate struct {
	Source    string           `json:"source"`
	Materials []map[string]any `json:"materials"`
}

// isSelenaChange reports whether every changed path is a .sel source.
func isSelenaChange(paths []string) bool {
	if len(paths) == 0 {
		return false
	}
	for _, path := range paths {
		if !strings.EqualFold(filepath.Ext(path), ".sel") {
			return false
		}
	}
	return true
}

// emitSelena handles a change to .sel sources only. SelenaChange rebuilds
// their artifacts; each result goes to the page as a "selena" event, and the
// dev script swaps the shaders of the live Scene3D materials in place, with
// no reload and no app restart. A compile error goes out as "build-error" and
// the page keeps drawing the last good shaders.
//
// The running app binary still embeds the old artifacts, so the server marks
// itself stale: the next full page load through the proxy rebuilds and
// restarts the app first (see refreshStaleApp).
func (s *Server) emitSelena(paths []string) {
	updates, err := s.SelenaChange(append([]string(nil), paths...))
	if err != nil {
		s.recordBuildError(err)
		s.logf("selena rebuild failed: %v", err)
		s.broadcast("build-error", map[string]any{
			"error": err.Error(),
			"time":  time.Now().Format(time.RFC3339Nano),
		})
		return
	}
	s.markBuilt()
	s.mu.Lock()
	s.appStale = s.appStale || len(updates) > 0
	s.mu.Unlock()
	for _, update := range updates {
		names := make([]string, 0, len(update.Materials))
		for _, m := range update.Materials {
			if name, ok := m["material"].(string); ok {
				names = append(names, name)
			}
		}
		s.logf("selena %s rebuilt, hot-swapping %s", update.Source, strings.Join(names, ", "))
		s.broadcast("selena", map[string]any{
			"source":    update.Source,
			"materials": update.Materials,
			"time":      time.Now().Format(time.RFC3339Nano),
		})
	}
}

// refreshStaleApp rebuilds and restarts the app before a page load when a
// Selena hot swap left it serving old embedded artifacts.
func (s *Server) refreshStaleApp(r *http.Request) {
	if r.Method != http.MethodGet || !isDocumentRequest(r) {
		return
	}
	s.mu.RLock()
	stale := s.appStale
	s.mu.RUnlock()
	if !stale {
		return
	}
	s.logf("selena artifacts changed since the app started; rebuilding before this page load")
	if err := s.runOnChange(); err != nil {
		s.recordBuildError(err)
		s.logf("rebuild after selena change failed: %v", err)
	}
}

func isDocumentRequest(r *http.Request) bool {
	if dest := r.Header.Get("Sec-Fetch-Dest"); dest != "" {
		return dest == "document"
	}
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}

// selenaSwapScript applies "selena" events to every mounted Scene3D: each
// mesh object whose Selena material has the rebuilt name receives the new
// shader fields through a SetMaterial command (kind 3), so the renderer
// compiles the new program and keeps the object's uniform values. A page
// with no matching object is left alone.
const selenaSwapScript = `<script data-gosx-dev-selena="true">(function(){if(window.__gosxDevSelena){return;}window.__gosxDevSelena=true;function swap(payload){var api=window.__gosx&&window.__gosx.scene3d;if(!api||typeof api.dispatchCommands!=="function"){return 0;}var byName={};(payload.materials||[]).forEach(function(m){if(m&&m.material){byName[m.material]=m;}});var swapped=0;document.querySelectorAll("[data-gosx-scene3d-mounted]").forEach(function(mount){var state=mount.__gosxScene3DState;if(!state||!state.objects||typeof state.objects.forEach!=="function"){return;}var commands=[];state.objects.forEach(function(object,id){var layout=object&&object.shaderLayout;if(!object||object.shaderBackend!=="selena"||!layout||!byName[layout.material]){return;}var next=byName[layout.material];var patch={customVertex:next.customVertex,customFragment:next.customFragment,customVertexWGSL:next.customVertexWGSL,customFragmentWGSL:next.customFragmentWGSL,shaderLayout:next.shaderLayout};if(object.specularFragmentGLSL||object.specularFragmentWGSL){patch.specularFragmentGLSL=next.specularFragmentGLSL||"";patch.specularFragmentWGSL=next.specularFragmentWGSL||"";}commands.push({kind:3,objectId:object.id||id,data:patch});});if(commands.length){swapped+=commands.length;api.dispatchCommands(mount,commands).catch(function(err){console.error("[gosx dev] selena hot-swap failed:",err);});}});return swapped;}window.__gosx_dev_selena_swap=swap;var source=window.__gosxDevEvents||new EventSource("/gosx/dev/events");source.addEventListener("selena",function(event){var payload;try{payload=JSON.parse(event.data||"{}");}catch(_){console.error("[gosx dev] bad selena payload");return;}var n=swap(payload);if(console.debug){console.debug("[gosx dev] selena "+payload.source+": swapped "+n+" object material(s)");}});})();</script>`
