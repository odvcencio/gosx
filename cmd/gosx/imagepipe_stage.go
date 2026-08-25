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
// own source encoding -- the fallback format a resized variant keeps beside
// its WebP output. GIF and WebP sources use PNG because either may carry
// alpha and imagepipe's current tqwebp path intentionally refuses alpha.
func imagePipeNativeFormat(ext string) (imagepipe.Format, bool) {
	switch strings.ToLower(ext) {
	case ".jpg", ".jpeg":
		return imagepipe.FormatJPEG, true
	case ".png":
		return imagepipe.FormatPNG, true
	case ".gif", ".webp":
		return imagepipe.FormatPNG, true
	default:
		return "", false
	}
}

// imagePipeExtraFormats lists built-in modern output formats generated beside
// the native fallback. FormatWebP uses tqwebp by default; a project may still
// replace it through imagepipe.RegisterEncoder before this stage runs.
var imagePipeExtraFormats = []imagepipe.Format{imagepipe.FormatWebP}

// stageImageVariants walks projectDir/public for raster images, resizes
// each down the AutoImageWidths ladder capped at its own intrinsic width,
// encodes every rung to its own native format plus WebP for opaque sources,
// and writes the hashed results into
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
	return stageImageVariantsFromPublicDir(filepath.Join(projectDir, "public"), distDir)
}
func stageImageVariantsFromPublicDir(publicDir, distDir string) ([]buildmanifest.ImageAsset, error) {
	imagesDir := filepath.Join(distDir, "assets", "images")
	if err := os.RemoveAll(imagesDir); err != nil {
		return nil, fmt.Errorf("clean image asset dir: %w", err)
	}
	info, err := os.Lstat(publicDir)
	if err != nil {
		return nil, nil
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return nil, fmt.Errorf("public image source must be a regular directory: %s", publicDir)
	}
	if err := os.MkdirAll(imagesDir, 0755); err != nil {
		return nil, fmt.Errorf("create image asset dir: %w", err)
	}

	candidates := server.AutoImageWidths(0)

	var assets []buildmanifest.ImageAsset
	walkErr := filepath.WalkDir(publicDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("public image source contains symlink: %s", path)
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
// writes its variants into imagesDir. ok is false (with a nil error) when
// the source has no usable ladder rungs, or no output format applies to it
// at all (an extension imagePipeNativeFormat does not recognize, with no
// imagePipeExtraFormats encoder registered either); err is non-nil when the
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

	var formats []imagepipe.Format
	if native, ok := imagePipeNativeFormat(ext); ok {
		formats = append(formats, native)
	}
	for _, extra := range imagePipeExtraFormats {
		formats = append(formats, extra)
	}
	if len(formats) == 0 {
		return buildmanifest.ImageAsset{}, false, nil
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
