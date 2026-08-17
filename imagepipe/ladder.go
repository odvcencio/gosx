package imagepipe

import "sort"

// Ladder returns the subset of candidates that do not exceed intrinsicWidth,
// deduplicated and sorted ascending, with intrinsicWidth itself appended
// when no candidate already lands on it exactly.
//
// It never upscales: every returned width is <= intrinsicWidth. That
// mirrors how server.AutoImageWidths(intrinsicWidth) caps the very same
// candidate list for the runtime <img> srcset it builds, so a width gosx
// build generated a variant for at build time is always a width the
// running app can ask for at request time -- cmd/gosx's build stage calls
// Ladder(intrinsicWidth, server.AutoImageWidths(0)) for exactly this
// reason. Non-positive candidate values are dropped.
//
// A non-positive intrinsicWidth disables the cap: Ladder returns
// candidates unchanged (deduplicated, sorted, no width appended), matching
// AutoImageWidths(0)'s own "no max" behavior.
func Ladder(intrinsicWidth int, candidates []int) []int {
	if intrinsicWidth <= 0 {
		return normalizeWidths(candidates)
	}

	widths := make([]int, 0, len(candidates)+1)
	for _, width := range candidates {
		if width > 0 && width <= intrinsicWidth {
			widths = append(widths, width)
		}
	}
	if len(widths) == 0 || widths[len(widths)-1] != intrinsicWidth {
		widths = append(widths, intrinsicWidth)
	}
	return normalizeWidths(widths)
}

func normalizeWidths(widths []int) []int {
	if len(widths) == 0 {
		return nil
	}
	seen := make(map[int]bool, len(widths))
	out := make([]int, 0, len(widths))
	for _, width := range widths {
		if width <= 0 || seen[width] {
			continue
		}
		seen[width] = true
		out = append(out, width)
	}
	sort.Ints(out)
	if len(out) == 0 {
		return nil
	}
	return out
}
