package physics

import (
	"math"

	"m31labs.dev/gosx/det"
)

// Kinematic character controller.
//
// A Character is a capsule that game code moves by intent ("walk this way,
// jump") rather than by force. Each Step the controller sweeps the capsule
// through the world with shape casts and slides it along whatever it hits, so
// it never tunnels and never sinks into geometry. It climbs ledges up to a step
// height, walks slopes up to a slope limit, follows the ground down stairs and
// slopes, rides moving platforms, and pushes dynamic bodies out of its way.
//
// The capsule lives in the world as a kinematic body. Dynamic bodies therefore
// collide with it, triggers see it, raycasts hit it, and other characters are
// blocked by it. The solver never moves it: only Step, Move and Teleport do.
//
// All of a character's state is in CharacterState, and World.StateSnapshot
// carries every character's state, so a world restored from a snapshot moves
// its characters exactly as the original would have.

const (
	characterMaxIterations = 8
	characterMaxPushes     = 8
	// characterMinMove is the shortest displacement worth a cast, in metres.
	characterMinMove = 1e-6
	// characterDepenetrationPasses bounds the recovery passes per step.
	characterDepenetrationPasses = 4
	// characterEdgeMinUp is the least upward tilt a contact needs before the
	// edge probe may call it ground. A contact flatter than this is a wall.
	characterEdgeMinUp = 0.02
)

// CharacterConfig configures a capsule character controller. The zero value of
// every field picks the default named in its comment.
type CharacterConfig struct {
	ID string
	// Position is the capsule centre in world space.
	Position Vec3
	// Up is the direction the character stands along. Zero means +Y. Gravity
	// should point against it.
	Up Vec3
	// Radius is the capsule radius. Zero means 0.35 m.
	Radius float64
	// TotalHeight is the full capsule height, from the bottom of the lower
	// cap to the top of the upper cap. Zero means 1.8 m. A value under
	// 2*Radius is raised to it, which makes the capsule a sphere.
	TotalHeight float64
	// SkinWidth is the gap the controller keeps between the capsule and
	// anything it touches. Zero means 0.02 m.
	SkinWidth float64
	// StepHeight is the tallest ledge the character walks up without
	// jumping. Zero means 0.3 m; a negative value disables stepping.
	StepHeight float64
	// MaxSlope is the steepest walkable slope, in radians from level. Zero
	// means 45 degrees. A steeper surface acts as a wall, and the character
	// slides down it.
	MaxSlope float64
	// SnapDistance is how far below its feet a walking character looks for
	// ground to stay on, which is what keeps it on stairs and down slopes.
	// Zero means StepHeight; a negative value disables snapping.
	SnapDistance float64
	// MaxSlideIterations caps the cast-and-slide passes of one move. Zero
	// means 4; the most is 8.
	MaxSlideIterations int
	// Gravity replaces the world gravity for this character. Zero uses the
	// world gravity. Only its component along Up acts.
	Gravity Vec3
	// JumpSpeed is the upward speed a jump starts with, in metres per
	// second. JumpSpeedForHeight converts a jump height into it. Zero
	// disables jumping.
	JumpSpeed float64
	// CoyoteTime lets a jump start up to this many seconds after the
	// character walked off a ledge.
	CoyoteTime float64
	// JumpBufferTime remembers a jump requested up to this many seconds
	// before the character lands.
	JumpBufferTime float64
	// MaxFallSpeed caps the downward speed in metres per second. Zero means
	// no cap.
	MaxFallSpeed float64
	// Mass is the character mass in kilograms. The controller uses it only
	// to push dynamic bodies and to weigh down a dynamic body it stands on.
	// Zero means 80.
	Mass float64
	// DisablePush stops the character from pushing dynamic bodies.
	DisablePush bool
	// Layer and Mask are the capsule's collision filter, as on
	// ColliderConfig. The controller's own casts use the same filter.
	Layer uint32
	Mask  uint32
}

// CharacterInput is what game code asks of a character for one step.
type CharacterInput struct {
	// Move is the desired horizontal velocity in metres per second, in world
	// space. Its component along the character's Up is ignored.
	Move Vec3
	// Jump requests a jump this step.
	Jump bool
}

// CharacterCollision flags which sides of the capsule touched something
// during a move.
type CharacterCollision uint8

const (
	// CollidedBelow: a surface the character could stand on by its tilt.
	CollidedBelow CharacterCollision = 1 << iota
	// CollidedSides: a wall or a slope too steep to walk.
	CollidedSides
	// CollidedAbove: a ceiling.
	CollidedAbove
)

