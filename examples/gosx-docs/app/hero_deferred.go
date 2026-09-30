package docs

import (
	_ "embed"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/island"
)

//go:generate python3 ../../../scripts/generate-docs-hero-gpu.py

//go:embed hero-gpu.generated.js
var heroGPUProbe string

//go:embed hero-loader.js
var heroLoader string

// HomeHero keeps the scene's manifest and scripts inert until the page-local
// loader upgrades it. A separate document lets Scene3D own its normal bootstrap
// and disposal without upgrading the parent page's lightweight runtime.
func HomeHero() gosx.Node {
	renderer := island.NewRenderer("docs-home-hero")
	mount := renderer.RenderEngine(HeroScene().EngineConfig(), gosx.Text(""))
	return gosx.Fragment(
		gosx.El("div", gosx.Attrs(gosx.Attr("class", "hero__still")),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "hero__still-ring"))),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "hero__still-orb"))),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "hero__still-box"))),
			gosx.El("span", gosx.Attrs(gosx.Attr("class", "hero__still-pyramid"))),
		),
		gosx.El("template", gosx.Attrs(gosx.BoolAttr("data-home-hero")),
			gosx.RawHTML(`<style>html,body{margin:0;width:100%;height:100%;overflow:hidden;background:#000}body>div{width:100%;height:100%}</style>`),
			mount, renderer.ManifestScript(), renderer.BootstrapScript(),
		),
		gosx.RawHTML(`<script data-gosx-navigation-replay="true">(function(){`+heroGPUProbe+heroLoader+`})();</script>`),
	)
}
