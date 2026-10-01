# Source-owned UI recipes

GoSX UI recipes are a small, offline catalog of readable component source. The
CLI copies only the recipes an application chooses into that application's
`app/ui` and `public/ui` directories. There is no runtime registry, generated
JavaScript, package dependency, or remote service behind the feature.

## Visual System

### Territory

**Dark Elegance — Obsidian.** The default recipe tokens use a near-black canvas,
quiet translucent surfaces, fine chrome borders, and a restrained warm-gold
accent. Components rely only on semantic custom properties, so an application
can replace the theme without editing component selectors.

### Typography

- Display: **Space Grotesk**, weight 700.
- Body: **Inter**, weights 400, 500, and 600.
- Mono: **JetBrains Mono**, weight 400.
- Scale: minor third (1.2) for a compact application UI: `0.75rem`, `0.875rem`,
  `1rem`, `1.2rem`, `1.44rem`, and a fluid `clamp()` display step.

The fonts are fallbacks, not bundled assets. Applications may self-host them or
override the three font tokens with their own stacks.

### Color architecture

- Dominant (60%): obsidian canvas `#050506`.
- Secondary (30%): translucent and raised near-black surfaces.
- Accent (10%): warm gold `#d4af37`, with a brighter focus color.
- Primary text `#f5f2e9`: 18.20:1 against the canvas (WCAG AAA).
- Secondary text `#c9c4b9`: 11.72:1 (WCAG AAA).
- Muted text `#9b978e`: 7.00:1 (WCAG AAA).
- Gold accent `#d4af37`: 9.69:1 (WCAG AAA).
- Error text `#ff8f82`: 9.22:1 (WCAG AAA).

These ratios measure text against the solid canvas only. Raised and translucent
surfaces, disabled controls, and other component states have different contrast;
the numbers do not establish AAA compliance for every component state.
Invalid fields also expose `aria-invalid`, disabled controls use native semantics,
and focus has a visible outline.

### Motion

**Subtle.** State transitions use 150ms and 200ms durations with
`cubic-bezier(0.16, 1, 0.3, 1)` for settling and
`cubic-bezier(0.34, 1.56, 0.64, 1)` for tactile feedback. Nonessential
transitions are removed under `prefers-reduced-motion: reduce`.

### Spacing and shape

The system follows an eight-pixel rhythm with fluid steps:

- `--gsx-space-xs`: `clamp(0.5rem, 0.46rem + 0.18vw, 0.75rem)`
- `--gsx-space-sm`: `clamp(0.75rem, 0.69rem + 0.24vw, 1rem)`
- `--gsx-space-md`: `clamp(1rem, 0.9rem + 0.48vw, 1.5rem)`
- `--gsx-space-lg`: `clamp(1.5rem, 1.3rem + 0.78vw, 2rem)`
- `--gsx-space-xl`: `clamp(2rem, 1.7rem + 1.2vw, 3rem)`
- `--gsx-space-2xl`: `clamp(3rem, 2.5rem + 2vw, 4rem)`
- `--gsx-space-3xl`: `clamp(4rem, 3rem + 4vw, 6rem)`

Soft corners belong to controls and panels; pills are reserved for compact
actions. The complete ready-to-override custom-property block is installed by
the `tokens` recipe at `public/ui/tokens.css`.

## Commands

Run commands from an application's module root, or pass `--root <dir>`:

```text
gosx ui list
gosx ui add button
gosx ui diff button
gosx ui add --update button
```

`list` is deterministic and needs no application or network. `add` installs the
named recipe and its dependencies. Existing identical files are left alone;
any differing file aborts the entire add before a recipe file is written. Use
`diff` to review local ownership changes. When the embedded catalog advances,
the explicit `--update` flag replaces only files that still match their prior
installed hashes and may add new catalog files. Locally modified or deleted
tracked files still stop the whole update; reconcile those changes manually.
v1 has no force mode, remote registry, or `ui init` step.

Every successful add updates `.gosx/ui/manifest.json`. It records catalog and
recipe versions, SHA-256 hashes, the SPDX license, and source provenance. It is
tool-owned metadata; component and stylesheet files remain ordinary application
source intended for editing.

### Installation guarantees and recovery

