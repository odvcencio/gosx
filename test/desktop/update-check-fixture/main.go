package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type manifest struct {
	App          string            `json:"app"`
	Channel      string            `json:"channel"`
	Version      string            `json:"version"`
	Released     string            `json:"released"`
	Notes        string            `json:"notes"`
	DownloadPage string            `json:"download_page"`
	Artifacts    map[string]string `json:"artifacts"`
}

func main() {
	address := flag.String("listen", "0.0.0.0:8210", "WSL localhost-forwarding test address")
	publicKeyPath := flag.String("public-key-file", "", "write the ephemeral public key")
	manifestPath := flag.String("manifest-file", "", "write the valid new-version manifest")
	signaturePath := flag.String("signature-file", "", "write the valid detached signature")
	flag.Parse()
	if *address != "0.0.0.0:8210" {
		log.Fatalf("fixture server may listen only on IPv4 port 8210, got %q", *address)
	}
	if *publicKeyPath == "" || *manifestPath == "" || *signaturePath == "" {
		log.Fatal("public key, manifest, and signature output paths are required")
	}
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	if err := writeFile(*publicKeyPath, []byte(base64.StdEncoding.EncodeToString(publicKey)+"\n")); err != nil {
		log.Fatal(err)
	}

	newManifest := makeManifest("2.0.0")
	newBytes, err := json.Marshal(newManifest)
	if err != nil {
		log.Fatal(err)
	}
	newBytes = append(newBytes, '\n')
	newSignature := ed25519.Sign(privateKey, newBytes)
	if err := writeFile(*manifestPath, newBytes); err != nil {
		log.Fatal(err)
	}
	if err := writeFile(*signaturePath, []byte(base64.StdEncoding.EncodeToString(newSignature)+"\n")); err != nil {
		log.Fatal(err)
	}

	listener, err := net.Listen("tcp", *address)
	if err != nil {
		log.Fatalf("bind update fixture on %s: %v", *address, err)
	}
	handler := http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		log.Printf("GET %s", request.URL.Path)
		if request.URL.Path == "/health" {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		pieces := strings.Split(strings.Trim(request.URL.Path, "/"), "/")
		if len(pieces) != 2 || (pieces[1] != "latest.json" && pieces[1] != "latest.json.sig") {
			http.NotFound(response, request)
			return
		}
		version := "2.0.0"
		if pieces[0] == "same" {
			version = "1.0.0"
		} else if pieces[0] != "new" && pieces[0] != "bad" {
			http.NotFound(response, request)
			return
		}
		data := newBytes
		signature := newSignature
		if version != "2.0.0" {
			currentBytes, marshalErr := json.Marshal(makeManifest(version))
			if marshalErr != nil {
				http.Error(response, marshalErr.Error(), http.StatusInternalServerError)
				return
			}
			currentBytes = append(currentBytes, '\n')
			data = currentBytes
			signature = ed25519.Sign(privateKey, currentBytes)
		}
		if pieces[0] == "bad" {
			signature = append([]byte(nil), signature...)
			signature[0] ^= 0xff
		}
		response.Header().Set("Cache-Control", "no-store")
		if pieces[1] == "latest.json" {
			response.Header().Set("Content-Type", "application/json")
			_, _ = response.Write(data)
			return
		}
		response.Header().Set("Content-Type", "application/octet-stream")
		_, _ = response.Write([]byte(base64.StdEncoding.EncodeToString(signature) + "\n"))
	})
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second}
	log.Printf("listening on %s for Windows localhost forwarding; client uses http://127.0.0.1:8210; public key sha256=%s", *address, digest(publicKey))
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func makeManifest(version string) manifest {
	return manifest{
		App: "wb.test", Channel: "stable", Version: version,
		Released: "2026-09-27", Notes: "WELDBREAKERS update fixture " + version + ".",
		DownloadPage: "https://example.invalid/wb-release",
		Artifacts:    map[string]string{"WELDBREAKERS-Setup-" + version + ".exe": strings.Repeat("a", 64)},
	}
}

func writeFile(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func digest(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
