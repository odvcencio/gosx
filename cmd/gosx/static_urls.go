package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// staticRewriteOptions carries what the static URL rewrite needs beyond the
// route path: the mount prefix, a way to tell published files from routes, and
// whether the page needs the WASM runtime at all.
type staticRewriteOptions struct {
	// Prefix is the build-time mount ("/games/x"), or "".
	Prefix string
	// AssetExists reports whether a mount-relative path ("/models/a.glb") is a
	// file in the output. Strings in JSON contracts that name such a file are
	// rewritten; route paths and prose are not.
	AssetExists func(rel string) bool
}

func (o staticRewriteOptions) rewritesJSONString(s string) bool {
	if !strings.HasPrefix(s, "/") || strings.HasPrefix(s, "//") || strings.HasPrefix(s, "/\\") {
		return false
	}
	p := s
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	rel := strings.TrimPrefix(p, o.Prefix)
	if o.Prefix != "" && !strings.HasPrefix(p, o.Prefix+"/") {
		rel = p
	}
	if strings.HasPrefix(rel, "/gosx/") {
		return true
	}
	return o.AssetExists != nil && o.AssetExists(rel)
}

// rewriteStaticJSON turns published-file URLs in a GoSX JSON contract
// (#gosx-manifest, #gosx-document) into document-relative URLs. It edits string
// literals in place, so key order and number formatting stay as rendered.
func rewriteStaticJSON(routePath, text string, opts staticRewriteOptions) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '"' {
			out.WriteByte(text[i])
			i++
			continue
		}
		j := i + 1
		for j < len(text) && text[j] != '"' {
			if text[j] == '\\' {
				j++
			}
			j++
		}
		if j >= len(text) {
			out.WriteString(text[i:])
			break
		}
		literal := text[i : j+1]
		var value string
		if err := json.Unmarshal([]byte(literal), &value); err == nil && opts.rewritesJSONString(value) {
			target := value
			if opts.Prefix != "" && !strings.HasPrefix(value, opts.Prefix+"/") {
				target = opts.Prefix + value
			}
			query := ""
			if k := strings.IndexAny(target, "?#"); k >= 0 {
				target, query = target[:k], target[k:]
			}
			rewritten := relativeStaticExportPath(routePath, target) + query
			encoded, _ := json.Marshal(rewritten)
			out.Write(encoded)
		} else {
			out.WriteString(literal)
		}
		i = j + 1
	}
	return out.String()
}

// dropDormantRuntimeBundles empties the manifest "bundles" map of a page whose
// capabilities need no WASM. The shipped client never reads that map for such a
// page, and keeping its WASM path would make the exporter stage a runtime
// binary (2 MB or more) that no request ever fetches.
func dropDormantRuntimeBundles(manifestJSON string) string {
	dec := json.NewDecoder(strings.NewReader(manifestJSON))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return manifestJSON
	}
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return manifestJSON
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return manifestJSON
		}
		if key, _ := keyTok.(string); key == "bundles" {
			end := int(dec.InputOffset())
			start := end - len(raw)
			if start < 0 || end > len(manifestJSON) || !strings.Contains(string(raw), ".wasm") {
				return manifestJSON
			}
			return manifestJSON[:start] + "{}" + manifestJSON[end:]
		}
	}
	return manifestJSON
}

// staticServerEndpoints lists root-absolute `/_gosx/` references left in a
// static page. A static bundle has no server to answer them.
func staticServerEndpoints(page string) []string {
	seen := map[string]bool{}
	rest := page
	for {
		i := strings.Index(rest, "/_gosx/")
		if i < 0 {
			break
		}
		end := i
		for end < len(rest) && !strings.ContainsRune("\"'<> \t\r\n\\)", rune(rest[end])) {
			end++
		}
		seen[rest[i:end]] = true
		rest = rest[end:]
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// auditStaticServerEndpoints walks the static tree and reports every HTML file
// that still names a `/_gosx/` endpoint.
func auditStaticServerEndpoints(root string) ([]string, error) {
	var findings []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if hits := staticServerEndpoints(string(data)); len(hits) > 0 {
			rel, _ := filepath.Rel(root, p)
			findings = append(findings, fmt.Sprintf("%s: %s", filepath.ToSlash(rel), strings.Join(hits, ", ")))
		}
		return nil
	})
	return findings, err
}

// checkStaticServerEndpoints fails an export, and warns on a production
// build, when a static page names a `/_gosx/` endpoint.
func checkStaticServerEndpoints(staticDir string, fatal bool) error {
	findings, err := auditStaticServerEndpoints(staticDir)
	if err != nil {
		return err
	}
	if len(findings) == 0 {
		return nil
	}
	msg := "static pages reference server endpoints that a static host cannot answer:\n  " + strings.Join(findings, "\n  ")
	if fatal {
		return errors.New(msg)
	}
	fmt.Fprintln(os.Stderr, "gosx build: warning: "+msg)
	return nil
}

// writeStaticZip writes the bundle at dir as one archive with index.html at the
// archive root. Compression sidecars (.gz, .br) stay out: a platform that
// wants them can compress, and they would double the upload size.
func writeStaticZip(dir, zipPath string) (int, error) {
	if err := os.MkdirAll(filepath.Dir(zipPath), 0755); err != nil {
		return 0, err
	}
	out, err := os.Create(zipPath)
	if err != nil {
		return 0, fmt.Errorf("create %s: %w", zipPath, err)
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	var files []string
	err = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if strings.HasSuffix(p, ".gz") || strings.HasSuffix(p, ".br") {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return 0, err
	}
	sort.Strings(files)
	for _, rel := range files {
		hdr := &zip.FileHeader{Name: rel, Method: zip.Deflate}
		hdr.Modified = time.Unix(0, 0).UTC() // reproducible archives
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return 0, err
		}
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			return 0, err
		}
		if _, err := w.Write(data); err != nil {
			return 0, err
		}
	}
	if err := zw.Close(); err != nil {
		return 0, err
	}
	return len(files), out.Close()
}
