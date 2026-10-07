//go:build !js || !wasm

package scene

import "m31labs.dev/gosx/engine"

// ShaderProgram returns an authored target artifact retained in the descriptor.
// Availability describes transport, not execution support on the current host.
func (m CustomMaterial) ShaderProgram(target string) (engine.ShaderProgram, bool) {
	return engine.ShaderProgramFromLayout(m.ShaderLayout, target)
}

// ShaderProgram returns a retained target artifact from canonical material IR.
func (m IRMaterial) ShaderProgram(target string) (engine.ShaderProgram, bool) {
	return engine.ShaderProgramFromLayout(m.ShaderLayout, target)
}
