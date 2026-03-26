package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type deploymentDescriptor struct {
	Version         int    `json:"version"`
	StaticDir       string `json:"staticDir"`
	ServerEntry     string `json:"serverEntry,omitempty"`
	EdgeEntry       string `json:"edgeEntry"`
	RoutesManifest  string `json:"routesManifest"`
	AssetsNamespace string `json:"assetsNamespace"`
	OriginEnv       string `json:"originEnv"`
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
		"async function fetchStatic(request, assetPath, env) {",
		"  if (!env || !env.ASSETS || typeof env.ASSETS.fetch !== \"function\") return null;",
		"  const url = new URL(request.url);",
		"  url.pathname = assetPath;",
		"  return env.ASSETS.fetch(new Request(url.toString(), request));",
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
	return strings.Join([]string{
		"{",
		"  \"$schema\": \"https://openapi.vercel.sh/vercel.json\",",
		"  \"cleanUrls\": true,",
		"  \"trailingSlash\": false,",
		"  \"headers\": [",
		"    {",
		"      \"source\": \"/assets/(.*)\",",
		"      \"headers\": [{ \"key\": \"Cache-Control\", \"value\": \"public, max-age=31536000, immutable\" }]",
		"    },",
		"    {",
		"      \"source\": \"/gosx/(.*)\",",
		"      \"headers\": [{ \"key\": \"Cache-Control\", \"value\": \"public, max-age=31536000, immutable\" }]",
		"    }",
		"  ]",
		"}",
		"",
	}, "\n")
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
