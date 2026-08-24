// Package bundlepolicy defines the safe project-to-bundle boundary shared by
// GoSX staging and the runtime public-file server.
package bundlepolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

type Config struct {
	Allow       []string `json:"allow"`
	AllowPublic []string `json:"allowPublic"`
	Exclude     []string `json:"exclude"`
}

type PolicyFile struct {
	AllowPublic []string `json:"allowPublic,omitempty"`
	Exclude     []string `json:"exclude,omitempty"`
}

type Root string

const (
	RootApp     Root = "app"
	RootContent Root = "content"
	RootPublic  Root = "public"
)

type Diagnostic struct{ Path, Message string }
type Diagnostics []Diagnostic

func (d Diagnostics) Empty() bool                         { return len(d) == 0 }
func (d Diagnostics) Merge(other Diagnostics) Diagnostics { return append(d, other...) }
func (d Diagnostics) Error() string {
	if len(d) == 0 {
		return ""
	}
	items := append(Diagnostics(nil), d...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Path == items[j].Path {
			return items[i].Message < items[j].Message
		}
		return items[i].Path < items[j].Path
	})
	var b strings.Builder
	b.WriteString("GoSX bundle validation failed:\n")
	for _, item := range items {
		fmt.Fprintf(&b, "  - %s: %s\n", item.Path, item.Message)
	}
	b.WriteString("Move invalid files outside app/, content/, or public/, remove them, or add an intentional safe allow entry; secrets and symlinks can never be allowed.")
	return strings.TrimSpace(b.String())
}

func ValidateConfig(cfg Config) Diagnostics {
	var out Diagnostics
	allow, public, exclude := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for _, raw := range cfg.Allow {
		rel, err := Normalize(raw)
		if err != nil {
			out = append(out, Diagnostic{raw, "build.bundle.allow " + err.Error()})
			continue
		}
		if !hasRoot(rel, string(RootApp)) && !hasRoot(rel, string(RootContent)) {
			out = append(out, Diagnostic{rel, "build.bundle.allow must be under app/ or content/"})
			continue
		}
		if _, ok := allow[rel]; ok {
			out = append(out, Diagnostic{rel, "duplicate build.bundle.allow entry"})
			continue
		}
		allow[rel] = struct{}{}
	}
	for _, raw := range cfg.AllowPublic {
		rel, err := Normalize(raw)
		if err != nil {
			out = append(out, Diagnostic{raw, "build.bundle.allowPublic " + err.Error()})
			continue
		}
		if !hasRoot(rel, string(RootPublic)) || rel == string(RootPublic) {
			out = append(out, Diagnostic{rel, "build.bundle.allowPublic must name an exact file under public/"})
			continue
		}
		if _, ok := public[rel]; ok {
			out = append(out, Diagnostic{rel, "duplicate build.bundle.allowPublic entry"})
			continue
		}
		public[rel] = struct{}{}
	}
	for _, raw := range cfg.Exclude {
		rel, err := Normalize(raw)
		if err != nil {
			out = append(out, Diagnostic{raw, "build.bundle.exclude " + err.Error()})
			continue
		}
		if _, ok := exclude[rel]; ok {
			out = append(out, Diagnostic{rel, "duplicate build.bundle.exclude entry"})
			continue
		}
		exclude[rel] = struct{}{}
	}
	for rel := range allow {
		if excludedBy(rel, exclude) {
			out = append(out, Diagnostic{rel, "build.bundle.allow conflicts with build.bundle.exclude"})
		}
	}
	for rel := range public {
		if excludedBy(rel, exclude) {
			out = append(out, Diagnostic{rel, "build.bundle.allowPublic conflicts with build.bundle.exclude"})
		}
	}
	return out
}

func Normalize(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("path is empty")
	}
	if strings.Contains(raw, "\\") {
		return "", errors.New("backslashes are not allowed; use slash-separated paths")
	}
	if strings.HasPrefix(raw, "/") || filepath.IsAbs(raw) {
		return "", errors.New("absolute paths are not allowed")
	}
	if strings.Contains(raw, "\x00") {
		return "", errors.New("NUL bytes are not allowed")
	}
	clean := path.Clean(raw)
	if clean == "." || clean == "" {
		return "", errors.New("path is empty")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("path traversal is not allowed")
	}
	if clean != raw {
		return "", fmt.Errorf("path must be normalized exactly as %q", clean)
	}
	root := strings.Split(clean, "/")[0]
	if root != string(RootApp) && root != string(RootContent) && root != string(RootPublic) {
		return "", errors.New("path is outside app/, content/, and public/")
	}
	return clean, nil
}

func PolicyFileFor(cfg Config) PolicyFile {
	return PolicyFile{AllowPublic: append([]string(nil), cfg.AllowPublic...), Exclude: append([]string(nil), cfg.Exclude...)}
}

