# Screenshots

Remakes every app screenshot in `docs/images/` (the website and the README hero) from made-up demo data.
Never take screenshots from real transcripts: they hold prompts, code, paths and sometimes secrets.

```sh
scripts/screenshots/run.sh                    # dates end today
scripts/screenshots/run.sh --today 2026-11-12 # pin the last day with sessions
scripts/screenshots/run.sh --keep             # keep the temp dir (fake HOME, raw PNGs, server log)
```

Needs Go, Node with npm, python3, `cwebp` (`brew install webp`) and `sips` (macOS) or ImageMagick.
Playwright and its Chromium are installed into this folder on the first run.

## What it does

| File | Role |
| --- | --- |
| `gen_mock.py` | Writes fake Claude Code, Codex CLI, Gemini CLI and OpenCode history for user `demo` and four made-up projects (`acme-storefront`, `weather-api`, `pixel-notes`, `infra-terraform`) into a temporary HOME. The seed is fixed, so the sessions are the same every run; only the dates move. |
| `run.sh` | Builds the app in Wails server mode (the UI served over HTTP, no window), starts it with that HOME, gives the sources demo plans (Claude Max $100 with demo usage limits, Codex Plus $20, Gemini free, OpenCode on an API key) and demo budgets, runs `shots.mjs`, then scales the PNGs to 1800 px and writes WebP to `docs/images/`, plus `history.png` for `og:image` and GitHub's social preview. |
| `shots.mjs` | Drives the app with Playwright at 1440×900 @2x: opens *The checkout total is off by one cent…* in `acme-storefront` (sorted by cost) and takes each view in the macOS 26, Windows 11 and GNOME looks, light and dark. It fails if a real filesystem path is visible in the UI. |

## After a UI change

If a shot breaks, the selectors in `shots.mjs` are what to fix: views are the sidebar's `snSidebarItem`
buttons, projects and sessions are `sn-tree-item`s found by `aria-label`, and the Look and Color scheme
pickers are the `<select>`s labelled that way in Settings. Run with `--keep` and look at the raw PNGs.

A new screenshot needs a `shot('name')` in `shots.mjs` and a reference in `docs/index.html` (and `docs/site.js`
for the hero switcher).
