package wire

import "m31labs.dev/gosx/internal/pagecaps"

// Only these reasons can preserve complete coverage when input is omitted.
// Zero and every unrecognised reason fail closed in drop.
type referenceDropReason uint8

const (
	dropUnresolved            referenceDropReason = iota
	dropInertHTML                                 // known metadata attributes, ordinary text/comments
	dropInertDataScript                           // body and src of a recognised non-executing data type
	dropExternalScriptBody                        // src is present; the browser ignores the inline body
	dropTemplateContent                           // ordinary HTML template contents or presentation
	dropOpaqueData                                // matching image/font MIME in a non-executable context
	dropCSSFragment                               // empty/fragment CSS URL, never a fetch/module target
	dropDormantManifest                           // a runtime, program or bundle not selected by its gate
	dropManifestMetadata                          // explicitly classified schema metadata, never opaque loader config
	dropNonLoadingSyntax                          // syntax with no loader of its own; children still scanned
	dropNestedScan                                // the same source is handled by a descendant scan
	dropDuplicateReference                        // another reference already preserves this URL and kind
	dropSandboxedExecutable                       // inherited sandbox permission blocks executable sources
	dropOverriddenFrameSource                     // a present srcdoc replaces iframe src, including empty srcdoc
	dropInvalidManifest                           // the loader catches invalid JSON on its first ID candidate
	dropEmptySyntax                               // no CSS/JavaScript instructions to scan
)

// ReferenceSet keeps its public shape; omission tracing is scanner state only.
type referenceScanner struct {
	ReferenceSet
	onDrop                                func(referenceDropReason)
	document                              *pagecaps.Document
	scriptsBlocked                        bool
	srcdocDepth, srcdocCount, srcdocBytes int
}

func (out *referenceScanner) drop(reason referenceDropReason) {
	switch reason {
	case dropInertHTML, dropInertDataScript, dropExternalScriptBody,
		dropTemplateContent, dropOpaqueData, dropCSSFragment, dropDormantManifest, dropManifestMetadata,
		dropNonLoadingSyntax, dropNestedScan, dropDuplicateReference, dropSandboxedExecutable, dropOverriddenFrameSource, dropInvalidManifest, dropEmptySyntax:
	default:
		out.Complete = false
	}
	if out.onDrop != nil {
		out.onDrop(reason)
	}
}