Installers take an OS-backed exclusive lock at `.gosx/ui/install.lock` before
reading the manifest or source, then revalidate under that lock. Leave the lock
file in place: the OS releases the lock when a process exits. Concurrent GoSX
installers serialize; a second installer waits up to 15 seconds before returning
a busy error. The metadata directory and empty destination directories can remain
after a failed preflight. Add `.gosx/ui/` to the application's ignore rules if this
tool-owned metadata should not be committed.

Every changed source file and the manifest are staged before replacement. An
error during replacement rolls back the whole set through saved originals.
The manifest is installed last. Files are published by linking complete staged
bytes into an absent destination, so a new file created between validation and
publication is preserved. The destination filesystem must support hard links.
Individual file publications are atomic, but the whole
multi-file operation is **not an atomic snapshot for external readers**: a dev
server or editor can observe intermediate files while installation is running.
When all source and manifest writes commit but backup cleanup fails, the command
reports success with a cleanup warning and keeps the recovery journal.

If rollback itself fails, the command reports that explicitly, preserves the
available backups, and leaves `.gosx/ui/transaction.json`. Rollback also stops
and preserves unexpected content if it detects an editor changing or replacing
a newly installed file. Further adds stop
until the transaction is reviewed. The journal records each destination, its
before/after hashes, and stage/backup names relative to its destination directory.
Preserve those files, compare their hashes, and restore the original files (or
finish the intended installation) before removing the journal. An interrupted
stage may also leave `.gosx-ui-stage-*` or `.gosx-ui-backup-*` files. Do not delete
them until their relationship to the transaction is understood.

This is error recovery, not power-loss durability: process termination, machine
crashes, filesystem failure, or a failed rollback can require manual recovery.
The installer uses Go's descriptor-backed `os.Root` and pinned destination
directories to prevent parent-symlink swaps from redirecting operations outside
those directories. It rejects symlinks, special files, traversal, Windows device
names, alternate data streams, and control characters in catalog paths. It does
not defend against privileged mount changes, an actor moving already-open
directories elsewhere, or an uncooperative process editing files during the last
check/rename window. The lock coordinates GoSX installers, not arbitrary editors.
Installation is supported on Linux, macOS, BSD, and Windows; platforms without
descriptor-backed roots and OS file locking fail closed.

Installed metadata must have one JSON document, unique object members, exact
catalog provenance, known recipes and owned paths, and valid release versions
within the catalog's supported major version through the current CLI version.
An older recipe may own a subset of its current paths, allowing additive updates;
renamed or removed paths require an explicit future migration. Metadata is an
ownership ledger, not a cryptographic attestation of historical catalog bytes.

`diff` escapes terminal controls, including ANSI/OSC, carriage returns, and bidi
formatting. It summarizes binary or large content with lengths and hashes.
Reads are limited to 1 MiB per file; detailed diffs are limited to 64 KiB and
512 newlines per side, with at most 128 KiB of escaped output per file.

## Using a recipe

After `gosx ui add button`, load the installed token and component styles from
your document layout (or import them from an existing public stylesheet):

```gosx
<link rel="stylesheet" href="/ui/tokens.css" />
<link rel="stylesheet" href="/ui/button.css" />
```

Then import the shared component directory from a route file:

```gosx
package account

import ui "../ui"

component Page() {
	return <main>
		<ui.Button Type="submit" Variant="primary" Size="md" Disabled={false}>
			Save changes
		</ui.Button>
	</main>
}
```

The exact relative import depends on the route directory. Each strict props
field rendered by a recipe is explicit at the call site. Supported initial
variants are documented in the installed source comments:

- Button: `primary`, `secondary`, `ghost`, `danger`; sizes `sm`, `md`, `lg`.
- Card: `default`, `raised`, `quiet`.
- Input: native text-like input types, with native disabled/required semantics
  and an explicit boolean invalid state plus visible error message.

These are server components. They render semantic HTML and ship no client
runtime. If a future recipe genuinely owns client behavior, it must declare an
island explicitly and carry its own runtime and size evidence.

## Portability and size

All component CSS consumes the `--gsx-*` semantic tokens; raw palette, font,
spacing, and motion values live only in `tokens.css`. Override those variables
after loading the token file to theme the recipes without rewriting selectors.

The catalog is embedded only in the `gosx` CLI package. It is not in the GoSX
application dependency graph, and `gosx init` installs none of it. Consequently
an unselected recipe contributes exactly zero source, CSS, JavaScript, WASM, or
server-binary bytes to an application.
