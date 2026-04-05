package embed

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

// Tokenizer encodes and decodes text using Byte-Pair Encoding.
type Tokenizer struct {
	vocab    map[string]int // token string -> ID
	inverse  []string       // ID -> token string
	merges   []mergePair    // ordered merge rules, highest priority first
	special  specialTokens  // CLS, SEP, PAD, UNK IDs
	hasSpec  bool           // whether special tokens are configured
	useGlyph bool           // whether to use Ġ (U+0120) space convention
}

type mergePair struct {
	a, b string
	rank int
}

type specialTokens struct {
	CLS int
	SEP int
	PAD int
	UNK int
}

// TokenizerFiles specifies the paths to load a tokenizer from disk.
type TokenizerFiles struct {
	VocabPath         string // vocab.json: {"token": id, ...}
	MergesPath        string // merges.txt: one "tokenA tokenB" per line
	SpecialTokensPath string // special_tokens.json: {"[CLS]": id, ...}
}

// NewTokenizer creates a tokenizer from an in-memory vocabulary and merge rules.
// Each merge rule is a string of the form "tokenA tokenB".
// Special tokens ([CLS], [SEP], etc.) are detected from the vocabulary if present.
func NewTokenizer(vocab map[string]int, merges []string) *Tokenizer {
	// Parse merge strings into mergePair structs
	parsed := make([]mergePair, 0, len(merges))
	for i, m := range merges {
		parts := strings.SplitN(m, " ", 2)
		if len(parts) != 2 {
			continue
		}
		parsed = append(parsed, mergePair{a: parts[0], b: parts[1], rank: i})
	}

	// Build inverse vocab (ID -> token string)
	maxID := 0
	for _, id := range vocab {
		if id > maxID {
			maxID = id
		}
	}
	inverse := make([]string, maxID+1)
	for tok, id := range vocab {
		if id >= 0 && id < len(inverse) {
			inverse[id] = tok
		}
	}

	// Detect special tokens from vocabulary
	spec := specialTokens{}
	hasSpec := false
	if id, ok := vocab["[CLS]"]; ok {
		spec.CLS = id
		hasSpec = true
	}
	if id, ok := vocab["[SEP]"]; ok {
		spec.SEP = id
		hasSpec = true
	}
	if id, ok := vocab["[PAD]"]; ok {
		spec.PAD = id
		hasSpec = true
	}
	if id, ok := vocab["[UNK]"]; ok {
		spec.UNK = id
		hasSpec = true
	}

	// Detect whether the vocabulary uses the Ġ (U+0120) space convention.
	// If any token contains \u0120, use the glyph convention for pre-tokenization.
	useGlyph := false
	for tok := range vocab {
		if strings.ContainsRune(tok, '\u0120') {
			useGlyph = true
			break
		}
	}

	return &Tokenizer{
		vocab:    vocab,
		inverse:  inverse,
		merges:   parsed,
		special:  spec,
		hasSpec:  hasSpec,
		useGlyph: useGlyph,
	}
}

// LoadTokenizer reads vocabulary, merge rules, and special tokens from files.
func LoadTokenizer(files TokenizerFiles) (*Tokenizer, error) {
	vocab, err := loadVocab(files.VocabPath)
	if err != nil {
		return nil, fmt.Errorf("embed: load vocab: %w", err)
	}

	merges, err := loadMerges(files.MergesPath)
	if err != nil {
		return nil, fmt.Errorf("embed: load merges: %w", err)
	}

	special, err := loadSpecialTokens(files.SpecialTokensPath)
	if err != nil {
		return nil, fmt.Errorf("embed: load special tokens: %w", err)
	}

	// Build inverse vocab (ID -> token string)
	maxID := 0
	for _, id := range vocab {
		if id > maxID {
			maxID = id
		}
	}
	inverse := make([]string, maxID+1)
	for tok, id := range vocab {
		if id >= 0 && id < len(inverse) {
			inverse[id] = tok
		}
	}

	// Detect Ġ space convention
	useGlyph := false
	for tok := range vocab {
		if strings.ContainsRune(tok, '\u0120') {
			useGlyph = true
			break
		}
	}

	return &Tokenizer{
		vocab:    vocab,
		inverse:  inverse,
		merges:   merges,
		special:  special,
		hasSpec:  true,
		useGlyph: useGlyph,
	}, nil
}

// Encode converts text to a sequence of token IDs.
// If special tokens are configured, prepends [CLS] and appends [SEP].
func (t *Tokenizer) Encode(text string) []int {
	if t.hasSpec {
		return t.encodeWithSpecial(text)
	}
	return t.encodeRaw(text)
}

// encodeWithSpecial wraps the token sequence in [CLS] ... [SEP].
func (t *Tokenizer) encodeWithSpecial(text string) []int {
	if text == "" {
		return []int{t.special.CLS, t.special.SEP}
	}

	words := t.preTokenizeText(text)
	var ids []int
	ids = append(ids, t.special.CLS)

	for _, word := range words {
		wordIDs := t.encodeWord(word)
		ids = append(ids, wordIDs...)
	}

	ids = append(ids, t.special.SEP)
	return ids
}

// encodeRaw encodes text to token IDs without special token framing.
func (t *Tokenizer) encodeRaw(text string) []int {
	if text == "" {
		return nil
	}

	words := t.preTokenizeText(text)
	var ids []int
	for _, word := range words {
		wordIDs := t.encodeWord(word)
		ids = append(ids, wordIDs...)
	}
	return ids
}

