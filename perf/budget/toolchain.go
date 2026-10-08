package budget

import (
	"errors"
	"runtime/debug"
)

type PlatformArchives struct {
	LinuxX64   string `json:"linux-x64,omitempty"`
	WindowsX64 string `json:"windows-x64,omitempty"`
}

// Toolchain records normalization pins separately from release producers.
type Toolchain struct {
	Schema           string           `json:"schema"`
	Go               string           `json:"go"`
	TinyGo           string           `json:"tinygo"`
	LLVM             string           `json:"llvm"`
	Binaryen         string           `json:"binaryen"`
	BinaryenSHA256   string           `json:"binaryenSHA256"`
	Brotli           string           `json:"brotli"`
	BrotliQuality    int              `json:"brotliQuality"`
	BrotliWindow     int              `json:"brotliWindow"`
	GzipLevel        int              `json:"gzipLevel"`
	Zopfli           string           `json:"zopfli"`
	ZopfliIterations int              `json:"zopfliIterations"`
	BuilderBrotli    string           `json:"builderBrotli"`
	Esbuild          string           `json:"esbuild"`
	ChromeSnapshot   string           `json:"chromeSnapshot"`
	ChromeProduct    string           `json:"chromeProduct"`
	PlatformArchives PlatformArchives `json:"platformArchives"`
	Fonts            []Ref            `json:"fonts"`
	CDProto          string           `json:"cdproto"`
	Chromedp         string           `json:"chromedp"`
	Node             string           `json:"node"`
}

func LoadToolchain(path string, opts LoadOptions) (*Toolchain, error) {
	var t Toolchain
	root, err := loadInput(path, opts, "Toolchain", &t)
	if err != nil {
		return nil, err
	}
	if err := t.validate(root); err != nil {
		return nil, err
	}
	return &t, nil
}

func (t Toolchain) validate(root string) error {
	if info, ok := debug.ReadBuildInfo(); ok {
		if err := t.validateModules(info.Deps); err != nil {
			return err
		}
	}
	seen := make(map[string]bool)
	for _, font := range t.Fonts {
		if seen[font.File] {
			return errors.New("duplicate font reference")
		}
		seen[font.File] = true
		if _, err := readReference(root, font, 16<<20); err != nil {
			return err
		}
	}
	return nil
}

func (t Toolchain) validateModules(modules []*debug.Module) error {
	pins := map[string]string{"github.com/andybalholm/brotli": t.Brotli, "github.com/chromedp/cdproto": t.CDProto, "github.com/chromedp/chromedp": t.Chromedp}
	for _, module := range modules {
		if pin, ok := pins[module.Path]; ok {
			if module.Replace != nil || module.Version != pin {
				return errors.New("selected module does not match toolchain pin")
			}
		}
	}
	return nil
}
