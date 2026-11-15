package speech

import "testing"

// racingGrammar is a small synthetic racing announcer used across the tests.
const racingGrammar = `{
  "pools": {
    "driver": ["okafor", "vance", {"key": "reyes", "text": "Reyes"}, "lindqvist"],
    "corner": ["one", "hairpin", "chicane"]
  },
  "number_max": 99,
  "categories": {
    "chatter": {"cooldown": 4},
    "call": {"cooldown": 0}
  },
  "phrases": [
    {"id": "lead.change", "event": "lead_change", "category": "call", "priority": 5, "cooldown": 3, "max_wait": 3,
     "variants": [
       {"say": "{driver} takes the lead", "weight": 3},
       {"say": "and it is {driver} out front now", "weight": 1},
       {"say": "{num:number} takes the lead", "when": [{"field": "driver", "op": "lacks"}]}
     ]},
    {"id": "lap.call", "event": "lap", "category": "chatter", "priority": 1, "cooldown": 8,
     "variants": [
       {"say": "lap {lap:number} of {total:number}"},
       {"say": "that is {~200} lap {lap:number}", "when": [{"field": "lap", "op": ">=", "value": 10}]}
     ]},
    {"id": "crash", "event": "crash", "category": "call", "priority": 9, "uninterruptible": true,
     "variants": [{"say": "oh no! {driver} is in the wall at the {corner}"}, {"say": "{driver} has crashed {corner?}"}]},
    {"id": "gap.close", "event": "gap", "category": "chatter", "priority": 2,
     "when": [{"field": "gap", "op": "<", "value": 1.5}],
     "variants": [{"say": "{driver} is closing in"}]},
    {"id": "gap.far", "event": "gap", "category": "chatter", "priority": 2,
     "when": [{"field": "gap", "op": ">=", "value": 1.5}],
     "variants": [{"say": "{driver} is pulling away"}]}
  ]
}`

func compileRacing(t testing.TB) (*Compiled, *Library) {
	t.Helper()
	g, err := ParseGrammar([]byte(racingGrammar))
	if err != nil {
		t.Fatal(err)
	}
	lib := NewLibrary(48000)
	c, err := g.Compile(lib)
	if err != nil {
		t.Fatal(err)
	}
	return c, lib
}

// say returns the fragment keys of u joined by "|".
func say(lib *Library, u *Utterance) string {
	s := ""
	for i := 0; i < u.N; i++ {
		if i > 0 {
			s += "|"
		}
		s += lib.Key(u.Frags[i].ID)
	}
	return s
}
