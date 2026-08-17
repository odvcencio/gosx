package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"m31labs.dev/gosx/buildmanifest"
	"m31labs.dev/gosx/imagepipe"
	"m31labs.dev/gosx/server"
)

// imagePipeSourceExtensions are the source file extensions gosx build's
// image variant stage probes and resizes. imagepipe itself (via its
// blank-imported golang.org/x/image/webp decoder) can already probe and
// decode all four; this set only decides which files under public/ this
// stage bothers to walk into imagepipe.Process at all.
var imagePipeSourceExtensions = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true,
}

// imagePipeNativeFormat returns the output format that best matches ext's
// own source encoding -- the format a caller that never asked for WebP
// explicitly would still expect a resized variant to keep. It mirrors
// selectTargetImageFormat's source side in server/image.go: GIF sources
// resize to PNG (a GIF's own format is not one Process encodes to), and an
// unrecognized extension has no native fallback.
func imagePipeNativeFormat(ext string) (imagepipe.Format, bool) {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return imagepipe.FormatJPEG, true
	case ".png":
		return imagepipe.FormatPNG, true
	case ".gif":
		return imagepipe.FormatPNG, true
	default:
		return "", false
	}
}

// stageImageVariants walks projectDir/public for raster images, resizes
// each down the AutoImageWidths ladder capped at its own intrinsic width,
// encodes every rung to WebP (plus, for JPEG/PNG/GIF sources, that same
// native format as a fallback), and writes the hashed results into
// distDir/assets/images -- beside the runtime, island, and CSS asset
// buckets gosx build already writes under distDir/assets, and right next
// to the public/ copy stageDeploymentBundle just performed (issue #200).
//
// Every image variant file it writes goes through
// writeHashedWithoutCompressedSidecars, exactly like every other build
// output in this file: gzip/brotli sidecars would waste build time
// re-compressing already-compressed image bytes.
//
// It never fails the whole build over one bad image: a source this stage
// cannot probe (corrupt file, dimensions it cannot use) is skipped with a
// warning on stderr and omitted from the returned assets, matching how
// sidecar CSS is best-effort elsewhere in this file.
func stageImageVariants(projectDir, distDir string) ([]buildmanifest.ImageAsset, error) {
	publicDir := filepath.Join(projectDir, "public")
	info, err := os.Stat(publicDir)
	if err != nil || !info.IsDir() {
		return nil, nil
	}

	imagesDir := filepath.Join(distDir, "assets", "images")
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return nil, fmt.Errorf("create image asset dir: %w", err)
	}

	candidates := server.AutoImageWidths(0)

	var assets []buildmanifest.ImageAsset
	walkErr := filepath.WalkDir(publicDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if !imagePipeSourceExtensions[ext] {
			return nil
		}

		rel, err := filepath.Rel(publicDir, path)
		if err != nil {
			return fmt.Errorf("relative image path %s: %w", path, err)
		}
		source := "/" + filepath.ToSlash(rel)

		asset, ok, procErr := processImagePipeSource(path, source, ext, imagesDir, candidates)
		if procErr != nil {
			fmt.Fprintf(os.Stderr, "    Images: skip %s (%v)\n", source, procErr)
			return nil
		}
		if !ok {
			return nil
		}
		assets = append(assets, asset)
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk public images: %w", walkErr)
	}

	sort.Slice(assets, func(i, j int) bool { return assets[i].Source < assets[j].Source })
	return assets, nil
}

// processImagePipeSource probes, resizes, and encodes one source image and
// writes its variants into imagesDir. ok is false (with a nil error) only
// when the source has no usable ladder rungs; err is non-nil when the
// source could not be probed, decoded, or its variants could not be
// written.
func processImagePipeSource(srcPath, source, ext, imagesDir string, candidates []int) (buildmanifest.ImageAsset, bool, error) {
	dims, _, err := imagepipe.Probe(srcPath)
	if err != nil {
		return buildmanifest.ImageAsset{}, false, err
	}
	if dims.Width <= 0 || dims.Height <= 0 {
		return buildmanifest.ImageAsset{}, false, fmt.Errorf("unusable dimensions %dx%d", dims.Width, dims.Height)
	}

	widths := imagepipe.Ladder(dims.Width, candidates)
	if len(widths) == 0 {
		return buildmanifest.ImageAsset{}, false, nil
	}

	formats := []imagepipe.Format{imagepipe.FormatWebP}
	if native, ok := imagePipeNativeFormat(ext); ok {
		formats = append(formats, native)
	}

	_, variants, err := imagepipe.Process(srcPath, widths, formats, imagepipe.EncodeOptions{})
	if err != nil {
		return buildmanifest.ImageAsset{}, false, err
	}

	baseName := imageAssetBaseName(source)
	variantAssets := make([]buildmanifest.ImageVariantAsset, 0, len(variants))
	for _, variant := range variants {
		name := fmt.Sprintf("%s-%dw", baseName, variant.Width)
		hashed, err := writeHashedWithoutCompressedSidecars(imagesDir, name, variant.Format.Ext(), variant.Data)
		if err != nil {
			return buildmanifest.ImageAsset{}, false, fmt.Errorf("write %s variant: %w", variant.Format, err)
		}
		variantAssets = append(variantAssets, buildmanifest.ImageVariantAsset{
			Width:       variant.Width,
			Format:      string(variant.Format),
			HashedAsset: hashed,
		})
	}

	return buildmanifest.ImageAsset{
		Source:   source,
		Width:    dims.Width,
		Height:   dims.Height,
		Variants: variantAssets,
	}, true, nil
}

// imageAssetBaseName turns a root-relative source path such as
// "/photos/team.jpg" into a filesystem- and URL-safe base name such as
// "photos_team", mirroring cssAssetBaseName's own convention for the same
// purpose.
func imageAssetBaseName(source string) string {
	rel := strings.TrimSuffix(strings.TrimPrefix(source, "/"), filepath.Ext(source))
	if rel == "" {
		return "image"
	}
	replacer := strings.NewReplacer("/", "_", "\\", "_", " ", "_", ".", "_")
	name := strings.Trim(replacer.Replace(rel), "_")
	if name == "" {
		return "image"
	}
	return name
}