func EncodePolicyFile(policy PolicyFile) ([]byte, error) { return json.MarshalIndent(policy, "", "  ") }
func DecodePolicyFile(data []byte) (PolicyFile, error) {
	var policy PolicyFile
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&policy); err != nil {
		return PolicyFile{}, err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		if err == nil {
			return PolicyFile{}, errors.New("policy contains multiple JSON values")
		}
		return PolicyFile{}, err
	}
	if d := ValidateConfig(Config{AllowPublic: policy.AllowPublic, Exclude: policy.Exclude}); !d.Empty() {
		return PolicyFile{}, errors.New(d.Error())
	}
	return policy, nil
}

// LoadProjectPolicy reads only the build.bundle portion of a source project's
// gosx.config.json. The source server uses this when no staged bundle-policy
// sidecar exists; generated bundles prefer that sidecar so runtime behavior
// remains independent of the source checkout.
func LoadProjectPolicy(projectDir string) (PolicyFile, error) {
	data, err := os.ReadFile(filepath.Join(projectDir, "gosx.config.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return PolicyFile{}, nil
		}
		return PolicyFile{}, err
	}
	var envelope struct {
		Build json.RawMessage `json:"build"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return PolicyFile{}, err
	}
	if len(envelope.Build) == 0 || string(envelope.Build) == "null" {
		return PolicyFile{}, nil
	}
	var build map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Build, &build); err != nil {
		return PolicyFile{}, err
	}
	rawBundle, ok := build["bundle"]
	if !ok || len(rawBundle) == 0 || string(rawBundle) == "null" {
		return PolicyFile{}, nil
	}
	var bundle Config
	dec := json.NewDecoder(strings.NewReader(string(rawBundle)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&bundle); err != nil {
		return PolicyFile{}, err
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return PolicyFile{}, errors.New("bundle policy contains multiple JSON values")
	}
	if d := ValidateConfig(bundle); !d.Empty() {
		return PolicyFile{}, errors.New(d.Error())
	}
	return PolicyFileFor(bundle), nil
}

func IsExcluded(rel string, values []string) bool {
	for _, raw := range values {
		n, err := Normalize(raw)
		if err == nil && (rel == n || strings.HasPrefix(rel, n+"/")) {
			return true
		}
	}
	return false
}

func IsPublicMutableAllowed(rel string, values []string) bool {
	for _, raw := range values {
		n, err := Normalize(raw)
		if err == nil && n == rel {
			return true
		}
	}
	return false
}

func ValidateTree(rootDir string, root Root, cfg Config) Diagnostics {
	var out Diagnostics
	rootDir = filepath.Clean(rootDir)
	info, err := os.Lstat(rootDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return Diagnostics{{string(root), "cannot inspect root: " + err.Error()}}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Diagnostics{{string(root), "symlink roots are not allowed"}}
	}
	if !info.IsDir() {
		return Diagnostics{{string(root), "bundle root is not a directory"}}
	}
	err = filepath.WalkDir(rootDir, func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			rel, _ := filepath.Rel(rootDir, full)
			out = append(out, Diagnostic{joinRel(string(root), rel), "cannot inspect: " + walkErr.Error()})
			return nil
		}
		within, relErr := filepath.Rel(rootDir, full)
		if relErr != nil {
			out = append(out, Diagnostic{string(root), "cannot normalize path: " + relErr.Error()})
			return nil
		}
		if within == "." {
			return nil
		}
		rel := joinRel(string(root), within)
		info, statErr := os.Lstat(full)
		if statErr != nil {
			out = append(out, Diagnostic{rel, "cannot inspect: " + statErr.Error()})
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			out = append(out, Diagnostic{rel, "symlinks are not allowed (no dereference)"})
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if privateDir(info.Name()) {
				out = append(out, Diagnostic{rel, "private metadata directory is not allowed"})
				return fs.SkipDir
			}
			if secretPath(rel, info.Name()) {
				out = append(out, Diagnostic{rel, "secret or credential material is never allowed in a bundle"})
				return fs.SkipDir
			}
			if IsExcluded(rel, cfg.Exclude) {
				return fs.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			out = append(out, Diagnostic{rel, "only regular files and directories are allowed"})
			return nil
		}
		if secretPath(rel, info.Name()) {
			out = append(out, Diagnostic{rel, "secret or credential material is never allowed in a bundle"})
			return nil
		}
		if mutableState(info.Name()) && !mutableAllowed(rel, root, cfg) {
			out = append(out, Diagnostic{rel, "mutable database/state files are denied unless this exact file is explicitly allowed"})
		}
		if IsExcluded(rel, cfg.Exclude) {
			return nil
		}
		return nil
	})
	if err != nil {
		out = append(out, Diagnostic{string(root), "walk failed: " + err.Error()})
	}
	return out
}

func ValidateProject(projectDir string, cfg Config) Diagnostics {
	out := ValidateConfig(cfg)
	for _, root := range []Root{RootApp, RootContent, RootPublic} {
		out = out.Merge(ValidateTree(filepath.Join(projectDir, string(root)), root, cfg))
	}
	out = out.Merge(validateConfiguredPaths(projectDir, cfg))
	return out
}

// validateConfiguredPaths makes allowances fail closed. An allowance is a
// statement about a concrete source path, not a promise that a future build
// may materialize arbitrary data there. In particular, allowPublic must name
// one existing regular file so that the runtime cannot silently broaden an
// anonymous exposure when a directory is later created at that path.
func validateConfiguredPaths(projectDir string, cfg Config) Diagnostics {
	var out Diagnostics
	for _, raw := range append(append([]string{}, cfg.Allow...), cfg.AllowPublic...) {
		rel, err := Normalize(raw)
		if err != nil {
			continue
		}
		full := filepath.Join(projectDir, filepath.FromSlash(rel))
		info, statErr := os.Lstat(full)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				out = append(out, Diagnostic{rel, "configured bundle path does not exist"})
			} else {
				out = append(out, Diagnostic{rel, "cannot inspect configured bundle path: " + statErr.Error()})
			}
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			out = append(out, Diagnostic{rel, "configured bundle paths cannot be symlinks"})
			continue
		}
		if !info.Mode().IsRegular() {
			out = append(out, Diagnostic{rel, "build.bundle allowances must name exact regular files"})
		}
	}
	return out
}

func AuditProject(projectDir string, cfg Config) Diagnostics {
	var out Diagnostics
	for _, root := range []Root{RootApp, RootContent, RootPublic} {
		out = out.Merge(ValidateTree(filepath.Join(projectDir, string(root)), root, cfg))
	}
	return out
}

// AuditArtifact checks the complete generated artifact after hooks and
// secondary stages have run. The three source roots retain their explicit
// allowance semantics; every other artifact path is hard-deny by default.
func AuditArtifact(distDir string, cfg Config) Diagnostics {
	out := AuditProject(distDir, cfg)
	err := filepath.WalkDir(distDir, func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			rel, _ := filepath.Rel(distDir, full)
			out = append(out, Diagnostic{filepath.ToSlash(rel), "cannot inspect: " + walkErr.Error()})
			return nil
		}
		relOS, err := filepath.Rel(distDir, full)
		if err != nil {
			out = append(out, Diagnostic{"dist", "cannot normalize path: " + err.Error()})
			return nil
		}
		if relOS == "." {
			return nil
		}
		rel := filepath.ToSlash(relOS)
		first := strings.Split(rel, "/")[0]
		if first == string(RootApp) || first == string(RootContent) || first == string(RootPublic) {
			return nil
		}
		info, statErr := os.Lstat(full)
		if statErr != nil {
			out = append(out, Diagnostic{rel, "cannot inspect: " + statErr.Error()})
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			out = append(out, Diagnostic{rel, "symlinks are not allowed (no dereference)"})
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if info.IsDir() {
			if privateDir(info.Name()) {
				out = append(out, Diagnostic{rel, "private metadata directory is not allowed"})
				return fs.SkipDir
			}
			if secretPath(rel, info.Name()) {
				out = append(out, Diagnostic{rel, "secret or credential material is never allowed in a bundle"})
				return fs.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			out = append(out, Diagnostic{rel, "only regular files and directories are allowed"})
			return nil
		}
		if secretPath(rel, info.Name()) {
			out = append(out, Diagnostic{rel, "secret or credential material is never allowed in a bundle"})
		}
		if mutableState(info.Name()) && !artifactPublicAllowed(rel, cfg.AllowPublic) {
			out = append(out, Diagnostic{rel, "mutable database/state files are not allowed outside staged source roots"})
		}
		return nil
	})
	if err != nil {
		out = append(out, Diagnostic{"dist", "walk failed: " + err.Error()})
	}
	return out
}

func artifactPublicAllowed(rel string, allowPublic []string) bool {
	var publicRel string
	switch {
	case strings.HasPrefix(rel, "static/"):
		publicRel = "public/" + strings.TrimPrefix(rel, "static/")
	case strings.HasPrefix(rel, "offline/public/"):
		publicRel = "public/" + strings.TrimPrefix(rel, "offline/public/")
	default:
		return false
	}
	return IsPublicMutableAllowed(publicRel, allowPublic)
}

func CopyTree(src, dst string, root Root, cfg Config) error {
	if d := ValidateTree(src, root, cfg); !d.Empty() {
		return errors.New(d.Error())
	}
	info, err := os.Lstat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s is not a regular directory", src)
	}
	return filepath.WalkDir(src, func(full string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		within, err := filepath.Rel(src, full)
		if err != nil {
			return err
		}
		if within == "." {
			return nil
		}
		rel := joinRel(string(root), within)
		stat, err := os.Lstat(full)
		if err != nil {
			return err
		}
		if stat.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s changed to a symlink during staging", rel)
		}
		if secretPath(rel, stat.Name()) {
			return fmt.Errorf("%s contains secret or credential material", rel)
		}
		if mutableState(stat.Name()) && !mutableAllowed(rel, root, cfg) {
			return fmt.Errorf("%s contains mutable database/state data", rel)
		}
		if IsExcluded(rel, cfg.Exclude) {
			if stat.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if stat.IsDir() {
			return os.MkdirAll(filepath.Join(dst, filepath.FromSlash(within)), 0755)
		}
		if !stat.Mode().IsRegular() || strings.HasSuffix(stat.Name(), "_test.go") {
			return nil
		}
		target := filepath.Join(dst, filepath.FromSlash(within))
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return err
		}
		return copyFile(full, target, stat.Mode().Perm())
	})
}

func mutableAllowed(rel string, root Root, cfg Config) bool {
	if root == RootPublic {
		return IsPublicMutableAllowed(rel, cfg.AllowPublic)
	}
	return (root == RootApp || root == RootContent) && isExactAllowed(rel, cfg.Allow)
}

func AuditTree(rootDir string, roots ...Root) error {
	if len(roots) == 0 {
		roots = []Root{RootApp, RootContent, RootPublic}
	}
	var out Diagnostics
	for _, root := range roots {
		out = out.Merge(ValidateTree(filepath.Join(rootDir, string(root)), root, Config{}))
	}
	if out.Empty() {
		return nil
	}
	return errors.New(out.Error())
}

func PublicPath(publicDir, raw string, allowPublic, excludes []string) (string, bool) {
	rel, err := normalizePublic(raw)
	if err != nil || IsExcluded("public/"+rel, excludes) {
		return "", false
	}
	if secretPath("public/"+rel, filepath.Base(rel)) {
		return "", false
	}
	if mutableState(filepath.Base(rel)) && !IsPublicMutableAllowed("public/"+rel, allowPublic) {
		return "", false
	}
	info, err := os.Lstat(publicDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", false
	}
	current := publicDir
	for _, part := range strings.Split(rel, "/") {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err = os.Lstat(current)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return "", false
		}
	}
	info, err = os.Lstat(current)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return current, true
}

func normalizePublic(raw string) (string, error) {
	if strings.Contains(raw, "\\") || strings.Contains(raw, "\x00") {
		return "", errors.New("invalid public path")
	}
	trimmed := strings.TrimLeft(raw, "/")
	for _, part := range strings.Split(trimmed, "/") {
		if part == ".." {
			return "", errors.New("path traversal is not allowed")
		}
	}
	clean := path.Clean("/" + trimmed)
	if clean == "/" {
		return "", errors.New("invalid public path")
	}
	return strings.TrimPrefix(clean, "/"), nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode&0777)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func joinRel(root, rel string) string {
	if rel == "." || rel == "" {
		return root
	}
	return root + "/" + filepath.ToSlash(rel)
}
func hasRoot(rel, root string) bool { return rel == root || strings.HasPrefix(rel, root+"/") }

func excludedBy(rel string, values map[string]struct{}) bool {
	for value := range values {
		if rel == value || strings.HasPrefix(rel, value+"/") {
			return true
		}
	}
	return false
}
func isExactAllowed(rel string, values []string) bool {
	for _, raw := range values {
		n, err := Normalize(raw)
		if err == nil && n == rel {
			return true
		}
	}
	return false
}
func privateDir(name string) bool {
	switch name {
	case ".git", ".ssh", ".aws", ".gnupg":
		return true
	default:
		return false
	}
}
func secretPath(rel, name string) bool {
	for _, part := range strings.Split(rel, "/") {
		if privateDir(part) {
			return true
		}
	}
	lower := strings.ToLower(name)
	if lower == ".env" || strings.HasPrefix(lower, ".env.") {
		return true
	}
	switch lower {
	case "credentials", "credentials.json", "service-account.json", "client_secret.json", "client-secrets.json", "secrets.json", ".npmrc", ".netrc", "id_rsa", "id_ed25519", "authorized_keys":
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".pem", ".key", ".p12", ".pfx", ".jks", ".keystore":
		return true
	default:
		return false
	}
}
func mutableState(name string) bool {
	lower := strings.ToLower(name)
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}
	switch strings.ToLower(filepath.Ext(lower)) {
	case ".db", ".db3", ".sqlite", ".sqlite3", ".mdb", ".ldb", ".rdb", ".state", ".lock":
		return true
	default:
		return false
	}
}