// CharacterMove reports what one Step or Move did.
type CharacterMove struct {
	Collisions CharacterCollision
	// Displacement is how far the capsule moved, platform carry included.
	Displacement Vec3
	// Carried is the part of Displacement the ground body carried the
	// character through.
	Carried Vec3
	// CarriedRotation is the rotation the ground body turned through since
	// the last step. Apply it to the character's facing to turn with a
	// rotating platform. It is the identity when nothing carried the
	// character.
	CarriedRotation Quat
	// Stepped reports that the character climbed a ledge.
	Stepped bool
	// Jumped reports that a jump started this step.
	Jumped bool
	// Landed reports that the character reached the ground this step after
	// being airborne.
	Landed bool
	// Pushed counts the dynamic bodies the character pushed.
	Pushed int
}

// CharacterState is everything a character carries from one step to the next.
// World.StateSnapshot includes it for every character, and
// World.ApplyState and Character.SetState restore it.
type CharacterState struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Position Vec3   `json:"position"`
	Velocity Vec3   `json:"velocity,omitzero"`
	Grounded bool   `json:"grounded,omitempty"`
	// GroundNormal is the walkable surface normal under the character.
	GroundNormal Vec3 `json:"groundNormal,omitzero"`
	// GroundCollider is the collider index the character stands on, or 0.
	GroundCollider int `json:"groundCollider,omitempty"`
	// GroundAnchor is the character's foot point in the ground body's local
	// frame, and GroundRotation that body's rotation, both as of the end of
	// the last step. Together they give the platform carry of the next step.
	GroundAnchor   Vec3 `json:"groundAnchor,omitzero"`
	GroundRotation Quat `json:"groundRotation,omitzero"`
	// Inherited is the platform velocity a jump carried into the air.
	Inherited Vec3 `json:"inherited,omitzero"`
	// AirTime is how long the character has been off the ground, in seconds.
	AirTime float64 `json:"airTime,omitempty"`
	// JumpTimer is how much longer a buffered jump request stays live.
	JumpTimer float64 `json:"jumpTimer,omitempty"`
	// Jumped reports a jump since the character last stood on the ground.
	Jumped bool `json:"jumped,omitempty"`
}

// Character is a kinematic capsule controller. Create one with
// World.AddCharacter.
type Character struct {
	ID string

	world    *World
	body     *RigidBody
	collider *Collider
	index    int

	up           Vec3
	rotation     Quat
	shape        QueryShape
	radius       float64
	footOffset   float64
	skin         float64
	stepHeight   float64
	snapDistance float64
	cosMaxSlope  float64
	iterations   int
	gravity      Vec3
	jumpSpeed    float64
	coyoteTime   float64
	jumpBuffer   float64
	maxFallSpeed float64
	mass         float64
	push         bool
	layer        uint32
	mask         uint32

	velocity       Vec3
	grounded       bool
	groundNormal   Vec3
	groundPoint    Vec3
	ground         *Collider
	groundAnchor   Vec3
	groundRotation Quat
	inherited      Vec3
	airTime        float64
	jumpTimer      float64
	jumped         bool

	// Per-step scratch.
	dt           float64
	stepGrounded bool
	moveVelocity Vec3
	platformVel  Vec3
	pushed       [characterMaxPushes]*RigidBody
	pushCount    int
	overlaps     []*Collider
	ceiling      bool
}

// JumpSpeedForHeight returns the launch speed that reaches height metres
// under a gravity of the given magnitude.
func JumpSpeedForHeight(height, gravity float64) float64 {
	if height <= 0 || gravity == 0 {
		return 0
	}
	return math.Sqrt(float64(2 * math.Abs(gravity) * height))
}

// AddCharacter creates a character and its kinematic capsule body.
func (w *World) AddCharacter(config CharacterConfig) *Character {
	if w == nil {
		return nil
	}
	c := &Character{ID: config.ID, world: w}
	c.configure(config)
	c.body = w.AddBody(BodyConfig{
		ID:           config.ID,
		Mass:         c.mass,
		Position:     config.Position,
		Rotation:     c.rotation,
		IsKinematic:  true,
		LockRotation: true,
	})
	c.collider = c.body.AddCollider(ColliderConfig{
		Shape:  ShapeCapsule,
		Radius: c.shape.Radius,
		Height: c.shape.Height,
		Layer:  config.Layer,
		Mask:   config.Mask,
	})
	w.nextCharacterIndex++
	c.index = w.nextCharacterIndex
	c.groundRotation = IdentityQuat()
	w.characters = append(w.characters, c)
	return c
}

