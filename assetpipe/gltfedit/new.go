package gltfedit

import (
	"encoding/json"
	"fmt"
)

// New returns an empty glTF 2.0 document that a generator fills with
// AddAccessor, AddNode, AddBufferView and SetSection. generator is written to
// asset.generator.
func New(generator string) *Document {
	d := &Document{staging: -1, root: map[string]json.RawMessage{}}
	_ = setSection(d.root, "asset", map[string]string{"version": "2.0", "generator": generator})
	return d
}

// modelled lists the sections the Document keeps as typed fields. SetSection
// refuses them, because the writer would overwrite the raw value.
var modelled = map[string]bool{
	"accessors": true, "bufferViews": true, "buffers": true, "meshes": true,
	"nodes": true, "scenes": true, "skins": true, "animations": true,
	"extensionsUsed": true, "extensionsRequired": true,
}

// SetSection stores a top-level section the Document does not model as a
// typed field, such as "materials", "textures", "samplers", "images" or
// "scene". An image's bufferView index is remapped on write like an
// accessor's.
func (d *Document) SetSection(key string, value any) error {
	if modelled[key] {
		return fmt.Errorf("%w: section %q is a typed Document field", ErrUnsupported, key)
	}
	if d.root == nil {
		d.root = map[string]json.RawMessage{}
	}
	return setSection(d.root, key, value)
}

// Section decodes a top-level section into target. It reports false when the
// document has no such section.
func (d *Document) Section(key string, target any) (bool, error) {
	if _, ok := d.root[key]; !ok {
		return false, nil
	}
	return true, d.decodeSection(key, target)
}

// AddBufferView copies raw bytes, such as an encoded image, into the
// document's binary buffer and returns the new buffer view's index.
func (d *Document) AddBufferView(data []byte, target int) int {
	return d.appendView(data, target)
}
