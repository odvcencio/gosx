module m31labs.dev/gosx/cmd/buildbootstrap

go 1.26

require (
	github.com/andybalholm/brotli v1.2.2
	github.com/evanw/esbuild v0.28.1
	github.com/klauspost/compress v1.19.0
	github.com/odvcencio/gotreesitter v0.47.0
	github.com/tdewolff/minify/v2 v2.24.13
	github.com/tdewolff/parse/v2 v2.8.12
	m31labs.dev/gosx v0.0.0
)

require golang.org/x/sys v0.43.0 // indirect

replace m31labs.dev/gosx => ../..
