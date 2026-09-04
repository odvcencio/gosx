package uirecipe

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const maxSourceBytes = 1 << 20

func openAppRoot(name string) (*os.Root, error) {
	if runtime.GOOS == "js" || runtime.GOOS == "plan9" {
		return nil, fmt.Errorf("UI recipes require a platform with descriptor-backed os.Root")
	}
	if strings.TrimSpace(name) == "" {
		name = "."
	}
	abs, err := filepath.Abs(name)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("open application root: %w", err)
	}
	for _, required := range []string{"go.mod", "app"} {
		info, err := root.Lstat(required)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || (required == "app" && !info.IsDir()) || (required == "go.mod" && !info.Mode().IsRegular()) {
			root.Close()
			return nil, fmt.Errorf("application root requires a real %s", required)
		}
	}
	return root, nil
}

// Each directory is opened under the application handle and pinned. Lstat is
// a policy check, never the traversal boundary: os.Root enforces that boundary
// during the operation even if an attacker swaps a parent after the check.
func openParent(app *os.Root, target string, create bool) (*os.Root, error) {
	if err := validateCatalogPath("target", target); err != nil {
		return nil, err
	}
	dir := path.Dir(target)
	current := ""
	if dir != "." {
		for _, part := range strings.Split(dir, "/") {
			current = path.Join(current, part)
			info, err := app.Lstat(current)
			if errors.Is(err, os.ErrNotExist) && create {
				if err = app.Mkdir(current, 0755); err != nil && !errors.Is(err, os.ErrExist) {
					return nil, err
				}
				info, err = app.Lstat(current)
			}
			if err != nil {
				return nil, err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("refusing target %q: %s is a symlink", target, current)
			}
			if !info.IsDir() {
				return nil, fmt.Errorf("refusing target %q: parent is not a directory", target)
			}
		}
	}
	before, err := app.Lstat(dir)
	if err != nil {
		return nil, err
	}
	parent, err := app.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	after, err := parent.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		parent.Close()
		return nil, fmt.Errorf("parent of %s changed while opening", target)
	}
	return parent, nil
}

func readRootFile(root *os.Root, target string, limit int64) ([]byte, error) {
	parent, err := openParent(root, target, false)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	return readRegular(parent, path.Base(target), limit)
}

func openRegular(parent *os.Root, name string, flags int, mode os.FileMode) (*os.File, error) {
	before, err := parent.Lstat(name)
	if err != nil && !(errors.Is(err, os.ErrNotExist) && flags&os.O_CREATE != 0) {
		return nil, err
	}
	if err == nil && !before.Mode().IsRegular() {
		return nil, fmt.Errorf("refusing %q: destination is not a regular file (possibly a symlink)", name)
	}
	f, err := parent.OpenFile(name, flags|nonblockingOpenFlag, mode)
	if err != nil {
		return nil, err
	}
	after, statErr := f.Stat()
	linked, linkErr := parent.Lstat(name)
	if statErr != nil || linkErr != nil || !after.Mode().IsRegular() || !linked.Mode().IsRegular() || !os.SameFile(after, linked) || (before != nil && !os.SameFile(before, after)) {
		f.Close()
		return nil, fmt.Errorf("refusing %q: file changed while opening", name)
	}
	return f, nil
}

func readRegular(parent *os.Root, name string, limit int64) ([]byte, error) {
	f, err := openRegular(parent, name, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%s exceeds the %d-byte UI source limit", name, limit)
	}
	return data, nil
}

