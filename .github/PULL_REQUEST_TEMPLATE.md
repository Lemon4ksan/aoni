## Description

Provide a concise summary of the change. Link the related issue below.

Fixes # (issue)

## Type of change

- [ ] Bug fix (non-breaking change which fixes an issue)
- [ ] New feature (non-breaking change which adds functionality)
- [ ] Breaking change (fix or feature that would cause existing functionality to break)
- [ ] Performance improvement
- [ ] Refactoring (no functional changes)
- [ ] Documentation update

## Checklist

- [ ] I have run `make lint` — no new linter errors
- [ ] I have run `make race` — all tests pass with the race detector
- [ ] New functionality is covered by tests
- [ ] Commits are atomic and have clear messages
- [ ] Public API changes are documented in `doc.go` and/or `README.md`
- [ ] This change is generic enough for a shared HTTP client library (no app-specific business logic)

## How has this been tested?

Describe how you verified the change works. Include relevant test names or benchmark results if applicable.

```
go test -run TestXxx -v ./...
```

## Additional context

Add any other context, screenshots, or benchmark comparisons here.
