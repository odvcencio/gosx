# GoSX Documentation Site

The public documentation site is a working GoSX application and product proof. It keeps the existing visual identity while making every navigation, search, version, demo, and deployment claim observable and testable.

## Visual System

### Territory

**Dark Elegance.** Preserve the near-black canvas, restrained translucent surfaces, fine borders, chrome display type, and warm gold accent already established by the site. Functionality should feel integrated into this system: search is a quiet command surface, version data reads like instrumentation, and status colors remain subordinate to content.

### Typography

- Display: **Space Grotesk**, 700, for page and section headings.
- Body: **Inter**, 400, 500, and 600, using the self-hosted files already shipped by the docs app.
- Mono: **JetBrains Mono**, 400, for code, commands, versions, paths, runtime facts, and compact labels.
- Scale: the existing fluid major-third scale in `app/global.css`.

### Color architecture

- Dominant: `#000000` site canvas.
- Secondary: low-opacity white surfaces over the canvas.
- Primary text: `rgba(255, 255, 255, 0.92)`.
- Secondary text: `rgba(255, 255, 255, 0.70)`.
- Muted text: `rgba(255, 255, 255, 0.52)`.
- Accent: `#d4af37`, with `#c9a227` as its deeper state.
- Demo route accents remain permitted only inside the demo system; shared navigation, search, and provenance stay gold/chrome.
- Status colors must meet WCAG AA when used as text and must never be the only carrier of meaning.

### Motion

**Subtle and explanatory.** Keep the existing fast (200ms), standard (300ms), and slow (500ms) durations and easing curves. Search results, disclosures, and navigation state may fade or translate slightly; core content must never wait on animation. All nonessential animation stops under `prefers-reduced-motion: reduce`.

### Spacing and shape

- Use the existing responsive spacing tokens (`--space-xs` through `--space-3xl`) and the established eight-pixel rhythm.
- Keep surfaces mostly square or softly rounded; reserve pill shapes for actions, tags, versions, and compact status controls.
- Maintain the 72rem editorial content width and avoid masking overflow that would hide functional content.

### Functional binding

- The masthead exposes Docs, Demos, Search, the running framework version, and source.
- Documentation pages expose a persistent grouped guide index on wide screens and the same information through an accessible disclosure on narrow screens.
- Search works as an ordinary server-rendered form before any browser enhancement.
- Build and runtime provenance is visible in the footer and available as machine-readable JSON.
- Every promoted demo states its implementation surface, status, source, and limits; controls without a working action do not render.
