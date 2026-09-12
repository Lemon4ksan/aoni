# Codebase Style & Architecture Guide

This document defines coding standards, package layout, memory allocation guidelines, and the versioning policy for the `aoni` repository.

## 1. Principles
* **Progressive Disclosure**: Advanced resilience (Happy Eyeballs, WAF solving) is enabled via options without altering base call contracts.
* **Zero-Allocation Hot Paths**: `fast`, `codec`, `fingerprint` paths must maintain minimal heap footprint via `sync.Pool` and pre-allocation.
* **RFC Compliance**: Specifications (RFC 9460, 6265bis, 9112) take precedence in protocol framing and fallbacks.

## 2. Initialization & Memory
* **Constructors**: Use `New...` factory functions. Unkeyed struct literals are forbidden.
* **Options**: Use functional options (`option.With...`) immutably on `*aoni.Config`.
* **Pooling**: Pooled objects must implement `Reset()`. Limit capacities (e.g., discard byte buffers > 64 KB).

## 3. Naming & Typography
* **Acronym Casing**: Enforced strict casing (`HTTP`, `URL`, `TLS`, `DNS`, `QUIC`, `gRPC`, `H2`, `IPv6`).
* **Package Naming**: Single-word, lowercase, self-describing (`mod`, `codec`, `resiliency`).

## 4. Package Layout
* Files must not exceed 600–800 lines.
* Import blocks: Standard library, third-party dependencies, internal repository packages.

## 5. Documentation Blueprint
Every exported symbol must document:
1. Summary Line.
2. Context & Rationale.
3. Wire Representation (if applicable).
4. Code Example.
5. Invariants (Allocations, RFC Compliance).

## 6. API Versioning Policy
* **Current Stage (v0.x)**: Breaking changes permitted for architecture refinement and zero-alloc consistency.
* **Future Stage (v1.0.0+)**: Permanent backward compatibility. Additive evolution only.
