package watch

import (
	"fmt"

	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

func init() {
	route.MustRegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return landingSnapshot(), nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       "Goetrope Watch Surface | GoSX",
				Description: "An isolated, server-driven prototype for the Goetrope viewer on GoSX.",
			}, nil
		},
	})
}

func landingSnapshot() map[string]any {
	return map[string]any{
		"hero": map[string]any{
			"eyebrow":    "Server-driven prototype",
			"title":      "Reimagine the viewer as a page the server fully understands.",
			"lede":       "Room state, queue titles, subtitle readiness, and next-up metadata are rendered before the browser ever needs to make a guess.",
			"watch_href": "/watch/alpha",
			"note":       "This is a Gosx scaffold only. It is not wired to production playback control.",
		},
		"highlights": []map[string]any{
			{
				"kicker": "1",
				"title":  "Canonical state stays on the server.",
				"body":   "The room snapshot and normalized queue titles are loaded by the page module, not assembled inside a sprawling client loop.",
			},
			{
				"kicker": "2",
				"title":  "Playback becomes a narrow transport shell.",
				"body":   "The page can reserve a tiny interactive island later, but the document itself remains the source of truth for layout and copy.",
			},
			{
				"kicker": "3",
				"title":  "Subtitles warm in the background.",
				"body":   "A watch room can advertise subtitle cache status immediately so the user never stares at a blank loading overlay.",
			},
		},
		"rooms": []map[string]any{
			{
				"code":           "alpha",
				"title":          "Room Alpha",
				"display_title":  "Alien",
				"status":         "Playing",
				"subtitle_state": "Prewarmed",
			},
			{
				"code":           "beta",
				"title":          "Room Beta",
				"display_title":  "Aliens: Special Edition",
				"status":         "Queued",
				"subtitle_state": "Cached",
			},
			{
				"code":           "gamma",
				"title":          "Room Gamma",
				"display_title":  "Alien 3",
				"status":         "Ready",
				"subtitle_state": "Warm",
			},
		},
	}
}

func openRoomHref(code string) string {
	return fmt.Sprintf("/watch/%s", code)
}
