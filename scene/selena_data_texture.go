package scene

import (
	"fmt"
	"strings"

	"m31labs.dev/selena/bindings"
)

// validateSelenaDataTextureRef checks a texture param bound to a data texture
// ("gosx:data:<key>"). Only a 2D texture can hold host-written texels, and a
// malformed key would never match a write, so both fail at material creation.
func validateSelenaDataTextureRef(texture bindings.Texture, ref string) error {
	key, ok := strings.CutPrefix(strings.TrimSpace(ref), DataTextureRefPrefix)
	if !ok {
		return nil
	}
	if err := ValidateDataTextureKey(key); err != nil {
		return fmt.Errorf("Selena texture %s: %w", texture.Name, err)
	}
	if texture.Dimension != "2d" {
		return fmt.Errorf("Selena texture %s: %s requires a 2d texture, got %s", texture.Name, ref, texture.Dimension)
	}
	if texture.Format != "" {
		if _, err := ParseTextureFormat(texture.Format); err != nil {
			return fmt.Errorf("Selena texture %s: %w", texture.Name, err)
		}
	}
	return nil
}
