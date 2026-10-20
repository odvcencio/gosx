package main

import (
	"fmt"
	"io/fs"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"m31labs.dev/gosx/internal/bundlepolicy"
	"m31labs.dev/gosx/internal/httpcompress"
)

// writeTextSidecars uses the same Brotli 11 and gzip 9 writers as runtime
// assets. Only useful variants are retained; originals remain deployable.
func writeTextSidecars(root string, policy bundlepolicy.Config) error {
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".br" || ext == ".gz" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		policyPath := "public/" + filepath.ToSlash(rel)
		if bundlepolicy.IsExcluded(policyPath, policy.Exclude) {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() < httpcompress.MinimumSize {
			return nil
		}
		contentType := mime.TypeByExtension(ext)
		if contentType == "" {
			file, err := os.Open(path)
			if err != nil {
				return err
			}
			var prefix [512]byte
			n, readErr := file.Read(prefix[:])
			closeErr := file.Close()
			if readErr != nil {
				return readErr
			}
			if closeErr != nil {
				return closeErr
			}
			contentType = http.DetectContentType(prefix[:n])
		}
		if !httpcompress.Compressible(contentType) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, variant := range []struct {
			ext   string
			write func(string, []byte) error
		}{{".br", writeBrotliSidecarIfSmaller}, {".gz", writeGzipSidecarIfSmaller}} {
			if bundlepolicy.IsExcluded(policyPath+variant.ext, policy.Exclude) {
				err = removeFileIfExists(path + variant.ext)
			} else {
				err = variant.write(path, data)
			}
			if err != nil {
				return fmt.Errorf("compress %s: %w", path, err)
			}
		}
		return nil
	})
}
