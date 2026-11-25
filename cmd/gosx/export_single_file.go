package main

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"m31labs.dev/gosx/internal/mediatype"
)

// singleFileShell is the page that unpacks the payload in the browser. It is
// a plain HTML template so the loader reads as the JavaScript it is.
//
//go:embed singlefile/shell.html
var singleFileShell string

// singleFileResult describes what writeSingleFile packed.
type singleFileResult struct {
	Files int   // bundled files, not counting index.html
	Bytes int64 // size of the HTML file
	// OtherPages lists .html files other than index.html and 404.html. A
	// single file holds one page, so these are left out.
	OtherPages []string
}

// singleFilePayload is the JSON that the shell decodes. Assets map a path in
// the bundle to its MIME type and its base64 bytes.
type singleFilePayload struct {
	Page   string               `json:"page"`
	Assets map[string][2]string `json:"assets"`
}

// writeSingleFile packs the static folder dir into one HTML file at outPath.
// The output depends only on the bytes in dir: paths are sorted, the MIME
// types come from a fixed table, and the gzip header carries no timestamp.
func writeSingleFile(dir, outPath string) (singleFileResult, error) {
	var res singleFileResult
	pageBytes, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		return res, fmt.Errorf("single file: %w", err)
	}
	var rels []string
	err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch strings.ToLower(path.Ext(rel)) {
		case ".gz", ".br", ".map":
			return nil
		case ".html":
			if rel != "index.html" && rel != "404.html" {
				res.OtherPages = append(res.OtherPages, rel)
			}
			return nil
		}
		rels = append(rels, rel)
		return nil
	})
	if err != nil {
		return res, err
	}
	sort.Strings(rels)
	sort.Strings(res.OtherPages)

	payload := singleFilePayload{Page: string(pageBytes), Assets: make(map[string][2]string, len(rels))}
	for _, rel := range rels {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return res, err
		}
		payload.Assets[rel] = [2]string{singleFileMIME(rel), base64.StdEncoding.EncodeToString(data)}
	}
	res.Files = len(rels)

	// json.Marshal sorts map keys, so the payload bytes are stable.
	raw, err := json.Marshal(payload)
	if err != nil {
		return res, err
	}
	var packed bytes.Buffer
	gz, err := gzip.NewWriterLevel(&packed, gzip.BestCompression)
	if err != nil {
		return res, err
	}
	if _, err := gz.Write(raw); err != nil {
		return res, err
	}
	if err := gz.Close(); err != nil {
		return res, err
	}

	shell := renderSingleFileShell(string(pageBytes), base64.StdEncoding.EncodeToString(packed.Bytes()))
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return res, err
	}
	if err := os.WriteFile(outPath, []byte(shell), 0o644); err != nil {
		return res, err
	}
	res.Bytes = int64(len(shell))
	return res, nil
}

var (
	singleFileTagPattern  = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	singleFileAttrPattern = regexp.MustCompile(`(?is)([a-z][a-z0-9:-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
	singleFileTitle       = regexp.MustCompile(`(?is)<title\b[^>]*>(.*?)</title>`)
	singleFileHTMLTag     = regexp.MustCompile(`(?is)<html\b[^>]*>`)
	singleFileColor       = regexp.MustCompile(`^#(?:[0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
)

func singleFileAttrs(tag string) map[string]string {
	attrs := map[string]string{}
	for _, m := range singleFileAttrPattern.FindAllStringSubmatch(tag, -1) {
		value := m[2] + m[3] + m[4]
		attrs[strings.ToLower(m[1])] = html.UnescapeString(value)
	}
	return attrs
}

// renderSingleFileShell fills the shell with the page's own title, language,
// viewport and theme colour, so the loading screen looks like the app.
func renderSingleFileShell(page, packed string) string {
	lang, title := "en", ""
	viewport := "width=device-width, initial-scale=1"
	theme := ""
	if m := singleFileHTMLTag.FindString(page); m != "" {
		if v := strings.TrimSpace(singleFileAttrs(m)["lang"]); v != "" {
			lang = v
		}
	}
	if m := singleFileTitle.FindStringSubmatch(page); m != nil {
		title = strings.TrimSpace(html.UnescapeString(m[1]))
	}
	for _, tag := range singleFileTagPattern.FindAllString(page, -1) {
		attrs := singleFileAttrs(tag)
		switch strings.ToLower(attrs["name"]) {
		case "viewport":
			if attrs["content"] != "" {
				viewport = attrs["content"]
			}
		case "theme-color":
			if singleFileColor.MatchString(attrs["content"]) {
				theme = attrs["content"]
			}
		}
	}

	background, foreground := "#111111", "#f2f2f2"
	themeMeta := ""
	if theme != "" {
		background = theme
		themeMeta = `<meta name="theme-color" content="` + html.EscapeString(theme) + `">` + "\n"
		if singleFileIsLight(theme) {
			foreground = "#111111"
		}
	}
	status := "Opening…"
	if title != "" {
		status = "Opening " + title + "…"
	}
	return strings.NewReplacer(
		"__LANG__", html.EscapeString(lang),
		"__VIEWPORT__", html.EscapeString(viewport),
		"__THEME_META__", themeMeta,
		"__TITLE__", html.EscapeString(title),
		"__BACKGROUND__", background,
		"__FOREGROUND__", foreground,
		"__STATUS__", html.EscapeString(status),
		"__PACKAGE__", packed,
	).Replace(singleFileShell)
}

// singleFileIsLight reports whether a #rgb or #rrggbb colour needs dark text.
func singleFileIsLight(color string) bool {
	hex := strings.TrimPrefix(color, "#")
	if len(hex) == 3 {
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	n, err := strconv.ParseUint(hex, 16, 32)
	if err != nil {
		return false
	}
	r, g, b := float64(n>>16&0xff), float64(n>>8&0xff), float64(n&0xff)
	return 0.2126*r+0.7152*g+0.0722*b > 140
}

// singleFileMIME names a file's type from GoSX's fixed table, not from the
// host's MIME database, so the same bundle packs to the same bytes on every
// machine. The browser needs the right type for WebAssembly streaming,
// scripts, styles and fonts.
func singleFileMIME(rel string) string {
	if t := mediatype.ByExtension(path.Ext(rel)); t != "" {
		return t
	}
	return "application/octet-stream"
}
