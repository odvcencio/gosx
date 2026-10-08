package budget

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// WriteJSON validates before writing any bytes. Keys are sorted and output has
// one LF; the artifact contains only the public Report representation.
func WriteJSON(w io.Writer, report Report) error {
	v, err := defaultPublicValidator()
	if err != nil {
		return err
	}
	return v.WriteJSON(w, report)
}
func (v *PublicValidator) WriteJSON(w io.Writer, report Report) error {
	data, err := canonicalPublicJSON(report)
	if err != nil {
		return err
	}
	if err := v.Validate(bytes.NewReader(data), "json"); err != nil {
		return err
	}
	return writePublicBytes(w, data)
}

func canonicalPublicJSON(report Report) ([]byte, error) {
	data, err := json.Marshal(report)
	if err != nil {
		return nil, inputReference(invalidInput(""), "report", "")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if decoder.Decode(&value) != nil {
		return nil, inputReference(invalidInput(""), "report", "")
	}
	data, err = json.Marshal(value)
	if err != nil {
		return nil, inputReference(invalidInput(""), "report", "")
	}
	return append(data, '\n'), nil
}

// WriteMarkdown emits fixed prose and a table backed by the complete validated
// JSON record. The validator reconstructs this exact representation.
func WriteMarkdown(w io.Writer, report Report) error {
	v, err := defaultPublicValidator()
	if err != nil {
		return err
	}
	return v.WriteMarkdown(w, report)
}
func (v *PublicValidator) WriteMarkdown(w io.Writer, report Report) error {
	data, err := renderPublicMarkdown(report)
	if err != nil {
		return err
	}
	if err := v.Validate(bytes.NewReader(data), "markdown"); err != nil {
		return err
	}
	return writePublicBytes(w, data)
}
func renderPublicMarkdown(report Report) ([]byte, error) {
	data, err := canonicalPublicJSON(report)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	out.WriteString("# Performance budget report\n\n")
	fmt.Fprintf(&out, "Mode: `%s`.\n\n", report.Mode)
	out.WriteString("| App | Route | Type | Scenario | Normalized B | Wire B | Framework B | App B | Status |\n| --- | --- | --- | --- | ---: | ---: | ---: | ---: | --- |\n")
	for _, row := range report.Rows {
		fmt.Fprintf(&out, "| %s | %s | %s | %s | %d | %d | %d | %d | %s |\n", row.App, row.RouteTemplate, row.PageType, row.Scenario, row.NormalizedBytes, row.WireBytes, row.FrameworkBytes, row.AppBytes, row.Status)
	}
	out.WriteString("\n```json\n")
	out.Write(data)
	out.WriteString("```\n")
	return out.Bytes(), nil
}

// ReadField accepts the public histogram format and registered identifiers only.
func ReadField(r io.Reader) (*FieldSnapshot, error) {
	data, err := readPublicArtifact(r, maxInputBytes)
	if err != nil {
		return nil, err
	}
	decoded, err := DecodeRecord(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	field, ok := decoded.(*FieldSnapshot)
	if !ok {
		return nil, inputReference(invalidInput("/schema"), "field", "")
	}
	v, err := defaultPublicValidator()
	if err != nil {
		return nil, err
	}
	if err := v.validateMembership(field); err != nil {
		return nil, err
	}
	return field, nil
}

func writePublicBytes(w io.Writer, data []byte) error {
	n, err := w.Write(data)
	if err != nil || n != len(data) {
		return &InputError{Code: "write-failed", Reference: "public-record"}
	}
	return nil
}
