## Summary of Changes

A concise technical explanation of what this PR introduces or resolves.

Fixes #[issue-number]

## Type of Change

- [ ] feat: New feature or driver capability
- [ ] fix: Bug fix in proxy, driver, or CLI
- [ ] perf: Performance optimization
- [ ] docs: Documentation update
- [ ] test: Unit, integration, or fuzz test addition
- [ ] refactor: Internal code reorganization

## Architectural Decisions & Trade-offs

Describe any design decisions, lock strategies, wire protocol implications, or cross-platform constraints considered:

## Verification & Testing

Detail the automated and manual verification performed:
- [ ] Unit tests pass: `go test -race ./...`
- [ ] Integration tests pass (if modifying drivers): `go test -tags=integration ./...`
- [ ] Tested against target database (PostgreSQL, MySQL, SQLite, MongoDB)
- [ ] Verified Git hook behavior (`post-checkout` / `post-merge`)

## Checklist

- [ ] Code complies with [AGENTS.md](AGENTS.md) Go standards (context propagation, error wrapping).
- [ ] Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/).
- [ ] Documentation (`README.md`, `ARCHITECTURE.md`, `docs/`) updated if user-facing behavior changed.
