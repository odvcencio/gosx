package wasm

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"

	"m31labs.dev/gosx/engine"
)

// Reply is the answer to one call made with a Caller.
type Reply struct {
	// Tag is the tag the call was made with.
	Tag string
	// Value is the JSON of the handler's result. It is nil when Err is set.
	Value []byte
	// Err is empty on success. Otherwise it is the handler's error text or one
	// of the codes the page and the client write: "timeout", "worker-crashed",
	// "reply-lost" (the reply fell out of Result.Recent before the client read
	// it), "no-writer", "bad-call:..." and "bad-args:...".
	Err string
}

// Decode unmarshals the reply's value into v. It returns the reply's error
// instead when the call failed.
func (r Reply) Decode(v any) error {
	if r.Err != "" {
		return errors.New(r.Err)
	}
	if len(r.Value) == 0 {
		return nil // a reply with no value leaves v as it is
	}
	return json.Unmarshal(r.Value, v)
}

// DecodeReply returns the reply's value as an R, or the reply's error.
func DecodeReply[R any](r Reply) (R, error) {
	var v R
	err := r.Decode(&v)
	return v, err
}

// CallerOptions tunes NewCallerWith.
type CallerOptions struct {
	// Window is how many calls the client keeps written and unanswered at once.
	// Zero or one keeps one, which works against any engine: the next call is
	// written when the reply to the last arrives. A larger window writes the
	// calls as a mailbox, an array of every unanswered call, so that they all
	// reach the engine together and wait there instead of in the client. The
	// engine must be built with a GoSX that serves mailboxes (host.ServeCalls
	// from this version or later); an older engine ignores an array.
	Window int
	// After is the ID of the last call the engine has handled. The client
	// numbers its calls from After+1, because the engine ignores any ID at or
	// below the last one it handled. Zero starts at 1, which is right when the
	// client and the engine start together. When the engine can outlive the
	// client (the client's own engine remounts), pass the ID in the current
	// value of the result signal.
	After uint64
}

// Caller is the client of one call channel (see ServeCalls): the page-side
// half that numbers calls, writes them to "$channel", reads the replies from
// "$channel.result", and gives each reply back with the tag of its call.
//
// A Caller does no I/O of its own. The code that owns the signals feeds it:
// give NewCaller the function that writes a shared signal, call Call to make a
// call, and pass every value of ResultSignal() to Result. Both return the
// replies they completed, so the caller decides when and where to run the
// code that follows a reply. This keeps Caller free of any browser type and
// lets it run in a native test.
//
// Calls are queued, sent in order, and answered in order. By default one call
// is written at a time and the rest wait in the client (see CallerOptions.Window
// for more). A write that fails is answered at once with its error and the
// queue moves on, so a failure never leaves the channel blocked. A result that
// is newer than a call (its reply left the engine's Recent list, or the page
// wrote a timeout for a later call) completes the call from Result.Recent
// when the reply is still there, and as "reply-lost" when it is not.
//
// A Caller is safe for concurrent use. It never holds its lock while it calls
// the write function, because a write to a shared signal notifies the
// engine's own subscribers in the same call stack, and one of them may call
// Result before the write returns.
type Caller struct {
	channel string
	write   func(signal string, v any) error
	window  int

	mu      sync.Mutex
	next    uint64
	flight  []sentCall // written and unanswered, in ID order
	queue   []queuedCall
	writing bool // a stack is inside write; it also sends what the others queue
}

type queuedCall struct {
	name string
	args json.RawMessage
	tag  string
}

type sentCall struct {
	call engine.Call[json.RawMessage]
	tag  string
}

// wireResult is engine.Result with the value left as JSON.
type wireResult = engine.Result[json.RawMessage]

// NewCaller returns a client for channel that keeps one call written at a time.
// write sets a shared signal; it is ctx.SetSignal for an engine context.
func NewCaller(channel string, write func(signal string, v any) error) *Caller {
	return NewCallerWith(channel, write, CallerOptions{})
}

// NewCallerWith is NewCaller with options.
func NewCallerWith(channel string, write func(signal string, v any) error, opts CallerOptions) *Caller {
	if opts.Window < 1 {
		opts.Window = 1
	}
	return &Caller{channel: strings.TrimPrefix(strings.TrimSpace(channel), "$"), write: write, window: opts.Window, next: opts.After}
}

// CallSignal is the signal the client writes calls to.
func (c *Caller) CallSignal() string { return "$" + c.channel }

// ResultSignal is the signal to subscribe to for replies.
func (c *Caller) ResultSignal() string { return "$" + c.channel + ".result" }

