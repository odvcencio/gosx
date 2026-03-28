package watch

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/odvcencio/gosx/route"
	"github.com/odvcencio/gosx/server"
)

func init() {
	_, thisFile, _, _ := runtime.Caller(0)
	source := filepath.Join(filepath.Dir(thisFile), "[code]", "page.gsx")

	route.MustRegisterFileModule(route.FileModuleFor(source, route.FileModuleOptions{
		Load: func(ctx *route.RouteContext, page route.FilePage) (any, error) {
			code := strings.TrimSpace(ctx.Param("code"))
			if code == "" {
				code = "alpha"
			}
			return watchSnapshot(code), nil
		},
		Metadata: func(ctx *route.RouteContext, page route.FilePage, data any) (server.Metadata, error) {
			code := strings.TrimSpace(ctx.Param("code"))
			if code == "" {
				code = "alpha"
			}
			return server.Metadata{
				Title:       fmt.Sprintf("Room %s | Goetrope Watch", strings.ToUpper(code)),
				Description: "Server-rendered room snapshot with normalized queue titles and cached subtitle state.",
			}, nil
		},
	}))
}

func watchSnapshot(code string) map[string]any {
	code = strings.TrimSpace(strings.ToLower(code))
	if code == "" {
		code = "alpha"
	}

	roomTitles := map[string]string{
		"alpha": "Room Alpha",
		"beta":  "Room Beta",
		"gamma": "Room Gamma",
	}
	roomTitle := roomTitles[code]
	if roomTitle == "" {
		roomTitle = fmt.Sprintf("Room %s", strings.ToUpper(code))
	}

	return map[string]any{
		"room": map[string]any{
			"code":           code,
			"title":          roomTitle,
			"tagline":        "Canonical room clock stays on the server.",
			"watch_href":     fmt.Sprintf("/watch/%s", code),
			"viewer_count":   "142",
			"live_state":     "Playing now",
			"subtitle_state": "Subtitles prewarmed",
		},
		"stream": map[string]any{
			"status":  "Healthy",
			"quality": "720p",
			"latency": "1.2s behind edge",
			"buffer":  "Durable",
		},
		"current_item": map[string]any{
			"display_title": "Alien: The Director's Cut",
			"title":         "Alien",
			"year":          "1979",
			"runtime":       "1h 57m",
			"synopsis":      "The page is server-rendered first, so the player shell only has to occupy a narrow transport lane.",
			"position":      "2:43:17",
			"chapter":       "The Signal",
		},
		"player": map[string]any{
			"title":            "Transport shell",
			"subtitle":         "Reserve client work for media transport only.",
			"badge":            "Server-first",
			"primary_action":   "Resume",
			"secondary_action": "Subtitle warmup",
			"tertiary_action":  "Hold canonical position",
		},
		"subtitle_state": map[string]any{
			"label":  "Subtitles ready",
			"detail": "Text and bitmap cues were warmed before the room handoff.",
			"track":  "English SDH",
			"mode":   "Prewarmed cache",
			"queued": "5 upcoming cues",
		},
		"next_up": map[string]any{
			"display_title": "Aliens: Special Edition",
			"reason":        "Already cached and ready to advance.",
			"year":          "1986",
		},
		"queue": []map[string]any{
			{
				"index":          "01",
				"display_title":  "Alien",
				"title":          "Alien",
				"year":           "1979",
				"status":         "Playing",
				"subtitle_state": "Ready",
			},
			{
				"index":          "02",
				"display_title":  "Aliens: Special Edition",
				"title":          "Aliens",
				"year":           "1986",
				"status":         "Queued",
				"subtitle_state": "Cached",
			},
			{
				"index":          "03",
				"display_title":  "Alien 3",
				"title":          "Alien 3",
				"year":           "1992",
				"status":         "Warm",
				"subtitle_state": "Warm",
			},
			{
				"index":          "04",
				"display_title":  "Alien Resurrection",
				"title":          "Alien Resurrection",
				"year":           "1997",
				"status":         "Ready",
				"subtitle_state": "Ready",
			},
		},
		"moments": []map[string]any{
			{
				"label": "Canonical position",
				"value": "Held across reloads",
			},
			{
				"label": "Subtitle prep",
				"value": "Queued ahead of handoff",
			},
			{
				"label": "Stream durability",
				"value": "Dampens jitter before it grows",
			},
		},
	}
}
