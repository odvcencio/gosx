package app

import (
	model "example.com/gosx-islands-files/model"
	"m31labs.dev/gosx/signal"
)

type ImportedMapProps struct {
	Flags map[string]model.Flag
}

//gosx:island
func ImportedMap(props ImportedMapProps) Node {
	visible := signal.New(false)
	show := func() { visible.Set(true) }
	return <section id="imported-map">
		<button id="imported-map-show" onClick={show}>Show imported map value</button>
		<If cond={visible.Get()}><span id="imported-map-value">{props.Flags["missing"]}</span></If>
	</section>
}
