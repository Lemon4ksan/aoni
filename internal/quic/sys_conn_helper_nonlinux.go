// Copyright (c) 2016 the quic-go authors. All rights reserved.
// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build !linux

package quic

func forceSetReceiveBuffer(c any, bytes int) error { return nil }
func forceSetSendBuffer(c any, bytes int) error    { return nil }

func appendUDPSegmentSizeMsg([]byte, uint16) []byte { return nil }
func isGSOError(error) bool                         { return false }
func isPermissionError(err error) bool              { return false }
