// Package gpu is GoSX's platform-neutral GPU abstraction layer.
//
// The interfaces here are shaped for WebGPU semantics but kept abstract
// enough that a WebGL2 backend could implement them without the abstraction
// having to grow sideways. Concrete backends live in sibling packages
// (render/gpu/jsgpu for WebGPU via syscall/js; render/gpu/stub for non-WASM
// builds) and are selected by build tag, not at runtime.
//
// Scope boundaries: gpu owns GPU resources and command submission. It does
// not know about scenes, materials, or the GoSX RenderBundle — those belong
// in render/bundle. A gpu.Device is the unit of backend; everything else
// (buffers, shaders, pipelines, bind groups, encoders) is created through it.
package gpu
