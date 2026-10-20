// Package embeddingtest describes browser embedding capabilities independently
// of the document walker, for its parser, reference and budget tests.
package embeddingtest

type Element struct {
	Name, Spec                     string
	Sandbox, Srcdoc, Src, Frameset bool
}

// Elements is the single test oracle for the embeddings pagecaps walks.
// HTML 4.8.5 defines iframe's src, srcdoc and sandbox processing; HTML 16.3.2
// defines frame's src processing and frameset context, without sandbox/srcdoc.
var Elements = []Element{
	{Name: "iframe", Sandbox: true, Srcdoc: true, Src: true,
		Spec: "https://html.spec.whatwg.org/multipage/iframe-embed-object.html#the-iframe-element"},
	{Name: "frame", Sandbox: false, Srcdoc: false, Src: true, Frameset: true,
		Spec: "https://html.spec.whatwg.org/multipage/obsolete.html#frames"},
}

func Lookup(name string) Element {
	for _, element := range Elements {
		if element.Name == name {
			return element
		}
	}
	panic("embedding needs a spec-backed test oracle: " + name)
}

func (element Element) Markup(attributes string) string {
	body := "<" + element.Name + " " + attributes + ">"
	if !element.Frameset {
		body += "</" + element.Name + ">"
	}
	return body
}

func (element Element) Document(markup string) string {
	if element.Frameset {
		return "<!doctype html><html><head></head><frameset>" + markup + "</frameset></html>"
	}
	return markup
}
