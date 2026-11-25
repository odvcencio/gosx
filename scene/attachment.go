package scene

import "strings"

// Attachment fixes the meshes of another glTF file to one joint of a
// skinned Model, so gear rides a character: a helmet's facemask on the head,
// a glove on a hand, a towel at the hip. The meshes are authored where they
// sit on the model in its bind pose, in the model's own units; each vertex
// is bound wholly to the joint, so the joint matrices that pose the body
// pose the gear on the same frame, on the GPU, with nothing to update per
// frame.
//
// Every model that names the same Src shares one loaded copy of it. A
// hidden attachment costs no draw; ModelPatch.Attachments shows and hides
// attachments of a mounted model without loading it again, so a model can
// carry every piece of gear it might wear and show the pieces its player
// wears.
type Attachment struct {
	// Name identifies the attachment within its model, for ModelPatch.
	Name string
	// Src is the glTF or GLB file that holds the meshes.
	Src string
	// Mesh picks one mesh of Src by its glTF name; empty takes them all.
	Mesh string
	// Joint is the name of the glTF node, a joint of the model's skin, the
	// meshes ride.
	Joint string
	// Hidden starts the attachment hidden.
	Hidden bool
	// Material replaces the meshes' own material. Nil draws them with the
	// model's Material when it has one, so gear painted on the model's
	// texture atlas takes the model's colours.
	Material Material
}

// AttachmentIR is an Attachment on the wire.
type AttachmentIR struct {
	Name     string    `json:"name"`
	Src      string    `json:"src"`
	Mesh     string    `json:"mesh,omitempty"`
	Joint    string    `json:"joint"`
	Hidden   bool      `json:"hidden,omitempty"`
	Material *ObjectIR `json:"material,omitempty"`
}

// lowerAttachments returns the attachments that name a joint and a source,
// trimmed, or nil.
func lowerAttachments(list []Attachment) []AttachmentIR {
	var out []AttachmentIR
	for _, a := range list {
		name, src, joint := strings.TrimSpace(a.Name), strings.TrimSpace(a.Src), strings.TrimSpace(a.Joint)
		if name == "" || src == "" || joint == "" {
			continue
		}
		ir := AttachmentIR{Name: name, Src: src, Mesh: strings.TrimSpace(a.Mesh), Joint: joint, Hidden: a.Hidden}
		if props := legacyMaterial(a.Material); len(props) > 0 {
			m := ObjectIR{ID: name, Kind: "attachment"}
			applyMaterialProps(&m, props)
			ir.Material = &m
		}
		out = append(out, ir)
	}
	return out
}