func lockInstall(metadata *os.Root) (func(), error) {
	f, err := openRegular(metadata, "install.lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		locked, err := tryFileLock(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		if locked {
			return func() { releaseFileLock(f); f.Close() }, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("UI installation is busy; retry after the other installer finishes")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type transactionHook func(phase string, index int) error
type transaction struct {
	app, metadata *os.Root
	entries       []*transactionFile
	hook          transactionHook
	preserve      bool
}
type transactionFile struct {
	target                            string
	parent                            *os.Root
	before, desired                   []byte
	exists, changed, saved, installed bool
	stage, backup                     string
	installedInfo                     os.FileInfo
}

func (tx *transaction) at(phase string, i int) error {
	if tx.hook != nil {
		return tx.hook(phase, i)
	}
	return nil
}

func (tx *transaction) prepare(target string, content []byte) (*transactionFile, error) {
	parent, err := openParent(tx.app, target, true)
	if err != nil {
		return nil, err
	}
	entry := &transactionFile{target: target, parent: parent, desired: content}
	tx.entries = append(tx.entries, entry)
	entry.before, err = readRegular(parent, path.Base(target), maxSourceBytes)
	entry.exists = err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	return entry, nil
}

func stageFile(parent *os.Root, kind string, content []byte) (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	name := ".gosx-ui-" + kind + "-" + hex.EncodeToString(random[:])
	f, err := parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	_, writeErr := f.Write(content)
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		parent.Remove(name)
		return "", err
	}
	return name, nil
}

func (tx *transaction) commit() ([]string, error) {
	changed := false
	for i, entry := range tx.entries {
		if !entry.changed {
			continue
		}
		changed = true
		if err := tx.at("stage", i); err != nil {
			return nil, err
		}
		var err error
		entry.stage, err = stageFile(entry.parent, "stage", entry.desired)
		if err != nil {
			return nil, err
		}
		// Set permissions through the open file, never Root.Chmod's pathname.
		f, err := openRegular(entry.parent, entry.stage, os.O_RDWR, 0)
		if err != nil {
			return nil, err
		}
		err = f.Chmod(0644)
		closeErr := f.Close()
		if err = errors.Join(err, closeErr); err != nil {
			return nil, err
		}
		if entry.exists {
			entry.backup, err = stageFile(entry.parent, "backup", nil)
			if err != nil {
				return nil, err
			}
		}
	}
	if !changed {
		return nil, nil
	}
	if err := tx.at("staged", -1); err != nil {
		return nil, err
	}
	if err := tx.writeJournal(); err != nil {
		return nil, err
	}
	for i, entry := range tx.entries {
		// Check unchanged inputs too: a local edit while staging must abort.
		if err := tx.at("install", i); err != nil {
			return nil, tx.rollback(err)
		}
		if err := tx.revalidate(entry); err != nil {
			return nil, tx.rollback(err)
		}
		if !entry.changed {
			continue
		}
		if entry.exists {
			if err := entry.parent.Rename(path.Base(entry.target), entry.backup); err != nil {
				return nil, tx.rollback(err)
			}
			entry.saved = true
		}
		// Publish only to an absent name, preserving a file created by an editor
		// after revalidation. A filesystem without hard links fails closed.
		info, err := entry.parent.Lstat(entry.stage)
		if err != nil {
			return nil, tx.rollback(err)
		}
		if !info.Mode().IsRegular() {
			return nil, tx.rollback(fmt.Errorf("staged file for %s changed", entry.target))
		}
		if err := tx.at("publish", i); err != nil {
			return nil, tx.rollback(err)
		}
		if err := entry.parent.Link(entry.stage, path.Base(entry.target)); err != nil {
			return nil, tx.rollback(err)
		}
		entry.installedInfo = info
		entry.installed = true
		if err := entry.parent.Remove(entry.stage); err != nil {
			return nil, tx.rollback(err)
		}
		entry.stage = ""
	}
	// A completed source+manifest transaction stays successful if only removal
	// of recovery files fails. Report that condition and retain the journal.
	if err := tx.cleanup(); err != nil {
		tx.preserve = true
		return []string{"installation committed; recovery cleanup is pending: " + err.Error()}, nil
	}
	return nil, nil
}

func (tx *transaction) revalidate(entry *transactionFile) error {
	current, err := openParent(tx.app, entry.target, false)
	if err != nil {
		return err
	}
	defer current.Close()
	a, err := current.Stat(".")
	if err != nil {
		return err
	}
	b, err := entry.parent.Stat(".")
	if err != nil || !os.SameFile(a, b) {
		return fmt.Errorf("parent of %s changed during installation", entry.target)
	}
	data, err := readRegular(entry.parent, path.Base(entry.target), maxSourceBytes)
	if !entry.exists && errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !entry.exists || !bytes.Equal(data, entry.before) {
		return fmt.Errorf("%s changed during installation", entry.target)
	}
	return nil
}

type recoveryFile struct {
	Path         string `json:"path"`
	Existed      bool   `json:"existed"`
	BeforeSHA256 string `json:"beforeSHA256"`
	AfterSHA256  string `json:"afterSHA256"`
	Stage        string `json:"stage"`
	Backup       string `json:"backup,omitempty"`
}

func (tx *transaction) writeJournal() error {
	var entries []recoveryFile
	for _, e := range tx.entries {
		if e.changed {
			entries = append(entries, recoveryFile{e.target, e.exists, contentHash(e.before), contentHash(e.desired), e.stage, e.backup})
		}
	}
	data, err := json.MarshalIndent(struct {
		SchemaVersion int            `json:"schemaVersion"`
		Files         []recoveryFile `json:"files"`
	}{1, entries}, "", "  ")
	if err != nil {
		return err
	}
	f, err := tx.metadata.OpenFile("transaction.json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	if err == nil {
		err = f.Sync()
	}
	err = errors.Join(err, f.Close())
	if err != nil {
		tx.metadata.Remove("transaction.json")
	}
	return err
}

func (tx *transaction) rollback(cause error) error {
	var failures []error
	for i := len(tx.entries) - 1; i >= 0; i-- {
		e := tx.entries[i]
		if !e.installed && !e.saved {
			continue
		}
		if err := tx.at("rollback", i); err != nil {
			failures = append(failures, err)
			continue
		}
		if e.installed {
			info, err := e.parent.Lstat(path.Base(e.target))
			data, readErr := readRegular(e.parent, path.Base(e.target), maxSourceBytes)
			if err != nil || readErr != nil || !os.SameFile(info, e.installedInfo) || !bytes.Equal(data, e.desired) {
				failures = append(failures, fmt.Errorf("%s changed after installation; retaining it and recovery files", e.target))
				continue
			}
			if err := e.parent.Remove(path.Base(e.target)); err != nil {
				failures = append(failures, err)
				continue
			}
			e.installed = false
		}
		if e.saved {
			// A newly created destination during rollback also remains owned by
			// its creator. Keep the saved original available in that case.
			if err := e.parent.Link(e.backup, path.Base(e.target)); err != nil {
				failures = append(failures, err)
				continue
			}
			if err := e.parent.Remove(e.backup); err != nil {
				failures = append(failures, err)
				continue
			}
			e.saved = false
			e.backup = ""
		}
	}
	if len(failures) == 0 {
		if err := tx.cleanup(); err != nil {
			failures = append(failures, err)
		}
	}
	if len(failures) > 0 {
		tx.preserve = true
		return fmt.Errorf("installation failed (%v); rollback or recovery cleanup also failed (%v); preserve .gosx/ui/transaction.json and its recovery files for manual recovery", cause, errors.Join(failures...))
	}
	return fmt.Errorf("installation failed and was rolled back: %w", cause)
}

func (tx *transaction) cleanup() error {
	var failures []error
	for _, e := range tx.entries {
		for _, name := range []string{e.stage, e.backup} {
			if name == "" {
				continue
			}
			if err := e.parent.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
				failures = append(failures, err)
			}
		}
	}
	if len(failures) > 0 {
		return errors.Join(failures...)
	}
	if err := tx.metadata.Remove("transaction.json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (tx *transaction) close() {
	if !tx.preserve {
		for _, e := range tx.entries {
			for _, name := range []string{e.stage, e.backup} {
				if name != "" {
					e.parent.Remove(name)
				}
			}
		}
	}
	for _, e := range tx.entries {
		e.parent.Close()
	}
}
