// Copyright 2026 The Aoni Authors. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/internal/codegen/project"
)

// CmdConfig manages .vortex.yml workspace settings, defaults, lint rules, and plugins.
type CmdConfig struct{}

func (c *CmdConfig) Name() string      { return "config" }
func (c *CmdConfig) Aliases() []string { return []string{"cfg", "conf"} }
func (c *CmdConfig) Synopsis() string {
	return "View, query, and modify .vortex.yml workspace settings and lint rules"
}

func (c *CmdConfig) Usage() string {
	return "vortex config [list|get|set|unset|lint|plugin] [key] [value] [flags]"
}

func (c *CmdConfig) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		dirFlag      = fs.String("dir", "", "Target workspace directory (default: current repository root)")
		jsonFlag     = fs.Bool("json", false, "Output in JSON format")
		contractFlag = fs.String("contract", "", "Target contract name for plugin or contract-specific settings")
		outFlag      = fs.String("out", "", "Output path for plugin targets")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex config — Workspace Configuration Manager\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex config [list] [--json]                         List all workspace settings\n")
		fmt.Fprintf(
			stderr,
			"  vortex config get <key>                               Get specific configuration property\n",
		)
		fmt.Fprintf(stderr, "  vortex config set <key> <value>                       Set configuration property\n")
		fmt.Fprintf(stderr, "  vortex config unset <key>                             Reset configuration property\n")
		fmt.Fprintf(stderr, "  vortex config lint <disable|enable|ignore|strict>     Configure lint rules\n")
		fmt.Fprintf(stderr, "  vortex config plugin <add|rm|list> [target]           Manage polyglot plugins\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  vortex config set defaults.casing snake_case\n")
		fmt.Fprintf(stderr, "  vortex config set defaults.engine fast\n")
		fmt.Fprintf(stderr, "  vortex config set defaults.retry 3\n")
		fmt.Fprintf(stderr, "  vortex config set defaults.timeout 15s\n")
		fmt.Fprintf(stderr, "  vortex config lint disable S001 W002\n")
		fmt.Fprintf(stderr, "  vortex config plugin add ts --out=src/api.ts --contract=Market\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	var flags, nonFlags []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			if (arg == "-dir" || arg == "-contract" || arg == "-out") &&
				i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				flags = append(flags, args[i+1])
				i++
			}
		} else {
			nonFlags = append(nonFlags, arg)
		}
	}

	if err := fs.Parse(append(flags, nonFlags...)); err != nil {
		return err
	}

	targetDir := *dirFlag
	if targetDir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			cwd = "."
		}

		rootDir, _, _ := project.FindRoot(cwd)
		targetDir = rootDir
	}

	cfg, err := project.Load(targetDir)
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	action := "list"

	var keyArg, valArg string

	posArgs := fs.Args()
	if len(posArgs) > 0 {
		first := strings.ToLower(posArgs[0])
		switch first {
		case "list", "ls":
			action = "list"
		case "get":
			action = "get"

			if len(posArgs) > 1 {
				keyArg = posArgs[1]
			}

		case "set":
			action = "set"

			if len(posArgs) > 1 {
				keyArg = posArgs[1]
			}

			if len(posArgs) > 2 {
				valArg = posArgs[2]
			}

		case "unset", "rm", "del", "delete":
			action = "unset"

			if len(posArgs) > 1 {
				keyArg = posArgs[1]
			}

		case "lint":
			action = "lint"

			if len(posArgs) > 1 {
				keyArg = posArgs[1]
			}

			if len(posArgs) > 2 {
				valArg = strings.Join(posArgs[2:], " ")
			}

		case "plugin", "plugins":
			action = "plugin"

			if len(posArgs) > 1 {
				keyArg = posArgs[1]
			}

			if len(posArgs) > 2 {
				valArg = posArgs[2]
			}

		default:
			// If contains dot, e.g. "defaults.casing snake_case", treat as get/set
			if strings.Contains(posArgs[0], ".") {
				keyArg = posArgs[0]
				if len(posArgs) > 1 {
					action = "set"
					valArg = posArgs[1]
				} else {
					action = "get"
				}
			} else {
				action = "get"
				keyArg = posArgs[0]
			}
		}
	}

	switch action {
	case "list":
		return c.runList(stdout, cfg, *jsonFlag)
	case "get":
		return c.runGet(stdout, cfg, keyArg)
	case "set":
		return c.runSet(stdout, cfg, keyArg, valArg)
	case "unset":
		return c.runUnset(stdout, cfg, keyArg)
	case "lint":
		return c.runLint(stdout, cfg, keyArg, valArg)
	case "plugin":
		return c.runPlugin(stdout, cfg, keyArg, valArg, *outFlag, *contractFlag)
	default:
		return fmt.Errorf("unknown action %q", action)
	}
}

