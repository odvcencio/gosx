package watch

import (
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"

	"m31labs.dev/gosx/route"
	"m31labs.dev/gosx/server"
)

type watchPlayer struct {
	Title           string
	Subtitle        string
	Badge           string
	PrimaryAction   string
	SecondaryAction string
	TertiaryAction  string
}

type watchQueueItem struct {
	Index         string
	DisplayTitle  string
	Title         string
	Year          string
	Status        string
	SubtitleState string
}

type watchMoment struct {
	Label string
	Value string
}

func init() {
	_, thisFile, _, _ := runtime.Caller(0)
	source := filepath.Join(filepath.Dir(thisFile), "[code]", "page.gsx")

	if err := route.RegisterFileModule(route.FileModuleFor(source, route.FileModuleOptions{
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
				Title:       server.Title{Default: fmt.Sprintf("Room %s | Goetrope Watch", strings.ToUpper(code))},
				Description: "Server-rendered room snapshot with normalized queue titles and cached subtitle state.",
			}, nil
		},
	})); err != nil {
		log.Fatal(err)
	}
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
		"player": watchPlayer{
			Title:           "Transport shell",
			Subtitle:        "Reserve client work for media transport only.",
			Badge:           "Server-first",
			PrimaryAction:   "Resume",
			SecondaryAction: "Subtitle warmup",
			TertiaryAction:  "Hold canonical position",
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
		"queue": []watchQueueItem{
			{
				Index:         "01",
				DisplayTitle:  "Alien",
				Title:         "Alien",
				Year:          "1979",
				Status:        "Playing",
				SubtitleState: "Ready",
			},
			{
				Index:         "02",
				DisplayTitle:  "Aliens: Special Edition",
				Title:         "Aliens",
				Year:          "1986",
				Status:        "Queued",
				SubtitleState: "Cached",
			},
			{
				Index:         "03",
				DisplayTitle:  "Alien 3",
				Title:         "Alien 3",
				Year:          "1992",
				Status:        "Warm",
				SubtitleState: "Warm",
			},
			{
				Index:         "04",
				DisplayTitle:  "Alien Resurrection",
				Title:         "Alien Resurrection",
				Year:          "1997",
				Status:        "Ready",
				SubtitleState: "Ready",
			},
		},
		"moments": []watchMoment{
			{
				Label: "Canonical position",
				Value: "Held across reloads",
			},
			{
				Label: "Subtitle prep",
				Value: "Queued ahead of handoff",
			},
			{
				Label: "Stream durability",
				Value: "Dampens jitter before it grows",
			},
		},
	}
}
