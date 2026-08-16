# GEMINI.md — Development Guidelines & AI Assistant Protocol

This document outlines the architecture, coding standards, build/test commands, and interaction rules for AI assistants and software engineers working in the **aoni** repository.

## 1. Project Overview

**`aoni`** is a unified, ultra-high-performance Internet Protocol engine for Go. It consolidates modern IETF RFC standards, W3C specifications, and Chromium-grade network resilience mechanisms into a single, profile-driven zero-allocation architecture.

> **Core Engineering Manifesto**:
> _«Как только байты должны покинуть одну машину и попасть на другую — это произойдет с 0 аллокаций, на максимальной скорости кремния, без рассинхрона типов и без шанса быть заблокированным WAF»._
> _(«The moment bytes leave one machine to reach another — it happens with 0 allocations, at silicon line speed, with zero type drift, and zero chance of WAF interception.»)_

### Key Capabilities & Architectural Pillars
- **Dual Engines under a Single Interface**:
  - `aoni.Client` — 100% `net/http` compatibility, full middleware chain support, seamless standard ecosystem integration.
  - `aoni/fast` (`fast.Client`) — Native engine built on top of `fasthttp` + H2/H3 for extreme silicon throughput (1.5M+ RPS, absolute zero allocations under parallel I/O).
- **Chromium-Grade Resilience & RFC Compliance**:
  - **Happy Eyeballs v3**: Protocol racing across H3/H2/H1.
  - **Auto-Recovery**: Automatic connection re-routing on HTTP 421 (Misdirected Request), HTTP 408 (Request Timeout), HTTP 425 (Too Early / 0-RTT rejection).
  - **Dynamic Backoff**: Cooldown mechanisms for broken `Alt-Svc` endpoints.
- **TLS Evasion & Stealth Fingerprinting**:
  - Full browser impersonation (Chrome, Firefox, Safari).
  - TLS 1.3 Encrypted Client Hello (ECH / RFC 9460 via DoH/DoQ).
  - Pure-Go JA3/JA4/JA4H fingerprint calculation and emulation.
  - TCP/IP p0f stack spoofing and HPACK framing control.
- **Generics-First Ergonomics & Codecs**:
  - Type-safe single-line calls via `fluent.FetchTo[T]` and `request.GetTo[T]`.
  - Native decoders for JSON, XML, Protobuf, and gRPC-Web (5-byte framing & trailer validation).
- **Real-Time Protocols**: WebSockets over H2 Extended CONNECT (RFC 8441), Socket.IO v5 / Engine.IO v4, SSE, and NDJSON streaming.
- **Proxy Isolation & Utilities**: Proxy-isolated Cookie Jars (`ProxyIsolatedCookieJar`), proxy rotators, IPv6 subnet rotators, and DoH/DoT/DoQ DNS resolvers.

## 2. Repository Layout

```text
aoni/
├── client.go, config.go, dial.go ...  // Core standard aoni client (net/http compatible)
├── option/                            // Functional client options (option.WithBaseURL, option.WithChrome...)
├── mod/                               // Per-request modifiers (mod.WithVar, mod.WithHeader, mod.WithQuery...)
├── request/                           // High-level generic helpers (request.GetTo[T], PostTo, PostProtoTo)
├── fluent/                            // Chainable Request Builder API (fluent.FetchTo[T], fluent.R)
├── fast/                              // Ultra-fast client engine built on fasthttp
├── cookie/                            // Proxy-isolated cookie jars, Netscape format, RFC 6265 path sorting
├── fingerprint/                       // TLS/JA4/p0f evasion, HTTP/2 framing, CDN padding
├── netutil/                           // Proxy rotators, DoH/DoT/DoQ resolvers, ECH, IPv6 subnet rotators
├── codec/                             // Response decoders (JSON, Proto, gRPC-Web, XML) & url.Values encoders
├── realtime/                          // WebSockets, Socket.IO v5, SSE & NDJSON streams
├── resiliency/                        // Response caching, WAF challenge solvers, Circuit Breakers, Load Balancers
├── telemetry/                         // HAR generators, EWMA latency trackers, embedded web inspector
├── cmd/                               // CLI utilities (coverage analyzer, OpenAPI code generator)
├── docs/                              // Technical architecture docs (NETWORK_STACK.md, VOODOO.md, COOKBOOK.md)
├── examples/                          // Runnable usage & evasion examples
└── scripts/                           // TLS spec comparison & browser version update scripts
```

## 3. Core Architectural Principles & Technical Constraints

1. **Strict Library Scope (No Application-Specific Logic)**:
   - `aoni` is a generic networking framework. Code **must not** contain narrow business logic, hardcoded app domain rules, or proprietary non-standard proxy protocols.
2. **Chromium-Grade Network Stability**:
   - Outbound requests and transport layers must maintain industrial fault tolerance under un-reliable networks. Always handle connection pool invalidation, stale keep-alives, socket leaks, and protocol fallbacks cleanly.
3. **Zero Performance Regressions & High Readability**:
   - **Zero Allocation Mindset**: Avoid heap allocations in high-throughput hot paths (`fast`, `codec`, `fluent`, `fingerprint`). Utilize `sync.Pool` for buffers and builders. Pre-allocate slices (`make([]T, 0, capacity)`).
   - **No Hidden Magic / High Readability**: Performance optimizations must not obscure code clarity or introduce cryptic side effects. Code readability and clean interface abstractions remain paramount.
   - Avoid `reflect` wherever generics or interface-based dispatch suffice.
