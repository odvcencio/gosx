//go:build !tinygo

// Package gsxparse shares parser setup without sharing a parser concurrently.
package gsxparse

import (
	"sync"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

var pools sync.Map

func Parse(language *gotreesitter.Language, source []byte) (*gotreesitter.Tree, error) {
	pool, ok := pools.Load(language)
	if !ok {
		pool, _ = pools.LoadOrStore(language, gotreesitter.NewParserPool(language))
	}
	return pool.(*gotreesitter.ParserPool).Parse(source)
}
