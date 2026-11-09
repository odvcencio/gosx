package scene_test

import (
	"testing"

	"m31labs.dev/gosx/game/audio"
	"m31labs.dev/gosx/scene"
)

// The game mixer drives crowd noise without scene importing game/audio.
var _ scene.CrowdBus = (*audio.Mixer)(nil)

func TestCrowdNoiseDrivesTheGameMixer(t *testing.T) {
	d, err := scene.NewCrowdDirector(scene.Crowd{Stands: []scene.CrowdStand{{Start: scene.Vector3{X: -10, Z: 20}, End: scene.Vector3{X: 10, Z: 20}, Rows: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.SetExcitement(0, 1, 0.001); err != nil {
		t.Fatal(err)
	}
	if err := d.SetAction(0, scene.CrowdCheer); err != nil {
		t.Fatal(err)
	}
	mixer := audio.NewMixer(crowdTestHost{}, audio.StandardManifest())
	noise := scene.CrowdNoise{Bus: audio.SFXBus, Quiet: 0.1, Loud: 0.9}
	got := noise.Follow(mixer, d.Audio(1), 0)
	if got < 0.85 || mixer.Volume(audio.SFXBus) != got {
		t.Fatalf("bus volume %v, mixer reports %v", got, mixer.Volume(audio.SFXBus))
	}
}

// crowdTestHost is a silent audio host: enough of WebAudio for a mixer.
type crowdTestHost struct{}

type crowdTestNode struct{}

func (crowdTestNode) Connect(audio.Node)                           {}
func (crowdTestNode) Disconnect()                                  {}
func (crowdTestNode) SetGain(float64)                              {}
func (crowdTestNode) Configure(float64, float64, float64, float64) {}

func (crowdTestHost) CurrentTime() float64                         { return 0 }
func (crowdTestHost) State() string                                { return "running" }
func (crowdTestHost) Resume() error                                { return nil }
func (crowdTestHost) CreateGain() audio.GainNode                   { return crowdTestNode{} }
func (crowdTestHost) CreateCompressor() audio.CompressorNode       { return crowdTestNode{} }
func (crowdTestHost) CreateSource(audio.Buffer) audio.SourceNode   { return nil }
func (crowdTestHost) Destination() audio.Node                      { return crowdTestNode{} }
func (crowdTestHost) DecodeAudioData([]byte) (audio.Buffer, error) { return nil, nil }
