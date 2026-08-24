# 00 — Canonical Service Blueprint

This directory demonstrates the **Golden Standard & Canonical Architectural Blueprint** for writing service clients and API wrappers in Go using the **Aoni** networking engine.

---

## 🎯 Key Architectural Pillars Demonstrated

1. **Decoupled Engine via `aoni.RequestDoer`**:
   - The service constructor accepts `aoni.RequestDoer` (interface), allowing callers to pass either `fast.NewClient()` (zero-alloc line speed) or standard `aoni.NewClient()`.
   - If `nil` is provided, it automatically defaults to `fast.NewClient()`.

2. **No Direct `net/http` Usage**:
   - Uses high-level, type-safe generics from `github.com/lemon4ksan/aoni/request` (`request.GetTo[T]`, `request.PostTo[T]`).

3. **Per-Request Modifiers (`aoni/mod`)**:
   - Flexible composition of headers, queries, timeouts, and auth tokens without mutating shared client state (`mod.WithBearerAuth`, `mod.WithFormValues`, `mod.WithQuery`).

4. **Idiomatic Error Classification (`aoni.Is*`)**:
   - Evaluates HTTP response errors using typed helpers (`aoni.IsNotFound`, `aoni.IsUnauthorized`, `aoni.IsRateLimited`, `aoni.IsForbidden`).

---

## 📁 File Structure

File | Purpose
:--- | :---
[`client.go`](client.go) | Canonical service struct and constructor pattern (`NewUserService`).
[`models.go`](models.go) | Typed DTO structs (`UserDTO`, `CreateUserRequest`, `LoginResponse`).
[`methods.go`](methods.go) | Implementation of REST endpoints (GET, POST JSON, POST Form, Query).
[`main.go`](main.go) | Self-contained runnable demonstration running against a test HTTP mock.

---

## 🏃 Running the Example

```bash
go run ./examples/00_canonical_service
```
