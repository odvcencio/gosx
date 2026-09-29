package server

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"time"

	"github.com/andybalholm/brotli"
	"m31labs.dev/gosx/buildmanifest"
	runtimehost "m31labs.dev/gosx/client/runtime/host"
)

// Hashing and compression happen once at process startup. The embedded asset
// also serves go run and dev apps that have no build manifest or runtime root.
var navigationAsset = newNavigationAsset()

type embeddedNavigationAsset struct {
	metadata buildmanifest.HashedAsset
	raw      []byte
	gzip     []byte
	brotli   []byte
}

func newNavigationAsset() embeddedNavigationAsset {
	raw := []byte(runtimehost.NavigationRuntime)
	hash := buildmanifest.ContentHash(raw)
	compress := func(writer io.WriteCloser, buffer *bytes.Buffer) []byte {
		if _, err := writer.Write(raw); err != nil {
			panic(err)
		}
		if err := writer.Close(); err != nil {
			panic(err)
		}
		return buffer.Bytes()
	}
	var gz, br bytes.Buffer
	zw, err := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	if err != nil {
		panic(err)
	}
	return embeddedNavigationAsset{
		metadata: buildmanifest.HashedAsset{
			File:      "navigation." + hash + ".js",
			Hash:      hash,
			Size:      int64(len(raw)),
			Integrity: buildmanifest.ContentIntegrity(raw),
		},
		raw:    raw,
		gzip:   compress(zw, &gz),
		brotli: compress(brotli.NewWriterLevel(&br, brotli.BestCompression), &br),
	}
}

func serveNavigationRuntime(w http.ResponseWriter, r *http.Request) {
	data := navigationAsset.raw
	encoding := ""
	if requestAcceptsBrotli(r) {
		data, encoding = navigationAsset.brotli, "br"
	} else if requestAcceptsGzip(r) {
		data, encoding = navigationAsset.gzip, "gzip"
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Add("Vary", "Accept-Encoding")
	w.Header().Set("ETag", `"`+navigationAsset.metadata.Hash+"-"+encoding+`"`)
	if encoding != "" {
		w.Header().Set("Content-Encoding", encoding)
	}
	MarkObservedRequest(r, "runtime", r.URL.Path)
	http.ServeContent(w, r, navigationAsset.metadata.File, time.Time{}, bytes.NewReader(data))
}