// configure applies a config's defaults.
func (c *Character) configure(config CharacterConfig) {
	up := config.Up.Normalize()
	if up.Len2() <= epsilon {
		up = Vec3{Y: 1}
	}
	c.up = up
	c.rotation = quatFromTo(Vec3{Y: 1}, up)

	radius := math.Abs(config.Radius)
	if radius <= 0 {
		radius = 0.35
	}
	height := math.Abs(config.TotalHeight)
	if height <= 0 {
		height = 1.8
	}
	if height < 2*radius {
		height = 2 * radius
	}
	c.radius = radius
	c.footOffset = height * 0.5
	c.shape = CapsuleShape(radius, height-2*radius)

	c.skin = config.SkinWidth
	if c.skin <= 0 {
		c.skin = 0.02
	}
	c.stepHeight = config.StepHeight
	switch {
	case c.stepHeight == 0:
		c.stepHeight = 0.3
	case c.stepHeight < 0:
		c.stepHeight = 0
	}
	c.snapDistance = config.SnapDistance
	switch {
	case c.snapDistance == 0:
		c.snapDistance = c.stepHeight
	case c.snapDistance < 0:
		c.snapDistance = 0
	}
	slope := config.MaxSlope
	if slope <= 0 {
		slope = math.Pi / 4
	}
	c.cosMaxSlope = det.Cos(slope)
	c.iterations = config.MaxSlideIterations
	if c.iterations <= 0 {
		c.iterations = 4
	}
	if c.iterations > characterMaxIterations {
		c.iterations = characterMaxIterations
	}
	c.gravity = config.Gravity
	c.jumpSpeed = math.Max(0, config.JumpSpeed)
	c.coyoteTime = math.Max(0, config.CoyoteTime)
	c.jumpBuffer = math.Max(0, config.JumpBufferTime)
	c.maxFallSpeed = math.Max(0, config.MaxFallSpeed)
	c.mass = config.Mass
	if c.mass <= 0 {
		c.mass = 80
	}
	c.push = !config.DisablePush
	c.layer, c.mask = normalizeLayer(config.Layer, config.Mask)
}

// quatFromTo returns the shortest rotation that turns unit vector from onto
// unit vector to.
func quatFromTo(from, to Vec3) Quat {
	d := from.Dot(to)
	if d >= 1-1e-12 {
		return IdentityQuat()
	}
	if d <= -1+1e-12 {
		axis, _ := orthonormalBasis(from)
		return QuatFromAxisAngle(axis, math.Pi)
	}
	axis := from.Cross(to)
	return Quat{X: axis.X, Y: axis.Y, Z: axis.Z, W: 1 + d}.Normalize()
}

// RemoveCharacter removes a character and its body from the world.
func (w *World) RemoveCharacter(c *Character) bool {
	if w == nil || c == nil || c.world != w {
		return false
	}
	return w.RemoveBody(c.body)
}

// dropCharacterForBody forgets the character that owns a removed body.
func (w *World) dropCharacterForBody(body *RigidBody) {
	for i, c := range w.characters {
		if c.body != body {
			continue
		}
		copy(w.characters[i:], w.characters[i+1:])
		w.characters[len(w.characters)-1] = nil
		w.characters = w.characters[:len(w.characters)-1]
		c.world = nil
		return
	}
}

// CharacterCount returns the number of characters in the world.
func (w *World) CharacterCount() int {
	if w == nil {
		return 0
	}
	return len(w.characters)
}

// CharacterAt returns the i-th character in creation order.
func (w *World) CharacterAt(i int) *Character {
	if w == nil || i < 0 || i >= len(w.characters) {
		return nil
	}
	return w.characters[i]
}

// Characters returns a copy of the character list in creation order.
func (w *World) Characters() []*Character {
	if w == nil || len(w.characters) == 0 {
		return nil
	}
	out := make([]*Character, len(w.characters))
	copy(out, w.characters)
	return out
}

// Index returns the world-assigned character index, starting at 1.
func (c *Character) Index() int { return c.index }

// Body returns the character's kinematic body.
func (c *Character) Body() *RigidBody { return c.body }

// Collider returns the character's capsule collider.
func (c *Character) Collider() *Collider { return c.collider }

// Up returns the direction the character stands along.
func (c *Character) Up() Vec3 { return c.up }

// Radius returns the capsule radius.
func (c *Character) Radius() float64 { return c.radius }

// TotalHeight returns the full capsule height.
func (c *Character) TotalHeight() float64 { return float64(2 * c.footOffset) }

// Position returns the capsule centre.
func (c *Character) Position() Vec3 { return c.body.Position }

