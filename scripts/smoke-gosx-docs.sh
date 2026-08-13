#!/usr/bin/env sh
set -eu

base_url="${1:-${GOSX_DOCS_URL:-https://gosx.m31labs.dev}}"
base_url="${base_url%/}"
expected_framework_version="${GOSX_DOCS_EXPECT_FRAMEWORK_VERSION:-}"
expected_revision="${GOSX_DOCS_EXPECT_REVISION:-}"
expected_built_at="${GOSX_DOCS_EXPECT_BUILT_AT:-}"
expected_public_url="${GOSX_DOCS_EXPECT_PUBLIC_URL:-$base_url}"
expected_public_url="${expected_public_url%/}"
curl_cmd="${CURL:-curl}"

for command_name in "$curl_cmd" awk find grep jq mktemp od sed sleep tr wc; do
	if ! command -v "$command_name" >/dev/null 2>&1; then
		echo "gosx docs smoke: required command is unavailable: ${command_name}" >&2
		exit 1
	fi
done

tmp_dir="$(mktemp -d)"
cleanup() {
	if [ -n "$tmp_dir" ] && [ "$tmp_dir" != "/" ] && [ -d "$tmp_dir" ]; then
		find "$tmp_dir" -type f -delete
		find "$tmp_dir" -depth -type d -empty -delete
	fi
}
on_exit() {
	exit_status=$?
	trap - EXIT INT TERM
	cleanup
	exit "$exit_status"
}
trap on_exit EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

http_status() {
	awk '/^HTTP\// { status = $2 } END { gsub("\\r", "", status); print status }' "$1"
}

assert_status() {
	headers="$1"
	expected="$2"
	label="$3"
	actual="$(http_status "$headers")"
	if [ "$actual" != "$expected" ]; then
		echo "gosx docs smoke: ${label} returned HTTP ${actual:-unknown}; expected ${expected}" >&2
		exit 1
	fi
}

fetch_exact() {
	path="$1"
	output="$2"
	retries="${3:-3}"
	headers="${output}.headers"
	if ! "$curl_cmd" --fail --silent --show-error --max-redirs 0 \
		--retry "$retries" --retry-delay 1 --retry-max-time 15 \
		--connect-timeout 10 --max-time 45 --dump-header "$headers" \
		"${base_url}${path}" -o "$output"; then
		return 1
	fi
	status="$(http_status "$headers")"
	[ "$status" = "200" ]
}

assert_redirect() {
	path="$1"
	expected_status="$2"
	expected_location="$3"
	label="$4"
	headers="$tmp_dir/redirect-$label.headers"
	"$curl_cmd" --silent --show-error --max-redirs 0 \
		--connect-timeout 10 --max-time 30 --dump-header "$headers" \
		"${base_url}${path}" -o /dev/null
	assert_status "$headers" "$expected_status" "$path"
	location="$(awk -F ': *' 'tolower($1) == "location" { value = $2 } END { gsub("\\r", "", value); print value }' "$headers")"
	if [ "$location" != "$expected_location" ]; then
		echo "gosx docs smoke: ${path} redirects to ${location:-nowhere}; expected ${expected_location}" >&2
		exit 1
	fi
}

assert_min_size() {
	file="$1"
	minimum="$2"
	label="$3"
	size="$(wc -c <"$file" | tr -d ' ')"
	if [ "$size" -lt "$minimum" ]; then
		echo "gosx docs smoke: ${label} is only ${size} bytes; expected at least ${minimum}" >&2
		exit 1
	fi
}

for endpoint in health ready site sitemap home docs search demos; do
	case "$endpoint" in
		health) path="/healthz"; output="$tmp_dir/health.json" ;;
		ready) path="/readyz"; output="$tmp_dir/ready.json" ;;
		site) path="/api/site"; output="$tmp_dir/site.json" ;;
		sitemap) path="/sitemap.xml"; output="$tmp_dir/sitemap.xml" ;;
		home) path="/"; output="$tmp_dir/home.html" ;;
		docs) path="/docs"; output="$tmp_dir/docs.html" ;;
		search) path="/docs?q=webgpu"; output="$tmp_dir/search.html" ;;
		demos) path="/demos/"; output="$tmp_dir/demos.html" ;;
	 esac
	if ! fetch_exact "$path" "$output"; then
		echo "gosx docs smoke: ${path} did not return an unredirected HTTP 200 response" >&2
		exit 1
	fi
