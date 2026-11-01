//go:build !js || !wasm

package wasm

import (
	"context"
	"m31labs.dev/gosx/scene"
)

type Scene3D struct{}

func (Context) Scene3D(any) Scene3D                             { return Scene3D{} }
func (Context) Scene3DByID(string) Scene3D                      { return Scene3D{} }
func (Scene3D) Ready(context.Context) error                     { return ErrUnsupported }
func (Scene3D) Dispatch(context.Context, []scene.Command) error { return ErrUnsupported }
func (Scene3D) Live(context.Context, string, any) error         { return ErrUnsupported }
func (Scene3D) Camera() (scene.IRCamera, error)                 { return scene.IRCamera{}, ErrUnsupported }
func (Scene3D) SetCamera(context.Context, any) error            { return ErrUnsupported }
func (Scene3D) SetOrbit(context.Context, scene.Orbit) error     { return ErrUnsupported }

func (Scene3D) SetAnimationClock(context.Context, float64, bool) error { return ErrUnsupported }

// OceanWaterReader is the browser-only water frame reader; off the browser it
// never has a reading.
type OceanWaterReader struct{}

func (Scene3D) OceanWater() (*OceanWaterReader, error) { return nil, ErrUnsupported }
func (*OceanWaterReader) Read() (scene.OceanWaterFrame, bool) {
	return scene.OceanWaterFrame{}, false
}
func (*OceanWaterReader) SetProbe([]scene.OceanProbePoint)        {}
func (*OceanWaterReader) ReadProbe(*scene.OceanProbeReading) bool { return false }