// preTokenizeText dispatches to the appropriate pre-tokenizer based on
// whether the vocabulary uses the Ġ (U+0120) space convention.
func (t *Tokenizer) preTokenizeText(text string) []string {
	if t.useGlyph {
		return preTokenize(text)
	}
	return preTokenizeSimple(text)
}

// Decode converts token IDs back to text. Special tokens are omitted.
func (t *Tokenizer) Decode(ids []int) string {
	var sb strings.Builder
	for _, id := range ids {
		if t.hasSpec && (id == t.special.CLS || id == t.special.SEP || id == t.special.PAD) {
			continue
		}
		if id >= 0 && id < len(t.inverse) {
			tok := t.inverse[id]
			// BPE convention: "\u0120" (U+0120) represents a leading space
			tok = strings.ReplaceAll(tok, "\u0120", " ")
			sb.WriteString(tok)
		}
	}
	return sb.String()
}

// VocabSize returns the total number of tokens in the vocabulary.
func (t *Tokenizer) VocabSize() int {
	return len(t.vocab)
}

// encodeWord applies BPE merges to a single pre-tokenized word.
func (t *Tokenizer) encodeWord(word string) []int {
	// Split word into initial character-level tokens (UTF-8 runes)
	symbols := make([]string, 0, utf8.RuneCountInString(word))
	for _, r := range word {
		symbols = append(symbols, string(r))
	}

	// Build merge priority lookup
	mergeRank := make(map[string]int, len(t.merges))
	for i, m := range t.merges {
		mergeRank[m.a+" "+m.b] = i
	}

	// Iteratively merge the highest-priority pair
	for len(symbols) > 1 {
		bestRank := len(t.merges)
		bestIdx := -1

		for i := 0; i < len(symbols)-1; i++ {
			key := symbols[i] + " " + symbols[i+1]
			if rank, ok := mergeRank[key]; ok && rank < bestRank {
				bestRank = rank
				bestIdx = i
			}
		}

		if bestIdx < 0 {
			break // no more applicable merges
		}

		// Apply merge at bestIdx
		merged := symbols[bestIdx] + symbols[bestIdx+1]
		newSymbols := make([]string, 0, len(symbols)-1)
		newSymbols = append(newSymbols, symbols[:bestIdx]...)
		newSymbols = append(newSymbols, merged)
		newSymbols = append(newSymbols, symbols[bestIdx+2:]...)
		symbols = newSymbols
	}

	// Map symbols to vocabulary IDs
	ids := make([]int, len(symbols))
	for i, sym := range symbols {
		if id, ok := t.vocab[sym]; ok {
			ids[i] = id
		} else if t.hasSpec {
			ids[i] = t.special.UNK
		} else {
			// No UNK token configured: use -1 as sentinel
			ids[i] = -1
		}
	}

	// Filter out -1 sentinels (unknown chars with no UNK token)
	if !t.hasSpec {
		filtered := ids[:0]
		for _, id := range ids {
			if id >= 0 {
				filtered = append(filtered, id)
			}
		}
		return filtered
	}

	return ids
}

// preTokenizeSimple splits text into words and whitespace tokens.
// Spaces are kept as individual tokens, no Ġ convention.
func preTokenizeSimple(text string) []string {
	var words []string
	var current strings.Builder

	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			words = append(words, string(r))
			continue
		}

		// Split punctuation into its own token
		if isPunctuation(r) {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			words = append(words, string(r))
			continue
		}

		current.WriteRune(r)
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

// preTokenize splits text on whitespace and punctuation boundaries.
// Leading spaces are encoded as the BPE "\u0120" prefix on the following word.
func preTokenize(text string) []string {
	var words []string
	var current strings.Builder
	prevSpace := true

	for _, r := range text {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			prevSpace = true
			continue
		}

		// Add space prefix for BPE convention (U+0120 before non-first words)
		if prevSpace && len(words) > 0 {
			current.WriteRune('\u0120') // Ġ
		}
		prevSpace = false

		// Split punctuation into its own token
		if isPunctuation(r) {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
			words = append(words, string(r))
			continue
		}

		current.WriteRune(r)
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}

func isPunctuation(r rune) bool {
	switch r {
	case '.', ',', '!', '?', ';', ':', '"', '\'', '(', ')', '[', ']', '{', '}',
		'-', '/', '\\', '@', '#', '$', '%', '^', '&', '*', '+', '=', '<', '>', '|', '~', '`':
		return true
	}
	return false
}

// --- File loaders ---

func loadVocab(path string) (map[string]int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var vocab map[string]int
	if err := json.Unmarshal(data, &vocab); err != nil {
		return nil, err
	}
	return vocab, nil
}

func loadMerges(path string) ([]mergePair, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var merges []mergePair
	scanner := bufio.NewScanner(f)
	rank := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		merges = append(merges, mergePair{a: parts[0], b: parts[1], rank: rank})
		rank++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return merges, nil
}

func loadSpecialTokens(path string) (specialTokens, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return specialTokens{}, err
	}
	var raw map[string]int
	if err := json.Unmarshal(data, &raw); err != nil {
		return specialTokens{}, err
	}
	return specialTokens{
		CLS: raw["[CLS]"],
		SEP: raw["[SEP]"],
		PAD: raw["[PAD]"],
		UNK: raw["[UNK]"],
	}, nil
}