// FootPosition returns the bottom of the capsule.
func (c *Character) FootPosition() Vec3 { return c.foot(c.body.Position) }

// Velocity returns the controller's velocity: the horizontal speed it was
// asked for plus the vertical speed gravity and jumps gave it.
func (c *Character) Velocity() Vec3 { return c.velocity }

// SetVelocity replaces the controller's velocity. A positive component along
// Up lifts a grounded character off the ground on the next step, which is how
// game code applies a launch or a knockback.
func (c *Character) SetVelocity(v Vec3) { c.velocity = v }

// Grounded reports whether the character stood on walkable ground at the end
// of the last step.
func (c *Character) Grounded() bool { return c.grounded }

// GroundNormal returns the walkable surface normal under a grounded
// character, and the zero vector otherwise.
func (c *Character) GroundNormal() Vec3 { return c.groundNormal }

// GroundCollider returns the collider a grounded character stands on.
func (c *Character) GroundCollider() *Collider { return c.ground }

// Teleport moves the character to position without sweeping and forgets its
// ground, so it starts its next step airborne.
func (c *Character) Teleport(position Vec3) {
	c.body.Position = position
	c.body.Velocity = Vec3{}
	c.clearGround()
}

func (c *Character) foot(center Vec3) Vec3 {
	return center.Sub(c.up.Mul(c.footOffset))
}

// groundLevel is the point a skin below the foot: where the ground under a
// grounded character is.
func (c *Character) groundLevel(center Vec3) Vec3 {
	return center.Sub(c.up.Mul(c.footOffset + c.skin))
}

// horizontal removes the component of v along Up.
func (c *Character) horizontal(v Vec3) Vec3 {
	return v.Sub(c.up.Mul(v.Dot(c.up)))
}

func (c *Character) gravityVector() Vec3 {
	if c.gravity.Len2() > 0 {
		return c.gravity
	}
	return c.world.gravity
}

func (c *Character) filter() QueryFilter {
	return QueryFilter{Mask: c.mask, Layer: c.layer, ExcludeBody: c.body}
}

func (c *Character) cast(position, direction Vec3, distance float64, filter QueryFilter) (ShapeCastHit, bool) {
	return c.world.ShapeCast(c.shape, position, c.rotation, direction, distance, filter)
}

// Step advances the character by dt seconds under input: platform carry,
// recovery from overlaps, jump and gravity, the horizontal move with stepping
// and slope handling, then the vertical move with ground detection and
// snapping. Call it once per fixed step, before World.Step.
func (c *Character) Step(dt float64, input CharacterInput) CharacterMove {
	move := CharacterMove{CarriedRotation: IdentityQuat()}
	if c == nil || c.world == nil || !(dt > 0) {
		return move
	}
	c.dt = dt
	c.pushCount = 0
	c.platformVel = Vec3{}
	start := c.body.Position
	position := start
	wasGrounded := c.grounded

	position = c.carry(position, &move)
	position = c.depenetrate(position)

	vertical := c.velocity.Dot(c.up)
	lateral := c.horizontal(input.Move)
	wantJump := input.Jump || c.jumpTimer > 0
	canJump := c.jumpSpeed > 0 && !c.jumped && (c.grounded || c.airTime <= c.coyoteTime)
	switch {
	case wantJump && canJump:
		vertical = c.jumpSpeed + math.Max(0, c.platformVel.Dot(c.up))
		c.inherited = c.horizontal(c.platformVel)
		c.grounded = false
		c.jumped = true
		c.jumpTimer = 0
		move.Jumped = true
	case input.Jump:
		c.jumpTimer = c.jumpBuffer
	default:
		c.jumpTimer = math.Max(0, c.jumpTimer-dt)
	}
	if c.grounded && vertical <= 0 {
		vertical = 0
	} else {
		c.grounded = c.grounded && vertical <= 0
		vertical += float64(c.gravityVector().Dot(c.up) * dt)
		if c.maxFallSpeed > 0 && vertical < -c.maxFallSpeed {
			vertical = -c.maxFallSpeed
		}
	}
	if !c.grounded {
		lateral = lateral.Add(c.inherited)
	}

	c.stepGrounded = c.grounded
	c.moveVelocity = lateral
	walkMove := lateral.Mul(dt)
	if c.grounded && c.groundNormal.Dot(c.up) >= c.cosMaxSlope {
		// Walk along the ground plane, so a slope costs no speed and the
		// cast keeps its skin from the ground.
		walkMove = c.alongSlope(walkMove, c.groundNormal)
	}
	position = c.slide(position, walkMove, slideLateral, &move)

	if vertical > 0 {
		var bumped bool
		c.moveVelocity = c.up.Mul(vertical)
		position, bumped = c.rise(position, float64(vertical*dt), &move)
		if bumped {
			vertical = 0
		}
		c.clearGround()
	} else {
		snap := 0.0
		if c.stepGrounded && !move.Jumped {
			snap = c.snapDistance
		}
		c.moveVelocity = c.up.Mul(vertical)
		var landed bool
		position, landed = c.descend(position, float64(-vertical*dt), snap, &move)
		if landed {
			vertical = 0
			if !wasGrounded {
				move.Landed = true
			}
		}
	}

	if c.grounded {
		c.airTime = 0
		c.jumped = false
		c.inherited = Vec3{}
		c.weighDown()
	} else {
		c.airTime += dt
	}
	c.velocity = lateral.Add(c.up.Mul(vertical))
	c.body.Position = position
	move.Displacement = position.Sub(start)
	c.body.Velocity = move.Displacement.Div(dt)
	c.recordAnchor()
	return move
}

