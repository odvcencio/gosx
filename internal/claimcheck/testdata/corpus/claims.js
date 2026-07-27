// JavaScript fixtures. The walk reads .js and .mjs beside .go, so a browser
// comment can claim a fact about a Go file and the reverse.
//
// gofmt rewrites two adjacent backticks inside a Go comment into a typographic
// quotation mark, so the empty-pattern case has to live in a file gofmt never
// touches.

// This one holds.
// gosx:claim has target.go `func Alpha` case-ok-js
export const usesAlpha = true;

// This one parses and is false.
// gosx:claim has target.go `func Gamma` case-false-js
export const missingSymbol = true;

// This one asserts nothing, so it must not read as a pass.
// gosx:claim has target.go `` case-bad-empty-pattern
export const emptyPattern = true;
