// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cookie provides proxy-isolated cookie jars, Netscape exports, and persistence storage engines.
//
// Specification Adherence:
// Conforms to IETF RFC 6265 (HTTP State Management Mechanism) and RFC 6265bis
// (Cookies: HTTP State Management Mechanism - CHIPS Partitioned Cookies).
//
// # Key Components
//
//   - [ProxyIsolatedJar]: Thread-safe cookie jar isolating cookies by proxy endpoint to prevent cross-session leakage.
//   - [SQLStorage]: SQLite/PostgreSQL persistence backend for long-lived cookie sessions.
//   - [JSONFileStorage]: Lightweight JSON file storage engine for session persistence.
//   - [Export], [Import], [ExportJSON], [ImportJSON]: Import and export cookie states across clients.
//   - [Mirror]: Synchronize specific cookies across multiple target URLs.
package cookie