func (c *CmdConfig) runList(stdout io.Writer, cfg *project.Config, jsonOut bool) error {
	if jsonOut {
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return err
		}

		fmt.Fprintln(stdout, string(data))

		return nil
	}

	fmt.Fprintf(stdout, "⚡ Vortex Workspace Configuration (%s)\n\n", cfg.ConfigPath)

	fmt.Fprintf(stdout, "● Defaults:\n")
	fmt.Fprintf(stdout, "  • Casing:  %s\n", defaultStr(cfg.Defaults.Casing, "snake_case"))
	fmt.Fprintf(stdout, "  • Engine:  %s\n", defaultStr(cfg.Defaults.Engine, "fast"))

	if cfg.Defaults.Retry > 0 {
		fmt.Fprintf(stdout, "  • Retry:   %d\n", cfg.Defaults.Retry)
	}

	if cfg.Defaults.Timeout != "" {
		fmt.Fprintf(stdout, "  • Timeout: %s\n", cfg.Defaults.Timeout)
	}

	if cfg.Defaults.Persona != "" {
		fmt.Fprintf(stdout, "  • Persona: %s\n", cfg.Defaults.Persona)
	}

	fmt.Fprintf(stdout, "\n● Lint Rules:\n")
	fmt.Fprintf(stdout, "  • Strict:  %v\n", cfg.Lint.Strict)

	if len(cfg.Lint.Disable) > 0 {
		fmt.Fprintf(stdout, "  • Disable: %s\n", strings.Join(cfg.Lint.Disable, ", "))
	}

	if len(cfg.Lint.Ignore) > 0 {
		fmt.Fprintf(stdout, "  • Ignore:  %s\n", strings.Join(cfg.Lint.Ignore, ", "))
	}

	if len(cfg.Ignore) > 0 {
		fmt.Fprintf(stdout, "  • Global:  %s\n", strings.Join(cfg.Ignore, ", "))
	}

	fmt.Fprintf(stdout, "\n● Contracts (%d configured):\n", len(cfg.Contracts))

	for _, ct := range cfg.Contracts {
		src := "(none)"
		if ct.Upstream != nil && ct.Upstream.Source != "" {
			src = ct.Upstream.Source
		}

		fmt.Fprintf(stdout, "  ↳ %-14s -> %s [upstream: %s]\n", ct.Name, ct.File, src)
	}

	return nil
}

func (c *CmdConfig) runGet(stdout io.Writer, cfg *project.Config, key string) error {
	if key == "" {
		return errors.New("usage: vortex config get <key>")
	}

	val, err := getProperty(cfg, key)
	if err != nil {
		return err
	}

	fmt.Fprintln(stdout, val)

	return nil
}

