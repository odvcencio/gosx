package strictcheck

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func newCollectionModule(t *testing.T) string {
	t.Helper()
	dir := newTestModule(t)
	mustWrite(t, filepath.Join(dir, "gosxstub/signal/signal.go"), `package signal
type Signal[T any] struct { value T }
func New[T any](value T) *Signal[T] { return &Signal[T]{value: value} }
func NewShared[T any](_ string, value T) *Signal[T] { return New(value) }
func (s *Signal[T]) Get() T { return s.value }
func (s *Signal[T]) Set(value T) { s.value = value }
`)
	return dir
}

const collectionCheckSource = `package app
import state "m31labs.dev/gosx/signal"
type Props struct { Initial string }
//gosx:island
component Collection(props: Props) {
 items := state.New([]string{props.Initial})
 draft := state.New("")
 edit := func() { draft.Set(value) }
 add := func() { items.Set(append(items.Get(), draft.Get())) }
 return <section><input value={draft.Get()} onInput={edit}/><button onClick={add}>Add</button><ul><Each of={items.Get()} as="item"><li>{item}</li></Each></ul></section>
}
`

func TestCheckIslandCollectionUsesGoTypes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
	}{
		{"text input", collectionCheckSource},
		{"helper reads event", strings.Replace(collectionCheckSource, `edit := func() { draft.Set(value) }`, `helper := func() { draft.Set(value) }; edit := func() { helper() }`, 1)},
		{"guarded collection", strings.Replace(collectionCheckSource, `items.Set(append(items.Get(), draft.Get()))`, `if draft.Get() == "" || len(items.Get()) >= 12 { return }; items.Set(append(items.Get(), draft.Get()))`, 1)},
		{"event values in branches", strings.Replace(collectionCheckSource, `draft.Set(value)`, `if checked { draft.Set(value) } else { return }`, 1)},
		{"shared slice", strings.Replace(collectionCheckSource, `state.New([]string{props.Initial})`, `state.NewShared("collection", []string{props.Initial})`, 1)},
		{"explicit slice type", strings.Replace(collectionCheckSource, `state.New([]string{props.Initial})`, `state.New[[]string]([]string{props.Initial})`, 1)},
		{"signal named value", strings.Replace(strings.Replace(collectionCheckSource, `draft := state.New("")`, "draft := state.New(\"\")\n value := state.New(\"bound signal\")", 1), `draft.Set(value)`, `draft.Set(value.Get())`, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newCollectionModule(t)
			path := filepath.Join(dir, "page.gsx")
			mustWrite(t, path, tc.source)
			if err := CheckFile(context.Background(), path); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckIslandCollectionProps(t *testing.T) {
	for _, tc := range []struct{ name, source string }{
		{"strings", strings.Replace(strings.Replace(collectionCheckSource, `Initial string`, `Initial []string`, 1), `state.New([]string{props.Initial})`, `state.New(props.Initial)`, 1)},
		{"records", `package app
import state "m31labs.dev/gosx/signal"
type Block struct { Name string; Active bool }
type Props struct { Initial []Block }
//gosx:island
component Collection(props: Props) {
 items := state.New(props.Initial)
 clear := func() { items.Set([]Block{}) }
 return <section><button onClick={clear}>Clear</button><ul><Each of={items.Get()} as="item"><li>{item.Name}</li></Each></ul></section>
}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := newCollectionModule(t)
			path := filepath.Join(dir, "page.gsx")
			mustWrite(t, path, tc.source)
			if err := CheckFile(context.Background(), path); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckIslandGuardConditionUsesGoTypes(t *testing.T) {
	dir := newCollectionModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, strings.Replace(collectionCheckSource, `draft.Set(value)`, `if value { draft.Set(value) }`, 1))
	err := CheckFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "non-boolean") {
		t.Fatalf("string event value accepted as a guard: %v", err)
	}
}

func TestCheckIslandEventValueKeepsItsType(t *testing.T) {
	dir := newCollectionModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, strings.Replace(collectionCheckSource, `draft.Set(value)`, `draft.Set(checked)`, 1))
	err := CheckFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "bool") || !strings.Contains(err.Error(), "string") {
		t.Fatalf("a boolean event value entered a string signal: %v", err)
	}
}

func TestCheckIslandEventValueRejectsPackageShadow(t *testing.T) {
	dir := newCollectionModule(t)
	path := filepath.Join(dir, "page.gsx")
	mustWrite(t, path, strings.Replace(collectionCheckSource, `type Props`, "const value = \"package value\"\ntype Props", 1))
	err := CheckFile(context.Background(), path)
	if err == nil || !strings.Contains(err.Error(), "implicit event field value is shadowed") {
		t.Fatalf("ambiguous package/event binding accepted: %v", err)
	}
}
