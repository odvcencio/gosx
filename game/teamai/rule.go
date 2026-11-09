package teamai

// ruleBinder gives every rule and chooser name a Book index while the Spec
// compiles, and remembers where each name was used so an unknown name is
// reported at the entry that used it.
type ruleBinder struct {
	preds    []string
	predRefs []ruleUse
	choosers []string
	chRefs   []ruleUse
}

type ruleUse struct {
	path string
	name string
}

func (rb *ruleBinder) predicate(is *issues, path, name string) int16 {
	rb.predRefs = append(rb.predRefs, ruleUse{path: path, name: name})
	if i := indexOf(rb.preds, name); i >= 0 {
		return int16(i)
	}
	rb.preds = append(rb.preds, name)
	return int16(len(rb.preds) - 1)
}

func (rb *ruleBinder) chooser(is *issues, path, name string) int16 {
	rb.chRefs = append(rb.chRefs, ruleUse{path: path, name: name})
	if i := indexOf(rb.choosers, name); i >= 0 {
		return int16(i)
	}
	rb.choosers = append(rb.choosers, name)
	return int16(len(rb.choosers) - 1)
}
