package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"m31labs.dev/gosx/server"
)

const blackglassROMMaxBytes = 8 << 20

// The optional cartridge is a deployment input, outside source control and the
// static public directory. Its bytes are served by the lazy cartridge endpoint.
// The same endpoint stays available without it so a normal checkout loads only
// cartridges selected on the player's device.
func mountBlackglassCartridge(app *server.App, root string) error {
	path := strings.TrimSpace(os.Getenv("GOSX_BLACKGLASS_ROM_FILE"))
	explicit := path != ""
	if !explicit {
		path = filepath.Join(root, "runtime-assets", "blackglass-cartridge.gbc")
	}
	rom, err := readBlackglassCartridge(path)
	if err != nil && !(os.IsNotExist(err) && !explicit) {
		return fmt.Errorf("Blackglass deployment cartridge: %w", err)
	}
	manifest := map[string]any{"available": len(rom) > 0}
	hash := ""
	if len(rom) > 0 {
		digest := sha256.Sum256(rom)
		hash = hex.EncodeToString(digest[:])
		manifest["sha256"] = hash
		manifest["bytes"] = len(rom)
		manifest["name"] = strings.TrimRight(string(rom[0x134:0x143]), "\x00 ") + ".gbc"
		manifest["url"] = "/api/blackglass-cartridge/rom?sha256=" + hash
	}
	app.Mount("/api/blackglass-cartridge", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "private, no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(manifest)
		}
	}))
	app.Mount("/api/blackglass-cartridge/rom", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(rom) == 0 {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if requested := r.URL.Query().Get("sha256"); requested != "" && requested != hash {
			http.Error(w, "Cartridge revision changed", http.StatusGone)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.Header().Set("ETag", `"`+hash+`"`)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, "blackglass-cartridge.gbc", time.Time{}, bytes.NewReader(rom))
	}))
	return nil
}

func readBlackglassCartridge(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	rom, err := io.ReadAll(io.LimitReader(file, blackglassROMMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(rom) < 32768 || len(rom) > blackglassROMMaxBytes || len(rom)%16384 != 0 {
		return nil, fmt.Errorf("expected a GB/GBC cartridge between 32 KiB and 8 MiB")
	}
	var checksum byte
	for _, b := range rom[0x134:0x14d] {
		checksum -= b + 1
	}
	if checksum != rom[0x14d] {
		return nil, fmt.Errorf("cartridge header checksum is invalid")
	}
	return rom, nil
}
