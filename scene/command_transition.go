package scene

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// CommandTransition describes two absolute command poses. Matching numeric
// fields interpolate; identities, topology and absent fields change at the end.
// Use explicit RemoveObject commands for visibility changes. Omitted commands
// retain their other endpoint, so a transition can own only part of a scene.
// Prepare once for native sampling, or bind it with Program.BindSceneTransition.
type CommandTransition struct {
	From []Command `json:"from"`
	To   []Command `json:"to"`
}

func (t CommandTransition) transitionJSON() ([]byte, error) {
	if len(t.From) > 10000 || len(t.To) > 10000 {
		return nil, fmt.Errorf("scene transition supports at most 10000 commands per pose")
	}
	from, to := t.From, t.To
	if from == nil {
		from = []Command{}
	}
	if to == nil {
		to = []Command{}
	}
	data, err := json.Marshal(struct {
		Version int       `json:"version"`
		From    []Command `json:"from"`
		To      []Command `json:"to"`
	}{1, from, to})
	if err != nil || len(data) > 4<<20 {
		return nil, fmt.Errorf("scene transition exceeds 4 MiB or contains unsupported values")
	}
	return data, nil
}

// MotionTransitionJSON implements motion.SceneTransitionSource without making
// the motion package depend on Scene3D's command schema.
func (t CommandTransition) MotionTransitionJSON() ([]byte, error) {
	data, err := t.transitionJSON()
	if err != nil {
		return nil, err
	}
	if _, err := prepareCommandTransition(data); err != nil {
		return nil, err
	}
	return data, nil
}

// MarshalJSON writes the same versioned plan consumed by the browser sampler.
func (t CommandTransition) MarshalJSON() ([]byte, error) { return t.MotionTransitionJSON() }

// CommandTransitionPlayer owns immutable, normalized snapshots. Sampling never
// reads live scene state, so a backward seek reconstructs the same command pose.
type CommandTransitionPlayer struct {
	plans  []commandTransitionPlan
	orders [2][]int
}
type commandTransitionPlan struct {
	from, to *Command
	data     *commandTransitionValue
}

type commandTransitionValue struct {
	a, b     any
	ap, bp   bool
	kind     byte
	keys     []string
	children []*commandTransitionValue
	color    [6]float64
}

var transitionDiscreteFields = map[string]bool{
	"id": true, "kind": true, "type": true, "name": true, "geometry": true,
	"text": true, "count": true, "lineSegments": true, "indices": true,
	"segments": true, "radialSegments": true, "tubularSegments": true,
}

// Prepare validates and compiles a transition. Keep the player for repeated
// samples instead of re-encoding or rebuilding command maps on every frame.
func (t CommandTransition) Prepare() (*CommandTransitionPlayer, error) {
	data, err := t.transitionJSON()
	if err != nil {
		return nil, err
	}
	return prepareCommandTransition(data)
}

