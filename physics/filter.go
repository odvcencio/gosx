package physics

// Collision layers.
//
// Every collider belongs to one or more layers (its Layer bits) and collides
// with the layers its Mask names. Two colliders interact only when each one's
// Layer appears in the other's Mask:
//
//	a.Layer()&b.Mask() != 0 && b.Layer()&a.Mask() != 0
//
// The rule holds everywhere a pair can meet: the broadphase drops a pair that
// fails it before any narrowphase work, Collide and CollideAll return no
// manifold for it, the continuous collision pass ignores it, and so does the
// trigger pass. Queries (raycasts, shape casts and overlaps) take a
// QueryFilter instead, which names the layers the query hits.
//
// The zero ColliderConfig keeps the behaviour colliders had before layers
// existed: Layer zero means LayerDefault and Mask zero means LayerAll.

const (
	// LayerDefault is the layer a collider joins when its config leaves Layer
	// zero.
	LayerDefault uint32 = 1
	// LayerAll names every layer. It is the mask a collider uses when its
	// config leaves Mask zero.
	LayerAll uint32 = ^uint32(0)
)

// normalizeLayer applies the zero-value defaults to a layer and a mask.
func normalizeLayer(layer, mask uint32) (uint32, uint32) {
	if layer == 0 {
		layer = LayerDefault
	}
	if mask == 0 {
		mask = LayerAll
	}
	return layer, mask
}

// Layer returns the layer bits the collider belongs to.
func (c *Collider) Layer() uint32 {
	if c == nil {
		return 0
	}
	return c.layer
}

// Mask returns the layer bits the collider collides with.
func (c *Collider) Mask() uint32 {
	if c == nil {
		return 0
	}
	return c.mask
}

// SetCollisionFilter replaces the collider's layer and mask. Zero values take
// the same defaults as ColliderConfig: LayerDefault and LayerAll. The change
// applies from the next step and the next query.
func (c *Collider) SetCollisionFilter(layer, mask uint32) {
	if c == nil {
		return
	}
	c.layer, c.mask = normalizeLayer(layer, mask)
}

// collidersInteract reports whether the layer rule lets two colliders meet.
func collidersInteract(a, b *Collider) bool {
	return a.layer&b.mask != 0 && b.layer&a.mask != 0
}

// QueryFilter selects the colliders a raycast, shape cast or overlap query
// can report. The zero value hits every solid collider and skips triggers.
type QueryFilter struct {
	// Mask names the layers the query hits. Zero means LayerAll.
	Mask uint32
	// Layer, when nonzero, is the layer the query itself belongs to. A
	// collider whose Mask does not include it is skipped, which makes a query
	// see exactly what a collider with this Layer and Mask would collide with.
	Layer uint32
	// IncludeTriggers makes the query report trigger colliders as well.
	IncludeTriggers bool
	// ExcludeBody skips every collider attached to this body. A character
	// controller uses it to keep its own capsule out of its queries.
	ExcludeBody *RigidBody
	// ExcludeCollider skips one collider.
	ExcludeCollider *Collider

	// excludeBody2 is a second excluded body, which the character controller
	// uses to sweep along with the platform it stands on.
	excludeBody2 *RigidBody
}

// accepts reports whether the filter lets the query consider a collider.
func (f *QueryFilter) accepts(c *Collider) bool {
	if c == nil || c.invalid != nil {
		return false
	}
	if c.IsTrigger && !f.IncludeTriggers {
		return false
	}
	if c == f.ExcludeCollider {
		return false
	}
	if c.Body != nil && (c.Body == f.ExcludeBody || c.Body == f.excludeBody2) {
		return false
	}
	mask := f.Mask
	if mask == 0 {
		mask = LayerAll
	}
	if c.layer&mask == 0 {
		return false
	}
	if f.Layer != 0 && c.mask&f.Layer == 0 {
		return false
	}
	return true
}
