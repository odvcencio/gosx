package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/internal/httpcompress"
)

type deploymentDescriptor struct {
	Version         int                   `json:"version"`
	StaticDir       string                `json:"staticDir"`
	ServerEntry     string                `json:"serverEntry,omitempty"`
	EdgeEntry       string                `json:"edgeEntry"`
	RoutesManifest  string                `json:"routesManifest"`
	AssetsNamespace string                `json:"assetsNamespace"`
	OriginEnv       string                `json:"originEnv"`
	Compression     deploymentCompression `json:"compression"`
}

type deploymentCompression struct {
	Encodings    []string          `json:"encodings"`
	Suffixes     map[string]string `json:"suffixes"`
	Vary         string            `json:"vary"`
	MinimumBytes int               `json:"minimumBytes"`
}

func writeEdgeBundle(distDir string, manifest exportManifest, builtServer bool) error {
	edgeDir := filepath.Join(distDir, "edge")
	platformDir := filepath.Join(distDir, "platform")
	if err := os.MkdirAll(edgeDir, 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(platformDir, 0755); err != nil {
		return err
	}

	if err := os.WriteFile(filepath.Join(edgeDir, "worker.js"), []byte(edgeWorkerSource(manifest)), 0644); err != nil {
		return err
	}

	descriptor := deploymentDescriptor{
		Version:         1,
		StaticDir:       "static",
		EdgeEntry:       filepath.ToSlash(filepath.Join("edge", "worker.js")),
		RoutesManifest:  "export.json",
		AssetsNamespace: "ASSETS",
		OriginEnv:       "GOSX_ORIGIN",
		Compression: deploymentCompression{
			Encodings:    []string{"br", "gzip"},
			Suffixes:     map[string]string{"br": ".br", "gzip": ".gz"},
			Vary:         "Accept-Encoding",
			MinimumBytes: httpcompress.MinimumSize,
		},
	}
	if builtServer {
		descriptor.ServerEntry = filepath.ToSlash(filepath.Join("server", "app"))
	}
	data, err := json.MarshalIndent(descriptor, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal deployment descriptor: %w", err)
	}
	if err := os.WriteFile(filepath.Join(platformDir, "deployment.json"), data, 0644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(platformDir, "vercel.json"), []byte(vercelConfigSource()), 0644); err != nil {
		return err
	}
	return nil
}

func edgeWorkerSource(manifest exportManifest) string {
	routes := make([]exportRoute, 0, len(manifest.Routes))
	for _, route := range manifest.Routes {
		route.Path = normalizeExportRoutePath(route.Path)
		route.File = filepath.ToSlash(strings.TrimSpace(route.File))
		routes = append(routes, route)
	}
	routesJSON, err := json.Marshal(routes)
	if err != nil {
		routesJSON = []byte("[]")
	}

	return strings.Join([]string{
		"const GOSX_STATIC_ROUTES = new Map((" + string(routesJSON) + ").map((route) => [normalizePath(route.path), route.file]));",
		"const GOSX_STATIC_PREFIXES = [\"/assets/\", \"/gosx/\"];",
		"",
		"function normalizePath(pathname) {",
		"  if (!pathname) return \"/\";",
		"  if (!pathname.startsWith(\"/\")) pathname = \"/\" + pathname;",
		"  if (pathname.length > 1 && pathname.endsWith(\"/\")) pathname = pathname.slice(0, -1);",
		"  return pathname || \"/\";",
		"}",
		"",
		"function isStaticAsset(pathname) {",
		"  return GOSX_STATIC_PREFIXES.some((prefix) => pathname.startsWith(prefix)) || /\\.[a-z0-9]+$/i.test(pathname);",
		"}",
		"",
		"function acceptsEncoding(header, encoding) {",
		"  let wildcard = false;",
		"  for (const part of (header || \"\").split(\",\")) {",
		"    const [name, ...params] = part.split(\";\");",
		"    const token = name.trim().toLowerCase();",
		"    if (token !== encoding && token !== \"*\") continue;",
		"    let allowed = true;",
		"    for (const param of params) {",
		"      const [key, value] = param.trim().split(\"=\");",
		"      if (key.trim().toLowerCase() !== \"q\") continue;",
		"      const q = Number(value);",
		"      allowed = Number.isFinite(q) && q > 0 && q <= 1;",
		"    }",
		"    if (token === encoding) return allowed;",
		"    wildcard = allowed;",
		"  }",
		"  return wildcard;",
		"}",
		"",
		"function varyAcceptEncoding(headers) {",
		"  const tokens = (headers.get(\"Vary\") || \"\").split(\",\").map((value) => value.trim().toLowerCase());",
		"  if (!tokens.includes(\"*\") && !tokens.includes(\"accept-encoding\")) headers.append(\"Vary\", \"Accept-Encoding\");",
		"}",
		"",
		"function isCompressible(contentType) {",
		"  const type = (contentType || \"\").split(\";\")[0].trim().toLowerCase();",
		"  return type.startsWith(\"text/\") || (type.startsWith(\"application/\") &&",
		"    ([\"application/javascript\", \"application/x-javascript\", \"application/json\", \"application/xml\", \"application/graphql\", \"application/x-www-form-urlencoded\"].includes(type) || /\\+(json|xml)$/.test(type)));",
		"}",
		"",
		"async function fetchStatic(request, assetPath, env) {",
		"  if (!env || !env.ASSETS || typeof env.ASSETS.fetch !== \"function\") return null;",
		"  const url = new URL(request.url);",
		"  url.pathname = assetPath;",
		"  const sourceRequest = new Request(url.toString(), request);",
		"  sourceRequest.headers.set(\"Accept-Encoding\", \"identity\");",
		"  const response = await env.ASSETS.fetch(sourceRequest);",
		"  if (!response) return null;",
		"  const headers = new Headers(response.headers);",
		"  varyAcceptEncoding(headers);",
		"  const identity = () => new Response(response.body, { status: response.status, statusText: response.statusText, headers, encodeBody: \"manual\" });",
		"  const accept = request.headers.get(\"Accept-Encoding\");",
		"  if (response.status === 304 && (acceptsEncoding(accept, \"br\") || acceptsEncoding(accept, \"gzip\"))) {",
		"    const etag = headers.get(\"ETag\");",
		"    if (etag && !etag.startsWith(\"W/\")) headers.set(\"ETag\", \"W/\" + etag);",
		"  }",
		"  const length = headers.get(\"Content-Length\");",
		"  if (request.method !== \"GET\" || request.headers.has(\"Range\") ||",
		"      (request.headers.get(\"Upgrade\") || \"\").toLowerCase() === \"websocket\" ||",
		"      response.status !== 200 || headers.has(\"Content-Encoding\") ||",
		"      !isCompressible(headers.get(\"Content-Type\")) || (length !== null && Number(length) < 1024)) return identity();",
		"  for (const [encoding, suffix] of [[\"br\", \".br\"], [\"gzip\", \".gz\"]]) {",
		"    if (!acceptsEncoding(accept, encoding)) continue;",
		"    const variantURL = new URL(url);",
		"    variantURL.pathname += suffix;",
		"    const variantRequest = new Request(variantURL.toString(), sourceRequest);",
		"    for (const name of [\"If-None-Match\", \"If-Modified-Since\", \"If-Match\", \"If-Unmodified-Since\"]) variantRequest.headers.delete(name);",
		"    const variant = await env.ASSETS.fetch(variantRequest);",
		"    if (!variant || variant.status !== 200 || (variant.headers.has(\"Content-Encoding\") && variant.headers.get(\"Content-Encoding\") !== encoding)) {",
		"      if (variant && variant.body) await variant.body.cancel();",
		"      continue;",
		"    }",
		"    headers.set(\"Content-Encoding\", encoding);",
		"    headers.delete(\"Content-Length\");",
		"    if (variant.headers.has(\"Content-Length\")) headers.set(\"Content-Length\", variant.headers.get(\"Content-Length\"));",
		"    const etag = headers.get(\"ETag\");",
		"    if (etag && !etag.startsWith(\"W/\")) headers.set(\"ETag\", \"W/\" + etag);",
		"    if (response.body) await response.body.cancel();",
		"    return new Response(variant.body, { status: 200, headers, encodeBody: \"manual\" });",
		"  }",
		"  return identity();",
		"}",
		"",
		"function edgeProxyRequest(request, origin) {",
		"  const url = new URL(request.url);",
		"  const upstream = new URL(url.pathname + url.search, origin);",
		"  const init = {",
		"    method: request.method,",
		"    headers: new Headers(request.headers),",
		"    redirect: \"manual\",",
		"  };",
		"  if (request.method !== \"GET\" && request.method !== \"HEAD\") {",
		"    init.body = request.body;",
		"  }",
		"  init.headers.set(\"x-gosx-edge\", \"1\");",
		"  return new Request(upstream.toString(), init);",
		"}",
		"",
		"export default {",
		"  async fetch(request, env) {",
		"    const url = new URL(request.url);",
		"    const pathname = normalizePath(url.pathname);",
		"",
		"    if (request.method === \"GET\" || request.method === \"HEAD\") {",
		"      const routeAsset = GOSX_STATIC_ROUTES.get(pathname);",
		"      if (routeAsset) {",
		"        const response = await fetchStatic(request, \"/\" + routeAsset, env);",
		"        if (response) return response;",
		"      }",
		"      if (isStaticAsset(pathname)) {",
		"        const response = await fetchStatic(request, url.pathname, env);",
		"        if (response) return response;",
		"      }",
		"    }",
		"",
		"    const origin = (env && (env.GOSX_ORIGIN || env.ORIGIN)) || \"\";",
		"    if (!origin) {",
		"      return new Response(\"Missing GOSX_ORIGIN for GoSX edge fallback.\", { status: 502 });",
		"    }",
		"    return fetch(edgeProxyRequest(request, origin));",
		"  },",
		"};",
		"",
	}, "\n")
}

func vercelConfigSource() string {
	type header struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	type rule struct {
		Source  string   `json:"source"`
		Headers []header `json:"headers"`
	}
	rules := []rule{
		{"/assets/(.*)", []header{{"Cache-Control", "public, max-age=31536000, immutable"}}},
		{"/gosx/(.*)", []header{{"Cache-Control", "public, max-age=31536000, immutable"}}},
		{"/(.*)", []header{{"Vary", "Accept-Encoding"}}},
	}
	for _, variant := range []struct{ encoding, suffix string }{{"br", ".br"}, {"gzip", ".gz"}} {
		rules = append(rules, rule{"/(.*)" + variant.suffix, []header{{"Content-Encoding", variant.encoding}}})
		for _, media := range []struct{ ext, contentType string }{
			{"html", "text/html; charset=utf-8"},
			{"css", "text/css; charset=utf-8"},
			{"js", "application/javascript; charset=utf-8"},
			{"json", "application/json; charset=utf-8"},
			{"webmanifest", "application/manifest+json; charset=utf-8"},
			{"xml", "application/xml; charset=utf-8"},
			{"txt", "text/plain; charset=utf-8"},
		} {
			rules = append(rules, rule{"/(.*)." + media.ext + variant.suffix, []header{{"Content-Type", media.contentType}}})
		}
	}
	config := struct {
		Schema        string `json:"$schema"`
		CleanURLs     bool   `json:"cleanUrls"`
		TrailingSlash bool   `json:"trailingSlash"`
		Headers       []rule `json:"headers"`
	}{"https://openapi.vercel.sh/vercel.json", true, false, rules}
	data, _ := json.MarshalIndent(config, "", "  ")
	return string(data) + "\n"
}

func normalizeExportRoutePath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if !strings.HasPrefix(value, "/") {
		value = "/" + value
	}
	if len(value) > 1 && strings.HasSuffix(value, "/") {
		value = strings.TrimRight(value, "/")
	}
	return value
}
