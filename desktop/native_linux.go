//go:build linux && (amd64 || arm64)

package desktop

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/ebitengine/purego"
)

var linuxMenuCallback = purego.NewCallback(func(_ uintptr, id uintptr) {
	if a := activeLinuxApp.Load(); a != nil {
		a.mu.Lock()
		fn := a.menuActions[id]
		a.mu.Unlock()
		if fn != nil {
			fn()
		}
	}
})

// GTK uses underscores for mnemonic markers; GoSX menu labels use ampersands.
func linuxMenuLabel(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '_':
			out.WriteString("__")
		case '&':
			if i+1 < len(s) && s[i+1] == '&' {
				out.WriteByte('&')
				i++
			} else if i+1 < len(s) {
				out.WriteByte('_')
			}
		default:
			out.WriteByte(s[i])
		}
	}
	return out.String()
}
func (a *linuxApp) SetMenuBar(menu Menu) error {
	a.mu.Lock()
	a.menu = &menu
	running := a.running
	a.mu.Unlock()
	if !running {
		return nil
	}
	return a.onUI(func(_ *gtkAPI, _, _ uintptr) error { a.installMenu(); return nil })
}
func (a *linuxApp) installMenu() {
	a.mu.Lock()
	g, box, old, menu := a.gtk, a.box, a.menuBar, a.menu
	a.mu.Unlock()
	if box == 0 || menu == nil {
		return
	}
	bar := g.gtkMenuBarNew()
	actions := map[uintptr]func(){}
	var add func(uintptr, []MenuItem)
	add = func(shell uintptr, items []MenuItem) {
		for _, item := range items {
			var widget uintptr
			if item.Separator {
				widget = g.gtkSeparatorMenuItemNew()
			} else if item.Checked {
				widget = g.gtkCheckMenuItemNew(linuxMenuLabel(item.Label))
				g.gtkCheckMenuItemSetActive(widget, 1)
			} else {
				widget = g.gtkMenuItemNew(linuxMenuLabel(item.Label))
			}
			if item.Disabled {
				g.gtkWidgetSetSensitive(widget, 0)
			}
			if item.Submenu != nil {
				sub := g.gtkMenuNew()
				add(sub, item.Submenu.Items)
				g.gtkMenuItemSetSubmenu(widget, sub)
			} else if item.OnClick != nil {
				a.nextAction++
				id := a.nextAction
				actions[id] = item.OnClick
				g.signalConnect(widget, "activate", linuxMenuCallback, id, 0, 0)
			}
			g.gtkMenuShellAppend(shell, widget)
		}
	}
	add(bar, menu.Items)
	if old != 0 {
		g.gtkWidgetDestroy(old)
	}
	a.mu.Lock()
	a.menuBar = bar
	a.menuActions = actions
	a.mu.Unlock()
	g.gtkBoxPackStart(box, bar, 0, 0, 0)
	g.gtkBoxReorderChild(box, bar, 0)
	g.gtkWidgetShowAll(bar)
}
func (a *linuxApp) SetMinSize(width, height int) error {
	if width < 0 || height < 0 {
		return fmt.Errorf("%w: negative minimum size", ErrInvalidOptions)
	}
	a.mu.Lock()
	a.minWidth = width
	a.minHeight = height
	running := a.running
	a.mu.Unlock()
	if !running {
		return nil
	}
	return a.onUI(func(g *gtkAPI, w, v uintptr) error {
		g.gtkWidgetSetSizeRequest(w, int32(width), int32(height))
		return nil
	})
}
func (a *linuxApp) configureFileDialog(g *gtkAPI, dialog uintptr, titleDir, filename string, filters []FileFilter) {
	if titleDir != "" {
		g.gtkFileChooserSetFolder(dialog, titleDir)
	}
	if filename != "" {
		g.gtkFileChooserSetName(dialog, filename)
	}
	for _, filter := range filters {
		f := g.gtkFileFilterNew()
		g.gtkFileFilterSetName(f, filter.Name)
		for _, pattern := range strings.Split(filter.Pattern, ";") {
			if pattern = strings.TrimSpace(pattern); pattern != "" {
				g.gtkFileFilterAddPattern(f, pattern)
			}
		}
		g.gtkFileChooserAddFilter(dialog, f)
	}
}
func linuxCString(g *gtkAPI, p uintptr) string {
	if p == 0 {
		return ""
	}
	size := g.strlen(p)
	if size > 1<<20 {
		return ""
	}
	return string(unsafe.Slice((*byte)(unsafe.Pointer(p)), int(size)))
}

type linuxGSList struct{ data, next uintptr }

func (a *linuxApp) OpenFileDialog(opts OpenFileOptions) ([]string, error) {
	var paths []string
	err := a.onUI(func(g *gtkAPI, w, v uintptr) error {
		dialog := g.gtkFileChooserNativeNew(opts.Title, w, 0, "Open", "Cancel")
		if dialog == 0 {
			return fmt.Errorf("desktop: create open dialog")
		}
		defer g.objectUnref(dialog)
		defer g.gtkNativeDialogDestroy(dialog)
		a.configureFileDialog(g, dialog, opts.InitialDir, "", opts.Filters)
		if opts.InitialFilename != "" {
			g.gtkFileChooserSetFilename(dialog, filepath.Join(opts.InitialDir, opts.InitialFilename))
		}
		if opts.AllowMultiple {
			g.gtkFileChooserSetMultiple(dialog, 1)
		}
		if g.gtkNativeDialogRun(dialog) != -3 {
			return nil
		}
		if opts.AllowMultiple {
			head := g.gtkFileChooserFilenames(dialog)
			defer g.slistFree(head)
			for p := head; p != 0; {
				node := (*linuxGSList)(unsafe.Pointer(p))
				paths = append(paths, linuxCString(g, node.data))
				g.free(node.data)
				p = node.next
			}
		} else {
			p := g.gtkFileChooserFilename(dialog)
			if p != 0 {
				paths = append(paths, linuxCString(g, p))
				g.free(p)
			}
		}
		return nil
	})
	return paths, err
}
func (a *linuxApp) SaveFileDialog(opts SaveFileOptions) (string, error) {
	var path string
	err := a.onUI(func(g *gtkAPI, w, v uintptr) error {
		dialog := g.gtkFileChooserNativeNew(opts.Title, w, 1, "Save", "Cancel")
		if dialog == 0 {
			return fmt.Errorf("desktop: create save dialog")
		}
		defer g.objectUnref(dialog)
		defer g.gtkNativeDialogDestroy(dialog)
		a.configureFileDialog(g, dialog, opts.InitialDir, opts.InitialFilename, opts.Filters)
		if opts.OverwritePrompt {
			g.gtkFileChooserSetOverwrite(dialog, 1)
		}
		if g.gtkNativeDialogRun(dialog) != -3 {
			return nil
		}
		p := g.gtkFileChooserFilename(dialog)
		if p != 0 {
			path = linuxCString(g, p)
			g.free(p)
		}
		if path != "" && opts.DefaultExt != "" && filepath.Ext(path) == "" {
			extended := path + "." + strings.TrimPrefix(opts.DefaultExt, ".")
			if opts.OverwritePrompt {
				if _, err := os.Stat(extended); err == nil {
					return fmt.Errorf("file already exists: %s; select its full name to confirm replacement", extended)
				} else if !os.IsNotExist(err) {
					return err
				}
			}
			path = extended
		}
		return nil
	})
	return path, err
}
func (a *linuxApp) OpenURL(url string) error {
	cmd := exec.Command("xdg-open", url)
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
