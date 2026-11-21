package focus

import "testing"

// attrs is a control's attributes, for KeyOf.
type attrs map[string]string

func (a attrs) Attr(name string) string  { return a[name] }
func (a attrs) HasAttr(name string) bool { _, ok := a[name]; return ok }

func TestKeyOf(t *testing.T) {
	for _, c := range []struct {
		el   attrs
		want string
	}{
		{attrs{"id": "film-play", "data-control": "play"}, `[id="film-play"]`},
		{attrs{"data-control": "snap", "data-call": "12"}, `[data-control="snap"]`},
		{attrs{"data-call": "12", "data-group": "pistol"}, `[data-call="12"]`},
		{attrs{"data-group": "pistol", "data-do": "close"}, `[data-group="pistol"]`},
		{attrs{"data-do": "close"}, `[data-do="close"]`},
		// An empty id names nothing; an empty data value is still a name.
		{attrs{"id": "", "data-control": ""}, `[data-control=""]`},
		{attrs{"data-call": `say "hut" \ twice`}, `[data-call="say \"hut\" \\ twice"]`},
		{attrs{"data-do": "two\nlines"}, `[data-do="two\a lines"]`},
		{attrs{"class": "primary"}, ""},
		{attrs{}, ""},
	} {
		if got := KeyOf(c.el); got != c.want {
			t.Errorf("KeyOf(%v) = %q, want %q", c.el, got, c.want)
		}
	}
}
