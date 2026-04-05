package workspace

import (
	"github.com/odvcencio/gosx/crdt"
	"github.com/odvcencio/gosx/vecdb"
)

// observer watches a CRDT doc for vector mutations and keeps a vecdb index in sync.
type observer struct {
	doc *crdt.Doc
	idx *vecdb.Index
	dim int
}

func newObserver(doc *crdt.Doc, idx *vecdb.Index, dim int) *observer {
	obs := &observer{doc: doc, idx: idx, dim: dim}
	doc.OnChange(obs.handlePatches)
	return obs
}

func (o *observer) handlePatches(patches []crdt.Patch) {
	for _, p := range patches {
		switch p.Action {
		case "put":
			if p.Value.Kind != crdt.ValueKindVector {
				continue
			}
			vec := p.Value.Vector()
			if len(vec) != o.dim {
				continue
			}
			o.idx.Add(string(p.Prop), vec)
		case "delete":
			o.idx.Remove(string(p.Prop))
		}
	}
}
