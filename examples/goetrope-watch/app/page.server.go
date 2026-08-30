package watch

import (
	"fmt"
	"log"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

type landingHighlight struct {
	Kicker string
	Title  string
	Body   string
}

type landingRoom struct {
	Code          string
	Title         string
	DisplayTitle  string
	Status        string
	SubtitleState string
}

func init() {
	if err := route.RegisterFileModuleHere(route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			return landingSnapshot(), nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			return server.Metadata{
				Title:       server.Title{Default: "Goetrope Watch Surface | GoSX"},
				Description: "An isolated, server-driven prototype for the Goetrope viewer on GoSX.",
			}, nil
		},
	}); err != nil {
		log.Fatal(err)
	}
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
		"highlights": []landingHighlight{
			{
				Kicker: "1",
				Title:  "Canonical state stays on the server.",
				Body:   "The room snapshot and normalized queue titles are loaded by the page module, not assembled inside a sprawling client loop.",
			},
			{
				Kicker: "2",
				Title:  "Playback becomes a narrow transport shell.",
				Body:   "The page can reserve a tiny interactive island later, but the document itself remains the source of truth for layout and copy.",
			},
			{
				Kicker: "3",
				Title:  "Subtitles warm in the background.",
				Body:   "A watch room can advertise subtitle cache status immediately so the user never stares at a blank loading overlay.",
			},
		},
		"rooms": []landingRoom{
			{
				Code:          "alpha",
				Title:         "Room Alpha",
				DisplayTitle:  "Alien",
				Status:        "Playing",
				SubtitleState: "Prewarmed",
			},
			{
				Code:          "beta",
				Title:         "Room Beta",
				DisplayTitle:  "Aliens: Special Edition",
				Status:        "Queued",
				SubtitleState: "Cached",
			},
			{
				Code:          "gamma",
				Title:         "Room Gamma",
				DisplayTitle:  "Alien 3",
				Status:        "Ready",
				SubtitleState: "Warm",
			},
		},
	}
}

func openRoomHref(code string) string {
	return fmt.Sprintf("/watch/%s", code)
}