// Move sweeps the capsule through displacement, sliding along what it hits,
// with no gravity, stepping or ground snapping. It keeps the ground the
// character last stood on. Use it for scripted motion; use Step for walking.
func (c *Character) Move(displacement Vec3) CharacterMove {
	move := CharacterMove{CarriedRotation: IdentityQuat()}
	if c == nil || c.world == nil {
		return move
	}
	c.pushCount = 0
	c.moveVelocity = Vec3{}
	start := c.body.Position
	position := c.slide(start, displacement, slidePlain, &move)
	c.body.Position = position
	move.Displacement = position.Sub(start)
	c.recordAnchor()
	return move
}

type slideMode uint8

const (
	// slidePlain projects the remaining motion onto every plane it meets.
	slidePlain slideMode = iota
	// slideLateral is the walking move: it climbs steps, follows walkable
	// slopes at full speed and treats steep slopes as walls.
	slideLateral
	// slideCarry is the platform carry, which ignores the ground body.
	slideCarry
)

// slide is collide-and-slide: cast, move up to the hit less the skin, remove
// the motion into the hit surface, and repeat with what is left. Moving into a
// crease between two surfaces follows the crease; a third surface stops the
// move.
func (c *Character) slide(position, displacement Vec3, mode slideMode, move *CharacterMove) Vec3 {
	filter := c.filter()
	if mode == slideCarry && c.ground != nil {
		filter.excludeBody2 = c.ground.Body
		if c.ground.Body == nil {
			filter.ExcludeCollider = c.ground
		}
	}
	remaining := displacement
	var planes [characterMaxIterations]Vec3
	planeCount := 0
	for iter := 0; iter < c.iterations; iter++ {
		distance := remaining.Len()
		if distance <= characterMinMove {
			break
		}
		direction := remaining.Div(distance)
		hit, ok := c.cast(position, direction, distance+c.skin, filter)
		if !ok {
			position = position.Add(remaining)
			break
		}
		travel := clampFloat(hit.Distance-c.skin, 0, distance)
		position = position.Add(direction.Mul(travel))
		remaining = direction.Mul(distance - travel)
		c.recordCollision(hit.Normal, move)
		c.pushBody(hit, move)

		normal := hit.Normal
		if mode == slideLateral {
			upward := normal.Dot(c.up)
			if upward < c.cosMaxSlope {
				// Dynamic bodies are pushed, never climbed.
				climbable := hit.Body == nil || !hit.Body.IsDynamic()
				if climbable && c.stepGrounded && c.stepHeight > 0 && upward > -characterEdgeMinUp {
					if stepped, rest, ok := c.tryStep(position, remaining, filter); ok {
						position, remaining = stepped, rest
						move.Stepped = true
						continue
					}
				}
				if _, walkable := c.walkable(hit, filter, c.groundLevel(position), c.stepHeight); walkable && c.stepGrounded {
					// The bottom of the capsule rides over a ledge it
					// stands on: follow the contact surface up.
					remaining = c.alongSlope(remaining, normal)
				} else {
					// A wall, or a slope too steep to walk: block only the
					// horizontal motion, so the character never climbs it.
					normal = c.horizontal(normal).Normalize()
					if normal.Len2() <= epsilon {
						break
					}
					remaining = remaining.Sub(normal.Mul(remaining.Dot(normal)))
				}
			} else if c.stepGrounded {
				remaining = c.alongSlope(remaining, normal)
			} else {
				normal = c.horizontal(normal).Normalize()
				if normal.Len2() <= epsilon {
					break
				}
				remaining = remaining.Sub(normal.Mul(remaining.Dot(normal)))
			}
		} else {
			remaining = remaining.Sub(normal.Mul(remaining.Dot(normal)))
		}
		remaining = followCrease(remaining, normal, planes[:planeCount])
		if planeCount < len(planes) {
			planes[planeCount] = normal
			planeCount++
		}
	}
	return position
}

