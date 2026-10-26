package engine

import "testing"

func TestKernelRuntimeOwnsNoApplicationProgram(t *testing.T) {
	if !RuntimeSupported(RuntimeKernel) || ValidateRuntime(RuntimeKernel, "") != nil {
		t.Fatal("framework kernel runtime is not supported")
	}
	if ValidateRuntime(RuntimeKernel, "/application.wasm") == nil {
		t.Fatal("kernel runtime accepted an application program")
	}
}
