# Contributing to Claudealyzer

Thanks for helping. Bug reports, new agent adapters, pricing updates and UI fixes are all welcome.

Developer setup, project layout, tests and packaging are in **[DEVELOPMENT.md](DEVELOPMENT.md)**.

## Before you start

- **Bugs:** open an issue with the *Bug report* template. Include your OS, the app version and the agent
  (Claude Code, Codex, Gemini) and its version.
- **Larger changes** (a new screen, a new agent, a change to how costs are calculated): open an issue first, so we
  can agree on the approach before you write the code.
- **Security problems:** do **not** open a public issue. See [SECURITY.md](SECURITY.md).

## Privacy rule: never commit real transcripts

Agent transcripts contain prompts, source code, file paths and sometimes secrets. Test fixtures, screenshots and
issue attachments must use **synthetic data** only (made-up user, projects and prompts). If you need to show a real
transcript structure in an issue, strip the content and keep only the keys.

## Pull requests

1. Fork the repo and create a branch from `main` (`fix/…`, `feat/…`, `docs/…`).
2. Keep a PR focused on one change.
3. Make sure these pass:
   ```sh
   go vet ./...
   go test ./...
   cd frontend && npm run build
   ```
4. If you changed a Go service signature, regenerate the bindings with `wails3 generate bindings` and commit them.
5. For UI changes, add a before/after screenshot (using demo data) to the PR.
6. Add a line under **Unreleased** in [CHANGELOG.md](CHANGELOG.md) for user-visible changes.

### Commit messages

- Imperative mood, short subject line (≤ 72 characters): `Fix session list selection highlight`,
  `Add OpenCode adapter`.
- Explain the *why* in the body when it is not obvious.

## Adding a new agent

Follow the steps in [DEVELOPMENT.md → Adding an agent](DEVELOPMENT.md#adding-an-agent). In short: an adapter in
`internal/adapters/<agent>/`, detection in `internal/core`, synthetic `testdata/` plus a test, and the agent's models
in the pricing table. Note which agent version you tested against in the PR.

## Updating prices

Prices live in `internal/pricing/default_pricing.json` and are **versioned by date**. Add a new entry with a `from`
date instead of editing an existing one, so older sessions keep their historical price. Link the official pricing page
you used in the PR description.

## Code of conduct

This project follows the [Contributor Covenant](CODE_OF_CONDUCT.md). By taking part you agree to follow it.

## License

By contributing, you agree that your contributions are licensed under the [MIT License](LICENSE).
