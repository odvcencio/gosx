package physics

// RemoveBody removes a body and all attached colliders from the world.
// Body/collider indexes are not reused, which keeps replay ordering stable.
func (w *World) RemoveBody(body *RigidBody) bool {
	if w == nil || body == nil || body.world != w {
		return false
	}
	removedColliders := make(map[*Collider]struct{}, len(body.colliders))
	for _, collider := range body.colliders {
		if collider == nil {
			continue
		}
		removedColliders[collider] = struct{}{}
		collider.Body = nil
	}
	body.colliders = nil
	body.world = nil
	w.bodies = removeBodyFromSlice(w.bodies, body)
	w.colliders = filterColliders(w.colliders, removedColliders)
	w.constraints = filterConstraintsForBody(w.constraints, body)
	w.pruneContactState(removedColliders)
	for collider := range removedColliders {
		collider.index = 0
	}
	return true
}

// RemoveCollider removes one collider from the world. If the collider is
// attached to a body, the body remains in the world without that collider.
func (w *World) RemoveCollider(collider *Collider) bool {
	if w == nil || collider == nil {
		return false
	}
	found := false
	for _, existing := range w.colliders {
		if existing == collider {
			found = true
			break
		}
	}
	if !found {
		return false
	}

	if collider.Body != nil {
		collider.Body.colliders = removeColliderFromSlice(collider.Body.colliders, collider)
	}
	removed := map[*Collider]struct{}{collider: {}}
	w.colliders = filterColliders(w.colliders, removed)
	w.pruneContactState(removed)
	collider.Body = nil
	collider.index = 0
	return true
}

func removeBodyFromSlice(bodies []*RigidBody, body *RigidBody) []*RigidBody {
	for i, existing := range bodies {
		if existing == body {
			copy(bodies[i:], bodies[i+1:])
			bodies[len(bodies)-1] = nil
			return bodies[:len(bodies)-1]
		}
	}
	return bodies
}

func removeColliderFromSlice(colliders []*Collider, collider *Collider) []*Collider {
	for i, existing := range colliders {
		if existing == collider {
			copy(colliders[i:], colliders[i+1:])
			colliders[len(colliders)-1] = nil
			return colliders[:len(colliders)-1]
		}
	}
	return colliders
}

func filterColliders(colliders []*Collider, removed map[*Collider]struct{}) []*Collider {
	if len(removed) == 0 {
		return colliders
	}
	out := colliders[:0]
	for _, collider := range colliders {
		if _, drop := removed[collider]; !drop {
			out = append(out, collider)
		}
	}
	for i := len(out); i < len(colliders); i++ {
		colliders[i] = nil
	}
	return out
}

func filterConstraintsForBody(constraints []Constraint, body *RigidBody) []Constraint {
	if body == nil {
		return constraints
	}
	out := constraints[:0]
	for _, constraint := range constraints {
		if !constraintTouchesBody(constraint, body) {
			out = append(out, constraint)
		}
	}
	for i := len(out); i < len(constraints); i++ {
		constraints[i] = nil
	}
	return out
}

func constraintTouchesBody(constraint Constraint, body *RigidBody) bool {
	switch c := constraint.(type) {
	case *DistanceConstraint:
		return c.BodyA == body || c.BodyB == body
	}
	return false
}

func (w *World) pruneContactState(removed map[*Collider]struct{}) {
	if w == nil || len(removed) == 0 {
		return
	}
	contacts := w.contacts[:0]
	for _, contact := range w.contacts {
		if _, drop := removed[contact.ColliderA]; drop {
			continue
		}
		if _, drop := removed[contact.ColliderB]; drop {
			continue
		}
		contacts = append(contacts, contact)
	}
	for i := len(contacts); i < len(w.contacts); i++ {
		w.contacts[i] = ContactManifold{}
	}
	w.contacts = contacts

	if len(w.contactCache) > 0 {
		removedIndexes := make(map[int]struct{}, len(removed))
		for collider := range removed {
			if collider != nil && collider.index != 0 {
				removedIndexes[collider.index] = struct{}{}
			}
		}
		for key := range w.contactCache {
			if _, drop := removedIndexes[key.a]; drop {
				delete(w.contactCache, key)
				continue
			}
			if _, drop := removedIndexes[key.b]; drop {
				delete(w.contactCache, key)
			}
		}
	}
}