// followCrease keeps motion that the newest plane allows from running back
// into an earlier one: two planes leave only their crease line, three leave
// nothing.
func followCrease(motion, normal Vec3, planes []Vec3) Vec3 {
	for i, plane := range planes {
		if motion.Dot(plane) >= -characterMinMove {
			continue
		}
		crease := normal.Cross(plane).Normalize()
		if crease.Len2() <= epsilon {
			return Vec3{}
		}
		motion = crease.Mul(motion.Dot(crease))
		for j, other := range planes {
			if j != i && motion.Dot(other) < -characterMinMove {
				return Vec3{}
			}
		}
		return motion
	}
	return motion
}

// alongSlope turns horizontal motion onto a walkable surface, keeping its
// heading and its length, so walking uphill does not slow the character.
func (c *Character) alongSlope(motion, normal Vec3) Vec3 {
	length := motion.Len()
	upward := normal.Dot(c.up)
	if length <= characterMinMove || upward <= epsilon {
		return motion.Sub(normal.Mul(motion.Dot(normal)))
	}
	flat := c.horizontal(motion)
	// Lift the horizontal heading until it lies in the surface plane.
	tangent := flat.Sub(c.up.Mul(flat.Dot(normal) / upward)).Normalize()
	if tangent.Len2() <= epsilon {
		return Vec3{}
	}
	return tangent.Mul(length)
}

// walkable decides whether a contact can be stood on, and returns the ground
// normal to use. A contact tilted within the slope limit is ground. A
// contact on the rim of a ledge is tilted further, because the capsule's
// round bottom meets the edge, not the face; a short ray just past the edge
// then reads the face under it, and that face decides. A rim counts only
// when it sits no more than maxRise above base, so the rim rule never lets
// the character up a ledge taller than its step height.
func (c *Character) walkable(hit ShapeCastHit, filter QueryFilter, base Vec3, maxRise float64) (Vec3, bool) {
	upward := hit.Normal.Dot(c.up)
	if upward >= c.cosMaxSlope {
		return hit.Normal, true
	}
	if upward <= characterEdgeMinUp || hit.Point.Sub(base).Dot(c.up) > maxRise {
		return Vec3{}, false
	}
	inward := c.horizontal(hit.Normal.Neg()).Normalize()
	probe := math.Max(c.skin, 0.005)
	origin := hit.Point.Add(inward.Mul(probe)).Add(c.up.Mul(probe))
	ray, ok := c.world.RaycastFiltered(Ray{Origin: origin, Direction: c.up.Neg()}, float64(2*probe), filter)
	if !ok || ray.Distance <= 0 || ray.Normal.Dot(c.up) < c.cosMaxSlope {
		return Vec3{}, false
	}
	return ray.Normal, true
}

// tryStep lifts the capsule by up to the step height, moves it forward by the
// blocked horizontal motion, and sets it down. The step holds only when the
// capsule comes down on walkable ground; otherwise the obstacle is a wall.
func (c *Character) tryStep(position, remaining Vec3, filter QueryFilter) (Vec3, Vec3, bool) {
	flat := c.horizontal(remaining)
	distance := flat.Len()
	if distance <= characterMinMove {
		return position, remaining, false
	}
	direction := flat.Div(distance)

	raise := c.stepHeight
	if hit, ok := c.cast(position, c.up, raise+c.skin, filter); ok {
		raise = hit.Distance - c.skin
	}
	if raise <= characterMinMove {
		return position, remaining, false
	}
	raised := position.Add(c.up.Mul(raise))

	forward := distance
	if hit, ok := c.cast(raised, direction, distance+c.skin, filter); ok {
		forward = hit.Distance - c.skin
	}
	if forward <= characterMinMove {
		return position, remaining, false
	}
	ahead := raised.Add(direction.Mul(forward))

	down, ok := c.cast(ahead, c.up.Neg(), raise+c.skin, filter)
	if !ok || down.StartOverlap {
		return position, remaining, false
	}
	// The step is the height of what the capsule lands on, measured from
	// the ground it started on.
	base := c.groundLevel(position)
	if down.Point.Sub(base).Dot(c.up) > c.stepHeight {
		return position, remaining, false
	}
	normal, walkable := c.walkable(down, filter, base, c.stepHeight)
	if !walkable {
		return position, remaining, false
	}
	landed := ahead.Sub(c.up.Mul(math.Max(0, down.Distance-c.skin)))
	if landed.Sub(position).Dot(c.up) <= characterMinMove {
		return position, remaining, false
	}
	c.groundNormal = normal
	return landed, direction.Mul(distance - forward), true
}

