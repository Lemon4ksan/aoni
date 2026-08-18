// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package workspace

import (
	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
)

// Commands returns all workspace management commands configured with the application runner.
func Commands(runner base.AppRunner) []base.Command {
	return []base.Command{
		&CmdInit{},
		&CmdStatus{},
		&CmdConfig{},
		&CmdClean{},
		&CmdCompletion{},
		&CmdExplain{},
		&CmdExample{},
		&CmdList{},
		&CmdWork{runner: runner},
	}
}