4. **Strict IETF RFC & W3C Standards Adherence**:
   - All protocol handlers, HTTP status code fallbacks (421, 408, 425), HPACK/QPACK framing, cookie sorting (RFC 6265), and header casing must conform rigorously to IETF RFCs.
5. **Explicit Resource & Memory Management**:
   - Guarantee proper socket cleanup and response body closing (`defer resp.Body.Close()`). Always reset and release pooled objects before returning them to `sync.Pool`.
6. **Backward Compatibility**:
   - Public package APIs must remain stable. Breaking changes require major architectural justification.
7. **Style Guide**:
   - Refer to the [docs/STYLE_GUIDE.md](docs/STYLE_GUIDE.md) for code style and formatting guidelines.

## 4. Code Style & Quality Standards

- **Go Version**: 1.25.4+ (leverage modern language capabilities and generics).
- **Documentation**: Every exported identifier (type, struct, interface, function, constant) **must** have clear Godoc documentation.
- **Error Handling**: Use explicit error wrapping with `%w` (`fmt.Errorf("...: %w", err)`). Never swallow or ignore errors silently.
- **Code Formatting & Imports**:
  - Code must be formatted using `gofmt` and `gci` prior to committing.
  - Maximum line length is 120 characters (`golines`).
  - Import grouping order: 1) Standard library, 2) Third-party dependencies, 3) Internal imports (`github.com/lemon4ksan/aoni`).

### Linter Enforcement
The repository uses `.golangci.yml` with a strict suite of linters (`errcheck`, `govet`, `staticcheck`, `bodyclose`, `noctx`, `gosec`, `revive`, `wsl_v5`, `gocritic`, `prealloc`, `perfsprint`).

Run linter check:
```bash
make lint
```

Auto-format code and apply linter fixes:
```bash
make format
```

## 5. Development & Testing Commands

| Command | Purpose |
| :--- | :--- |
| `make test` | Run fast unit tests across all library packages |
| `make race` | Run full unit test suite with race detector (`-race -timeout 60s`) |
| `make cover` | Calculate exact core library coverage report (`cmd/coverage`) |
| `make cover-html` | Generate coverage report and open interactive HTML report in browser |
| `make lint` | Execute `golangci-lint` code quality checks |
| `make format` | Auto-format Go code (`go fmt`, `addlicense`, `golangci-lint --fix`) |
| `make check-tls-spec` | Validate local TLS ClientHello profiles against `utls` specifications |
| `make update-browsers-apply` | Apply browser version and fingerprint updates |

For benchmarking:
```bash
go test -bench=Benchmark -benchmem ./...
```

## 6. Commit & Pull Request Guidelines

### Conventional Commits Format:
```text
<type>(<scope>): <short summary>

<detailed description of changes>
```

Commit Types:
- `feat`: New feature or capability
- `fix`: Bug fix
- `impr`: Performance optimization / allocation reduction
- `refactor`: Refactoring without behavioral changes
- `docs`: Documentation updates
- `test`: Adding or updating tests
- `chore`: Version updates, build tasks, or other non-code changes
- `git`: Git-related changes like merges
- `ci`: CI/CD pipeline changes

Example:
```text
feat(resilience): add HTTP 421 auto-recovery and cert target fallback

Implements connection re-routing on HTTP 421 status code.
- Auto-invalidates connection pool entry on cert mismatch.
- Adds test cases in resiliency suite.
```

Example for large squash commits:
```text
refactor(module1,module2): move execution core into internal

Submodule 1:
  - Feature 1
  - Feature 2

Submodule 2:
  - Feature 3
  - Feature 4
```

### Pre-PR Checklist:
1. `make format` passes cleanly without warnings.
2. `make race` passes all tests without data races.
3. New public types/functions are fully tested and documented in godoc.
4. No application-specific business logic is present.

## 7. AI Assistant Workflow & Rules

1. **Verification Mandatory**:
   - Before reporting a task complete, **always** run `make format` and `make race` (or appropriate `go test`) to verify there are no compilation errors, type mismatches, data races, or resource leaks.
2. **Preserve Comments & Context**:
   - Do not delete or mangle existing inline code comments and Godoc documentation unless rendered obsolete by code changes.
3. **No Performance Regressions**:
   - When modifying hot paths (`fast`, `codec`, `fluent`, `fingerprint`, `netutil`), ensure zero unnecessary memory allocations are introduced. Use pooled buffers (`sync.Pool`) and slice capacity hints.
4. **Maintain High Readability**:
   - Prefer clean, self-describing abstractions over overly obscure or overly clever micro-hacks. High performance must coexist with readable, maintainable Go code.
5. **No Symptom Masking**:
   - Never suppress errors, swallow exceptions, or alter failing assertions without identifying and resolving the underlying root cause.
6. **Anti-Blind Trust & Human Review Enforcement**:
   - The AI assistant is a pair-programming partner, not a substitute for human engineering vigilance.
   - If the user demonstrates blind reliance — blindly merging/submitting large architectural changes, refactors, or commits without inspecting diffs — the AI assistant **must explicitly remind the contributor to perform human code review**.
   - Prevent unverified "vibe coding" or blind AI delegation from degrading the precision and safety of the codebase.
7. **Clean PR & Commit Descriptions (No Artificial AI Formatting)**:
   - When generating Pull Request descriptions or git commit messages, never insert local IDE/agent links (`file:///...`), nested code ticks inside markdown links, or artificial AI boilerplate.
   - Keep PR descriptions and commit messages clean, human-crafted, concise, and immediately copy-pasteable into GitHub without post-processing.

