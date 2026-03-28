package bridge

import (
	"encoding/json"
	"fmt"

	"github.com/odvcencio/gosx/crdt"
	crdtsync "github.com/odvcencio/gosx/crdt/sync"
	"github.com/odvcencio/gosx/signal"
)

// CRDTSignal mirrors a signal write into a CRDT document.
type CRDTSignal[T any] struct {
	inner  *signal.Signal[T]
	doc    *crdt.Doc
	obj    crdt.ObjID
	prop   crdt.Prop
	encode func(T) (crdt.Value, error)
	decode func(crdt.Value) (T, error)
}

func NewCRDTSignal[T any](
	inner *signal.Signal[T],
	doc *crdt.Doc,
	obj crdt.ObjID,
	prop crdt.Prop,
	encode func(T) (crdt.Value, error),
	decode func(crdt.Value) (T, error),
) *CRDTSignal[T] {
	return &CRDTSignal[T]{
		inner:  inner,
		doc:    doc,
		obj:    obj,
		prop:   prop,
		encode: encode,
		decode: decode,
	}
}

func (s *CRDTSignal[T]) Set(val T) error {
	value, err := s.encode(val)
	if err != nil {
		return err
	}
	if err := s.doc.Put(s.obj, s.prop, value); err != nil {
		return err
	}
	s.doc.Commit("")
	s.inner.Set(val)
	return nil
}

func (s *CRDTSignal[T]) Get() T {
	return s.inner.Get()
}

func (s *CRDTSignal[T]) ApplyValue(value crdt.Value) error {
	decoded, err := s.decode(value)
	if err != nil {
		return err
	}
	s.inner.Set(decoded)
	return nil
}

// CRDTBridge manages a local replica plus per-peer sync state.
type CRDTBridge struct {
	doc   *crdt.Doc
	state *crdtsync.State
}

func NewCRDTBridge() *CRDTBridge {
	return &CRDTBridge{
		doc:   crdt.NewDoc(),
		state: crdtsync.NewState(),
	}
}

func (b *CRDTBridge) Doc() *crdt.Doc {
	return b.doc
}

func (b *CRDTBridge) InitDoc(data []byte) error {
	if len(data) == 0 {
		b.doc = crdt.NewDoc()
		b.state = crdtsync.NewState()
		return nil
	}
	doc, err := crdt.Load(data)
	if err != nil {
		return err
	}
	b.doc = doc
	b.state = crdtsync.NewState()
	return nil
}

func (b *CRDTBridge) Sync(msg []byte) ([]byte, error) {
	if err := b.doc.ReceiveSyncMessage(b.state, msg); err != nil {
		return nil, err
	}
	reply, ok := b.doc.GenerateSyncMessage(b.state)
	if !ok {
		return nil, nil
	}
	return reply, nil
}

func (b *CRDTBridge) Put(obj crdt.ObjID, prop crdt.Prop, valueJSON string) error {
	var raw any
	if err := json.Unmarshal([]byte(valueJSON), &raw); err != nil {
		return fmt.Errorf("decode crdt put payload: %w", err)
	}
	value, err := crdt.ValueFromAny(raw)
	if err != nil {
		return err
	}
	if err := b.doc.Put(obj, prop, value); err != nil {
		return err
	}
	b.doc.Commit("")
	return nil
}

func (b *CRDTBridge) Get(obj crdt.ObjID, prop crdt.Prop) (string, error) {
	value, _, err := b.doc.Get(obj, prop)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(value.ToAny())
	if err != nil {
		return "", err
	}
	return string(data), nil
}
