package scene3d

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// decodeEnvelope is shared by the inbound gate, command readers, and View.
// Property lookup is case-sensitive, and a create kind must occur at most once
// and hold a string. Reusing the decoder and record buffers keeps scene reads
// from allocating a new parser for each record.
func (c *createDecoder) decodeEnvelope(payload string) (err error) {
	c.payload = append(c.payload[:0], payload...)
	c.envelope.Kind = ""
	c.envelope.Geometry = ""
	c.envelope.Props = c.envelope.Props[:0]
	c.envelopeSrc.Reset(c.payload)
	if c.envelopeDec == nil {
		c.envelopeDec = json.NewDecoder(&c.envelopeSrc)
	}
	decoder := c.envelopeDec
	start := decoder.InputOffset()
	defer func() {
		if err != nil {
			c.envelopeDec = nil
		}
	}()
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return fmt.Errorf("scene3d: invalid create envelope")
	}
	var kindSeen, geometrySeen, propsSeen bool
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("scene3d: invalid create property")
		}
		switch name {
		case "kind":
			if kindSeen {
				return fmt.Errorf("scene3d: duplicate create kind")
			}
			kindSeen = true
			token, err := decoder.Token()
			kind, ok := token.(string)
			if err != nil || !ok {
				return fmt.Errorf("scene3d: create kind must be a string")
			}
			c.envelope.Kind = kind
		case "geometry":
			if geometrySeen {
				return fmt.Errorf("scene3d: duplicate create geometry")
			}
			geometrySeen = true
			if err := decoder.Decode(&c.envelope.Geometry); err != nil {
				return err
			}
		case "props":
			if propsSeen {
				return fmt.Errorf("scene3d: duplicate create props")
			}
			propsSeen = true
			if err := decoder.Decode(&c.envelope.Props); err != nil {
				return err
			}
		default:
			if strings.EqualFold(name, "kind") {
				return fmt.Errorf("scene3d: ambiguous create kind")
			}
			if err := decoder.Decode(&c.ignored); err != nil {
				return err
			}
		}
	}
	if _, err := decoder.Token(); err != nil {
		return err
	}
	consumed := decoder.InputOffset() - start
	if consumed != int64(len(c.payload)) {
		// Do not carry trailing whitespace in the decoder into the next record.
		c.envelopeDec = nil
		if len(bytes.TrimSpace(c.payload[consumed:])) != 0 {
			return fmt.Errorf("scene3d: trailing create payload data")
		}
	}
	return nil
}
