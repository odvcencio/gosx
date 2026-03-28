package textlayout

import "github.com/rivo/uniseg"

func segmentWordRunStringsFallback(text string) []string {
	if text == "" {
		return nil
	}
	segments := make([]string, 0, 8)
	rest := text
	state := -1
	for rest != "" {
		segment, next, newState := uniseg.FirstWordInString(rest, state)
		if segment == "" {
			break
		}
		segments = append(segments, segment)
		rest = next
		state = newState
	}
	if len(segments) == 0 {
		return []string{text}
	}
	return segments
}