// rise moves the capsule up and reports whether it hit a ceiling.
func (c *Character) rise(position Vec3, distance float64, move *CharacterMove) (Vec3, bool) {
	c.ceiling = false
	position = c.slide(position, c.up.Mul(distance), slidePlain, move)
	return position, c.ceiling
}

// descend moves the capsule down by fall, looking a further snap below for
// ground, and reports whether it ended on walkable ground.
func (c *Character) descend(position Vec3, fall, snap float64, move *CharacterMove) (Vec3, bool) {
	filter := c.filter()
	reach := fall + snap
	hit, ok := c.cast(position, c.up.Neg(), reach+c.skin, filter)
	if !ok {
		c.clearGround()
		return position.Sub(c.up.Mul(fall)), false
	}
	gap := hit.Distance - c.skin
	normal, walkable := c.walkable(hit, filter, c.foot(position), math.Inf(1))
	if walkable {
		position = position.Sub(c.up.Mul(gap))
		c.recordCollision(hit.Normal, move)
		c.pushBody(hit, move)
		c.setGround(hit, normal)
		return position, true
	}
	c.clearGround()
	if gap > fall {
		// A steep surface within snap range only: do not snap onto it.
		return position.Sub(c.up.Mul(fall)), false
	}
	drop := math.Max(0, gap)
	position = position.Sub(c.up.Mul(drop))
	// Slide the rest of the fall down the steep surface.
	return c.slide(position, c.up.Mul(drop-fall), slidePlain, move), false
}

func (c *Character) setGround(hit ShapeCastHit, normal Vec3) {
	c.grounded = true
	c.ground = hit.Collider
	c.groundNormal = normal
	c.groundPoint = hit.Point
}

func (c *Character) clearGround() {
	c.grounded = false
	c.ground = nil
	c.groundNormal = Vec3{}
	c.groundPoint = Vec3{}
	c.groundAnchor = Vec3{}
	c.groundRotation = IdentityQuat()
}

// recordAnchor stores the foot point in the ground body's frame, so the next
// step can tell how far the body carried it.
func (c *Character) recordAnchor() {
	if c.ground == nil || c.ground.Body == nil {
		c.groundAnchor = Vec3{}
		c.groundRotation = IdentityQuat()
		return
	}
	body := c.ground.Body
	c.groundAnchor = body.Rotation.Conjugate().Rotate(c.foot(c.body.Position).Sub(body.Position))
	c.groundRotation = body.Rotation
}

// carry moves the character with the body it stood on at the end of the last
// step.
func (c *Character) carry(position Vec3, move *CharacterMove) Vec3 {
	if c.ground == nil || c.ground.Body == nil || c.ground.Body.world != c.world {
		return position
	}
	body := c.ground.Body
	anchor := body.Position.Add(body.Rotation.Rotate(c.groundAnchor))
	delta := anchor.Sub(c.foot(position))
	move.CarriedRotation = body.Rotation.Mul(c.groundRotation.Conjugate()).Normalize()
	if delta.Len2() <= float64(characterMinMove*characterMinMove) {
		return position
	}
	c.moveVelocity = delta.Div(c.dt)
	moved := c.slide(position, delta, slideCarry, move)
	move.Carried = moved.Sub(position)
	c.platformVel = move.Carried.Div(c.dt)
	return moved
}

// depenetrate pushes the capsule out of static and kinematic geometry that
// moved into it since the last step. Dynamic bodies are left to the solver,
// which pushes them out of the capsule from their side.
func (c *Character) depenetrate(position Vec3) Vec3 {
	filter := c.filter()
	for pass := 0; pass < characterDepenetrationPasses; pass++ {
		c.overlaps = c.world.Overlap(c.shape, position, c.rotation, filter, c.overlaps[:0])
		if len(c.overlaps) == 0 {
			break
		}
		var q overlapQuery
		prepareOverlap(&q, c.shape, position, c.rotation)
		q.tris, q.contacts, q.probe = &c.world.queryTris, &c.world.queryContacts, &c.world.queryProbe
		best := penetration{}
		found := false
		for _, other := range c.overlaps {
			if other.Body != nil && other.Body.IsDynamic() {
				continue
			}
			if pen, ok := penetrateCollider(other, &q); ok && (!found || pen.depth > best.depth) {
				best, found = pen, true
			}
		}
		if !found || best.depth <= characterMinMove {
			break
		}
		position = position.Sub(best.normal.Mul(best.depth + float64(c.skin*0.5)))
	}
	clear(c.overlaps)
	return position
}

