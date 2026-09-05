package main

import (
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

const (
	initTemplateApp  = "app"
	initTemplateDocs = "docs"
)

type scaffoldFile struct {
	Path     string
	Contents string
}

//go:embed templates/docs
var initTemplateFS embed.FS

func normalizeInitTemplate(template string) (string, error) {
	template = strings.ToLower(strings.TrimSpace(template))
	if template == "" {
		return initTemplateApp, nil
	}
	switch template {
	case initTemplateApp, initTemplateDocs:
		return template, nil
	default:
		return "", fmt.Errorf("unknown init template %q", template)
	}
}

func scaffoldFilesForTemplate(module, template string) ([]scaffoldFile, error) {
	switch template {
	case initTemplateApp:
		return []scaffoldFile{
			{Path: "go.mod", Contents: goModTemplate(module)},
			{Path: "main.go", Contents: mainTemplate(module)},
			{Path: ".env", Contents: envTemplate()},
			{Path: ".gitignore", Contents: gitignoreTemplate()},
			{Path: "app/layout.gsx", Contents: appLayoutTemplate()},
			{Path: "app/page.server.go", Contents: appHomeServerTemplate()},
			{Path: "app/page.gsx", Contents: appHomeTemplate()},
			{Path: "app/stack/page.server.go", Contents: appStackServerTemplate()},
			{Path: "app/stack/page.gsx", Contents: appStackTemplate()},
			{Path: "app/not-found.gsx", Contents: appNotFoundTemplate()},
			{Path: "app/error.gsx", Contents: appErrorTemplate()},
			{Path: "modules/modules.go", Contents: modulesTemplate(module)},
			{Path: "public/favicon.ico", Contents: starterFaviconICO()},
			{Path: "public/styles.css", Contents: stylesTemplate()},
			{Path: "perf-budget.json", Contents: starterPerfBudgetTemplate()},
		}, nil
	case initTemplateDocs:
		return docsTemplateFiles(module)
	default:
		return nil, fmt.Errorf("unknown init template %q", template)
	}
}

func docsTemplateFiles(module string) ([]scaffoldFile, error) {
	files := []scaffoldFile{
		{Path: "go.mod", Contents: goModTemplate(module)},
		{Path: ".env", Contents: docsEnvTemplate()},
		{Path: ".gitignore", Contents: gitignoreTemplate()},
	}

	err := fs.WalkDir(initTemplateFS, "templates/docs", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		contents, err := fs.ReadFile(initTemplateFS, path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		rel := strings.TrimPrefix(path, "templates/docs/")
		if rel == "" {
			return nil
		}
		if strings.HasSuffix(rel, ".gotmpl") {
			rel = strings.TrimSuffix(rel, ".gotmpl") + ".go"
		}
		files = append(files, scaffoldFile{
			Path:     rel,
			Contents: strings.ReplaceAll(string(contents), "__MODULE__", module),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})
	return files, nil
}

func starterFaviconICO() string {
	data, err := hex.DecodeString(starterFaviconICOHex)
	if err != nil {
		panic(fmt.Sprintf("decode starter favicon.ico: %v", err))
	}
	return string(data)
}

// starterFaviconICOHex is a 32x32 Windows ICO derived from
// examples/gosx-docs/public/favicon.svg at source SHA-256
// 98ad7c92213ae356d03593d24d76db32750ea6b764b8bee90c04960b5425b379.
//
// Derivation used for this source-controlled fixture:
// convert examples/gosx-docs/public/favicon.svg -define icon:auto-resize=32 favicon.ico
// Output: 4286 bytes, SHA-256 a9bef67f2f133707710a890da15b0b9e087eaa1fed190b35f4f3d6e663deb735.
const starterFaviconICOHex = "0000010001002020000001002000a8100000160000002800000020000000400000000100200000000000001000000000" +
	"0000000000000000000000000000ffffffffdfdfdfff585858ff040404ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff212121ffa1a1a1fffcfcfcffeaea" +
	"eaff535353ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff161616ffa1a1a1ff8d8d8dff070707ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff222222ff3c3c3cff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0909" +
	"09ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff030303ff0d0d" +
	"0dff161616ff161818ff071317ff061317ff061317ff061318ff030a0cff000000ff000000ff010203ff051115ff0613" +
	"17ff061317ff061418ff030b0dff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff020202ff1e1e1eff5d5d5dff888888ffaaaaaaffc6c5c5ffc5caccff5daac2ff2f9b" +
	"bdff319cbdff329ec0ff205e71ff040405ff000000ff091d24ff2d8fadff319dbeff319cbeff2f96b6ff10333eff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff121212ff6b6b" +
	"6bffc4c4c4ffe5e5e5ffecececffebebebffeaeaeaffeceaeaffafd7e3ff3fb3d6ff37b0d5ff37b1d6ff43a9c8ff6f7f" +
	"84ff302e2eff1d5769ff37b0d5ff37b1d6ff38b2d8ff277e98ff030b0dff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff181818ff9d9d9dffe6e6e6ffeaeaeaffe8e8e8ffe8e8e8ffe8e8" +
	"e8ffe8e8e8ffe9e8e8ffe0e5e7ff70c1daff35aed4ff37afd4ff39b0d5ff9ed1e1ffc1cdd1ff41a2c0ff36afd5ff37b0" +
	"d5ff34a5c8ff10343fff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff060606ff878787ffeaeaeaffe9e9e9ffe8e8e8ffe8e8e8ffe8e8e8ffe8e8e8ffeaeaeaffeaeaeaffeceaeaffbada" +
	"e4ff44b4d7ff36afd4ff35aed4ff60bcd9ff96cfe0ff3cb1d6ff37afd4ff38b1d6ff24728aff020708ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff3e3e3effd8d8d8ffe9e9e9ffe8e8" +
	"e8ffe8e8e8ffe8e8e8ffe8e8e8ffe5e5e5ffd1d1d1ffcfcfcfffcfcfcfffc9cdceff68b3cbff34aed3ff37afd4ff3eb1" +
	"d5ff4fb7d7ff36afd4ff37b0d5ff33a1c4ff0e2b35ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff010101ff818181ffebebebffe8e8e8ffe8e8e8ffe8e8e8ffe8e8e8ffeaeaeaffd1d1" +
	"d1ff303030ff171717ff181818ff171616ff1a353dff36a4c5ff37b0d5ff37afd4ff36afd4ff37afd4ff38b1d6ff216a" +
	"80ff010405ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0e0e" +
	"0effadadadffebebebffe8e8e8ffe8e8e8ffe8e8e8ffe8e8e8ffeaeaeaffcfcfcfff191919ff0a0a0aff181818ff1616" +
	"16ff2c2d2eff6ab0c6ff37b0d5ff37afd4ff37afd4ff37b0d5ff329ebfff0c252dff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff171717ffc9c9c9ffeaeaeaffe8e8e8ffe8e8" +
	"e8ffe8e8e8ffe8e8e8ffeaeaeaffcfcfcfff151515ff666666ffd3d3d3ffcfcfcfffd4d2d2ffc0d9e1ff44b3d6ff36af" +
	"d4ff37afd4ff38b2d7ff216a80ff010303ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff151515ffc3c3c3ffeaeaeaffe8e8e8ffe8e8e8ffe8e8e8ffe8e8e8ffeaeaeaffcfcf" +
	"cfff151515ff737373ffeeeeeeffeaeaeaffeceaeaffa9d4e1ff3cb1d5ff36afd4ff37afd4ff38b1d6ff29829dff040c" +
	"0fff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0b0b" +
	"0bffa2a2a2ffebebebffe8e8e8ffe8e8e8ffe8e8e8ffe8e8e8ffeaeaeaffcfcfcfff151515ff727272ffecececffe9e8" +
	"e8ffdce4e7ff63bdd9ff35aed4ff37afd4ff37afd4ff37afd5ff36abcfff14404dff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff7c7c7cffebebebffe8e8e8ffe8e8" +
	"e8ffe8e8e8ffe8e8e8ffe9e9e9ffdbdbdbff7e7e7effadadadffeaeaeaffeae9e8ffa8d3e1ff3bb0d4ff36afd4ff38af" +
	"d4ff3bb0d4ff37afd4ff38b1d7ff29849fff040c0fff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff3f3f3fffdadadaffe9e9e9ffe8e8e8ffe8e8e8ffe8e8e8ffe8e8e8ffe8e8" +
	"e8ffecececffeaeaeaffe9e8e8ffdce4e7ff63bdd9ff35aed4ff36afd4ff48b4d6ff68bfdaff36afd4ff37afd5ff36ab" +
	"cfff14404dff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff080808ff929292ffebebebffe9e9e9ffe8e8e8ffe8e8e8ffe8e8e8ffe8e8e8ffe8e8e8ffe8e8e8ffeae9e8ffa8d3" +
	"e1ff3bb0d4ff37afd4ff34aed4ff6dc0daffa8d4e1ff3fb2d6ff36afd4ff38b1d7ff29849fff040c0fff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff1f1f1fffa3a3a3ffe2e2" +
	"e2ffecececffecececffecececffecececffecececffedededffdee7eaff63bedaff35b0d5ff37b0d6ff3ab1d6ffa4d5" +
	"e4ffd7e0e3ff45a5c3ff36b0d6ff38b1d6ff36acd1ff14414eff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff111111ff4e4e4eff737373ff747474ff747474ff7474" +
	"74ff747474ff757373ff6c909bff379fbfff319cbdff319cbdff3697b5ff65767cff686767ff286375ff319ebfff319c" +
	"bdff329ec0ff24728aff03080aff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff02090bff0613" +
	"17ff061317ff061317ff051115ff010203ff000000ff030a0cff061318ff061317ff061317ff061216ff010203ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff1c1c1cff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff5e5e" +
	"5eff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff050505ffc0c0c0ff1d1d1dff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff010101ff595959fffdfdfdffa2a2a2ff1c1c1cff010101ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff060606ff535353ffdfdfdfffffff" +
	"fffffdfdfdffc1c1c1ff5e5e5eff1d1d1dff010101ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff000000ff0000" +
	"00ff000000ff000000ff080808ff393939ff8c8c8cffeaeaeaffffffffff000000000000000000000000000000000000" +
	"000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000" +
	"000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000" +
	"0000000000000000000000000000"

func docsEnvTemplate() string {
	return `PORT=8080
SESSION_SECRET=change-me-in-production
GOSX_ENV=development
`
}

func starterPerfBudgetTemplate() string {
	return `{
  "defaultProfile": "starter",
  "profiles": {
    "starter": {
      "description": "Default budget for the scaffolded server-first starter app.",
      "assertions": [
        "lcp <= 2500",
        "long_tasks <= 2",
        "tbt <= 150",
        "js_total_kb <= 120"
      ]
    }
  },
  "routes": [
    {"url": "/", "profile": "starter"},
    {"url": "/stack", "profile": "starter"}
  ]
}
`
}
