package speech

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Grammar is the authored, JSON-friendly description of what a commentator
// can say. Compile checks it and resolves it to an immutable Compiled value.
type Grammar struct {
	// Pools are bounded sets of recorded names a slot can bind to, for
	// example a fixed surname list. Keyed by pool name.
	Pools map[string][]PoolEntry `json:"pools,omitempty"`
	// NumberMax is the largest number the {field:number} slot speaks.
	// Default 99; the most is 999.
	NumberMax int `json:"number_max,omitempty"`
	// Categories configures groups of phrases that share a cooldown.
	Categories map[string]Category `json:"categories,omitempty"`
	// Phrases are the things the commentator can say.
	Phrases []Phrase `json:"phrases"`
}

// PoolEntry is one name in a pool. In JSON it is either a bare string (the
// key and the spoken text are the same) or {"key": "...", "text": "..."}.
type PoolEntry struct {
	Key  string `json:"key"`
	Text string `json:"text,omitempty"`
}

// UnmarshalJSON accepts a string or an object.
func (e *PoolEntry) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		e.Key, e.Text = s, ""
		return nil
	}
	type plain PoolEntry
	return json.Unmarshal(b, (*plain)(e))
}

// Category groups phrases that should not repeat too soon.
type Category struct {
	// Cooldown is the least time, in seconds, between two utterances of the
	// category. Zero means no limit.
	Cooldown float64 `json:"cooldown,omitempty"`
}

// Phrase answers one event kind. It has one or more spoken variants.
type Phrase struct {
	ID string `json:"id"`
	// Event is the Event.Kind this phrase answers.
	Event string `json:"event"`
	// Category shares a cooldown with other phrases. Optional.
	Category string `json:"category,omitempty"`
	// Priority ranks the utterance against what is already playing.
	Priority int `json:"priority,omitempty"`
	// Uninterruptible stops a higher-priority event from cutting this phrase.
	Uninterruptible bool `json:"uninterruptible,omitempty"`
	// Cooldown is the least time, in seconds, before this phrase repeats.
	Cooldown float64 `json:"cooldown,omitempty"`
	// MaxWait lets the phrase wait in the queue up to this many seconds when
	// the voice is busy. Zero drops it instead.
	MaxWait float64 `json:"max_wait,omitempty"`
	// Weight is the phrase's share when several phrases match. Default 1.
	Weight float64 `json:"weight,omitempty"`
	// When lists conditions on the event and context; all must hold.
	When []Condition `json:"when,omitempty"`
	// Variants are the alternative wordings.
	Variants []Variant `json:"variants"`
}

// Variant is one wording of a phrase.
type Variant struct {
	// Say is the template: literal text, {field} or {field:pool} slots,
	// {field:number} for numbers, {field?} for an optional slot that is
	// skipped when unbound, and {~150} for a 150 ms pause.
	Say    string      `json:"say"`
	Weight float64     `json:"weight,omitempty"`
	When   []Condition `json:"when,omitempty"`
}

// Condition tests an event field or a context value. Op is one of ==, !=,
// >, >=, <, <=, has (the field exists and is not empty) or lacks. Value is a
// number or a string.
type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value,omitempty"`
}

// ParseGrammar decodes JSON into a Grammar, rejecting unknown fields.
func ParseGrammar(data []byte) (*Grammar, error) {
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var g Grammar
	if err := dec.Decode(&g); err != nil {
		return nil, fmt.Errorf("speech: grammar: %w", err)
	}
	return &g, nil
}

type condOp uint8

const (
	opEq condOp = iota
	opNe
	opGt
	opGe
	opLt
	opLe
	opHas
	opLacks
)

type ccond struct {
	field string
	op    condOp
	num   float64
	str   string
	isStr bool
}

type partKind uint8

const (
	partLit partKind = iota
	partPool
	partNumber
	partPause
)

type part struct {
	kind     partKind
	frag     FragID
	field    string
	optional bool
	pool     *cpool
	pauseMs  int16
}

type cpool struct {
	name  string
	byStr map[string]FragID
	byNum map[int]FragID
}

type cvariant struct {
	weight float64
	conds  []ccond
	parts  []part
}

type cphrase struct {
	id            string
	cat           int // index into Compiled.cats, -1 for none
	priority      int
	interruptible bool
	cooldown      float64
	maxWait       float64
	weight        float64
	conds         []ccond
	variants      []cvariant
}

// Compiled is a checked, resolved grammar. It is immutable after Compile, so
// any number of Planners can share it.
type Compiled struct {
	lib      *Library
	phrases  []cphrase
	byKind   map[string][]int32
	catNames []string
	catCool  []float64
	numMax   int
	numUnits [20]FragID
	numTens  [10]FragID
	numHund  FragID
	frags    []Fragment
	maxCand  int
}

// Fragment is one recording the grammar needs: the key the runtime looks up
// and the text a voice should speak.
type Fragment struct {
	Key  string `json:"key"`
	Text string `json:"text"`
}

