package server

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/engine"
)

const defaultVideoEngineName = "GoSXVideo"

// VideoSource describes one candidate media source for a video element.
type VideoSource struct {
	Src   string `json:"src,omitempty"`
	Type  string `json:"type,omitempty"`
	Media string `json:"media,omitempty"`
}

// VideoTrack describes one text track for video playback.
type VideoTrack struct {
	ID       string `json:"id,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Src      string `json:"src,omitempty"`
	SrcLang  string `json:"srclang,omitempty"`
	Language string `json:"language,omitempty"`
	Label    string `json:"label,omitempty"`
	Title    string `json:"title,omitempty"`
	Name     string `json:"name,omitempty"`
	Default  bool   `json:"default,omitempty"`
	Forced   bool   `json:"forced,omitempty"`
}

// VideoProps configures both the server-rendered baseline <video> element and
// the built-in video engine runtime contract.
type VideoProps struct {
	EngineName     string         `json:"-"`
	Src            string         `json:"src,omitempty"`
	Sources        []VideoSource  `json:"sources,omitempty"`
	Poster         string         `json:"poster,omitempty"`
	Preload        string         `json:"preload,omitempty"`
	CrossOrigin    string         `json:"crossOrigin,omitempty"`
	AutoPlay       bool           `json:"autoPlay,omitempty"`
	Controls       bool           `json:"controls,omitempty"`
	Loop           bool           `json:"loop,omitempty"`
	Muted          bool           `json:"muted,omitempty"`
	PlaysInline    bool           `json:"playsInline,omitempty"`
	Width          int            `json:"width,omitempty"`
	Height         int            `json:"height,omitempty"`
	Volume         float64        `json:"volume,omitempty"`
	Rate           float64        `json:"rate,omitempty"`
	Sync           string         `json:"sync,omitempty"`
	SyncMode       string         `json:"syncMode,omitempty"`
	SyncStrategy   string         `json:"syncStrategy,omitempty"`
	HLS            map[string]any `json:"hls,omitempty"`
	HLSConfig      map[string]any `json:"hlsConfig,omitempty"`
	SubtitleBase   string         `json:"subtitleBase,omitempty"`
	SubtitleTrack  string         `json:"subtitleTrack,omitempty"`
	SubtitleTracks []VideoTrack   `json:"subtitleTracks,omitempty"`
}

// Video renders a server-side <video> baseline with optional <source> and
// <track> children so pages remain useful before bootstrap upgrades them.
func Video(props VideoProps, args ...any) gosx.Node {
	props = normalizeVideoProps(props)
	renderArgs := []any{
		gosx.Attrs(
			gosx.Attr("data-gosx-video-fallback", "true"),
		),
	}
	videoAttrs := []any{}
	if src := videoBaselineSrc(props); src != "" {
		videoAttrs = append(videoAttrs, gosx.Attr("src", src))
	}
	if poster := strings.TrimSpace(props.Poster); poster != "" {
		videoAttrs = append(videoAttrs, gosx.Attr("poster", AssetURL(poster)))
	}
	if preload := strings.TrimSpace(props.Preload); preload != "" {
		videoAttrs = append(videoAttrs, gosx.Attr("preload", preload))
	}
	if crossOrigin := strings.TrimSpace(props.CrossOrigin); crossOrigin != "" {
		videoAttrs = append(videoAttrs, gosx.Attr("crossorigin", crossOrigin))
	}
	if props.Width > 0 {
		videoAttrs = append(videoAttrs, gosx.Attr("width", props.Width))
	}
	if props.Height > 0 {
		videoAttrs = append(videoAttrs, gosx.Attr("height", props.Height))
	}
	if props.AutoPlay {
		videoAttrs = append(videoAttrs, gosx.BoolAttr("autoplay"))
	}
	if props.Controls {
		videoAttrs = append(videoAttrs, gosx.BoolAttr("controls"))
	}
	if props.Loop {
		videoAttrs = append(videoAttrs, gosx.BoolAttr("loop"))
	}
	if props.Muted {
		videoAttrs = append(videoAttrs, gosx.BoolAttr("muted"))
	}
	if props.PlaysInline {
		videoAttrs = append(videoAttrs, gosx.BoolAttr("playsinline"))
	}
	renderArgs = append(renderArgs, gosx.Attrs(videoAttrs...))
	extraChildren := []gosx.Node{}
	for _, arg := range args {
		switch value := arg.(type) {
		case gosx.AttrList:
			renderArgs = append(renderArgs, value)
		case gosx.Node:
			extraChildren = append(extraChildren, value)
		}
	}

	children := []any{}
	for _, source := range props.Sources {
		if src := strings.TrimSpace(source.Src); src != "" {
			sourceAttrs := []any{
				gosx.Attr("src", AssetURL(src)),
			}
			if typ := strings.TrimSpace(source.Type); typ != "" {
				sourceAttrs = append(sourceAttrs, gosx.Attr("type", typ))
			}
			if media := strings.TrimSpace(source.Media); media != "" {
				sourceAttrs = append(sourceAttrs, gosx.Attr("media", media))
			}
			children = append(children, gosx.El("source", gosx.Attrs(sourceAttrs...)))
		}
	}
	for _, track := range props.SubtitleTracks {
		trackSrc := videoTrackSource(track, props.SubtitleBase)
		if trackSrc == "" {
			continue
		}
		trackAttrs := []any{
			gosx.Attr("src", AssetURL(trackSrc)),
			gosx.Attr("kind", normalizeVideoTrackKind(track.Kind)),
		}
		if lang := normalizeVideoTrackLang(track); lang != "" {
			trackAttrs = append(trackAttrs, gosx.Attr("srclang", lang))
		}
		if label := normalizeVideoTrackLabel(track); label != "" {
			trackAttrs = append(trackAttrs, gosx.Attr("label", label))
		}
		if track.Default {
			trackAttrs = append(trackAttrs, gosx.BoolAttr("default"))
		}
		children = append(children, gosx.El("track", gosx.Attrs(trackAttrs...)))
	}
	renderArgs = append(renderArgs, children...)
	for _, child := range extraChildren {
		renderArgs = append(renderArgs, child)
	}
	return gosx.El("video", renderArgs...)
}

// VideoEngineConfig builds the engine.Config used by the built-in video engine.
func VideoEngineConfig(props VideoProps) engine.Config {
	props = normalizeVideoProps(props)
	return engine.Config{
		Name:         firstNonEmptyVideoString(props.EngineName, defaultVideoEngineName),
		Kind:         engine.KindVideo,
		Capabilities: []engine.Capability{engine.CapVideo, engine.CapFetch, engine.CapAudio},
		Props:        marshalVideoProps(props),
	}
}

// Video renders a built-in video engine with a server-rendered <video>
// baseline that bootstrap upgrades in place.
func (r *PageRuntime) Video(props VideoProps, args ...any) gosx.Node {
	fallback := Video(props, args...)
	return r.Engine(VideoEngineConfig(props), fallback)
}

// Video renders a built-in video engine for the current page.
func (s *PageState) Video(props VideoProps, args ...any) gosx.Node {
	if s == nil {
		return Video(props, args...)
	}
	return s.Runtime().Video(props, args...)
}

func marshalVideoProps(props VideoProps) json.RawMessage {
	data, err := json.Marshal(normalizeVideoProps(props))
	if err != nil {
		return nil
	}
	return data
}

func videoPropsFromValue(value any) (VideoProps, bool) {
	if value == nil {
		return VideoProps{}, false
	}
	data, err := json.Marshal(value)
	if err != nil {
		return VideoProps{}, false
	}
	var props VideoProps
	if err := json.Unmarshal(data, &props); err != nil {
		return VideoProps{}, false
	}
	return normalizeVideoProps(props), true
}

func normalizeVideoProps(props VideoProps) VideoProps {
	props.Src = AssetURL(props.Src)
	props.Poster = AssetURL(props.Poster)
	props.Preload = strings.TrimSpace(props.Preload)
	props.CrossOrigin = strings.TrimSpace(props.CrossOrigin)
	props.Sync = AssetURL(props.Sync)
	props.SyncMode = strings.TrimSpace(props.SyncMode)
	props.SyncStrategy = strings.TrimSpace(props.SyncStrategy)
	props.SubtitleBase = AssetURL(props.SubtitleBase)
	props.SubtitleTrack = strings.TrimSpace(props.SubtitleTrack)
	normalizedSources := make([]VideoSource, 0, len(props.Sources))
	for _, source := range props.Sources {
		source.Src = AssetURL(source.Src)
		source.Type = strings.TrimSpace(source.Type)
		source.Media = strings.TrimSpace(source.Media)
		if source.Src == "" {
			continue
		}
		normalizedSources = append(normalizedSources, source)
	}
	props.Sources = normalizedSources
	normalizedTracks := make([]VideoTrack, 0, len(props.SubtitleTracks))
	for _, track := range props.SubtitleTracks {
		track.ID = strings.TrimSpace(track.ID)
		track.Kind = normalizeVideoTrackKind(track.Kind)
		track.Src = AssetURL(track.Src)
		track.SrcLang = strings.TrimSpace(track.SrcLang)
		track.Language = strings.TrimSpace(track.Language)
		track.Label = strings.TrimSpace(track.Label)
		track.Title = strings.TrimSpace(track.Title)
		track.Name = strings.TrimSpace(track.Name)
		normalizedTracks = append(normalizedTracks, track)
	}
	props.SubtitleTracks = normalizedTracks
	return props
}

func videoBaselineSrc(props VideoProps) string {
	if len(props.Sources) > 0 {
		return ""
	}
	if props.Src == "" {
		return ""
	}
	return AssetURL(props.Src)
}

func videoTrackSource(track VideoTrack, subtitleBase string) string {
	if src := strings.TrimSpace(track.Src); src != "" {
		return src
	}
	subtitleBase = strings.TrimSpace(subtitleBase)
	if subtitleBase == "" || strings.TrimSpace(track.ID) == "" {
		return ""
	}
	return strings.TrimRight(subtitleBase, "/") + "/" + strings.TrimSpace(track.ID) + ".vtt"
}

func normalizeVideoTrackLabel(track VideoTrack) string {
	for _, value := range []string{track.Label, track.Title, track.Name, track.ID} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func normalizeVideoTrackLang(track VideoTrack) string {
	for _, value := range []string{track.SrcLang, track.Language} {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func normalizeVideoTrackKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "captions", "descriptions", "chapters", "metadata":
		return strings.ToLower(strings.TrimSpace(kind))
	default:
		return "subtitles"
	}
}

func firstNonEmptyVideoString(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}

func formatVideoFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
