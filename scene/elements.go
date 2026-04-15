package scene

import (
	"errors"
	"fmt"
	"strings"
)

// Scene3DProps is the compiler-facing catalog root for composable Scene3D
// elements. A gsx lowering pass can build this value and then call LowerScene3D.
type Scene3DProps struct {
	Camera      IRCamera
	Environment IREnvironment
	Materials   []IRMaterial
	Nodes       []IRNode
	Lights      []IRLight
	PostFX      []IRPostEffect
}

type MeshElementProps struct {
	ID            string
	MaterialIndex int
	Transform     IRTransform
	Kind          string
	Src           string
	Size          float64
	Width         float64
	Height        float64
	Depth         float64
	Radius        float64
	Segments      int
	CastShadow    bool
	ReceiveShadow bool
	Pickable      *bool
	Static        *bool
	Animation     string
	Loop          *bool
}

type PointsElementProps struct {
	ID          string
	Transform   IRTransform
	Count       int
	Positions   []float64
	Sizes       []float64
	Colors      []string
	Color       string
	Style       string
	Size        float64
	Opacity     float64
	BlendMode   string
	DepthWrite  *bool
	Attenuation bool
}

type InstancedMeshElementProps struct {
	ID            string
	MaterialIndex int
	Count         int
	Kind          string
	Width         float64
	Height        float64
	Depth         float64
	Radius        float64
	Segments      int
	Transforms    []float64
	CastShadow    bool
	ReceiveShadow bool
}

type ComputeParticlesElementProps struct {
	ID       string
	Count    int
	Emitter  IRParticleEmitter
	Forces   []IRParticleForce
	Material IRParticleMaterial
	Bounds   float64
}

type SpriteElementProps struct {
	ID        string
	Transform IRTransform
	Sprite    IRSpriteNode
}

type LabelElementProps struct {
	ID        string
	Transform IRTransform
	Label     IRLabelNode
}

// LowerScene3D materializes a compiler-built Scene3D catalog into canonical IR.
func LowerScene3D(props Scene3DProps) IR {
	return IR{
		Version:     IRVersion,
		Camera:      props.Camera,
		Environment: props.Environment,
		Materials:   append([]IRMaterial(nil), props.Materials...),
		Lights:      append([]IRLight(nil), props.Lights...),
		Nodes:       append([]IRNode(nil), props.Nodes...),
		PostFX:      append([]IRPostEffect(nil), props.PostFX...),
	}
}

func LowerMesh(props MeshElementProps) IRNode {
	return IRNode{
		Kind:          "mesh",
		ID:            strings.TrimSpace(props.ID),
		MaterialIndex: props.MaterialIndex,
		Transform:     props.Transform,
		Mesh: &IRMeshNode{
			Kind:          strings.TrimSpace(props.Kind),
			Src:           strings.TrimSpace(props.Src),
			Size:          props.Size,
			Width:         props.Width,
			Height:        props.Height,
			Depth:         props.Depth,
			Radius:        props.Radius,
			Segments:      props.Segments,
			CastShadow:    props.CastShadow,
			ReceiveShadow: props.ReceiveShadow,
			Pickable:      props.Pickable,
			Static:        props.Static,
			Animation:     strings.TrimSpace(props.Animation),
			Loop:          props.Loop,
		},
	}
}

func LowerPoints(props PointsElementProps) IRNode {
	return IRNode{
		Kind:      "points",
		ID:        strings.TrimSpace(props.ID),
		Transform: props.Transform,
		Points: &IRPointsNode{
			Count:       props.Count,
			Positions:   append([]float64(nil), props.Positions...),
			Sizes:       append([]float64(nil), props.Sizes...),
			Colors:      append([]string(nil), props.Colors...),
			Color:       strings.TrimSpace(props.Color),
			Style:       strings.TrimSpace(props.Style),
			Size:        props.Size,
			Opacity:     props.Opacity,
			BlendMode:   strings.TrimSpace(props.BlendMode),
			DepthWrite:  props.DepthWrite,
			Attenuation: props.Attenuation,
		},
	}
}

func LowerInstancedMesh(props InstancedMeshElementProps) IRNode {
	return IRNode{
		Kind:          "instanced-mesh",
		ID:            strings.TrimSpace(props.ID),
		MaterialIndex: props.MaterialIndex,
		InstancedMesh: &IRInstancedMesh{
			Count:         props.Count,
			Kind:          strings.TrimSpace(props.Kind),
			Width:         props.Width,
			Height:        props.Height,
			Depth:         props.Depth,
			Radius:        props.Radius,
			Segments:      props.Segments,
			Transforms:    append([]float64(nil), props.Transforms...),
			CastShadow:    props.CastShadow,
			ReceiveShadow: props.ReceiveShadow,
		},
	}
}

func LowerComputeParticles(props ComputeParticlesElementProps) IRNode {
	return IRNode{
		Kind:         "compute-particles",
		ID:           strings.TrimSpace(props.ID),
		Capabilities: []string{"compute"},
		Compute: &IRComputeNode{
			Count:    props.Count,
			Emitter:  props.Emitter,
			Forces:   append([]IRParticleForce(nil), props.Forces...),
			Material: props.Material,
			Bounds:   props.Bounds,
		},
	}
}

func LowerSprite(props SpriteElementProps) IRNode {
	return IRNode{
		Kind:      "sprite",
		ID:        strings.TrimSpace(props.ID),
		Transform: props.Transform,
		Sprite:    &props.Sprite,
	}
}

func LowerLabel(props LabelElementProps) IRNode {
	return IRNode{
		Kind:      "label",
		ID:        strings.TrimSpace(props.ID),
		Transform: props.Transform,
		Label:     &props.Label,
	}
}

// ValidateCapabilities enforces compile-time capability gates for lowered IR.
func ValidateCapabilities(ir IR, capabilities []string) error {
	have := map[string]bool{}
	for _, capability := range capabilities {
		have[strings.ToLower(strings.TrimSpace(capability))] = true
	}
	for _, node := range ir.Nodes {
		if node.Kind == "compute-particles" && !(have["webgpu"] || have["webgl2"] || have["compute"]) {
			return errors.New("ComputeParticles requires webgpu or webgl2 capability")
		}
		for _, required := range node.Capabilities {
			capability := strings.ToLower(strings.TrimSpace(required))
			if capability == "" || capability == "compute" && (have["webgpu"] || have["webgl2"]) {
				continue
			}
			if !have[capability] {
				id := strings.TrimSpace(node.ID)
				if id == "" {
					id = strings.TrimSpace(node.Kind)
				}
				return fmt.Errorf("Scene3D node %q requires %s capability", id, capability)
			}
		}
	}
	return nil
}
