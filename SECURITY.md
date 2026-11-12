# Security policy

Claudealyzer reads coding-agent transcripts, which can contain source code, file paths, prompts and secrets.
Security reports are taken seriously.

## Supported versions

Only the latest release gets security fixes.

| Version | Supported |
|---|---|
| Latest release | ✅ |
| Older releases | ❌ |

## Reporting a vulnerability

**Please do not open a public issue.**

Report it privately through GitHub:
[**Report a vulnerability**](https://github.com/molnarroland/Claudealyzer/security/advisories/new)
(*Security → Advisories → Report a vulnerability*).

Please include:

- what the issue is and what an attacker could do with it;
- steps to reproduce (with synthetic data, never real transcripts);
- the app version and OS.

You can expect a first reply within **7 days**. Once the issue is confirmed, a fix is released as soon as possible and
you are credited in the advisory, unless you prefer not to be.

## Scope

In scope, for example:

- the app writing to, or modifying, an agent's folder (it must be read-only);
- transcript data leaving the machine (the app must make no network calls);
- crafted transcript files that cause code execution or script injection in the UI;
- the app's own config and cache files exposing data to other users on the machine.

Out of scope: problems in the agents themselves (Claude Code, Codex, Gemini CLI), and anything that needs an attacker
who already controls your user account.
