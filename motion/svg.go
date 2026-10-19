package motion

import (
	"fmt"
	"strings"
)

// SVGMorphBinding samples compatible geometry between two server-rendered SVG
// snapshots. From and To are selectors within the MotionScope. Actors with the
// same data-morph-id retain their identity; incompatible geometry fades in.
// Signal supplies absolute progress from 0 to 1, including backward seeks.
type SVGMorphBinding struct {
	Signal SignalRef `json:"signal"`
	From   string    `json:"from"`
	To     string    `json:"to"`
}

// BindSVGMorph connects a scalar signal to SVG snapshots. Keep the source hidden
// and the destination readable in server HTML; GoSX enhances only the destination.
// Use Curve or Map with a Playhead to share playback with DOM and Scene3D motion.
func (p *Program) BindSVGMorph(signal SignalRef, from, to string) {
	p.SVGMorphs = append(p.SVGMorphs, SVGMorphBinding{Signal: signal, From: strings.TrimSpace(from), To: strings.TrimSpace(to)})
}

func validateSVGMorphs(bindings []SVGMorphBinding, ids map[string]struct{}) error {
	if len(bindings) > 128 {
		return fmt.Errorf("motion: at most 128 SVG morph bindings are supported")
	}
	for _, binding := range bindings {
		if _, exists := ids[string(binding.Signal)]; !exists {
			return fmt.Errorf("motion: SVG morph references unknown signal %q", binding.Signal)
		}
		if strings.TrimSpace(binding.From) == "" || strings.TrimSpace(binding.To) == "" || binding.From == binding.To || len(binding.From) > 1024 || len(binding.To) > 1024 {
			return fmt.Errorf("motion: SVG morph requires distinct source and destination selectors of at most 1024 bytes")
		}
	}
	return nil
}
