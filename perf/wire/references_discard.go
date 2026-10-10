package wire

// Only these reasons can preserve complete coverage when input is omitted.
// Zero and every unrecognised reason fail closed in drop.
type referenceDropReason uint8

const (
	dropUnresolved         referenceDropReason = iota
	dropInertHTML                              // known metadata attributes, ordinary text/comments
	dropInertDataScript                        // body and src of a recognised non-executing data type
	dropExternalScriptBody                     // src is present; the browser ignores the inline body
	dropTemplateContent                        // ordinary HTML template contents or presentation
	dropOpaqueData                             // matching image/font MIME in a non-executable context
	dropCSSFragment                            // empty/fragment CSS URL, never a fetch/module target
	dropDormantManifest                        // a runtime, program or bundle not selected by its gate
	dropManifestMetadata                       // known schema data outside the modelled fetch selectors
	dropNonLoadingSyntax                       // syntax with no loader of its own; children still scanned
	dropNestedScan                             // the same source is handled by a descendant scan
	dropDuplicateReference                     // another reference already preserves this URL and kind
	dropEmptySyntax                            // no CSS/JavaScript instructions to scan
)

func (out *ReferenceSet) drop(reason referenceDropReason) {
	switch reason {
	case dropInertHTML, dropInertDataScript, dropExternalScriptBody,
		dropTemplateContent, dropOpaqueData, dropCSSFragment, dropDormantManifest, dropManifestMetadata,
		dropNonLoadingSyntax, dropNestedScan, dropDuplicateReference, dropEmptySyntax:
	default:
		out.Complete = false
	}
	if out.onDrop != nil {
		out.onDrop(reason)
	}
}