func prepareCommandTransition(data []byte) (*CommandTransitionPlayer, error) {
	var wire struct {
		Version int       `json:"version"`
		From    []Command `json:"from"`
		To      []Command `json:"to"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, err
	}
	if wire.Version != 1 {
		return nil, fmt.Errorf("scene transition needs version 1")
	}
	budget := 200000
	for _, commands := range [][]Command{wire.From, wire.To} {
		for _, command := range commands {
			if err := validateCommandTransitionValue(command.Data, 0, &budget); err != nil {
				return nil, err
			}
		}
	}
	a, order, err := transitionCommandMap(wire.From)
	if err != nil {
		return nil, err
	}
	b, nextOrder, err := transitionCommandMap(wire.To)
	if err != nil {
		return nil, err
	}
	for _, key := range nextOrder {
		if _, exists := a[key]; !exists {
			order = append(order, key)
		}
	}
	player := &CommandTransitionPlayer{}
	budget = 200000
	for _, key := range order {
		plan := commandTransitionPlan{from: a[key], to: b[key]}
		if plan.from != nil && plan.to != nil && plan.from.Kind == plan.to.Kind && plan.from.Kind != CommandRemoveObject {
			if plan.from.Kind != CommandCreateObject || transitionObjectKind(plan.from) == transitionObjectKind(plan.to) {
				plan.data, err = compileCommandTransitionValue(plan.from.Data, true, plan.to.Data, true, "", 0, &budget)
				if err != nil {
					return nil, err
				}
			}
		}
		player.plans = append(player.plans, plan)
	}
	for endpoint := range player.orders {
		order := make([]int, len(player.plans))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(i, j int) bool {
			return player.plans[order[i]].command(endpoint == 1).Kind == CommandCreateObject && player.plans[order[j]].command(endpoint == 1).Kind != CommandCreateObject
		})
		player.orders[endpoint] = order
	}
	return player, nil
}

func (plan commandTransitionPlan) command(atEnd bool) *Command {
	if plan.to != nil && (plan.from == nil || atEnd) {
		return plan.to
	}
	return plan.from
}

func transitionObjectKind(c *Command) string {
	data, _ := c.Data.(map[string]any)
	return fmt.Sprint(data["kind"], ":", data["geometry"])
}

func transitionCommandMap(commands []Command) (map[string]*Command, []string, error) {
	out := make(map[string]*Command, len(commands))
	var order []string
	for _, raw := range commands {
		if raw.Kind < CommandCreateObject || raw.Kind > CommandSetGroups || raw.Kind <= CommandSetLight && raw.ObjectID == "" || len(raw.ObjectID) > 256 || raw.Kind != CommandRemoveObject && raw.Data == nil {
			return nil, nil, fmt.Errorf("scene transition contains an invalid command")
		}
		c := raw
		data, _ := c.Data.(map[string]any)
		if data != nil {
			if c.Kind == CommandSetTransform || c.Kind == CommandSetCamera {
				transitionDefaults(data, map[string]any{"x": float64(0), "y": float64(0), "z": float64(0), "rotationX": float64(0), "rotationY": float64(0), "rotationZ": float64(0)})
				if c.Kind == CommandSetTransform {
					transitionDefaults(data, map[string]any{"scaleX": float64(1), "scaleY": float64(1), "scaleZ": float64(1)})
				}
				if c.Kind == CommandSetCamera {
					data["transitionMS"] = float64(0)
				}
			}
			if c.Kind == CommandCreateObject {
				if props, ok := data["props"].(map[string]any); ok {
					transitionDefaults(props, map[string]any{"x": float64(0), "y": float64(0), "z": float64(0)})
					if data["kind"] == nil || data["kind"] == "mesh" {
						transitionDefaults(props, map[string]any{"rotationX": float64(0), "rotationY": float64(0), "rotationZ": float64(0), "scaleX": float64(1), "scaleY": float64(1), "scaleZ": float64(1)})
					}
					if points, ok := props["points"].([]any); ok {
						for _, point := range points {
							if p, ok := point.(map[string]any); ok {
								transitionDefaults(p, map[string]any{"x": float64(0), "y": float64(0), "z": float64(0)})
							}
						}
					}
				}
			}
		}
		key := fmt.Sprintf("%d:%s", c.Kind, c.ObjectID)
		if c.Kind <= CommandRemoveObject {
			key = "object:" + c.ObjectID
		}
		if _, exists := out[key]; !exists {
			order = append(order, key)
		}
		out[key] = &c
	}
	return out, order, nil
}

func transitionDefaults(data, defaults map[string]any) {
	//gosx:allow map-range fills each missing key independently
	for key, value := range defaults {
		if _, exists := data[key]; !exists {
			data[key] = value
		}
	}
}

func validateCommandTransitionValue(value any, depth int, budget *int) error {
	(*budget)--
	if depth > 32 || *budget < 0 {
		return fmt.Errorf("scene transition value budget exceeded")
	}
	switch v := value.(type) {
	case map[string]any:
		//gosx:allow map-range validation only; any unsafe key rejects the value
		for key, child := range v {
			if key == "__proto__" || key == "prototype" || key == "constructor" {
				return fmt.Errorf("scene transition contains an unsafe data key")
			}
			if err := validateCommandTransitionValue(child, depth+1, budget); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range v {
			if err := validateCommandTransitionValue(child, depth+1, budget); err != nil {
				return err
			}
		}
	}
	return nil
}

func compileCommandTransitionValue(a any, ap bool, b any, bp bool, key string, depth int, budget *int) (*commandTransitionValue, error) {
	(*budget)--
	if depth > 32 || *budget < 0 {
		return nil, fmt.Errorf("scene transition value budget exceeded")
	}
	node := &commandTransitionValue{a: a, b: b, ap: ap, bp: bp}
	if !ap || !bp || transitionDiscreteFields[key] {
		return node, nil
	}
	if _, ok := a.(float64); ok {
		if _, ok := b.(float64); ok {
			node.kind = 'n'
			return node, nil
		}
	}
	if key == "color" {
		as, aok := a.(string)
		bs, bok := b.(string)
		if aok && bok && len(as) == 7 && len(bs) == 7 && as[0] == '#' && bs[0] == '#' {
			av, ae := strconv.ParseUint(as[1:], 16, 24)
			bv, be := strconv.ParseUint(bs[1:], 16, 24)
			if ae == nil && be == nil {
				node.kind = 'c'
				for i := 0; i < 3; i++ {
					shift := uint(16 - 8*i)
					node.color[i] = float64(av >> shift & 255)
					node.color[i+3] = float64(bv >> shift & 255)
				}
				return node, nil
			}
		}
	}
	if aa, ok := a.([]any); ok {
		if bb, ok := b.([]any); ok && len(aa) == len(bb) {
			node.kind = 'a'
			for i := range aa {
				child, err := compileCommandTransitionValue(aa[i], true, bb[i], true, key, depth+1, budget)
				if err != nil {
					return nil, err
				}
				node.children = append(node.children, child)
			}
		}
	} else if aa, ok := a.(map[string]any); ok {
		if bb, ok := b.(map[string]any); ok {
			node.kind = 'o'
			//gosx:allow map-range keys are sorted after both loops
			for k := range aa {
				node.keys = append(node.keys, k)
			}
			//gosx:allow map-range keys are sorted after both loops
			for k := range bb {
				if _, exists := aa[k]; !exists {
					node.keys = append(node.keys, k)
				}
			}
			sort.Strings(node.keys)
			for _, k := range node.keys {
				av, ae := aa[k]
				bv, be := bb[k]
				child, err := compileCommandTransitionValue(av, ae, bv, be, k, depth+1, budget)
				if err != nil {
					return nil, err
				}
				node.children = append(node.children, child)
			}
		}
	}
	return node, nil
}

func cloneTransitionValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		//gosx:allow map-range copies each entry independently
		for k, x := range v {
			out[k] = cloneTransitionValue(x)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, x := range v {
			out[i] = cloneTransitionValue(x)
		}
		return out
	default:
		return value
	}
}

func (node *commandTransitionValue) sample(t float64) (any, bool) {
	if t == 0 {
		return cloneTransitionValue(node.a), node.ap
	}
	if t == 1 {
		return cloneTransitionValue(node.b), node.bp
	}
	switch node.kind {
	case 'n':
		return float64(node.a.(float64)*(1-t)) + float64(node.b.(float64)*t), true
	case 'c':
		return fmt.Sprintf("#%02x%02x%02x", int(math.Round(float64(node.color[0]*(1-t))+float64(node.color[3]*t))), int(math.Round(float64(node.color[1]*(1-t))+float64(node.color[4]*t))), int(math.Round(float64(node.color[2]*(1-t))+float64(node.color[5]*t)))), true
	case 'a':
		out := make([]any, len(node.children))
		for i, child := range node.children {
			out[i], _ = child.sample(t)
		}
		return out, true
	case 'o':
		out := make(map[string]any, len(node.children))
		for i, child := range node.children {
			if value, present := child.sample(t); present {
				out[node.keys[i]] = value
			}
		}
		return out, true
	default:
		return cloneTransitionValue(node.a), node.ap
	}
}

// Sample clamps finite progress to [0,1] and returns independently owned command
// data. Object upserts precede dependent transform/material/camera updates.
func (p *CommandTransitionPlayer) Sample(progress float64) ([]Command, error) {
	if p == nil || math.IsNaN(progress) || math.IsInf(progress, 0) {
		return nil, fmt.Errorf("scene transition progress must be finite")
	}
	t := math.Max(0, math.Min(1, progress))
	out := make([]Command, 0, len(p.plans))
	order := p.orders[0]
	if t == 1 {
		order = p.orders[1]
	}
	for _, index := range order {
		plan := p.plans[index]
		command := plan.command(t == 1)
		c := *command
		if plan.data != nil {
			c.Data, _ = plan.data.sample(t)
		} else {
			c.Data = cloneTransitionValue(c.Data)
		}
		out = append(out, c)
	}
	return out, nil
}
