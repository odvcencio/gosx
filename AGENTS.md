# Contributing to GoSX (agents and people)

## Upstream first

When a game needs a system that is not gameplay (game loop host, input mapping, camera, loading, settings, saves, rooms, audio mixing, asset build), build it in GoSX first and use it from there. Do not add a local copy. If GoSX's version does not fit, fix GoSX.

Do not add specs, plans, roadmaps or reports to the repo; they live in Hyphae. `docs/` holds user guides and references only.

Write only generic engine code here: no game rules, names or assets. When you move a system out of a game, strip all of those.

## Check releases against the real games

Every release is checked against the consumer games before it ships. Run the consumer corpus from a GoSX checkout:

    scripts/corpus/run.sh -r origin/main -b

It builds, vets, tests and WebAssembly-builds each consumer against this checkout. Read `scripts/corpus/README.md` for the checks, time budget and output. Hosted CI runs only the public consumers; the private ones need this local run.

## Breaking an exported API

GoSX is pre-v1, so clean breaks are allowed. Before you change an exported symbol, find its callers in this repo and in the consumers, migrate the in-repo callers, and list each consumer break in the PR.
