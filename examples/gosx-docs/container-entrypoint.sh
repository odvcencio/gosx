#!/usr/bin/env sh
set -eu

if [ -n "${GOSX_DOCS_IMAGE_REVISION:-}" ]; then
	export GOSX_DOCS_REVISION="$GOSX_DOCS_IMAGE_REVISION"
fi
if [ -n "${GOSX_DOCS_IMAGE_BUILT_AT:-}" ]; then
	export GOSX_DOCS_BUILT_AT="$GOSX_DOCS_IMAGE_BUILT_AT"
fi

exec /opt/gosx-docs/run.sh "$@"
