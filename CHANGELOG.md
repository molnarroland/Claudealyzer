# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.1] - 2026-11-18

### Changed

- Sessions now live in the sidebar as a project tree: projects are folders, their sessions are leaves. Sessions
  load when a project is expanded and reload together when the sort or filters change. The separate sessions
  column in History is gone.
- Spartan Native updated to 0.2.1, now pulled from npm and GitHub instead of vendored tarballs.
- Website and README screenshots converted to WebP and remade to show the new sidebar tree.
- Smaller macOS DMG background and file icon, with SVG sources added.

### Added

- Screenshot tooling in `scripts/screenshots/`: generates mock Claude Code, Codex and Gemini history, drives the
  app with Playwright and writes WebP to `docs/images`. Fails if a real filesystem path shows up in the UI.

## [0.1.0] - 2026-11-12

First public release.

### Added

- Desktop app built with Go + Wails v3 and an Angular / Spartan Native frontend with native looks for macOS,
  Windows and Linux.
- Read-only, local adapters for **Claude Code** (multiple profiles), **Codex CLI / Desktop** and **Gemini CLI**.
  OpenCode is detected but not read yet.
- Screens: History, Step detail, Session analysis, Overview, Calendar, Cost explained, Sources & settings.
- Insight rules with severity and estimated dollar impact; editable thresholds.
- Versioned pricing table and per-source billing (API key, subscription, team seat, free tier).
- Per-arch and universal macOS packaging tasks; cross-arch Linux Docker builds.
- Project website on GitHub Pages (`docs/`).
- MIT license, contributing guide, security policy, code of conduct, issue and PR templates.

### Fixed

- Session list selection highlight and row overlap.
- Output preview rendering as an ellipse on pill-radius skins.

[Unreleased]: https://github.com/molnarroland/Claudealyzer/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/molnarroland/Claudealyzer/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/molnarroland/Claudealyzer/releases/tag/v0.1.0
