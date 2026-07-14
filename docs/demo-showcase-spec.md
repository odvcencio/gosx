# GoSX Demo Showcase

The demo gallery is a product proof, not a component catalog. Every listed demo must make one GoSX capability understandable, interactive, and trustworthy within a few seconds. Claims in the gallery and shared chrome are part of the demo's acceptance contract.

## Visual System

### Territory

**Dark Elegance.** The existing demo shell uses a near-black canvas, restrained surfaces, fine borders, and a single luminous accent per experience. Preserve that quiet technical character. Demo-specific accents may identify a route, but must not replace the shared hierarchy.

### Typography

- Display: **Space Grotesk**, 700. Used for showcase titles and decisive headings.
- Body: **Inter**, 400, 500, and 600. This is an existing-project override to the general font ban; the font is already shipped in `public/fonts`.
- Mono: **JetBrains Mono**, 400. Used for source, metrics, backend names, and technical facts.
- Scale: **Major Third (1.25)**.

### Color architecture

- Dominant, 60%: `#0b0b0d` — the demo canvas.
- Secondary, 30%: `#151519` — navigation, drawers, and elevated technical surfaces.
- Accent, 10%: route-specific, with `#69e3c7` as the gallery default.
- Primary text: `#f5f5ef` on dominant, approximately 18:1 contrast (WCAG AAA).
- Secondary text: `#c7c7c2` on dominant, approximately 11.7:1 contrast (WCAG AAA).
- Muted text: `#9a9a9a` on dominant, approximately 6.9:1 contrast (WCAG AA).
- Accent text: `#69e3c7` on dominant, approximately 12.6:1 contrast (WCAG AAA).

Route accents must retain at least 4.5:1 contrast when used for text. Lower-contrast accents may be decorative only.

### Motion

**Subtle.** Motion explains state changes; it never competes with the demo.

- Fast: 150ms for hover and focus feedback.
- Standard: 200ms for controls and small state transitions.
- Reveal: 300ms for drawers and responsive navigation.
- `ease-out`: `cubic-bezier(0.16, 1, 0.3, 1)`.
- `ease-spring`: `cubic-bezier(0.34, 1.56, 0.64, 1)`.
- All nonessential transitions are disabled under `prefers-reduced-motion: reduce`.

### Spacing

Eight-pixel base, expressed through a responsive shared scale:

- `xs`: `clamp(0.5rem, 0.45rem + 0.2vw, 0.75rem)`
- `sm`: `clamp(0.75rem, 0.7rem + 0.25vw, 1rem)`
- `md`: `clamp(1rem, 0.9rem + 0.5vw, 1.5rem)`
- `lg`: `clamp(1.5rem, 1.3rem + 0.8vw, 2rem)`
- `xl`: `clamp(2rem, 1.7rem + 1.25vw, 3rem)`
- `2xl`: `clamp(3rem, 2.5rem + 2vw, 4rem)`
- `3xl`: `clamp(4rem, 3rem + 4vw, 6rem)`

### Binding tokens

```css
.demos-shell {
  --font-display: "Space Grotesk", sans-serif;
  --font-body: "Inter", sans-serif;
  --font-mono: "JetBrains Mono", monospace;

  --type-xs: 0.75rem;
  --type-sm: 0.875rem;
  --type-base: 1rem;
  --type-lg: 1.25rem;
  --type-xl: 1.5625rem;
  --type-2xl: 1.953rem;
  --type-3xl: clamp(2.25rem, 1.9rem + 1.5vw, 3.052rem);

  --color-canvas: #0b0b0d;
  --color-surface: #151519;
  --color-surface-raised: #1d1d22;
  --color-text: #f5f5ef;
  --color-text-secondary: #c7c7c2;
  --color-text-muted: #9a9a9a;
  --color-accent: #69e3c7;
  --color-border: rgba(255, 255, 255, 0.08);
  --color-border-strong: rgba(255, 255, 255, 0.16);
  --color-backdrop: rgba(0, 0, 0, 0.64);

  --duration-fast: 150ms;
  --duration-standard: 200ms;
  --duration-reveal: 300ms;
  --ease-out: cubic-bezier(0.16, 1, 0.3, 1);
  --ease-spring: cubic-bezier(0.34, 1.56, 0.64, 1);

  --space-xs: clamp(0.5rem, 0.45rem + 0.2vw, 0.75rem);
  --space-sm: clamp(0.75rem, 0.7rem + 0.25vw, 1rem);
  --space-md: clamp(1rem, 0.9rem + 0.5vw, 1.5rem);
  --space-lg: clamp(1.5rem, 1.3rem + 0.8vw, 2rem);
  --space-xl: clamp(2rem, 1.7rem + 1.25vw, 3rem);
  --space-2xl: clamp(3rem, 2.5rem + 2vw, 4rem);
  --space-3xl: clamp(4rem, 3rem + 4vw, 6rem);
}
```

## Shared demo contract

Every promoted demo must declare one promise, one GoSX lesson, truthful capability facets, its source path, packages, render mode, and limitations. Shared navigation and metadata surfaces must derive from that declaration. A control must either perform its advertised action or not render.

Statuses are:

- **Featured** — polished, truthful, and release-gated.
- **Live** — functional and useful, with documented limitations.
- **Lab** — technical diagnostics for advanced users.
- **Prototype** — visible only with clear experimental framing; never presented as complete.

