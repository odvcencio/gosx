package schema

import "m31labs.dev/gosx/internal/telemetryfields"

// FieldType identifies a scalar, bounded array, or object in a declared codec.
type FieldType = telemetryfields.FieldType

const (
	FieldInt      = telemetryfields.FieldInt
	FieldFloat    = telemetryfields.FieldFloat
	FieldBool     = telemetryfields.FieldBool
	FieldDuration = telemetryfields.FieldDuration
	FieldEnum     = telemetryfields.FieldEnum
	FieldInts     = telemetryfields.FieldInts
	FieldFloats   = telemetryfields.FieldFloats
	FieldEnums    = telemetryfields.FieldEnums
	FieldObject   = telemetryfields.FieldObject
)

// Fields is a copied domain projection. Mutating a returned view cannot change
// its activity or record. JSON orders keys lexicographically without HTML
// escaping; durations are milliseconds. There is no arbitrary string/raw body.
type Fields = telemetryfields.Fields

// Field contains only the value selected by Type. Enums originate in a trusted
// codec whitelist. Arrays and nested objects in returned views are copied.
type Field = telemetryfields.Field
