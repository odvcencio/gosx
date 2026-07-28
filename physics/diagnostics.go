package physics

import (
	"errors"
	"fmt"
	"strings"
)

// Diagnostic reports one collider or collider pair that cannot take part in
// collision. A diagnostic always means a configuration mistake: the shape was
// declared, the world accepted it, and it will never produce a contact.
//
// Read diagnostics with World.Diagnostics or World.Err, or let
// BuildWorldChecked return them as one error.
type Diagnostic struct {
	// ColliderIndex is the world-assigned index of the offending collider, or
	// 0 when the diagnostic is about the world as a whole.
	ColliderIndex int
	// BodyID names the owning body when the collider is attached to one.
	BodyID string
	// Shape is the declared shape of the offending collider.
	Shape ColliderShape
	// Err explains what is wrong.
	Err error
}

func (d Diagnostic) Error() string {
	var b strings.Builder
	b.WriteString(d.Err.Error())
	if d.ColliderIndex != 0 {
		fmt.Fprintf(&b, " (collider %d, shape %s", d.ColliderIndex, d.Shape)
		if d.BodyID != "" {
			fmt.Fprintf(&b, ", body %q", d.BodyID)
		}
		b.WriteString(")")
	}
	return b.String()
}

func (d Diagnostic) Unwrap() error {
	return d.Err
}

// ErrMeshPairUnsupported reports that two triangle mesh colliders were declared
// in a way that would need mesh-against-mesh collision, which is not
// implemented.
var ErrMeshPairUnsupported = errors.New("physics: triangle mesh against triangle mesh collision is not implemented")

// Diagnostics lists every collider in the world that cannot collide, plus every
// unsupported pairing. An empty result means every declared shape works.
//
// Call this after building a world. A collider that appears here is silent in
// the simulation: it passes through everything.
func (w *World) Diagnostics() []Diagnostic {
	if w == nil {
		return nil
	}
	var out []Diagnostic
	dynamicMeshes := 0
	meshes := 0
	for _, collider := range w.colliders {
		if collider == nil {
			continue
		}
		if err := collider.invalid; err != nil {
			out = append(out, Diagnostic{
				ColliderIndex: collider.index,
				BodyID:        colliderBodyID(collider),
				Shape:         collider.Shape,
				Err:           err,
			})
			continue
		}
		if collider.Shape == ShapeTriangleMesh {
			meshes++
			if !immovableCollider(collider) {
				dynamicMeshes++
			}
		}
	}
	// Two static meshes never pair in the broadphase, so only a moving mesh
	// can reach the unsupported combination.
	if dynamicMeshes > 0 && meshes > 1 {
		out = append(out, Diagnostic{Err: ErrMeshPairUnsupported})
	}
	return out
}

// Err joins every diagnostic into one error, or returns nil when the world is
// fully supported.
func (w *World) Err() error {
	diagnostics := w.Diagnostics()
	if len(diagnostics) == 0 {
		return nil
	}
	errs := make([]error, len(diagnostics))
	for i, d := range diagnostics {
		errs[i] = d
	}
	return errors.Join(errs...)
}

func colliderBodyID(c *Collider) string {
	if c == nil || c.Body == nil {
		return ""
	}
	return c.Body.ID
}
