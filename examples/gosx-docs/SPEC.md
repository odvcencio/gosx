## Visual System

- Territory: `Paper & Ink`
- Typography:
  - Display: `"Iowan Old Style", "Palatino Linotype", "Book Antiqua", Georgia, serif`, weight `700`
  - Body: `"Source Serif 4", "Iowan Old Style", Georgia, serif`, weights `400, 600`
  - Mono: `"IBM Plex Mono", "SFMono-Regular", "Menlo", monospace`, weight `500`
  - Scale ratio: `1.25`
- Color architecture:
  - Dominant: `#f5efe6`
  - Secondary: `#ebe1d2`
  - Accent: `#b4572e`
  - Text primary on dominant: `#1d2622`, AAA
  - Text secondary on dominant: `#4f5c55`, AA
  - Muted on dominant: `#6d786f`, AA-large
- Motion philosophy: `Subtle`
  - `--motion-fast: 180ms`
  - `--motion-base: 260ms`
  - `--motion-slow: 560ms`
  - `--ease-out: cubic-bezier(0.16, 1, 0.3, 1)`
  - `--ease-spring: cubic-bezier(0.34, 1.56, 0.64, 1)`
- Spacing scale:
  - `--space-xs: clamp(0.5rem, 0.6vw, 0.75rem)`
  - `--space-sm: clamp(0.875rem, 1vw, 1rem)`
  - `--space-md: clamp(1.25rem, 1.6vw, 1.5rem)`
  - `--space-lg: clamp(1.75rem, 2.5vw, 2rem)`
  - `--space-xl: clamp(2.5rem, 4vw, 3rem)`
  - `--space-2xl: clamp(3.5rem, 6vw, 4.5rem)`
  - `--space-3xl: clamp(5rem, 9vw, 7rem)`
- CSS custom properties:

```css
:root {
  --color-bg: #f5efe6;
  --color-surface: #ebe1d2;
  --color-surface-strong: #e1d2bc;
  --color-ink: #1d2622;
  --color-text-secondary: #4f5c55;
  --color-text-muted: #6d786f;
  --color-accent: #b4572e;
  --color-accent-soft: rgba(180, 87, 46, 0.12);
  --color-line: rgba(29, 38, 34, 0.14);
  --font-display: "Iowan Old Style", "Palatino Linotype", "Book Antiqua", Georgia, serif;
  --font-body: "Source Serif 4", "Iowan Old Style", Georgia, serif;
  --font-mono: "IBM Plex Mono", "SFMono-Regular", "Menlo", monospace;
  --motion-fast: 180ms;
  --motion-base: 260ms;
  --motion-slow: 560ms;
  --ease-out: cubic-bezier(0.16, 1, 0.3, 1);
  --ease-spring: cubic-bezier(0.34, 1.56, 0.64, 1);
  --space-xs: clamp(0.5rem, 0.6vw, 0.75rem);
  --space-sm: clamp(0.875rem, 1vw, 1rem);
  --space-md: clamp(1.25rem, 1.6vw, 1.5rem);
  --space-lg: clamp(1.75rem, 2.5vw, 2rem);
  --space-xl: clamp(2.5rem, 4vw, 3rem);
  --space-2xl: clamp(3.5rem, 6vw, 4.5rem);
  --space-3xl: clamp(5rem, 9vw, 7rem);
}
```
