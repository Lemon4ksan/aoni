// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package x provides supplementary protocols, vendor database connectors, and ecosystem extensions for the aoni networking stack.
//
// Unlike core aoni, which strictly adheres to IETF RFCs and W3C/Chromium networking standards to guarantee immutable API stability (Evergreen v1),
// the aoni/x sub-module houses higher-level application protocols, evolving specifications, and third-party vendor integrations:
//
//   - [github.com/lemon4ksan/aoni/x/socketio]: Full client implementation for Socket.IO v5 / Engine.IO v4 over aoni WebSockets.
//   - [github.com/lemon4ksan/aoni/x/geoip]: Fast MaxMind MMDB GeoIP2 / GeoLite2 geolocation database lookup.
package x
