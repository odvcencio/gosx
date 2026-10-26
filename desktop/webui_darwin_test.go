//go:build darwin && !ios && (amd64 || arm64)

package desktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type webFileReadout struct {
	Kind  string        `json:"kind"`
	Event string        `json:"event"`
	Files []webFileInfo `json:"files"`
	Error string        `json:"error"`
}

type webFileInfo struct {
	Name         string `json:"name"`
	Size         int    `json:"size"`
	Text         string `json:"text"`
	RelativePath string `json:"relativePath"`
}

// The page reads the browser-created File objects rather than merely reporting
// a panel callback. This also exercises WebKit's access to the selected URLs.
func webFilePage(attributes string) string {
	return `<!doctype html><body><input id="files" type="file" ` + attributes + `>
<script>
const input = document.getElementById('files');
async function reportFiles(event) {
  try {
    const files = await Promise.all(Array.from(input.files, async file => ({
      name: file.name, size: file.size, text: await file.text(),
      relativePath: file.webkitRelativePath
    })));
    files.sort((a, b) => a.relativePath.localeCompare(b.relativePath) || a.name.localeCompare(b.name));
    chrome.webview.postMessage(JSON.stringify({kind: 'gosx-web-file-input', event: event.type, files}));
  } catch (error) {
    chrome.webview.postMessage(JSON.stringify({kind: 'gosx-web-file-input', error: String(error)}));
  }
}
input.addEventListener('change', reportFiles);
input.addEventListener('cancel', reportFiles);
</script>`
}

func webFileMessages() (chan webFileReadout, func(string)) {
	messages := make(chan webFileReadout, 8)
	return messages, func(message string) {
		var result webFileReadout
		if json.Unmarshal([]byte(message), &result) == nil && result.Kind == "gosx-web-file-input" {
			select {
			case messages <- result:
			default:
			}
		}
	}
}

func clickWebFileInput(t *testing.T, app *App, messages <-chan webFileReadout, event string, files []webFileInfo) bool {
	t.Helper()
	if err := app.ExecuteScript(`document.getElementById('files').click()`); err != nil {
		t.Errorf("click file input: %v", err)
		return false
	}
	select {
	case result := <-messages:
		if result.Error != "" || result.Event != event || !reflect.DeepEqual(result.Files, files) {
			t.Errorf("file input = %+v, want event %q and files %+v", result, event, files)
			return false
		}
		return true
	case <-time.After(5 * time.Second):
		t.Errorf("file input never reported %q", event)
		return false
	}
}

func checkWebFilePanel(t *testing.T, panel uintptr, multiple, directories bool) {
	t.Helper()
	if pthreadMainNP() == 0 {
		t.Error("file panel callback did not run on the main thread")
	}
	if got := sendBool0(panel, objcSel("allowsMultipleSelection")); got != multiple {
		t.Errorf("allowsMultipleSelection = %v, want %v", got, multiple)
	}
	if got := sendBool0(panel, objcSel("canChooseDirectories")); got != directories {
		t.Errorf("canChooseDirectories = %v, want %v", got, directories)
	}
	if got := sendBool0(panel, objcSel("canChooseFiles")); got == directories {
		t.Errorf("canChooseFiles = %v, want %v", got, !directories)
	}
}

func webFileURLs(paths ...string) uintptr {
	urls := make([]uintptr, 0, len(paths))
	for _, path := range paths {
		urls = append(urls, send1(objcClass("NSURL"), objcSel("fileURLWithPath:"), nsString(path)))
	}
	return nsArray(urls)
}

func writeWebFile(t *testing.T, directory, name, text string) (string, webFileInfo) {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, webFileInfo{Name: name, Size: len(text), Text: text}
}

func TestDarwinWebFileInputSelection(t *testing.T) {
	requireGUI(t)
	for _, multiple := range []bool{false, true} {
		name, attributes := "single", ""
		if multiple {
			name, attributes = "multiple", "multiple"
		}
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			first, firstInfo := writeWebFile(t, directory, "A vocal ♫.txt", "A voice with Unicode ♫ and\na second line.\n")
			paths, want := []string{first}, []webFileInfo{firstInfo}
			if multiple {
				second, secondInfo := writeWebFile(t, directory, "B harmony.txt", "A second selected part.\n")
				paths, want = append(paths, second), append(want, secondInfo)
			}
			old := runWebFilePanel
			defer func() { runWebFilePanel = old }()
			calls := 0
			runWebFilePanel = func(panel uintptr) uintptr {
				calls++
				checkWebFilePanel(t, panel, multiple, false)
				return webFileURLs(paths...)
			}
			messages, receive := webFileMessages()
			runTestApp(t, Options{HTML: webFilePage(attributes), OnWebMessage: receive}, func(app *App, _ *darwinApp) {
				clickWebFileInput(t, app, messages, "change", want)
			})
			if calls != 1 {
				t.Errorf("file panel requests = %d, want 1", calls)
			}
		})
	}
}

func TestDarwinWebFileInputCancelAndReselect(t *testing.T) {
	requireGUI(t)
	directory := t.TempDir()
	first, firstInfo := writeWebFile(t, directory, "first take.txt", "First take.\n")
	second, secondInfo := writeWebFile(t, directory, "second take.txt", "Replacement take.\n")
	old := runWebFilePanel
	defer func() { runWebFilePanel = old }()
	calls := 0
	runWebFilePanel = func(panel uintptr) uintptr {
		calls++
		checkWebFilePanel(t, panel, false, false)
		switch calls {
		case 1:
			return webFileURLs(first)
		case 2:
			return 0 // Cancel without a visible panel or keyboard automation.
		case 3:
			return webFileURLs(second)
		default:
			t.Errorf("unexpected file panel request %d", calls)
			return 0
		}
	}
	messages, receive := webFileMessages()
	runTestApp(t, Options{HTML: webFilePage(""), OnWebMessage: receive}, func(app *App, _ *darwinApp) {
		if !clickWebFileInput(t, app, messages, "change", []webFileInfo{firstInfo}) {
			return
		}
		if !clickWebFileInput(t, app, messages, "cancel", []webFileInfo{firstInfo}) {
			return
		}
		clickWebFileInput(t, app, messages, "change", []webFileInfo{secondInfo})
	})
	if calls != 3 {
		t.Errorf("file panel requests = %d, want 3", calls)
	}
}

func TestDarwinWebDirectoryInputSelection(t *testing.T) {
	requireGUI(t)
	directory := filepath.Join(t.TempDir(), "Sample pack")
	nested := filepath.Join(directory, "harmonies")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	_, lead := writeWebFile(t, directory, "lead.txt", "Directory lead.\n")
	_, harmony := writeWebFile(t, nested, "backing.txt", "Nested harmony.\n")
	lead.RelativePath = "Sample pack/lead.txt"
	harmony.RelativePath = "Sample pack/harmonies/backing.txt"
	old := runWebFilePanel
	defer func() { runWebFilePanel = old }()
	calls := 0
	runWebFilePanel = func(panel uintptr) uintptr {
		calls++
		checkWebFilePanel(t, panel, true, true)
		return webFileURLs(directory)
	}
	messages, receive := webFileMessages()
	runTestApp(t, Options{HTML: webFilePage("multiple webkitdirectory"), OnWebMessage: receive}, func(app *App, _ *darwinApp) {
		clickWebFileInput(t, app, messages, "change", []webFileInfo{harmony, lead})
	})
	if calls != 1 {
		t.Errorf("directory panel requests = %d, want 1", calls)
	}
}
