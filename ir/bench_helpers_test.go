package ir_test

import "github.com/odvcencio/gosx/ir"

// benchCounterSource is the small island fixture used by Lower benchmarks.
const benchCounterSource = `package counter

//gosx:island
func Counter(props CounterProps) Node {
	return <div class="counter">
		<button onClick={decrement}>-</button>
		<span class="count">{count}</span>
		<button onClick={increment}>+</button>
	</div>
}
`

// benchFormSource is a slightly larger island that exercises more attributes,
// nested elements, and several handlers.
const benchFormSource = `package form

//gosx:island
func ContactForm(props FormProps) Node {
	return <form class="contact-form" onSubmit={handleSubmit}>
		<div class="field">
			<label for="name">Name</label>
			<input id="name" type="text" value={name} onInput={updateName} />
		</div>
		<div class="field">
			<label for="email">Email</label>
			<input id="email" type="email" value={email} onInput={updateEmail} />
		</div>
		<div class="field">
			<label for="message">Message</label>
			<textarea id="message" value={message} onInput={updateMessage}></textarea>
		</div>
		<div class="actions">
			<button type="reset" onClick={reset}>Reset</button>
			<button type="submit" disabled={!valid}>Send</button>
		</div>
	</form>
}
`

// benchExprScope returns a scope populated with names that the
// "parse expr complex" benchmark expression references, so resolution
// hits the same hot paths a real island parse would.
func benchExprScope() *ir.ExprScope {
	return &ir.ExprScope{
		Signals:       map[string]bool{"count": true, "items": true},
		SignalAliases: map[string]string{},
		Props:         map[string]bool{"user": true},
		Handlers:      map[string]bool{},
		EventFields:   map[string]bool{},
	}
}