// recordCollision sorts a contact normal into below, sides or above, and
// notes any downward-facing contact as a ceiling for rise.
func (c *Character) recordCollision(normal Vec3, move *CharacterMove) {
	upward := normal.Dot(c.up)
	if upward < -characterEdgeMinUp {
		c.ceiling = true
	}
	switch {
	case upward >= c.cosMaxSlope:
		move.Collisions |= CollidedBelow
	case upward <= -c.cosMaxSlope:
		move.Collisions |= CollidedAbove
	default:
		move.Collisions |= CollidedSides
	}
}

// pushBody gives a dynamic body the impulse of an inelastic hit by the
// character, once per body per move. A side push loses its vertical part, so
// pushing a crate never drives it into the floor.
func (c *Character) pushBody(hit ShapeCastHit, move *CharacterMove) {
	body := hit.Body
	if !c.push || body == nil || !body.IsDynamic() {
		return
	}
	for i := 0; i < c.pushCount; i++ {
		if c.pushed[i] == body {
			return
		}
	}
	if c.pushCount >= len(c.pushed) {
		return
	}
	c.pushed[c.pushCount] = body
	c.pushCount++

	into := hit.Normal.Neg()
	if math.Abs(hit.Normal.Dot(c.up)) < c.cosMaxSlope {
		into = c.horizontal(into).Normalize()
	}
	if into.Len2() <= epsilon {
		return
	}
	arm := hit.Point.Sub(body.Position)
	bodyVelocity := body.Velocity.Add(body.AngularVelocity.Cross(arm))
	closing := c.moveVelocity.Sub(bodyVelocity).Dot(into)
	if closing <= 0 {
		return
	}
	reduced := float64(c.mass*body.Mass) / (c.mass + body.Mass)
	body.ApplyImpulse(into.Mul(float64(closing*reduced)), hit.Point)
	move.Pushed++
}

// weighDown presses a grounded character's weight into a dynamic body it
// stands on.
func (c *Character) weighDown() {
	if !c.push || c.ground == nil || c.ground.Body == nil || !c.ground.Body.IsDynamic() {
		return
	}
	weight := c.gravityVector().Mul(float64(c.mass * c.dt))
	c.ground.Body.ApplyImpulse(weight, c.groundPoint)
}

// State returns the character's restorable state.
func (c *Character) State() CharacterState {
	state := CharacterState{
		Index:          c.index,
		ID:             c.ID,
		Position:       c.body.Position,
		Velocity:       c.velocity,
		Grounded:       c.grounded,
		GroundNormal:   c.groundNormal,
		GroundAnchor:   c.groundAnchor,
		GroundRotation: c.groundRotation,
		Inherited:      c.inherited,
		AirTime:        c.airTime,
		JumpTimer:      c.jumpTimer,
		Jumped:         c.jumped,
	}
	if c.ground != nil {
		state.GroundCollider = c.ground.index
	}
	return state
}

// SetState restores a state from State. The ground collider is found by
// index in the character's world; a ground that no longer exists leaves the
// character airborne.
func (c *Character) SetState(state CharacterState) {
	var ground *Collider
	if state.GroundCollider != 0 && c.world != nil {
		for _, collider := range c.world.colliders {
			if collider.index == state.GroundCollider {
				ground = collider
				break
			}
		}
	}
	c.applyState(state, ground)
}

func (c *Character) applyState(state CharacterState, ground *Collider) {
	c.body.Position = state.Position
	c.body.Velocity = Vec3{}
	c.velocity = state.Velocity
	c.grounded = state.Grounded && ground != nil
	c.ground = ground
	c.groundNormal = state.GroundNormal
	c.groundPoint = Vec3{}
	c.groundAnchor = state.GroundAnchor
	c.groundRotation = state.GroundRotation
	if c.groundRotation == (Quat{}) {
		c.groundRotation = IdentityQuat()
	}
	if ground == nil {
		c.groundNormal = Vec3{}
		c.groundAnchor = Vec3{}
		c.groundRotation = IdentityQuat()
	}
	c.inherited = state.Inherited
	c.airTime = state.AirTime
	c.jumpTimer = state.JumpTimer
	c.jumped = state.Jumped
}