done

"$curl_cmd" --fail --silent --show-error --request POST --max-redirs 0 \
	--connect-timeout 10 --max-time 30 --dump-header "$tmp_dir/probe.headers" \
	"${base_url}/api/site/probe" -o "$tmp_dir/probe.json"
assert_status "$tmp_dir/probe.headers" 200 "/api/site/probe"

jq -e '.ok == true' "$tmp_dir/health.json" >/dev/null
jq -e '.ok == true' "$tmp_dir/ready.json" >/dev/null
jq -e \
	'.site == "gosx-docs"
	 and .status == "ok"
	 and .apiVersion == "1"
	 and .framework == "GoSX"
	 and (.frameworkVersion | test("^v[0-9]+\\.[0-9]+\\.[0-9]+([+-][0-9A-Za-z.-]+)?$"))
	 and (.revision | test("^([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$"))
	 and (.builtAt | test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$"))
	 and ((.builtAt | fromdateiso8601?) != null)
	 and (.runtime | test("^go[0-9]+\\."))
	 and .publicURL == $publicURL' \
	--arg publicURL "$expected_public_url" "$tmp_dir/site.json" >/dev/null

actual_framework_version="$(jq -r '.frameworkVersion' "$tmp_dir/site.json")"
actual_revision="$(jq -r '.revision' "$tmp_dir/site.json")"
actual_built_at="$(jq -r '.builtAt' "$tmp_dir/site.json")"
site_origin="$(jq -r '.publicURL' "$tmp_dir/site.json")"

if [ -n "$expected_framework_version" ] && [ "$actual_framework_version" != "$expected_framework_version" ]; then
	echo "gosx docs smoke: framework ${actual_framework_version} does not match ${expected_framework_version}" >&2
	exit 1
fi
if [ -n "$expected_revision" ] && [ "$actual_revision" != "$expected_revision" ]; then
	echo "gosx docs smoke: revision ${actual_revision} does not match ${expected_revision}" >&2
	exit 1
fi
if [ -n "$expected_built_at" ] && [ "$actual_built_at" != "$expected_built_at" ]; then
	echo "gosx docs smoke: build time ${actual_built_at} does not match ${expected_built_at}" >&2
	exit 1
fi

jq -e --arg revision "$actual_revision" '.ok == true and .revision == $revision' "$tmp_dir/probe.json" >/dev/null
grep -qi '^cache-control:.*no-store' "$tmp_dir/site.json.headers"
grep -qi '^cache-control:.*no-store' "$tmp_dir/probe.headers"

for page in home docs demos; do
	grep -q '<html[^>]*lang="en"' "$tmp_dir/${page}.html"
	grep -q '<link rel="canonical"' "$tmp_dir/${page}.html"
done

grep -qi 'Scene3D' "$tmp_dir/search.html"
grep -q 'name="q"' "$tmp_dir/search.html"
grep -Fq "<link rel=\"canonical\" href=\"${site_origin}/docs\"" "$tmp_dir/search.html"

# Exercise a real session-backed named action. These routes are deliberately
# excluded from prerendering: a cached build-time CSRF token would not belong
# to the requesting browser session and every form submission would fail.
"$curl_cmd" --fail --silent --show-error --max-redirs 0 \
	--connect-timeout 10 --max-time 30 --cookie-jar "$tmp_dir/forms.cookies" \
	--dump-header "$tmp_dir/forms.html.headers" \
	"${base_url}/docs/forms" -o "$tmp_dir/forms.html"
assert_status "$tmp_dir/forms.html.headers" 200 "/docs/forms"
if grep -qi '^x-gosx-isr:' "$tmp_dir/forms.html.headers"; then
	echo "gosx docs smoke: interactive forms route was served from ISR" >&2
	exit 1
fi
csrf_token="$(sed -n 's/.*name="csrf_token" value="\([^"]*\)".*/\1/p' "$tmp_dir/forms.html" | sed -n '1p')"
if [ -z "$csrf_token" ]; then
	echo "gosx docs smoke: interactive forms route did not render a CSRF token" >&2
	exit 1
fi
"$curl_cmd" --fail --silent --show-error --location --max-redirs 3 \
	--connect-timeout 10 --max-time 30 \
	--cookie "$tmp_dir/forms.cookies" --cookie-jar "$tmp_dir/forms.cookies" \
	--data-urlencode "csrf_token=${csrf_token}" \
	--data-urlencode "email=smoke@example.com" \
	"${base_url}/docs/forms/__actions/subscribe" -o "$tmp_dir/forms-result.html"
grep -q 'Subscribed!' "$tmp_dir/forms-result.html"

# The docs index is deliberately dynamic so a query is never replaced by the
# prerendered /docs artifact. Its slash form canonicalizes to the bare route,
# while ordinary prerendered guides are served through GoSX ISR at a
# directory-slash URL. This checks serving, not a state-changing revalidation.
assert_redirect "/docs/" 308 "/docs" "docs-slash"
assert_redirect "/docs/?q=webgpu" 308 "/docs?q=webgpu" "docs-slash-query"
assert_redirect "/docs/getting-started" 301 "/docs/getting-started/" "guide-isr"
assert_redirect "/docs/getting-started?q=smoke" 301 "/docs/getting-started/?q=smoke" "guide-isr-query"
assert_redirect "/demos" 301 "/demos/" "demos-isr"
if ! fetch_exact "/docs/getting-started/" "$tmp_dir/guide.html"; then
	echo "gosx docs smoke: trailing-slash ISR guide did not return HTTP 200" >&2
	exit 1
fi
grep -Eqi '^x-gosx-isr:.*(HIT|STALE)' "$tmp_dir/guide.html.headers"
grep -Fq "<link rel=\"canonical\" href=\"${site_origin}/docs/getting-started\"" "$tmp_dir/guide.html"

# Fetch the actual deploy bundle, not just its HTML. Hashed runtime paths come
# from the rendered document. The CSS compatibility URL resolves through the
# build manifest to its immutable hashed asset.
runtime_asset="$(grep -o 'gosx/assets/runtime/bootstrap-runtime\.[0-9a-f][0-9a-f]*\.js' "$tmp_dir/home.html" | sed -n '1p')"
wasm_asset="$(grep -o '/gosx/assets/runtime/gosx-runtime\.[0-9a-f][0-9a-f]*\.wasm' "$tmp_dir/home.html" | sed -n '1p')"
if [ -z "$runtime_asset" ] || [ -z "$wasm_asset" ]; then
	echo "gosx docs smoke: rendered home page does not name hashed runtime assets" >&2
	exit 1
fi
runtime_asset="/${runtime_asset#/}"

if ! fetch_exact "$runtime_asset" "$tmp_dir/runtime.js"; then
	echo "gosx docs smoke: hashed runtime JavaScript is unavailable: ${runtime_asset}" >&2
	exit 1
fi
if ! fetch_exact "$wasm_asset" "$tmp_dir/runtime.wasm"; then
	echo "gosx docs smoke: hashed WASM runtime is unavailable: ${wasm_asset}" >&2
	exit 1
fi
if ! fetch_exact "/gosx/css/layout.css?v=${actual_revision}" "$tmp_dir/layout.css"; then
	echo "gosx docs smoke: build-manifest CSS asset is unavailable" >&2
	exit 1
fi
if ! fetch_exact "/checkers-native-preview.png" "$tmp_dir/checkers.png"; then
	echo "gosx docs smoke: public image asset is unavailable" >&2
	exit 1
fi

image_attempt=0
image_ok=0
while [ "$image_attempt" -lt 5 ]; do
	image_attempt=$((image_attempt + 1))
	# This endpoint has its own bounded loop because an initial cache miss may
	# return a non-retriable HTTP status. Disable curl's inner retry here so the
	# two bounds cannot multiply into twenty requests.
	if fetch_exact "/_gosx/image?src=%2Fcheckers-native-preview.png&w=320&format=png" "$tmp_dir/checkers-320.png" 0; then
		image_ok=1
		break
	fi
	if [ "$image_attempt" -lt 5 ]; then
		sleep 2
	fi
done
if [ "$image_ok" -ne 1 ]; then
	echo "gosx docs smoke: optimized image endpoint did not produce a variant" >&2
	exit 1
fi

grep -qi '^content-type:.*javascript' "$tmp_dir/runtime.js.headers"
grep -qi '^content-type:.*application/wasm' "$tmp_dir/runtime.wasm.headers"
grep -qi '^content-type:.*text/css' "$tmp_dir/layout.css.headers"
grep -qi '^content-type:.*image/png' "$tmp_dir/checkers.png.headers"
grep -qi '^content-type:.*image/png' "$tmp_dir/checkers-320.png.headers"
for headers in runtime.js.headers runtime.wasm.headers layout.css.headers checkers-320.png.headers; do
	grep -qi '^cache-control:.*immutable' "$tmp_dir/$headers"
done

assert_min_size "$tmp_dir/runtime.js" 10000 "runtime JavaScript"
assert_min_size "$tmp_dir/runtime.wasm" 100000 "WASM runtime"
assert_min_size "$tmp_dir/layout.css" 1000 "compiled CSS"
assert_min_size "$tmp_dir/checkers.png" 1000 "public image"
assert_min_size "$tmp_dir/checkers-320.png" 1000 "optimized image"

wasm_magic="$(od -An -tx1 -N4 "$tmp_dir/runtime.wasm" | tr -d ' \n')"
image_magic="$(od -An -tx1 -N8 "$tmp_dir/checkers.png" | tr -d ' \n')"
variant_magic="$(od -An -tx1 -N8 "$tmp_dir/checkers-320.png" | tr -d ' \n')"
if [ "$wasm_magic" != "0061736d" ] || [ "$image_magic" != "89504e470d0a1a0a" ] || [ "$variant_magic" != "89504e470d0a1a0a" ]; then
	echo "gosx docs smoke: runtime or image asset has an invalid file signature" >&2
	exit 1
fi

grep -Fq "${site_origin}/docs/getting-started" "$tmp_dir/sitemap.xml"
grep -Fq "${site_origin}/demos/playground" "$tmp_dir/sitemap.xml"
if grep -q '/test/' "$tmp_dir/sitemap.xml"; then
	echo "gosx docs smoke: sitemap exposes internal test routes" >&2
	exit 1
fi

sed -n 's:.*<loc>\([^<]*\)</loc>.*:\1:p' "$tmp_dir/sitemap.xml" >"$tmp_dir/routes.txt"
route_count=0
while IFS= read -r route_url; do
	case "$route_url" in
		"$site_origin"/*|"$site_origin") ;;
		*)
			echo "gosx docs smoke: sitemap contains foreign route ${route_url}" >&2
			exit 1
			;;
	esac
	route_count=$((route_count + 1))
	route_path="${route_url#"$site_origin"}"
	if [ -z "$route_path" ]; then
		route_path="/"
	fi
	"$curl_cmd" --fail --silent --show-error --location \
		--max-redirs 3 --connect-timeout 10 --max-time 45 \
		"${base_url}${route_path}" -o "$tmp_dir/route-${route_count}.html"
	grep -q '<html[^>]*lang="en"' "$tmp_dir/route-${route_count}.html"
	grep -q '<link rel="canonical"' "$tmp_dir/route-${route_count}.html"
done <"$tmp_dir/routes.txt"

if [ "$route_count" -lt 30 ]; then
	echo "gosx docs smoke: sitemap crawl covered only ${route_count} routes" >&2
	exit 1
fi

printf '%s\n' "gosx docs smoke: ${actual_framework_version} ${actual_revision} (${actual_built_at}), bundle assets, search, ISR, and ${route_count} public routes are live at ${base_url}"
