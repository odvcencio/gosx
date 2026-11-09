package main

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/net/html"
	"os"
	"path"
	"path/filepath"
	"regexp"
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

// staticServerEndpoints lists root-absolute `/_gosx/` references in the URL
// positions of a static page: attribute values and the string values of the
// GoSX JSON scripts. Page text that merely mentions an endpoint does not count.
// A reference that names a file the export staged (for example generated CSS
// under /_gosx/docs-css/) is not a server call, so exists reports those.
func staticServerEndpoints(page string, exists func(path string) bool) []string {
	seen := map[string]bool{}
	note := func(value string) {
		value = strings.TrimSpace(value)
		if !strings.HasPrefix(value, "/_gosx/") {
			return
		}
		p := value
		if i := strings.IndexAny(p, "?#"); i >= 0 {
			p = p[:i]
		}
		if exists != nil && exists(p) {
			return
		}
		seen[value] = true
	}
	z := html.NewTokenizer(strings.NewReader(page))
	inJSON := false
	for {
		switch z.Next() {
		case html.ErrorToken:
			out := make([]string, 0, len(seen))
			for v := range seen {
				out = append(out, v)
			}
			sort.Strings(out)
			return out
		case html.StartTagToken, html.SelfClosingTagToken:
			tok := z.Token()
			inJSON = false
			for _, a := range tok.Attr {
				if tok.Data == "script" && a.Key == "id" && (a.Val == "gosx-manifest" || a.Val == "gosx-document") {
					inJSON = true
				}
				if a.Key == "srcset" {
					for _, part := range strings.Split(a.Val, ",") {
						if f := strings.Fields(part); len(f) > 0 {
							note(f[0])
						}
					}
					continue
				}
				note(a.Val)
			}
		case html.TextToken:
			if !inJSON {
				continue
			}
			text := string(z.Text())
			for i := 0; i < len(text); i++ {
				if text[i] != '"' {
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
					break
				}
				var v string
				if json.Unmarshal([]byte(text[i:j+1]), &v) == nil {
					note(v)
				}
				i = j
			}
		case html.EndTagToken:
			inJSON = false
		}
	}
}

// auditStaticServerEndpoints walks the static tree and reports every HTML file
// that still names a `/_gosx/` endpoint.
func auditStaticServerEndpoints(root string) ([]string, error) {
	exists := func(p string) bool {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(p, "/"))))
		return err == nil && info.Mode().IsRegular()
	}
	var findings []string
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".html") {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if hits := staticServerEndpoints(string(data), exists); len(hits) > 0 {
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

var cssRootURLPattern = regexp.MustCompile(`(url\(\s*|@import\s+)(["']?)(/[^"')\s]*)(["']?)`)

// rewriteStaticCSS makes root-absolute url(...) and @import references in one
// stylesheet resolve against the stylesheet itself. cssRel is the file's path
// under the mount ("css/app.css"); prefix is the build-time mount or "".
func rewriteStaticCSS(cssRel, source string, opts staticRewriteOptions) string {
	dir := path.Dir("/" + filepath.ToSlash(cssRel))
	return cssRootURLPattern.ReplaceAllStringFunc(source, func(match string) string {
		m := cssRootURLPattern.FindStringSubmatch(match)
		value := m[3]
		if strings.HasPrefix(value, "//") || !opts.rewritesJSONString(value) {
			return match
		}
		target, query := value, ""
		if i := strings.IndexAny(target, "?#"); i >= 0 {
			target, query = target[:i], target[i:]
		}
		if opts.Prefix != "" && strings.HasPrefix(target, opts.Prefix+"/") {
			target = strings.TrimPrefix(target, opts.Prefix)
		}
		rel, err := filepath.Rel(filepath.FromSlash(dir), filepath.FromSlash(target))
		if err != nil {
			return match
		}
		return m[1] + m[2] + filepath.ToSlash(rel) + query + m[4]
	})
}

// rewriteStaticCSSTree rewrites every .css file under contentDir and drops the
// stale compressed copies of the ones it changed.
func rewriteStaticCSSTree(contentDir string, opts staticRewriteOptions) error {
	return filepath.Walk(contentDir, func(p string, info os.FileInfo, err error) error {
		if os.IsNotExist(err) {
			return nil // a sidecar this walk removed
		}
		if err != nil || info.IsDir() || !strings.HasSuffix(p, ".css") {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(contentDir, p)
		out := rewriteStaticCSS(rel, string(data), opts)
		if out == string(data) {
			return nil
		}
		if err := os.WriteFile(p, []byte(out), info.Mode().Perm()); err != nil {
			return err
		}
		for _, ext := range []string{".gz", ".br"} {
			if err := os.Remove(p + ext); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
		return nil
	})
}
