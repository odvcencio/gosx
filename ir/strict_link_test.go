package ir_test

import (
	"strings"
	"testing"
)

func TestStrictManagedLinkAttributes(t *testing.T) {
	for _, tc := range []struct{ attrs, want string }{
		{`href={props.Href} style="color:red" role="link" lang="en" dir="ltr" accesskey="d" tabindex={props.Index} hidden={props.Hidden} download prefetch={props.Hidden} data-index={props.Index} aria-label={props.Label}`, ""},
		{`href={props.Href + "?x=1"}`, ""},
		{`href={(props).Href}`, ""},
		{`href={(props).Hidden}`, `attribute "href" requires string, got bool`},
		{`href={nil}`, `not renderable`},
		{`href={-props.Index}`, `not renderable`},
		{`href={props.Index}`, `attribute "href" requires string, got int`},
		{`href`, `attribute "href" requires string, got bool`},
		{`href="/" hidden="yes"`, `attribute "hidden" requires bool, got string`},
		{`href="/" style={props.Index}`, `attribute "style" requires string, got int`},
		{`href="/" current={props.Hidden}`, `attribute "current" requires string, got bool`},
		{`href="/" tabindex={props.Hidden}`, `attribute "tabindex" requires string|int, got bool`},
		{`class="nav"`, `requires a string href`},
		{`href="/" class="a" className="b"`, `duplicate attribute "class"`},
		{`href="/" onclick="alert(1)"`, `does not support attribute "onclick"`},
		{`href="/" ariaCurrent="page"`, `does not support attribute "ariaCurrent"`},
		{`href="/" tabIndex="0"`, `does not support attribute "tabIndex"`},
		{`href="/" {...props}`, `does not accept spread`},
	} {
		t.Run(tc.attrs, func(t *testing.T) {
			source := []byte("package app\ntype Props struct { Href string; Label string; Index int; Hidden bool }\ncomponent Page(props: Props) {\n return <Link " + tc.attrs + ">{props.Label}</Link>\n}\n")
			_, err := parse(t, source)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "4:") {
				t.Fatalf("got %v want source diagnostic %s", err, tc.want)
			}
		})
	}
}

func TestStrictManagedLinkLoopAndShadow(t *testing.T) {
	for _, source := range []string{
		"package app\ntype Row struct { Href string }\ntype Props struct { Rows []Row }\ncomponent Page(props: Props) {\n return <Each of={props.Rows} as=\"row\" index=\"i\"><Link href={row.Href} data-index={i}/></Each>\n}\n",
		"package app\ncomponent Page() {\n return <Link/>\n}\ncomponent Link() {\n return <strong>local</strong>\n}\n",
	} {
		if _, err := parse(t, []byte(source)); err != nil {
			t.Fatal(err)
		}
	}
	bad := "package app\ntype Row struct { Href int }\ntype Props struct { Rows []Row }\ncomponent Page(props: Props) {\n return <Each of={props.Rows} as=\"row\"><Link href={row.Href}/></Each>\n}\n"
	if _, err := parse(t, []byte(bad)); err == nil || !strings.Contains(err.Error(), `attribute "href" requires string, got int`) {
		t.Fatal(err)
	}
}

func TestStrictManagedLinkSlotValidation(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`<Panel><Link slot="Header" href={props.Index}/></Panel>`, `attribute "href" requires string, got int`},
		{`<Panel><span slot="Header">{compute()}</span></Panel>`, `not renderable`},
		{`<Each of={props.Rows} as="row"><Panel><Link slot="Header" href={row.Href}/></Panel></Each>`, ""},
		{`<main><Each of={props.Rows} as="row"><span>{row.Href}</span></Each><Panel><Link slot="Header" href={row.Href}/></Panel></main>`, `not renderable`},
	} {
		t.Run(tc.body, func(t *testing.T) {
			source := "package app\ntype Row struct{Href string}\ntype Props struct{Index int; Rows []Row}\ncomponent Page(props: Props) {\n return " + tc.body + "\n}\ncomponent Panel() {\n return <header>{slotHeader}</header>\n}\n"
			_, err := parse(t, []byte(source))
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v want %s", err, tc.want)
			}
		})
	}
}
