//go:build !tinygo

package gsxparse

import (
	"fmt"
	"sync"
	"testing"

	"github.com/odvcencio/gotreesitter/grammars"
)

func TestParserPoolRetainedTrees(t *testing.T) {
	language := grammars.GoLanguage()
	var workers sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		workers.Go(func() {
			for i := 0; i < 20; i++ {
				first, err := Parse(language, []byte("package example\nvar value=1\n"))
				if err != nil {
					t.Error(err)
					return
				}
				second, err := Parse(language, []byte(fmt.Sprintf("package example\nvar label=%q\n", "a different source")))
				if err != nil {
					first.Release()
					t.Error(err)
					return
				}
				if first.RootNode().HasError() || first.RootNode().EndByte() != 28 || second.RootNode().HasError() {
					t.Error("pooled parse changed a retained tree")
				}
				second.Release()
				first.Release()
			}
		})
	}
	workers.Wait()
}
