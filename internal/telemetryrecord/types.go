// Package telemetryrecord owns the immutable portable record representation.
// Only framework packages can call its typed constructors.
package telemetryrecord

import (
	"m31labs.dev/gosx/internal/telemetryfields"
	"time"
)

const SchemaVersion = 1
const MaxRecordBytes = 16 << 10

type Persistence uint8

const (
	PersistenceNone Persistence = iota
	PersistenceMemory
	PersistenceLocal
	PersistenceExternal
)

type RecordType string

const (
	TypeVisit               RecordType = "visit"
	TypeHubSession          RecordType = "hub_session"
	TypeActivity            RecordType = "activity"
	TypeActivityEvent       RecordType = "activity_event"
	TypeClientHealthSummary RecordType = "client_health_summary"
)

type RecordState string

const (
	StateOpen       RecordState = "open"
	StateCheckpoint RecordState = "checkpoint"
	StateFinal      RecordState = "final"
	StateEvent      RecordState = "event"
)

type Envelope struct {
	Schema   int         `json:"schema"`
	Stream   string      `json:"stream"`
	Boot     string      `json:"boot"`
	Seq      uint64      `json:"seq"`
	Type     RecordType  `json:"type"`
	State    RecordState `json:"state"`
	At       time.Time   `json:"at"`
	Revision uint64      `json:"revision"`
	CRC32C   string      `json:"crc32c"`
}

type Identity struct {
	App      string `json:"app"`
	Version  string `json:"version"`
	Revision string `json:"revision"`
}

type Client struct {
	Platform string `json:"platform"`
	Browser  string `json:"browser"`
	Device   string `json:"device"`
}

type Vitals struct {
	LCPMS  *float64 `json:"lcp_ms,omitempty"`
	INPMS  *float64 `json:"inp_ms,omitempty"`
	TTFBMS *float64 `json:"ttfb_ms,omitempty"`
	CLS    *float64 `json:"cls,omitempty"`
}

type Visit struct {
	ID            string                 `json:"id"`
	StartedAt     time.Time              `json:"started_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	EndedAt       time.Time              `json:"ended_at,omitzero"`
	ElapsedMS     float64                `json:"elapsed_ms"`
	ActiveMS      float64                `json:"active_ms"`
	EndReason     string                 `json:"end_reason,omitempty"`
	InitialRoute  string                 `json:"initial_route"`
	PageCount     uint64                 `json:"page_count"`
	Client        Client                 `json:"client"`
	Warnings      uint64                 `json:"warnings"`
	Errors        uint64                 `json:"errors"`
	Vitals        *Vitals                `json:"vitals,omitempty"`
	VisitorHash   string                 `json:"visitor_hash,omitempty"`
	ClockAdjusted bool                   `json:"clock_adjusted,omitempty"`
	Fields        telemetryfields.Fields `json:"fields"`
	Identity      *Identity              `json:"identity,omitempty"`
}

type HubSession struct {
	ID            string    `json:"id"`
	Hub           string    `json:"hub"`
	VisitID       string    `json:"visit_id,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	EndedAt       time.Time `json:"ended_at,omitzero"`
	ElapsedMS     float64   `json:"elapsed_ms"`
	Reason        string    `json:"reason,omitempty"`
	MessagesIn    uint64    `json:"messages_in"`
	MessagesOut   uint64    `json:"messages_out"`
	BytesIn       uint64    `json:"bytes_in"`
	BytesOut      uint64    `json:"bytes_out"`
	TextDrops     uint64    `json:"text_drops"`
	BinaryDrops   uint64    `json:"binary_drops"`
	Reconnects    uint64    `json:"reconnects"`
	ClockAdjusted bool      `json:"clock_adjusted,omitempty"`
	Identity      *Identity `json:"identity,omitempty"`
}

type Dimension struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type Codec struct {
	Name    string `json:"name"`
	Version uint16 `json:"version"`
}
type ReasonCount struct {
	Reason string `json:"reason"`
	Count  uint64 `json:"count"`
}
type SessionLink struct {
	ID      string `json:"id"`
	VisitID string `json:"visit_id,omitempty"`
}
type Participant struct {
	ID             string                 `json:"id"`
	Seat           int                    `json:"seat"`
	Role           string                 `json:"role"`
	Human          bool                   `json:"human"`
	Joins          uint64                 `json:"joins"`
	Leaves         uint64                 `json:"leaves"`
	Reconnects     uint64                 `json:"reconnects"`
	Reasons        []ReasonCount          `json:"reasons,omitempty"`
	Sessions       []SessionLink          `json:"sessions,omitempty"`
	Client         *Client                `json:"client,omitempty"`
	SeatPresenceMS float64                `json:"seat_presence_ms"`
	LinksTruncated bool                   `json:"links_truncated,omitempty"`
	Codec          Codec                  `json:"codec"`
	Fields         telemetryfields.Fields `json:"fields"`
}

type TickHealth struct {
	Available       bool    `json:"available"`
	Samples         uint64  `json:"samples"`
	P50MS           float64 `json:"p50_ms"`
	P99MS           float64 `json:"p99_ms"`
	MaxMS           float64 `json:"max_ms"`
	Overruns        uint64  `json:"overruns"`
	BudgetMS        float64 `json:"budget_ms"`
	OverflowSamples uint64  `json:"overflow_samples"`
}
type Activity struct {
	ID             string                 `json:"id"`
	ParentID       string                 `json:"parent_id,omitempty"`
	Kind           string                 `json:"kind"`
	Dimensions     []Dimension            `json:"dimensions"`
	StartedAt      time.Time              `json:"started_at"`
	UpdatedAt      time.Time              `json:"updated_at"`
	EndedAt        time.Time              `json:"ended_at,omitzero"`
	ElapsedMS      float64                `json:"elapsed_ms"`
	Outcome        string                 `json:"outcome,omitempty"`
	Reason         string                 `json:"reason,omitempty"`
	Codec          Codec                  `json:"codec"`
	Participants   []Participant          `json:"participants"`
	TickHealth     *TickHealth            `json:"tick_health,omitempty"`
	EventsAccepted uint64                 `json:"events_accepted"`
	EventsDropped  uint64                 `json:"events_dropped"`
	ClockAdjusted  bool                   `json:"clock_adjusted,omitempty"`
	Fields         telemetryfields.Fields `json:"fields"`
	Identity       *Identity              `json:"identity,omitempty"`
}
type ActivityEvent struct {
	ActivityID string                 `json:"activity_id"`
	Seq        uint64                 `json:"seq"`
	Name       string                 `json:"name"`
	ObservedAt time.Time              `json:"observed_at"`
	Codec      Codec                  `json:"codec"`
	Fields     telemetryfields.Fields `json:"fields"`
}
type ClientHealthSummary struct {
	BucketAt   time.Time `json:"bucket_at"`
	Route      string    `json:"route"`
	Code       string    `json:"code"`
	RenderTier string    `json:"render_tier"`
	Viewport   string    `json:"viewport"`
	Count      uint64    `json:"count"`
}
