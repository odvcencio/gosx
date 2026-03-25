package gosx

// Re-export grammargen types and DSL functions for use in grammar definitions.

import (
	"github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammargen"
)

// Type aliases
type Grammar = grammargen.Grammar
type Rule = grammargen.Rule

// Constructor aliases
var (
	NewGrammar    = grammargen.NewGrammar
	ExtendGrammar = grammargen.ExtendGrammar
)

// DSL function aliases
var (
	Str         = grammargen.Str
	Pat         = grammargen.Pat
	Sym         = grammargen.Sym
	Seq         = grammargen.Seq
	Choice      = grammargen.Choice
	Repeat      = grammargen.Repeat
	Repeat1     = grammargen.Repeat1
	Optional    = grammargen.Optional
	Token       = grammargen.Token
	ImmToken    = grammargen.ImmToken
	Field       = grammargen.Field
	Prec        = grammargen.Prec
	PrecLeft    = grammargen.PrecLeft
	PrecRight   = grammargen.PrecRight
	PrecDynamic = grammargen.PrecDynamic
	Alias       = grammargen.Alias
	Blank       = grammargen.Blank
	CommaSep    = grammargen.CommaSep
	CommaSep1   = grammargen.CommaSep1
)

// Helper function aliases
var (
	AppendChoice            = grammargen.AppendChoice
	AddConflict             = grammargen.AddConflict
	Generate                = grammargen.Generate
	GenerateLanguage        = grammargen.GenerateLanguage
	GenerateLanguageAndBlob = grammargen.GenerateLanguageAndBlob
	LoadLanguageBlob        = gotreesitter.LoadLanguage
	GoGrammar               = grammargen.GoGrammar
)
