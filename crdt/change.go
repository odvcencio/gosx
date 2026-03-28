package crdt

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	enc "github.com/odvcencio/gosx/crdt/encoding"
)

type ActorID [16]byte
type ChangeHash [32]byte

type OpID struct {
	Counter uint64 `json:"counter"`
	Actor   string `json:"actor"`
}

type Op struct {
	ID     OpID   `json:"id"`
	Action string `json:"action"`
	Obj    ObjID  `json:"obj"`
	Prop   Prop   `json:"prop,omitempty"`
	Value  Value  `json:"value,omitempty"`
	After  string `json:"after,omitempty"`
}

type Change struct {
	Hash    ChangeHash   `json:"-"`
	ActorID string       `json:"actorId"`
	Seq     uint64       `json:"seq"`
	Deps    []ChangeHash `json:"deps,omitempty"`
	StartOp uint64       `json:"startOp"`
	Time    time.Time    `json:"time"`
	Message string       `json:"message,omitempty"`
	Ops     []Op         `json:"ops"`
}

type Patch struct {
	Obj    ObjID  `json:"obj"`
	Prop   Prop   `json:"prop"`
	Action string `json:"action"`
	Value  Value  `json:"value,omitempty"`
}

func NewActorID() (ActorID, error) {
	var actor ActorID
	if _, err := rand.Read(actor[:]); err != nil {
		return ActorID{}, err
	}
	return actor, nil
}

func ParseActorID(value string) (ActorID, error) {
	raw, err := hex.DecodeString(value)
	if err != nil {
		return ActorID{}, fmt.Errorf("decode actor id: %w", err)
	}
	if len(raw) != 16 {
		return ActorID{}, fmt.Errorf("invalid actor id length %d", len(raw))
	}
	var actor ActorID
	copy(actor[:], raw)
	return actor, nil
}

func (a ActorID) String() string {
	return hex.EncodeToString(a[:])
}

func (h ChangeHash) String() string {
	return hex.EncodeToString(h[:])
}

func ParseChangeHash(value string) (ChangeHash, error) {
	raw, err := hex.DecodeString(value)
	if err != nil {
		return ChangeHash{}, fmt.Errorf("decode change hash: %w", err)
	}
	if len(raw) != 32 {
		return ChangeHash{}, fmt.Errorf("invalid change hash length %d", len(raw))
	}
	var hash ChangeHash
	copy(hash[:], raw)
	return hash, nil
}

func (id OpID) String() string {
	return fmt.Sprintf("%d@%s", id.Counter, id.Actor)
}

func (id OpID) Less(other OpID) bool {
	if id.Counter != other.Counter {
		return id.Counter < other.Counter
	}
	return id.Actor < other.Actor
}

func (id OpID) Greater(other OpID) bool {
	return other.Less(id)
}

func EncodeChangeChunk(change Change) ([]byte, ChangeHash, error) {
	body, err := marshalJSON(change)
	if err != nil {
		return nil, ChangeHash{}, err
	}
	hash := ChangeHash(enc.ChangeHash(body))
	return enc.EncodeChange(body), hash, nil
}

func DecodeChangeChunk(data []byte) (Change, error) {
	body, err := enc.DecodeChange(data)
	if err != nil {
		return Change{}, err
	}
	var change Change
	if err := unmarshalJSON(body, &change); err != nil {
		return Change{}, fmt.Errorf("decode change body: %w", err)
	}
	change.Hash = ChangeHash(enc.ChangeHash(body))
	return change, nil
}
