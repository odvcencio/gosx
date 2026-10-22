package netplay

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sort"
)

// IdentityFile is one input to a simulation identity.
type IdentityFile struct {
	// Name identifies the file; use a slash-separated path relative to the
	// package, so the identity does not depend on where the code is checked out.
	Name string
	Data []byte
}

// ComputeIdentity hashes everything that decides what a simulation computes:
// its source files, its data tables (moves, frame data, roster), the Go
// toolchain version and, if the simulation uses it, the det toolkit version.
// Source files are hashed with CRLF folded to LF so a Windows checkout agrees
// with a Linux one; data files are hashed byte for byte. The order of the
// lists does not matter. Every target embeds the same value: js/wasm, the
// native relay and native desktop.
func ComputeIdentity(goVersion, detVersion string, sources, data []IdentityFile) ([32]byte, error) {
	h := sha256.New()
	put := func(tag, s string) {
		var n [8]byte
		binary.LittleEndian.PutUint64(n[:], uint64(len(s)))
		h.Write([]byte(tag))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	put("netplay-identity", "v1")
	put("go", goVersion)
	put("det", detVersion)
	for _, group := range []struct {
		tag   string
		files []IdentityFile
		fold  bool
	}{{"src", sources, true}, {"data", data, false}} {
		files := append([]IdentityFile(nil), group.files...)
		sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })
		for i, f := range files {
			if f.Name == "" || (i > 0 && files[i-1].Name == f.Name) {
				return [32]byte{}, errors.New("netplay: identity file names must be non-empty and unique")
			}
			body := f.Data
			if group.fold {
				body = bytes.ReplaceAll(body, []byte("\r\n"), []byte("\n"))
			}
			put(group.tag, f.Name)
			put(group.tag+"-body", string(body))
		}
	}
	var id [32]byte
	copy(id[:], h.Sum(nil))
	return id, nil
}