func (c *CmdConfig) runSet(stdout io.Writer, cfg *project.Config, key, val string) error {
	if key == "" || val == "" {
		return errors.New("usage: vortex config set <key> <value>")
	}

	if err := setProperty(cfg, key, val); err != nil {
		return err
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving configuration: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Set %s = %s\n", key, val)

	return nil
}

func (c *CmdConfig) runUnset(stdout io.Writer, cfg *project.Config, key string) error {
	if key == "" {
		return errors.New("usage: vortex config unset <key>")
	}

	if err := unsetProperty(cfg, key); err != nil {
		return err
	}

	if err := cfg.Save(); err != nil {
		return fmt.Errorf("saving configuration: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Unset %s\n", key)

	return nil
}

func (c *CmdConfig) runLint(stdout io.Writer, cfg *project.Config, action, rulesStr string) error {
	rules := strings.Fields(rulesStr)

	switch strings.ToLower(action) {
	case "strict":
		val := true
		if len(rules) > 0 {
			b, err := strconv.ParseBool(rules[0])
			if err != nil {
				return fmt.Errorf("invalid boolean value %q: %w", rules[0], err)
			}

			val = b
		}

		cfg.Lint.Strict = val
		_ = cfg.Save()

		fmt.Fprintf(stdout, "✔ Set lint.strict = %v\n", val)

		return nil

	case "disable":
		if len(rules) == 0 {
			return errors.New("usage: vortex config lint disable <rule1> [rule2...]")
		}

		for _, r := range rules {
			if !containsString(cfg.Lint.Disable, r) {
				cfg.Lint.Disable = append(cfg.Lint.Disable, r)
			}

			cfg.Lint.Enable = removeString(cfg.Lint.Enable, r)
		}

		_ = cfg.Save()

		fmt.Fprintf(stdout, "✔ Disabled lint rules: %s\n", strings.Join(rules, ", "))

		return nil

	case "enable":
		if len(rules) == 0 {
			return errors.New("usage: vortex config lint enable <rule1> [rule2...]")
		}

		for _, r := range rules {
			cfg.Lint.Disable = removeString(cfg.Lint.Disable, r)
			if !containsString(cfg.Lint.Enable, r) {
				cfg.Lint.Enable = append(cfg.Lint.Enable, r)
			}
		}

		_ = cfg.Save()

		fmt.Fprintf(stdout, "✔ Enabled lint rules: %s\n", strings.Join(rules, ", "))

		return nil

	case "ignore":
		if len(rules) == 0 {
			return errors.New("usage: vortex config lint ignore <rule1> [rule2...]")
		}

		for _, r := range rules {
			if !containsString(cfg.Lint.Ignore, r) {
				cfg.Lint.Ignore = append(cfg.Lint.Ignore, r)
			}
		}

		_ = cfg.Save()

		fmt.Fprintf(stdout, "✔ Ignored lint rules: %s\n", strings.Join(rules, ", "))

		return nil

	default:
		return fmt.Errorf("unknown lint action %q (supported: disable, enable, ignore, strict)", action)
	}
}

func (c *CmdConfig) runPlugin(
	stdout io.Writer,
	cfg *project.Config,
	action, target, outPath, contractName string,
) error {
	switch strings.ToLower(action) {
	case "add":
		if target == "" || outPath == "" {
			return errors.New("usage: vortex config plugin add <target> --out=<path> [--contract=<name>]")
		}

		if contractName != "" {
			idx := findContractIndex(cfg, contractName)
			if idx == -1 {
				return fmt.Errorf("contract %q not found", contractName)
			}

			cfg.Contracts[idx].Plugins = append(cfg.Contracts[idx].Plugins, project.PluginConfig{
				Name: target,
				Out:  outPath,
			})
		}

		_ = cfg.Save()

		fmt.Fprintf(stdout, "✔ Added plugin target %s (out: %s)\n", target, outPath)

		return nil

	case "rm", "remove":
		if target == "" {
			return errors.New("usage: vortex config plugin rm <target> [--contract=<name>]")
		}

		if contractName != "" {
			idx := findContractIndex(cfg, contractName)
			if idx != -1 {
				var filtered []project.PluginConfig
				for _, p := range cfg.Contracts[idx].Plugins {
					if !strings.EqualFold(p.Name, target) {
						filtered = append(filtered, p)
					}
				}

				cfg.Contracts[idx].Plugins = filtered
			}
		}

		_ = cfg.Save()

		fmt.Fprintf(stdout, "✔ Removed plugin target %s\n", target)

		return nil

	case "list", "":
		fmt.Fprintf(stdout, "● Configured Plugins:\n")

		hasAny := false
		for _, ct := range cfg.Contracts {
			for _, p := range ct.Plugins {
				hasAny = true

				fmt.Fprintf(stdout, "  • [%s] %s -> %s\n", ct.Name, p.Name, p.Out)
			}
		}

		if !hasAny {
			fmt.Fprintf(stdout, "  (no plugins configured)\n")
		}

		return nil

	default:
		return fmt.Errorf("unknown plugin action %q (supported: add, rm, list)", action)
	}
}

func getProperty(cfg *project.Config, key string) (string, error) {
	k := strings.ToLower(strings.TrimSpace(key))
	switch k {
	case "version":
		return strconv.Itoa(cfg.Version), nil
	case "defaults.casing", "casing":
		return cfg.Defaults.Casing, nil
	case "defaults.engine", "engine":
		return cfg.Defaults.Engine, nil
	case "defaults.retry", "retry":
		return strconv.Itoa(cfg.Defaults.Retry), nil
	case "defaults.timeout", "timeout":
		return cfg.Defaults.Timeout, nil
	case "defaults.persona", "persona":
		return cfg.Defaults.Persona, nil
	case "defaults.tlsspec", "tlsspec":
		return cfg.Defaults.TLSSpec, nil
	case "defaults.harness", "harness":
		return strconv.FormatBool(cfg.Defaults.Harness), nil
	case "defaults.mock", "mock":
		return strconv.FormatBool(cfg.Defaults.Mock), nil
	case "lint.strict":
		return strconv.FormatBool(cfg.Lint.Strict), nil
	case "lint.disable":
		return strings.Join(cfg.Lint.Disable, ", "), nil
	case "lint.ignore":
		return strings.Join(cfg.Lint.Ignore, ", "), nil
	case "lint.enable":
		return strings.Join(cfg.Lint.Enable, ", "), nil
	case "ignore":
		return strings.Join(cfg.Ignore, ", "), nil
	case "coverage.min":
		return fmt.Sprintf("%.1f", cfg.Coverage.Min), nil
	case "coverage.file":
		return cfg.Coverage.File, nil
	case "export.openapi.out":
		return cfg.Export.OpenAPI.Out, nil
	}

	return "", fmt.Errorf("unrecognized configuration key %q", key)
}

func setProperty(cfg *project.Config, key, val string) error {
	k := strings.ToLower(strings.TrimSpace(key))
	switch k {
	case "defaults.casing", "casing":
		val = strings.ToLower(strings.TrimSpace(val))
		if val != "snake_case" && val != "camelcase" && val != "kebab-case" {
			return errors.New("casing must be one of: snake_case, camelCase, kebab-case")
		}

		cfg.Defaults.Casing = val

		return nil

	case "defaults.engine", "engine":
		val = strings.ToLower(strings.TrimSpace(val))
		if val != "fast" && val != "standard" {
			return errors.New("engine must be one of: fast, standard")
		}

		cfg.Defaults.Engine = val

		return nil

	case "defaults.retry", "retry":
		n, err := strconv.Atoi(val)
		if err != nil || n < 0 {
			return errors.New("retry must be a non-negative integer")
		}

		cfg.Defaults.Retry = n

		return nil

	case "defaults.timeout", "timeout":
		if _, err := time.ParseDuration(val); err != nil {
			return fmt.Errorf("invalid duration format %q: %w", val, err)
		}

		cfg.Defaults.Timeout = val

		return nil

	case "defaults.persona", "persona":
		cfg.Defaults.Persona = val
		return nil

	case "defaults.tlsspec", "tlsspec":
		cfg.Defaults.TLSSpec = val
		return nil

	case "defaults.harness", "harness":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}

		cfg.Defaults.Harness = b

		return nil

	case "defaults.mock", "mock":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}

		cfg.Defaults.Mock = b

		return nil

	case "lint.strict":
		b, err := strconv.ParseBool(val)
		if err != nil {
			return err
		}

		cfg.Lint.Strict = b

		return nil

	case "coverage.min":
		f, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return err
		}

		cfg.Coverage.Min = f

		return nil

	case "coverage.file":
		cfg.Coverage.File = val
		return nil

	case "export.openapi.out":
		cfg.Export.OpenAPI.Out = val
		return nil
	}

	return fmt.Errorf("unrecognized configuration key %q", key)
}

