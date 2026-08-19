// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package server

import "golang.org/x/crypto/ssh"

// Pty describes pseudo-terminal allocation parameters and terminal modes
// requested by an SSH client during interactive session negotiation.
type Pty struct {
	Term   string
	Width  int
	Height int
	Modes  ssh.TerminalModes
}
