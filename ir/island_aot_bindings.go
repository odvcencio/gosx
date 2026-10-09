//go:build !tinygo

package ir

import gotreesitter "github.com/odvcencio/gotreesitter"

func (l *lowerer) collectAOTBindings(_ *gotreesitter.Node) {
	source := append([]byte(nil), l.src...)
	lang := l.lang
	l.prog.aotBindings = &aotSourceBindings{source: source, project: func(p *Program) (aotCheckingFile, error) {
		return aotProjectSource(p, source, lang)
	}, lower: func(data []byte) (*Program, error) {
		tree, err := gotreesitter.NewParser(lang).Parse(data)
		if err != nil {
			return nil, err
		}
		defer tree.Release()
		return Lower(tree.RootNode(), data, lang)
	}}
}
