package speech

// MaxFields is the most fields one Event carries.
const MaxFields = 12

// Field is one named value on an event or in the context. Num holds numbers;
// Str holds names and enums. A field with a non-empty Str binds to pools by
// that string; otherwise a pool whose keys are whole numbers binds by Num.
type Field struct {
	Key string
	Num float64
	Str string
}

// Event is something that happened in the game. It is a plain value with a
// fixed field array, so building and submitting one allocates nothing.
type Event struct {
	// Kind selects the phrases that answer the event.
	Kind string
	// Priority, when non-zero, replaces the phrase's own priority for this
	// event. A game uses it to mark a big play.
	Priority int
	Fields   [MaxFields]Field
	N        int
}

// Num sets a numeric field and returns the event for chaining. Fields past
// MaxFields are ignored.
func (e *Event) Num(key string, v float64) *Event { return e.set(Field{Key: key, Num: v}) }

// Str sets a string field and returns the event for chaining.
func (e *Event) Str(key, v string) *Event { return e.set(Field{Key: key, Str: v}) }

func (e *Event) set(f Field) *Event {
	for i := 0; i < e.N; i++ {
		if e.Fields[i].Key == f.Key {
			e.Fields[i] = f
			return e
		}
	}
	if e.N < MaxFields {
		e.Fields[e.N] = f
		e.N++
	}
	return e
}

// Reset clears the event so it can be reused.
func (e *Event) Reset(kind string) {
	*e = Event{Kind: kind}
}

func (e *Event) find(key string) (*Field, bool) {
	for i := 0; i < e.N; i++ {
		if e.Fields[i].Key == key {
			return &e.Fields[i], true
		}
	}
	return nil, false
}

// Context holds game state that outlasts one event: score difference, streak
// length, weather. Conditions see event fields first, then the context.
type Context struct {
	fields [MaxFields * 2]Field
	n      int
}

// Num sets a numeric context value.
func (c *Context) Num(key string, v float64) { c.put(Field{Key: key, Num: v}) }

// Str sets a string context value.
func (c *Context) Str(key, v string) { c.put(Field{Key: key, Str: v}) }

// Clear removes a context value.
func (c *Context) Clear(key string) {
	for i := 0; i < c.n; i++ {
		if c.fields[i].Key == key {
			c.fields[i] = c.fields[c.n-1]
			c.n--
			c.fields[c.n] = Field{}
			return
		}
	}
}

func (c *Context) put(f Field) {
	for i := 0; i < c.n; i++ {
		if c.fields[i].Key == f.Key {
			c.fields[i] = f
			return
		}
	}
	if c.n < len(c.fields) {
		c.fields[c.n] = f
		c.n++
	}
}

func (c *Context) find(key string) (*Field, bool) {
	for i := 0; i < c.n; i++ {
		if c.fields[i].Key == key {
			return &c.fields[i], true
		}
	}
	return nil, false
}

func lookup(ev *Event, ctx *Context, key string) (*Field, bool) {
	if f, ok := ev.find(key); ok {
		return f, true
	}
	return ctx.find(key)
}

func (k *ccond) holds(ev *Event, ctx *Context) bool {
	f, ok := lookup(ev, ctx, k.field)
	switch k.op {
	case opHas:
		return ok && (f.Str != "" || f.Num != 0)
	case opLacks:
		return !ok || (f.Str == "" && f.Num == 0)
	}
	if !ok {
		return false
	}
	if k.isStr {
		switch k.op {
		case opEq:
			return f.Str == k.str
		case opNe:
			return f.Str != k.str
		}
		return false
	}
	switch k.op {
	case opEq:
		return f.Num == k.num
	case opNe:
		return f.Num != k.num
	case opGt:
		return f.Num > k.num
	case opGe:
		return f.Num >= k.num
	case opLt:
		return f.Num < k.num
	case opLe:
		return f.Num <= k.num
	}
	return false
}

func condsHold(cs []ccond, ev *Event, ctx *Context) bool {
	for i := range cs {
		if !cs[i].holds(ev, ctx) {
			return false
		}
	}
	return true
}