var unitWords = [20]string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
	"eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}
var tensWords = [10]string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}

// Compile validates g and interns every fragment it needs into lib. The
// library then lists which keys still lack recordings (Library.Missing).
func (g *Grammar) Compile(lib *Library) (*Compiled, error) {
	c := &Compiled{lib: lib, byKind: map[string][]int32{}, numMax: g.NumberMax}
	if c.numMax == 0 {
		c.numMax = 99
	}
	if c.numMax < 0 || c.numMax > 999 {
		return nil, fmt.Errorf("speech: number_max %d outside 0..999", g.NumberMax)
	}
	seen := map[string]bool{}
	add := func(key, text string) FragID {
		id := lib.Intern(key)
		if !seen[key] {
			seen[key] = true
			c.frags = append(c.frags, Fragment{Key: key, Text: text})
		}
		return id
	}

	// Pools, in sorted name order so output is stable.
	names := make([]string, 0, len(g.Pools))
	for n := range g.Pools {
		names = append(names, n)
	}
	sort.Strings(names)
	pools := map[string]*cpool{}
	for _, n := range names {
		p := &cpool{name: n, byStr: map[string]FragID{}, byNum: map[int]FragID{}}
		for _, e := range g.Pools[n] {
			if e.Key == "" {
				return nil, fmt.Errorf("speech: pool %q has an entry with an empty key", n)
			}
			text := e.Text
			if text == "" {
				text = e.Key
			}
			if _, dup := p.byStr[e.Key]; dup {
				return nil, fmt.Errorf("speech: pool %q repeats key %q", n, e.Key)
			}
			id := add("pool/"+n+"/"+e.Key, text)
			p.byStr[e.Key] = id
			var num int
			if _, err := fmt.Sscanf(e.Key, "%d", &num); err == nil && fmt.Sprint(num) == e.Key {
				p.byNum[num] = id
			}
		}
		pools[n] = p
	}

	// Category indexes, sorted.
	for n := range g.Categories {
		c.catNames = append(c.catNames, n)
	}
	sort.Strings(c.catNames)
	catIdx := map[string]int{}
	for i, n := range c.catNames {
		catIdx[n] = i
		c.catCool = append(c.catCool, g.Categories[n].Cooldown)
	}

	usesNumber := false
	ids := map[string]bool{}
	for pi := range g.Phrases {
		ph := &g.Phrases[pi]
		if ph.ID == "" || ph.Event == "" {
			return nil, fmt.Errorf("speech: phrase %d needs an id and an event", pi)
		}
		if ids[ph.ID] {
			return nil, fmt.Errorf("speech: duplicate phrase id %q", ph.ID)
		}
		ids[ph.ID] = true
		if len(ph.Variants) == 0 {
			return nil, fmt.Errorf("speech: phrase %q has no variants", ph.ID)
		}
		cp := cphrase{id: ph.ID, cat: -1, priority: ph.Priority, interruptible: !ph.Uninterruptible,
			cooldown: ph.Cooldown, maxWait: ph.MaxWait, weight: ph.Weight}
		if cp.weight == 0 {
			cp.weight = 1
		}
		if cp.weight < 0 {
			return nil, fmt.Errorf("speech: phrase %q has a negative weight", ph.ID)
		}
		if ph.Category != "" {
			i, ok := catIdx[ph.Category]
			if !ok {
				// An undeclared category is allowed and has no cooldown.
				c.catNames = append(c.catNames, ph.Category)
				c.catCool = append(c.catCool, 0)
				i = len(c.catNames) - 1
				catIdx[ph.Category] = i
			}
			cp.cat = i
		}
		var err error
		if cp.conds, err = compileConds(ph.When); err != nil {
			return nil, fmt.Errorf("speech: phrase %q: %w", ph.ID, err)
		}
		for vi, v := range ph.Variants {
			cv := cvariant{weight: v.Weight}
			if cv.weight == 0 {
				cv.weight = 1
			}
			if cv.weight < 0 {
				return nil, fmt.Errorf("speech: phrase %q variant %d has a negative weight", ph.ID, vi)
			}
			if cv.conds, err = compileConds(v.When); err != nil {
				return nil, fmt.Errorf("speech: phrase %q variant %d: %w", ph.ID, vi, err)
			}
			parts, nums, err := parseTemplate(v.Say, pools, add)
			if err != nil {
				return nil, fmt.Errorf("speech: phrase %q variant %d: %w", ph.ID, vi, err)
			}
			usesNumber = usesNumber || nums
			if len(parts) == 0 {
				return nil, fmt.Errorf("speech: phrase %q variant %d is empty", ph.ID, vi)
			}
			if len(parts) > MaxFragments {
				return nil, fmt.Errorf("speech: phrase %q variant %d has %d parts; the limit is %d", ph.ID, vi, len(parts), MaxFragments)
			}
			cv.parts = parts
			cp.variants = append(cp.variants, cv)
		}
		c.byKind[ph.Event] = append(c.byKind[ph.Event], int32(len(c.phrases)))
		c.phrases = append(c.phrases, cp)
	}
	for _, list := range c.byKind {
		n := 0
		for _, pi := range list {
			n += len(c.phrases[pi].variants)
		}
		if n > c.maxCand {
			c.maxCand = n
		}
	}
	if usesNumber {
		for i := 0; i < 20 && i <= c.numMax; i++ {
			c.numUnits[i] = add("num/"+unitWords[i], unitWords[i])
		}
		for t := 2; t <= 9 && t*10 <= c.numMax; t++ {
			c.numTens[t] = add("num/"+tensWords[t], tensWords[t])
		}
		if c.numMax >= 100 {
			c.numHund = add("num/hundred", "hundred")
		}
	}
	// A number spoken as digits under 100 needs MaxFragments room: 3 parts max.
	return c, nil
}

