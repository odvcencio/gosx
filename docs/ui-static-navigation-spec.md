# Portable UI static navigation correction

The user confirmed preserving existing light-neutral styling and inherited fonts; defer redesign. This contract governs the narrow `ui.Tabs` semantic correction, not the docs/demo Dark Elegance shells or a new panel-owning Tabs widget. Existing-code preservation overrides new-design font/scale recommendations. Only the forced-colors accessibility exception below changes the stylesheet.

## Visual System

### Territory

Swiss Precision, interpreted as the existing compact, neutral, aligned control strip. Preserve `.gosx-ui-tabs`, `.gosx-ui-tab` and `.is-active`, the underline and existing focus outline. No new decorative treatment. Browser R2 demonstrated that forced colors maps current/inactive border colors to identical system ink. Root approved a forced-colors-only underline on `[aria-current=page]` as a color-independent state signal. Browser chooses its system ink; do not assume Canvas matches an inherited background, modify inactive borders or disable forced-color adjustment. Default styling, geometry and fonts remain unchanged.

### Typography

Display/headline and body font: inherited from the consuming application; this portable component must not select or download a font. Tab label weight 600, inherited font size. Mono: none. Existing library sizes .875rem/1rem/1.125rem/1.35rem are deliberately nonuniform; no new ratio or fluid clamp interpolation is introduced. Preserve the consuming application's typography, including an existing otherwise-banned font.

### Palette

Assumed reference canvas (dominant): white #ffffff; secondary light neutral #f3f4f6; accent/focus #2563eb. Tabs themselves remain transparent, not forced onto a canvas. Primary/current text #111827 is approximately 17.7:1 on white (AAA); secondary tab text #6b7280 approximately 4.83:1 (AA); focus blue approximately 5.17:1. Muted text uses that same secondary color for this strip. Existing border #e5e7eb is decorative, not a sole state indicator. Ratios apply only to the white reference canvas; arbitrary inherited backgrounds require consumer contrast verification. These are preservation roles, not a claim of measured 60/30/10 screen coverage.

### Motion

Minimal: tabs introduce no transitions/animation. Preserve existing button-only 150ms CSS ease transitions elsewhere. Existing ease is cubic-bezier(.25,.1,.25,1); no spring motion is used. Reserved documentation names ease-out and ease-spring both alias existing ease for this no-motion slice, not a new animation prescription. Reduced motion must not hide content or alter navigation.

### Spacing

Preserve fixed existing values, not a new responsive scale: strip gap .25rem; label padding .625rem .75rem; underline 2px; focus outline 2px, offset 2px. Library gap steps .375/.5/.75/1/1.5rem remain nonuniform. This explicit preservation exception avoids changing layout through a token/clamp rewrite.

### Binding preservation tokens

The following documents existing values; do not inject a new global theme or rewrite existing CSS in this semantic-only slice. Fixed values intentionally replace a new clamp scale under the approved preservation contract.

```css
/* Reference names only; consuming application owns inherited font/canvas. */
--font-display: inherit;
--font-body: inherit;
--font-mono: inherit; /* unused */
--type-tab: inherit;
--color-canvas: #ffffff;
--color-surface: #f3f4f6;
--color-text: #111827;
--color-text-secondary: #6b7280;
--color-text-muted: #6b7280;
--color-focus: #2563eb;
--color-border: #e5e7eb;
--duration-existing-button: 150ms;
--duration-tab: 0ms;
--ease-out: cubic-bezier(.25,.1,.25,1);
--ease-spring: cubic-bezier(.25,.1,.25,1); /* no spring animation */
--space-tab-gap: .25rem;
--space-tab-block: .625rem;
--space-tab-inline: .75rem;
--space-xs: .375rem;
--space-sm: .5rem;
--space-md: .75rem;
--space-lg: 1rem;
--space-xl: 1.5rem;
```

## Static semantics

`Tabs` keeps its Go types, registry name and CSS classes. It is a static navigation/action strip, not an ARIA tab widget: no tablist/tab roles, aria-selected, panels, automatic DOM IDs, roving focus or arrow-key handling. Enabled links and buttons use native browser interaction; disabled links become noninteractive spans and disabled buttons use native disabled. No application JavaScript or island runtime is required.

Visual active classes retain the existing per-item formula. Current-page semantics are separate: nonempty Active chooses the first matching ID only. That exact item gets aria-current=page only if it is an enabled link; missing/disabled/nonlink matches have no fallback. With empty Active, the first enabled active link wins. Duplicate logical IDs therefore never produce multiple current-page markers. Caller-provided root attributes remain an escape hatch; consumers must not reintroduce contradictory widget roles.

True Go-authored panel-owning Tabs, including panels/unique IDs/keyboard activation/focus restoration, remains a separate queued feature.
