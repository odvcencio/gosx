package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

// ManagedAssets serves an immutable filesystem at content-addressed URLs and
// revalidates unversioned URLs. Construct it from an embedded or immutable FS;
// changing files requires constructing a new handler so hashes stay truthful.
type ManagedAssets struct {
	prefix string
	files  fs.FS
	paths  map[string]string
	tags   map[string]string
}

func NewManagedAssets(prefix string, files fs.FS) (*ManagedAssets, error) {
	if files == nil || !strings.HasPrefix(prefix, "/") || strings.ContainsAny(prefix, "?#") {
		return nil, fmt.Errorf("managed assets require an absolute path prefix and filesystem")
	}
	prefix = strings.TrimRight(prefix, "/") + "/"
	a := &ManagedAssets{prefix: prefix, files: files, paths: map[string]string{}, tags: map[string]string{}}
	err := fs.WalkDir(files, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		data, err := fs.ReadFile(files, name)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		tag := hex.EncodeToString(hash[:])
		a.tags[name] = tag
		a.paths[prefix+name] = prefix + tag + "/" + name
		return nil
	})
	return a, err
}

// URL returns a versioned path for a known file, or the original path otherwise.
func (a *ManagedAssets) URL(value string) string {
	if v := a.paths[value]; v != "" {
		return v
	}
	return value
}

// Marshal resolves exact asset-valued JSON strings without rounding integers,
// rewriting object keys, or touching source code embedded in strings.
func (a *ManagedAssets) Marshal(value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return rewriteManagedAssetURLs(raw, a.paths), nil
}

func (a *ManagedAssets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, a.prefix) {
		http.NotFound(w, r)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, a.prefix)
	versioned := false
	if _, file, ok := strings.Cut(name, "/"); ok && a.paths[a.prefix+file] == r.URL.Path {
		name, versioned = file, true
	}
	tag, ok := a.tags[name]
	if !ok || !fs.ValidPath(name) {
		http.NotFound(w, r)
		return
	}
	if ct := mime.TypeByExtension(path.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	cache := NewCacheState()
	if versioned {
		cache.SetPolicy(CachePolicy{Public: true, MaxAge: 365 * 24 * time.Hour, Immutable: true})
	} else {
		cache.SetPolicy(CachePolicy{Public: true, MustRevalidate: true})
	}
	cache.SetETag(tag)
	addAcceptEncodingVary(w.Header())
	if ApplyCacheHeaders(r, w.Header(), http.StatusOK, cache, nil) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	http.ServeFileFS(w, r, a.files, name)
}

// Replace complete JSON string values without decoding shader sources or
// converting integers through float64. The input has just passed json.Marshal;
// walking quoted tokens also keeps escaped quotes and object keys untouched.
func rewriteManagedAssetURLs(raw []byte, paths map[string]string) []byte {
	maxToken := 0
	for path := range paths {
		// A RawMessage may spell every byte as a six-byte Unicode escape.
		maxToken = max(maxToken, 6*len(path)+2)
	}
	if maxToken == 0 {
		return raw
	}
	var out []byte
	copied := 0
	for i := 0; i < len(raw); i++ {
		if raw[i] != '"' {
			continue
		}
		start, escaped := i, false
		for i++; i < len(raw) && raw[i] != '"'; i++ {
			if raw[i] == '\\' {
				escaped = true
				i++
			}
		}
		end := i + 1
		if end-start > maxToken || end-start < 2 {
			continue
		}
		after := end
		for after < len(raw) && (raw[after] == ' ' || raw[after] == '\t' || raw[after] == '\r' || raw[after] == '\n') {
			after++
		}
		if after < len(raw) && raw[after] == ':' {
			continue
		}
		var path string
		if escaped {
			if json.Unmarshal(raw[start:end], &path) != nil {
				continue
			}
		} else {
			path = string(raw[start+1 : i])
		}
		replacement, ok := paths[path]
		if !ok {
			continue
		}
		if out == nil {
			out = make([]byte, 0, len(raw)+len(raw)/16+128)
		}
		quoted, _ := json.Marshal(replacement)
		out = append(out, raw[copied:start]...)
		out = append(out, quoted...)
		copied = end
	}
	if out == nil {
		return raw
	}
	return append(out, raw[copied:]...)
}
