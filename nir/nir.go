// Package nir is the target-agnostic intermediate representation for
// gosx-native. M1 ships only the node types Counter needs; types accrete per
// subsequent milestone.
package nir

import (
	"encoding/json"
	"fmt"
)

type Module struct {
	Version        int          `json:"version"`
	SourceLanguage string       `json:"source_language"`
	Components     []*Component `json:"components"`
}

type Component struct {
	Name      string          `json:"name"`
	Props     *Props          `json:"props,omitempty"`
	Signals   []*SignalDecl   `json:"signals,omitempty"`
	Computeds []*ComputedDecl `json:"computeds,omitempty"`
	Body      View            `json:"body"`
	Span      Span            `json:"span"`
}

func (c *Component) UnmarshalJSON(data []byte) error {
	type componentAlias Component
	var raw struct {
		*componentAlias
		Body json.RawMessage `json:"body"`
	}
	raw.componentAlias = (*componentAlias)(c)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if len(raw.Body) > 0 && string(raw.Body) != "null" {
		body, err := decodeView(raw.Body)
		if err != nil {
			return fmt.Errorf("body: %w", err)
		}
		c.Body = body
	}
	return nil
}

type Props struct {
	Fields []PropField `json:"fields"`
}

type PropField struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// View is the sum type for view-tree nodes. M1 ships only Element, Text, and
// ExprHole. Slot/Fragment/Conditional/Loop/ComponentRef land later as the
// corpus grows.
type View interface {
	isView()
}

type Element struct {
	Tag      string    `json:"tag"`
	Attrs    []Attr    `json:"attrs,omitempty"`
	Handlers []Handler `json:"handlers,omitempty"`
	Children []View    `json:"children,omitempty"`
	Span     Span      `json:"span"`
}

func (*Element) isView() {}

func (e *Element) UnmarshalJSON(data []byte) error {
	type elementAlias Element
	var raw struct {
		*elementAlias
		Children []json.RawMessage `json:"children"`
	}
	raw.elementAlias = (*elementAlias)(e)
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	children, err := decodeViews(raw.Children)
	if err != nil {
		return err
	}
	e.Children = children
	return nil
}

type Text struct {
	Value string `json:"value"`
	Span  Span   `json:"span"`
}

func (*Text) isView() {}

type ExprHole struct {
	Expr RxExpr `json:"expr"`
	Span Span   `json:"span"`
}

func (*ExprHole) isView() {}

type Attr struct {
	Name  string `json:"name"`
	Value RxExpr `json:"value"`
	Span  Span   `json:"span"`
}

type Handler struct {
	Event string  `json:"event"`
	Body  RxBlock `json:"body"`
	Span  Span    `json:"span"`
}

// RxExpr is the constrained reactive-expression sum type from the spec. M1
// covers only portable variants; per-target/native variants land later.
type RxExpr struct {
	Kind    string   `json:"kind"`
	Literal *Literal `json:"literal,omitempty"`
	Ref     string   `json:"ref,omitempty"`
	BinOp   *BinOp   `json:"binop,omitempty"`
	Call    *Call    `json:"call,omitempty"`
	Span    Span     `json:"span"`
}

type Literal struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

type BinOp struct {
	Op    string `json:"op"`
	Left  RxExpr `json:"left"`
	Right RxExpr `json:"right"`
}

type Call struct {
	Callee string   `json:"callee"`
	Args   []RxExpr `json:"args"`
}

type RxBlock struct {
	Stmts []RxStmt `json:"stmts"`
}

type RxStmt struct {
	Kind   string  `json:"kind"`
	Expr   *RxExpr `json:"expr,omitempty"`
	Target string  `json:"target,omitempty"`
	Value  *RxExpr `json:"value,omitempty"`
}

type SignalDecl struct {
	Name string  `json:"name"`
	Type string  `json:"type"`
	Init *RxExpr `json:"init"`
	Span Span    `json:"span"`
}

type ComputedDecl struct {
	Name string  `json:"name"`
	Type string  `json:"type"`
	Body *RxExpr `json:"body"`
	Span Span    `json:"span"`
}

type Span struct {
	File      string `json:"file"`
	StartByte int    `json:"start_byte"`
	EndByte   int    `json:"end_byte"`
	StartLine int    `json:"start_line"`
	StartCol  int    `json:"start_col"`
	EndLine   int    `json:"end_line"`
	EndCol    int    `json:"end_col"`
}

func decodeViews(raw []json.RawMessage) ([]View, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]View, 0, len(raw))
	for i, item := range raw {
		view, err := decodeView(item)
		if err != nil {
			return nil, fmt.Errorf("children[%d]: %w", i, err)
		}
		out = append(out, view)
	}
	return out, nil
}

func decodeView(data []byte) (View, error) {
	var probe struct {
		Tag   *string         `json:"tag"`
		Value *string         `json:"value"`
		Expr  json.RawMessage `json:"expr"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	switch {
	case probe.Tag != nil:
		var element Element
		if err := json.Unmarshal(data, &element); err != nil {
			return nil, err
		}
		return &element, nil
	case probe.Value != nil:
		var text Text
		if err := json.Unmarshal(data, &text); err != nil {
			return nil, err
		}
		return &text, nil
	case len(probe.Expr) > 0:
		var hole ExprHole
		if err := json.Unmarshal(data, &hole); err != nil {
			return nil, err
		}
		return &hole, nil
	default:
		return nil, fmt.Errorf("unknown view node: %s", string(data))
	}
}
