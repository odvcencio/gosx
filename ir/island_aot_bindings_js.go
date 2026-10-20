//go:build !tinygo && js

package ir

import gotreesitter "github.com/odvcencio/gotreesitter"

// The browser compiler emits VM programs and never captures host admission data.
func (l *lowerer) collectAOTBindings(_ *gotreesitter.Node) {}