// MaxFragments is the most fragments one utterance can hold.
const MaxFragments = 32

// Fragments lists every recording the grammar needs, in a stable order.
func (c *Compiled) Fragments() []Fragment { return append([]Fragment(nil), c.frags...) }

// Library returns the library the grammar was compiled against.
func (c *Compiled) Library() *Library { return c.lib }

// PhraseID returns the authored ID of phrase index i.
func (c *Compiled) PhraseID(i int) string {
	if i < 0 || i >= len(c.phrases) {
		return ""
	}
	return c.phrases[i].id
}

func compileConds(in []Condition) ([]ccond, error) {
	out := make([]ccond, 0, len(in))
	for _, k := range in {
		if k.Field == "" {
			return nil, fmt.Errorf("condition needs a field")
		}
		cc := ccond{field: k.Field}
		switch k.Op {
		case "==", "=":
			cc.op = opEq
		case "!=":
			cc.op = opNe
		case ">":
			cc.op = opGt
		case ">=":
			cc.op = opGe
		case "<":
			cc.op = opLt
		case "<=":
			cc.op = opLe
		case "has":
			cc.op = opHas
		case "lacks":
			cc.op = opLacks
		default:
			return nil, fmt.Errorf("condition on %q has unknown op %q", k.Field, k.Op)
		}
		switch v := k.Value.(type) {
		case nil:
			if cc.op != opHas && cc.op != opLacks {
				return nil, fmt.Errorf("condition on %q needs a value", k.Field)
			}
		case string:
			cc.str, cc.isStr = v, true
			if cc.op != opEq && cc.op != opNe && cc.op != opHas && cc.op != opLacks {
				return nil, fmt.Errorf("condition on %q: op %q does not compare strings", k.Field, k.Op)
			}
		case float64:
			cc.num = v
		case int:
			cc.num = float64(v)
		case bool:
			if v {
				cc.num = 1
			}
		default:
			return nil, fmt.Errorf("condition on %q has an unsupported value %v", k.Field, k.Value)
		}
		out = append(out, cc)
	}
	return out, nil
}

// parseTemplate splits a template into parts, interning literals as it goes.
func parseTemplate(s string, pools map[string]*cpool, add func(key, text string) FragID) (parts []part, usesNumber bool, err error) {
	lit := func(text string) {
		text = strings.Join(strings.Fields(text), " ")
		if text != "" {
			parts = append(parts, part{kind: partLit, frag: add("lit/"+text, text)})
		}
	}
	for len(s) > 0 {
		open := strings.IndexByte(s, '{')
		if open < 0 {
			lit(s)
			break
		}
		lit(s[:open])
		end := strings.IndexByte(s[open:], '}')
		if end < 0 {
			return nil, false, fmt.Errorf("unclosed { in %q", s)
		}
		tok := s[open+1 : open+end]
		s = s[open+end+1:]
		if strings.HasPrefix(tok, "~") {
			var ms int
			if _, e := fmt.Sscanf(tok[1:], "%d", &ms); e != nil || ms < 0 || ms > 5000 {
				return nil, false, fmt.Errorf("pause %q must be 0..5000 ms", tok)
			}
			parts = append(parts, part{kind: partPause, pauseMs: int16(ms)})
			continue
		}
		p := part{}
		if strings.HasSuffix(tok, "?") {
			p.optional = true
			tok = strings.TrimSuffix(tok, "?")
		}
		field, kind, _ := strings.Cut(tok, ":")
		field = strings.TrimSpace(field)
		kind = strings.TrimSpace(kind)
		if field == "" {
			return nil, false, fmt.Errorf("empty slot in template")
		}
		if kind == "" {
			kind = field
		}
		p.field = field
		if kind == "number" {
			p.kind = partNumber
			usesNumber = true
		} else {
			pool, ok := pools[kind]
			if !ok {
				return nil, false, fmt.Errorf("slot {%s} uses unknown pool %q", tok, kind)
			}
			p.kind, p.pool = partPool, pool
		}
		parts = append(parts, p)
	}
	return parts, usesNumber, nil
}
