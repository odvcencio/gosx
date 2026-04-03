package route

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/odvcencio/gosx/server"
)

var fileMetadataCache sync.Map

func addFileMetadata(ctx *RouteContext, files ...string) {
	if ctx == nil {
		return
	}
	for _, file := range files {
		meta, ok := fileMetadata(file)
		if !ok {
			continue
		}
		ctx.SetMetadata(meta)
	}
}

func fileMetadata(file string) (server.Metadata, bool) {
	metaPath := sidecarMetadataPath(file)
	if metaPath == "" {
		return server.Metadata{}, false
	}
	if cached, ok := fileMetadataCache.Load(metaPath); ok {
		meta, _ := cached.(server.Metadata)
		return meta, !isZeroMetadata(meta)
	}

	data, err := os.ReadFile(metaPath)
	if err != nil {
		fileMetadataCache.Store(metaPath, server.Metadata{})
		return server.Metadata{}, false
	}

	var meta server.Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		fileMetadataCache.Store(metaPath, server.Metadata{})
		return server.Metadata{}, false
	}
	fileMetadataCache.Store(metaPath, meta)
	return meta, !isZeroMetadata(meta)
}

func sidecarMetadataPath(file string) string {
	file = strings.TrimSpace(file)
	if file == "" {
		return ""
	}
	ext := filepath.Ext(file)
	if ext == "" {
		return ""
	}
	candidate := strings.TrimSuffix(file, ext) + ".meta.json"
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate
	}
	return ""
}

func isZeroMetadata(meta server.Metadata) bool {
	return isZeroMetadataTitle(meta.Title) &&
		strings.TrimSpace(meta.Description) == "" &&
		strings.TrimSpace(meta.MetadataBase) == "" &&
		(meta.Alternates == nil || isZeroMetadataAlternates(*meta.Alternates)) &&
		(meta.Robots == nil || isZeroMetadataRobots(*meta.Robots)) &&
		(meta.Icons == nil || isZeroMetadataIcons(*meta.Icons)) &&
		strings.TrimSpace(meta.Manifest) == "" &&
		(meta.Verification == nil || isZeroMetadataVerification(*meta.Verification)) &&
		len(meta.ThemeColor) == 0 &&
		(meta.OpenGraph == nil || isZeroMetadataOpenGraph(*meta.OpenGraph)) &&
		(meta.Twitter == nil || isZeroMetadataTwitter(*meta.Twitter)) &&
		len(meta.JSONLD) == 0 &&
		len(meta.Other) == 0 &&
		len(meta.Links) == 0
}

func isZeroMetadataTitle(title server.Title) bool {
	return strings.TrimSpace(title.Absolute) == "" &&
		strings.TrimSpace(title.Default) == "" &&
		strings.TrimSpace(title.Template) == ""
}

func isZeroMetadataAlternates(alternates server.Alternates) bool {
	return strings.TrimSpace(alternates.Canonical) == "" &&
		len(alternates.Languages) == 0 &&
		len(alternates.Media) == 0 &&
		len(alternates.Types) == 0
}

func isZeroMetadataRobots(robots server.Robots) bool {
	return robots.Index == nil &&
		robots.Follow == nil &&
		!robots.NoArchive &&
		!robots.NoImageAI &&
		!robots.NoTranslate &&
		(robots.GoogleBot == nil || (robots.GoogleBot.Index == nil &&
			robots.GoogleBot.Follow == nil &&
			robots.GoogleBot.MaxSnippet == 0 &&
			strings.TrimSpace(robots.GoogleBot.MaxImagePreview) == "" &&
			robots.GoogleBot.MaxVideoPreview == 0))
}

func isZeroMetadataIcons(icons server.Icons) bool {
	return len(icons.Icon) == 0 &&
		len(icons.Shortcut) == 0 &&
		len(icons.Apple) == 0 &&
		len(icons.Other) == 0
}

func isZeroMetadataVerification(verification server.Verification) bool {
	return strings.TrimSpace(verification.Google) == "" &&
		strings.TrimSpace(verification.Bing) == "" &&
		strings.TrimSpace(verification.Yandex) == "" &&
		strings.TrimSpace(verification.Yahoo) == "" &&
		len(verification.Other) == 0
}

func isZeroMetadataOpenGraph(openGraph server.OpenGraph) bool {
	return strings.TrimSpace(openGraph.Type) == "" &&
		strings.TrimSpace(openGraph.URL) == "" &&
		strings.TrimSpace(openGraph.SiteName) == "" &&
		strings.TrimSpace(openGraph.Locale) == "" &&
		strings.TrimSpace(openGraph.Title) == "" &&
		strings.TrimSpace(openGraph.Description) == "" &&
		len(openGraph.Images) == 0 &&
		openGraph.Article == nil
}

func isZeroMetadataTwitter(twitter server.Twitter) bool {
	return strings.TrimSpace(twitter.Card) == "" &&
		strings.TrimSpace(twitter.Site) == "" &&
		strings.TrimSpace(twitter.Creator) == "" &&
		strings.TrimSpace(twitter.Title) == "" &&
		strings.TrimSpace(twitter.Description) == "" &&
		len(twitter.Images) == 0
}
