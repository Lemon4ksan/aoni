// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe

import "errors"

// ErrSocketNotTCP indicates that a diagnostic operation requires an active TCP connection.
var ErrSocketNotTCP = errors.New("aoni/probe: connection is not a TCP socket")

// ErrUnsupportedPlatform is returned when a diagnostic feature is not supported on the host OS.
var ErrUnsupportedPlatform = errors.New("aoni/probe: diagnostic operation unsupported on this platform")
