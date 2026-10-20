package schema

import "m31labs.dev/gosx/internal/telemetryrecord"

type Envelope = telemetryrecord.Envelope
type Record = telemetryrecord.Record
type RecordType = telemetryrecord.RecordType
type RecordState = telemetryrecord.RecordState
type Identity = telemetryrecord.Identity
type Persistence = telemetryrecord.Persistence

const (
	SchemaVersion           = telemetryrecord.SchemaVersion
	PersistenceNone         = telemetryrecord.PersistenceNone
	PersistenceMemory       = telemetryrecord.PersistenceMemory
	PersistenceLocal        = telemetryrecord.PersistenceLocal
	PersistenceExternal     = telemetryrecord.PersistenceExternal
	TypeVisit               = telemetryrecord.TypeVisit
	TypeHubSession          = telemetryrecord.TypeHubSession
	TypeActivity            = telemetryrecord.TypeActivity
	TypeActivityEvent       = telemetryrecord.TypeActivityEvent
	TypeClientHealthSummary = telemetryrecord.TypeClientHealthSummary
	StateOpen               = telemetryrecord.StateOpen
	StateCheckpoint         = telemetryrecord.StateCheckpoint
	StateFinal              = telemetryrecord.StateFinal
	StateEvent              = telemetryrecord.StateEvent
)
