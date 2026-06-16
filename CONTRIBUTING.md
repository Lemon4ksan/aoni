# Contributing to aoni

Thank you for considering a contribution! aoni is a generic, middleware-driven HTTP client framework engineered for stability under industrial network loads. We welcome bug fixes, performance improvements, and features that benefit the
broad user base.

## Ground Rules

- **No application-specific logic.** aoni is a library. PRs that embed narrow business rules or proprietary proxy protocols will be closed.
- **Stability first.** Every change should leave the library at least as robust as before.
- **Backward compatibility.** Public API breaks require a major version bump and a compelling reason.

## Getting Started

```bash
git clone https://github.com/lemon4ksan/aoni
cd aoni
go mod download
make race   # run tests with race detector
make lint   # run golangci-lint
```

## Workflow

1. **Fork** the repository and create a feature branch from `main`.
2. Write your code following the existing style (check `.golangci.yml`).
3. Add or update tests — aim for `make race` to pass cleanly.
4. Run `make lint` and fix any new warnings.
5. Open a Pull Request against `main` using the provided PR template.

## Commit Style

Use conventional commits:

```
feat: add circuit breaker half-open state timeout
fix: prevent socket leak in multiReadBody on GC collection
perf: recycle byte buffers in RawDecoder
docs: document Unwrap chain contract
```

## Tests

| Command | Purpose |
|---------|---------|
| `make test` | Fast unit tests |
| `make race` | Tests with `-race` and 30 s timeout |
| `make cover` | Coverage report (opens in browser) |
| `go test -bench=. -benchmem ./...` | Benchmarks |

New public features **must** include tests. Bug fixes **should** include a
regression test.

## Code Style

- Run `gofmt` before committing (golangci-lint enforces this).
- Keep exported identifiers documented with a godoc comment.
- Prefer explicit error wrapping with `%w`.
- Avoid `reflect` where an interface-based approach works.

## Questions

Open a [Discussion](https://github.com/lemon4ksan/aoni/discussions) rather than
an issue for questions or design ideas.