func unsetProperty(cfg *project.Config, key string) error {
	k := strings.ToLower(strings.TrimSpace(key))
	switch k {
	case "defaults.casing", "casing":
		cfg.Defaults.Casing = ""
	case "defaults.engine", "engine":
		cfg.Defaults.Engine = ""
	case "defaults.retry", "retry":
		cfg.Defaults.Retry = 0
	case "defaults.timeout", "timeout":
		cfg.Defaults.Timeout = ""
	case "defaults.persona", "persona":
		cfg.Defaults.Persona = ""
	case "defaults.tlsspec", "tlsspec":
		cfg.Defaults.TLSSpec = ""
	case "defaults.harness", "harness":
		cfg.Defaults.Harness = false
	case "defaults.mock", "mock":
		cfg.Defaults.Mock = false
	case "lint.strict":
		cfg.Lint.Strict = false
	case "lint.disable":
		cfg.Lint.Disable = nil
	case "lint.ignore":
		cfg.Lint.Ignore = nil
	case "lint.enable":
		cfg.Lint.Enable = nil
	case "ignore":
		cfg.Ignore = nil
	default:
		return fmt.Errorf("unrecognized configuration key %q", key)
	}

	return nil
}

func findContractIndex(cfg *project.Config, target string) int {
	for i, ct := range cfg.Contracts {
		if strings.EqualFold(ct.Name, target) || strings.EqualFold(ct.Package, target) ||
			strings.EqualFold(ct.File, target) {
			return i
		}
	}

	return -1
}

func defaultStr(val, def string) string {
	if val == "" {
		return def
	}

	return val
}

func containsString(slice []string, val string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, val) {
			return true
		}
	}

	return false
}

func removeString(slice []string, val string) []string {
	var res []string
	for _, s := range slice {
		if !strings.EqualFold(s, val) {
			res = append(res, s)
		}
	}

	return res
}
