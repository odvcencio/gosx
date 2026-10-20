package docs

import (
	"testing"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

func TestBlackglassLightingUpdatesPreserveVesselAndCollectedModels(t *testing.T) {
	for _, from := range beachgen.Periods {
		for _, to := range beachgen.Periods {
			update := BlackglassBeachViewUpdate("pools", from, to)
			if update.Period != to || update.View != "pools" || update.Camera.PortraitFOV != 95 {
				t.Fatalf("incorrect view update: %+v", update)
			}
			if len(update.Commands) == 0 && from != to {
				t.Fatalf("%s to %s must change lighting", from, to)
			}
			for _, command := range update.Commands {
				switch command.Kind {
				case scene.CommandSetEnvironment, scene.CommandSetPostEffects:
				case scene.CommandRemoveObject, scene.CommandCreateObject:
					switch command.ObjectID {
					case "sun", "beacon-lantern", "beacon-beam", "beacon-beam-halo", "sun-grotto-patch":
					default:
						t.Errorf("%s to %s replaces persistent node %q", from, to, command.ObjectID)
					}
				default:
					t.Errorf("%s to %s must not replace world, camera or models: %+v", from, to, command)
				}
			}
		}
	}
	update := BlackglassBeachViewUpdate("unknown", "unknown", "unknown")
	if update.View != "shore" || update.Period != beachgen.PeriodGolden || len(update.Commands) != 0 {
		t.Fatalf("invalid inputs must resolve to bounded presets: %+v", update)
	}
}
