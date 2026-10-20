package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"golang.org/x/net/html"
)

// Fetch generated sidecar CSS while the export router and its scoped assets
// are alive. Both export and production builds use this prerender path.
func stageExportFileCSS(client *http.Client, baseURL, outputDir, input string, staged map[string]bool, mounts ...exportMount) error {
	var mount exportMount
	if len(mounts) > 0 {
		mount = mounts[0]
	}
	tokenizer := html.NewTokenizer(strings.NewReader(input))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			if err := tokenizer.Err(); err != io.EOF {
				return fmt.Errorf("read export stylesheets: %w", err)
			}
			return nil
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if token.Data != "link" {
				continue
			}
			href, rel, sidecar := "", "", false
			for _, attr := range token.Attr {
				switch attr.Key {
				case "href":
					href = attr.Val
				case "rel":
					rel = attr.Val
				case "data-gosx-file-css":
					sidecar = true
				}
			}
			if !sidecar || rel != "stylesheet" {
				continue
			}
			ref, err := url.Parse(href)
			if err != nil || ref.Scheme != "" || ref.Host != "" || !strings.HasPrefix(ref.Path, "/") || path.Clean(ref.Path) != ref.Path {
				return fmt.Errorf("invalid export stylesheet URL %q", href)
			}
			if staged[ref.Path] {
				continue
			}
			if err := fetchExportFileCSS(client, baseURL+mount.upstreamURL(ref.String()), filepath.Join(outputDir, filepath.FromSlash(strings.TrimPrefix(ref.Path, "/")))); err != nil {
				return err
			}
			staged[ref.Path] = true
		}
	}
}

func fetchExportFileCSS(client *http.Client, source, destination string) error {
	response, err := client.Get(source)
	if err != nil {
		return fmt.Errorf("fetch export stylesheet: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch export stylesheet %s: HTTP %d", source, response.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return fmt.Errorf("create export stylesheet directory: %w", err)
	}
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return fmt.Errorf("read export stylesheet: %w", err)
	}
	if err := os.WriteFile(destination, data, 0644); err != nil {
		return fmt.Errorf("write export stylesheet: %w", err)
	}
	return nil
}
