//go:build !(js && wasm)

package scene

import "errors"

var errSurfaceBrowser = errors.New("scene surface requires js/wasm")

type Surface struct{}

func NewSurface(string) *Surface                          { return &Surface{} }
func (*Surface) DispatchCommands(MountCommandBatch) error { return errSurfaceBrowser }
func (*Surface) Update(SurfaceOptions) error              { return errSurfaceBrowser }
func (*Surface) Resize(int, int) (bool, error)            { return false, errSurfaceBrowser }
func (*Surface) Dispose()                                 {}

type ContextLoss struct{}

func (*Surface) LoseContext() (*ContextLoss, error) { return nil, errSurfaceBrowser }
func (*ContextLoss) Restore() error                 { return errSurfaceBrowser }

type StreamWriter struct{}

func NewStreamWriter(string) *StreamWriter                    { return &StreamWriter{} }
func (*StreamWriter) Prepare() bool                           { return false }
func (*StreamWriter) Apply(InstanceStreamFrame) (bool, error) { return false, errSurfaceBrowser }
func (*StreamWriter) Dispose()                                {}
