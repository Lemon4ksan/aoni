// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import "github.com/lemon4ksan/aoni/cmd/vortex/internal/base"

// Commands returns the slice of daily core workflow commands.
func Commands(runner base.AppRunner) []base.Command {
	return []base.Command{
		&CmdAutoPilot{runner: runner},
		&CmdGen{},
		&CmdCheck{},
		&CmdMock{},
		&CmdSmoke{},
		&CmdEnv{},
	}
}
