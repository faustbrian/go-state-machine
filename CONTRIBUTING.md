# Contributing

Use Go 1.26.5 or newer. Create a conventional branch from `main`, keep commits
focused, and include tests and documentation for public behavior.

Run `make check` before opening a pull request. Transition changes must add
determinism and replay evidence. Persistence changes must run the shared
conformance suite plus real PostgreSQL integration and race tests. Public API
changes must update `docs/api.md` and `CHANGELOG.md`.

By participating, you agree to follow [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