// Pending is the number of calls waiting, including those written and not yet
// answered.
func (c *Caller) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.queue) + len(c.flight)
}

// Call queues a call. args is marshalled with encoding/json, so the contract's
// json tags apply. A call the page cannot take (the signal write fails) is
// answered at once with that error, in the returned replies, and the queue
// moves on; it never leaves the channel blocked.
func (c *Caller) Call(name string, args any, tag string) ([]Reply, error) {
	encoded, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.queue = append(c.queue, queuedCall{name: name, args: encoded, tag: tag})
	c.mu.Unlock()
	return c.pump(), nil
}

// pump writes the queued calls that fit the window. Writes that fail are
// answered with their error. One stack writes at a time, so the signal never
// sees an older list after a newer one; a stack that finds another writing
// returns, and the writer sends what it queued before it leaves.
func (c *Caller) pump() []Reply {
	var failed []Reply
	for {
		c.mu.Lock()
		if c.writing {
			c.mu.Unlock()
			return failed
		}
		var sent []sentCall
		for len(c.flight) < c.window && len(c.queue) > 0 {
			q := c.queue[0]
			c.queue = c.queue[1:]
			if c.write == nil {
				failed = append(failed, Reply{Tag: q.tag, Err: "no-writer"})
				continue
			}
			c.next++
			s := sentCall{call: engine.Call[json.RawMessage]{ID: c.next, Name: q.name, Args: q.args}, tag: q.tag}
			c.flight = append(c.flight, s)
			sent = append(sent, s)
		}
		if len(sent) == 0 {
			c.mu.Unlock()
			return failed
		}
		var payload any = sent[len(sent)-1].call
		if c.window > 1 {
			mailbox := make([]engine.Call[json.RawMessage], len(c.flight))
			for i, s := range c.flight {
				mailbox[i] = s.call
			}
			payload = mailbox
		}
		c.writing = true
		c.mu.Unlock()

		// The write may deliver a result into Result before it returns.
		err := c.write(c.CallSignal(), payload)

		c.mu.Lock()
		c.writing = false
		if err != nil {
			for _, s := range sent {
				if c.dropLocked(s.call.ID) {
					failed = append(failed, Reply{Tag: s.tag, Err: err.Error()})
				}
			}
		}
		c.mu.Unlock()
	}
}

// dropLocked removes the call with id from the written calls, if it is there.
func (c *Caller) dropLocked(id uint64) bool {
	for i, s := range c.flight {
		if s.call.ID == id {
			c.flight = append(c.flight[:i:i], c.flight[i+1:]...)
			return true
		}
	}
	return false
}

// Result takes the value of the result signal. It returns the replies it
// completes, in call order, and writes the next queued calls. A malformed
// value is ignored, and so is a result older than every written call. A result
// for a newer call than one written call completes that call from
// Result.Recent, or counts it as lost when the reply is no longer there.
func (c *Caller) Result(raw []byte) []Reply {
	var r wireResult
	if json.Unmarshal(raw, &r) != nil {
		return nil
	}
	c.mu.Lock()
	if len(c.flight) == 0 || r.ID < c.flight[0].call.ID {
		c.mu.Unlock()
		return nil
	}
	var replies []Reply
	done := 0
	for done < len(c.flight) && c.flight[done].call.ID <= r.ID {
		replies = append(replies, replyFor(c.flight[done], r))
		done++
	}
	c.flight = append([]sentCall(nil), c.flight[done:]...)
	c.mu.Unlock()
	return append(replies, c.pump()...)
}

// replyFor builds the reply to s from result r, where r.ID >= s's ID.
func replyFor(s sentCall, r wireResult) Reply {
	reply := Reply{Tag: s.tag}
	if r.ID == s.call.ID {
		reply.Err = r.Error
		if r.Error == "" {
			reply.Value = r.Value
		}
		return reply
	}
	reply.Err = "reply-lost"
	for _, ref := range r.Recent {
		if ref.ID != s.call.ID {
			continue
		}
		switch {
		case ref.Error != "":
			reply.Err = ref.Error
		case ref.Value != nil:
			reply.Err, reply.Value = "", ref.Value
		}
	}
	return reply
}

// Fail answers every call that is written or queued with err, for a crashed
// worker.
func (c *Caller) Fail(err string) []Reply {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Reply
	for _, s := range c.flight {
		out = append(out, Reply{Tag: s.tag, Err: err})
	}
	for _, q := range c.queue {
		out = append(out, Reply{Tag: q.tag, Err: err})
	}
	c.flight, c.queue = nil, nil
	return out
}
