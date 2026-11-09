//go:build !(js && wasm)

package scene

import (
	"context"
	"errors"
)

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

type TextureWriter struct{}

func NewTextureWriter(string) *TextureWriter { return &TextureWriter{} }
func (*TextureWriter) Prepare() bool         { return false }
func (*TextureWriter) Err() error            { return errSurfaceBrowser }
func (*TextureWriter) Create(string, int, int, TextureFormat) (bool, error) {
	return false, errSurfaceBrowser
}
func (*TextureWriter) WriteTexture(string, int, int, TextureFormat, []byte, int, int) (bool, error) {
	return false, errSurfaceBrowser
}
func (*TextureWriter) Delete(string) (bool, error) { return false, errSurfaceBrowser }
func (*TextureWriter) Dispose()                    {}

func (*Surface) PrepareModels(context.Context, []ModelPreparation) (ModelPreparationResult, error) {
	return ModelPreparationResult{}, errSurfaceBrowser
}
func (*Surface) ApplyTextureProfile(context.Context, TextureDeliveryProfile, []ModelPreparation) (ModelPreparationResult, error) {
	return ModelPreparationResult{}, errSurfaceBrowser
}

func (*Surface) OnGraphicsReset(func(GraphicsReset)) (func(), error) { return nil, errSurfaceBrowser }

func (*Surface) OnFrameSubmitted(func(MountFrameSubmission)) (func(), error) {
	return nil, errSurfaceBrowser
}
func (*Surface) CancelFrameRequest(string) error { return errSurfaceBrowser }
