package scene

import "m31labs.dev/gosx/det"

// MaxOceanProbePoints is how many points a host may ask the page to read back at once.
const MaxOceanProbePoints = 64

// OceanProbePoint is a world-space position (x, z, metres) at which a host asks for the drawn water's surface.
type OceanProbePoint struct{ X, Z float64 }

// OceanProbeReading is the drawn sea's surface at the points a host asked about, read back from the renderer.
// It is for drawing only: presentation never feeds a simulation. Keep it out of game rules, saves and replays.
//
// The heights are the renderer's displaced sea at the finest level of detail, not the drawn mesh. For each point
// (x, z) the reading gives the height of the surface point that the displacement (choppiness included) moves
// to (x, z): a compute pass on WebGPU and a pass into a float target on WebGL2, both sampling the same displacement
// and slope textures as the ocean shader over all cascades; on the Gerstner sea (no spectrum, or a spectral graph
// that is not running) it is the CPU query that mirrors the shader. Height is world Y, ocean level included (once).
// Normals are the surface normal's x and z components (NormalY derives the third).
//
// What it is not. The vertex shader samples the textures at a level of detail that grows with the mesh spacing
// and so with the distance from the camera; the reading is the level-zero field and does not depend on the camera.
// Against the mip-sampled mesh the difference is about 1 cm RMS at 20 m from the camera, 8 to 9 cm at 160 m and
// 11 to 12 cm at 215 m (three sigma: 4 to 35 cm). That is the sea a ship rides, which is why a host that moves a
// hull wants this field and not the visible waterline. Shoaling over a bathymetry, shore run-up and a ship wake's
// height, which the vertex shader adds, are not in a reading. A point with a NaN or infinite coordinate gives NaN.
//
// Seq and Generation name the drawn frame the reading was issued in: they are the Seq and Generation of the
// OceanWaterFrame of that frame. A host compares Seq with the Seq of the water frame it holds to know the
// age in frames. LatencyFrames is that age when the page wrote the reading.
//
// Latency. The points are issued in frame N, and the page publishes the reading in the record of frame N+L once the
// GPU has finished and the result can be read without waiting. The page never waits for the GPU. Measured on an
// RTX 5070 Ti in Chrome (35 points, every frame): WebGL2 L = 1, WebGPU L = 2 (the fence is polled in the next
// frame's update; WebGPU maps the staging buffer after the frame is submitted). In an engine worker add the message
// hop, 1 frame in the tests' model. A slow GPU or a busy page makes L larger; LatencyFrames and Seq say by how
// much. The first ask on WebGL2 builds one shader; on WebGPU it creates a pipeline asynchronously, so the first
// reading takes a few frames more. A Gerstner reading is computed in the record of the frame it describes
// (LatencyFrames 0).
//
// Flags. Unavailable is true when the page could not read the sea back (the pass could not be built, the
// fence failed, or the pipeline did not build): Count is then 0 and the heights are not valid; never use an old reading as current. A lost WebGPU device does not set it: readings stop and their age (Seq against the water frame's Seq) grows. Gerstner is
// true when the reading is the CPU Gerstner query's. A page that has not loaded the ocean query yet (the first
// Gerstner frames) writes nothing: ReadProbe reports false until it does.
//
// Readings counts the readings the page has written; it changes when a new one arrives. One ocean mount per page: the
// probe state is shared by mounts, like the water frame's.
type OceanProbeReading struct {
	Seq, Generation, Readings uint64
	// Request is the number of the SetProbe call whose points this reading answers (the first call is 1), so a host
	// that moves its points can tell which set of points a reading is for.
	Request               uint64
	Count                 int
	Gerstner, Unavailable bool
	LatencyFrames         uint32
	PageEpochMS           float64

	Height, NormalX, NormalZ [MaxOceanProbePoints]float64
}

// NormalY derives the surface normal's y component of point i from its x and z components.
func (r *OceanProbeReading) NormalY(i int) float64 {
	return det.Sqrt(max(0, 1-float64(r.NormalX[i]*r.NormalX[i])-float64(r.NormalZ[i]*r.NormalZ[i])))
}
