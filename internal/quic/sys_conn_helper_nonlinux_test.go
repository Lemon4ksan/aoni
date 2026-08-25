// Copyright (c) 2016 the quic-go authors. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux

package quic

import "errors"

var (
	errGSO          = errors.New("fake GSO error")
	errNotPermitted = errors.New("fake not permitted error")
)
