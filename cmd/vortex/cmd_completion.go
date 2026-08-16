// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// CmdCompletion outputs shell autocompletion scripts for Bash, Zsh, Fish, and PowerShell.
type CmdCompletion struct{}

func (c *CmdCompletion) Name() string      { return "completion" }
func (c *CmdCompletion) Aliases() []string { return []string{"autocomplete"} }
func (c *CmdCompletion) Synopsis() string {
	return "Generate shell autocompletion script (bash, zsh, fish, powershell)"
}
func (c *CmdCompletion) Usage() string { return "vortex completion [bash|zsh|fish|powershell]" }

func (c *CmdCompletion) Run(_ context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintf(stderr, "Error: shell type required (bash, zsh, fish, powershell)\n\n")
		fmt.Fprintf(stderr, "Usage:\n  %s\n", c.Usage())
		return errors.New("missing shell argument")
	}

	shell := strings.ToLower(args[0])

	switch shell {
	case "bash":
		fmt.Fprint(stdout, bashCompletionScript)
	case "zsh":
		fmt.Fprint(stdout, zshCompletionScript)
	case "powershell", "pwsh":
		fmt.Fprint(stdout, powershellCompletionScript)
	case "fish":
		fmt.Fprint(stdout, fishCompletionScript)
	default:
		return fmt.Errorf("unsupported shell %q. Supported: bash, zsh, fish, powershell", shell)
	}

	return nil
}

const bashCompletionScript = `# bash completion for vortex
_vortex_completions()
{
    local cur prev words cword
    _init_completion || return

    local commands="autopilot status init work gen harness check diff review accept log oapi proto bench prof cover list explain example pgo completion"

    if [[ ${cword} -eq 1 ]]; then
        COMPREPLY=( $(compgen -W "${commands}" -- ${cur}) )
        return 0
    fi
}
complete -F _vortex_completions vortex
`

const zshCompletionScript = `#compdef vortex

_vortex() {
    local -a commands
    commands=(
        'autopilot:Intelligent Auto-Pilot runner'
        'status:Display workspace contract status'
        'init:Initialize new workspace configuration'
        'work:Multi-repo workspace orchestrator'
        'gen:Compile and generate zero-allocation Go clients'
        'harness:Generate test and benchmark harness'
        'check:Static contract linter and inspector'
        'diff:Display unified visual diff'
        'review:Interactive contract diff reviewer'
        'accept:Accept proposal and merge upstream'
        'log:Print chronological contract commit log'
        'oapi:Export OpenAPI 3.1 schema'
        'proto:Convert contracts to Protobuf IDL'
        'bench:Run zero-allocation performance benchmarks'
        'prof:Silicon hardware and latency profiler'
        'cover:Analyze test coverage against thresholds'
        'pgo:Capture Profile-Guided Optimization profile'
        'completion:Generate shell autocompletion script'
    )

    _arguments \
        '1: :->command' \
        '*:: :->args'

    case $state in
        command)
            _describe -t commands 'vortex commands' commands
            ;;
    esac
}

_vortex "$@"
`

const powershellCompletionScript = `# PowerShell completion for vortex
Register-ArgumentCompleter -Native -CommandName vortex -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    $commands = @(
        [Management.Automation.CompletionResult]::new('autopilot', 'autopilot', 'Command', 'Intelligent Auto-Pilot runner'),
        [Management.Automation.CompletionResult]::new('status', 'status', 'Command', 'Display workspace contract status'),
        [Management.Automation.CompletionResult]::new('work', 'work', 'Command', 'Multi-repo workspace orchestrator'),
        [Management.Automation.CompletionResult]::new('gen', 'gen', 'Command', 'Compile and generate Go clients'),
        [Management.Automation.CompletionResult]::new('harness', 'harness', 'Command', 'Generate test and benchmark harness'),
        [Management.Automation.CompletionResult]::new('check', 'check', 'Command', 'Static contract linter and inspector'),
        [Management.Automation.CompletionResult]::new('diff', 'diff', 'Command', 'Display unified visual diff'),
        [Management.Automation.CompletionResult]::new('prof', 'prof', 'Command', 'Silicon hardware & latency profiler'),
        [Management.Automation.CompletionResult]::new('bench', 'bench', 'Command', 'Run zero-allocation benchmarks'),
        [Management.Automation.CompletionResult]::new('cover', 'cover', 'Command', 'Analyze test coverage'),
        [Management.Automation.CompletionResult]::new('pgo', 'pgo', 'Command', 'Capture Profile-Guided Optimization profile'),
        [Management.Automation.CompletionResult]::new('completion', 'completion', 'Command', 'Generate shell completion')
    )

    $commands | Where-Object { $_.CompletionText -like "$wordToComplete*" }
}
`

const fishCompletionScript = `# fish completion for vortex
complete -c vortex -f
complete -c vortex -n "__fish_use_subcommand" -a "autopilot" -d "Intelligent Auto-Pilot runner"
complete -c vortex -n "__fish_use_subcommand" -a "status" -d "Display workspace contract status"
complete -c vortex -n "__fish_use_subcommand" -a "work" -d "Multi-repo workspace orchestrator"
complete -c vortex -n "__fish_use_subcommand" -a "gen" -d "Compile and generate Go clients"
complete -c vortex -n "__fish_use_subcommand" -a "harness" -d "Generate test and benchmark harness"
complete -c vortex -n "__fish_use_subcommand" -a "check" -d "Static contract linter and inspector"
complete -c vortex -n "__fish_use_subcommand" -a "prof" -d "Silicon hardware & latency profiler"
complete -c vortex -n "__fish_use_subcommand" -a "pgo" -d "Capture Profile-Guided Optimization profile"
complete -c vortex -n "__fish_use_subcommand" -a "completion" -d "Generate shell completion"
`
