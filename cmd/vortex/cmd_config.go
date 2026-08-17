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
		fmt.Fprintf(
			stderr,
			"  vortex config secret [list|add|rm] [type] [key] [ENV] Configure secret & credential rules\n",
		)
		fmt.Fprintf(stderr, "  vortex config plugin <add|rm|list> [target]           Manage polyglot plugins\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(stderr, "  vortex config set defaults.casing snake_case\n")
		fmt.Fprintf(stderr, "  vortex config set defaults.engine fast\n")
		fmt.Fprintf(stderr, "  vortex config secret header x-auth-token AUTH_TOKEN\n")
		fmt.Fprintf(stderr, "  vortex config secret query api_key API_KEY\n")
		fmt.Fprintf(stderr, "  vortex config secret cookie session_id SESSION_ID\n")
		fmt.Fprintf(stderr, "  vortex config plugin add ts --out=src/api.ts --contract=Market\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	posArgs, err := ParseInterspersedFlags(fs, args)
	if err != nil {
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

		case "secret", "secrets":
			return c.runSecret(stdout, stderr, cfg, posArgs[1:])

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

func (c *CmdConfig) runSecret(stdout, _ io.Writer, cfg *project.Config, args []string) error {
	if len(args) == 0 || args[0] == "list" || args[0] == "ls" {
		return printSecretsConfig(stdout, cfg)
	}

	sub := strings.ToLower(args[0])
	subArgs := args[1:]

	switch sub {
	case "list", "ls":
		return printSecretsConfig(stdout, cfg)

	case "add":
		if len(subArgs) < 3 {
			return errors.New(
				"usage: vortex config secret add <header|query|cookie|body|path|pattern> <name> <ENV_VAR>",
			)
		}

		return addSecretRule(stdout, cfg, subArgs[0], subArgs[1], subArgs[2])

	case "rm", "remove", "del", "delete":
		if len(subArgs) < 2 {
			return errors.New("usage: vortex config secret rm <header|query|cookie|body|path|pattern> <name>")
		}

		return removeSecretRule(stdout, cfg, subArgs[0], subArgs[1])

	case "clear", "purge":
		cfg.Secrets = project.SecretsConfig{}
		if err := cfg.Save(); err != nil {
			return fmt.Errorf("saving configuration: %w", err)
		}

		fmt.Fprintf(stdout, "✔ Cleared all secret rules from .vortex.yml\n")

		return nil

	// Direct shortcuts: vortex config secret header x-auth AUTH_TOKEN
	case "header", "headers", "query", "cookie", "cookies", "body", "path", "paths", "pattern", "patterns":
		if len(subArgs) >= 2 {
			return addSecretRule(stdout, cfg, sub, subArgs[0], subArgs[1])
		}

		return fmt.Errorf("usage: vortex config secret %s <name> <ENV_VAR>", sub)

	default:
		return fmt.Errorf(
			"unknown secret subcommand %q (use list, add, rm, header, query, cookie, body, path, pattern)",
			sub,
		)
	}
}

func addSecretRule(stdout io.Writer, cfg *project.Config, targetType, key, envVar string) error {
	targetType = strings.ToLower(targetType)
	envVar = strings.ToUpper(strings.TrimSpace(envVar))

	switch targetType {
	case "header", "headers":
		if cfg.Secrets.Headers == nil {
			cfg.Secrets.Headers = make(map[string]string)
		}

		cfg.Secrets.Headers[key] = envVar
		if err := cfg.Save(); err != nil {
			return err
		}

		fmt.Fprintf(stdout, "✔ Added secret header mapping: %s -> ${%s}\n", key, envVar)

		return nil

	case "query", "queries":
		if cfg.Secrets.Query == nil {
			cfg.Secrets.Query = make(map[string]string)
		}

		cfg.Secrets.Query[key] = envVar
		if err := cfg.Save(); err != nil {
			return err
		}

		fmt.Fprintf(stdout, "✔ Added secret query mapping: ?%s -> ${%s}\n", key, envVar)

		return nil

	case "cookie", "cookies":
		if cfg.Secrets.Cookies == nil {
			cfg.Secrets.Cookies = make(map[string]string)
		}

		cfg.Secrets.Cookies[key] = envVar
		if err := cfg.Save(); err != nil {
			return err
		}

		fmt.Fprintf(stdout, "✔ Added secret cookie mapping: %s -> ${%s}\n", key, envVar)

		return nil

	case "body":
		if cfg.Secrets.Body == nil {
			cfg.Secrets.Body = make(map[string]string)
		}

		cfg.Secrets.Body[key] = envVar
		if err := cfg.Save(); err != nil {
			return err
		}

		fmt.Fprintf(stdout, "✔ Added secret body mapping: %s -> ${%s}\n", key, envVar)

		return nil

	case "path", "paths":
		cfg.Secrets.Paths = append(cfg.Secrets.Paths, project.SecretPathRule{
			Pattern: key,
			Var:     envVar,
		})
		if err := cfg.Save(); err != nil {
			return err
		}

		fmt.Fprintf(stdout, "✔ Added secret path rule: %s -> ${%s}\n", key, envVar)

		return nil

	case "pattern", "patterns", "regex":
		cfg.Secrets.Patterns = append(cfg.Secrets.Patterns, project.SecretPattern{
			Regex: key,
			Var:   envVar,
		})
		if err := cfg.Save(); err != nil {
			return err
		}

		fmt.Fprintf(stdout, "✔ Added secret regex pattern: %s -> ${%s}\n", key, envVar)

		return nil

	default:
		return fmt.Errorf("unknown secret type %q (must be header, query, cookie, body, path, pattern)", targetType)
	}
}

func removeSecretRule(stdout io.Writer, cfg *project.Config, targetType, key string) error {
	targetType = strings.ToLower(targetType)

	switch targetType {
	case "header", "headers":
		if cfg.Secrets.Headers != nil {
			delete(cfg.Secrets.Headers, key)
		}
	case "query", "queries":
		if cfg.Secrets.Query != nil {
			delete(cfg.Secrets.Query, key)
		}
	case "cookie", "cookies":
		if cfg.Secrets.Cookies != nil {
			delete(cfg.Secrets.Cookies, key)
		}
	case "body":
		if cfg.Secrets.Body != nil {
			delete(cfg.Secrets.Body, key)
		}
	case "path", "paths":
		var newPaths []project.SecretPathRule
		for _, p := range cfg.Secrets.Paths {
			if p.Pattern != key && p.Var != key {
				newPaths = append(newPaths, p)
			}
		}

		cfg.Secrets.Paths = newPaths

	case "pattern", "patterns", "regex":
		var newPatterns []project.SecretPattern
		for _, p := range cfg.Secrets.Patterns {
			if p.Regex != key && p.Var != key {
				newPatterns = append(newPatterns, p)
			}
		}

		cfg.Secrets.Patterns = newPatterns

	default:
		return fmt.Errorf("unknown secret type %q", targetType)
	}

	if err := cfg.Save(); err != nil {
		return err
	}

	fmt.Fprintf(stdout, "✔ Removed secret %s rule %q from .vortex.yml\n", targetType, key)

	return nil
}

func printSecretsConfig(stdout io.Writer, cfg *project.Config) error {
	fmt.Fprintf(stdout, "⚡ Vortex Secret & Credential Rules (.vortex.yml)\n\n")

	hasRules := false

	if len(cfg.Secrets.Headers) > 0 {
		hasRules = true

		fmt.Fprintf(stdout, "● HTTP Headers (secrets.headers):\n")

		for k, v := range cfg.Secrets.Headers {
			fmt.Fprintf(stdout, "  • %-24s -> ${%s}\n", k, v)
		}
	}

	if len(cfg.Secrets.Query) > 0 {
		hasRules = true

		fmt.Fprintf(stdout, "\n● URL Query Parameters (secrets.query):\n")

		for k, v := range cfg.Secrets.Query {
			fmt.Fprintf(stdout, "  • ?%-23s -> ${%s}\n", k, v)
		}
	}

	if len(cfg.Secrets.Cookies) > 0 {
		hasRules = true

		fmt.Fprintf(stdout, "\n● Cookies (secrets.cookies):\n")

		for k, v := range cfg.Secrets.Cookies {
			fmt.Fprintf(stdout, "  • %-24s -> ${%s}\n", k, v)
		}
	}

	if len(cfg.Secrets.Body) > 0 {
		hasRules = true

		fmt.Fprintf(stdout, "\n● Request Body Fields (secrets.body):\n")

		for k, v := range cfg.Secrets.Body {
			fmt.Fprintf(stdout, "  • %-24s -> ${%s}\n", k, v)
		}
	}

	if len(cfg.Secrets.Paths) > 0 {
		hasRules = true

		fmt.Fprintf(stdout, "\n● URL Path Rules (secrets.paths):\n")

		for _, p := range cfg.Secrets.Paths {
			fmt.Fprintf(stdout, "  • %-24s -> ${%s}\n", p.Pattern, p.Var)
		}
	}

	if len(cfg.Secrets.Patterns) > 0 {
		hasRules = true

		fmt.Fprintf(stdout, "\n● Regex Token Patterns (secrets.patterns):\n")

		for _, p := range cfg.Secrets.Patterns {
			fmt.Fprintf(stdout, "  • %-24s -> ${%s}\n", p.Regex, p.Var)
		}
	}

	if !hasRules {
		fmt.Fprintf(stdout, "No custom secret rules configured. Using automatic universal heuristics.\n")
		fmt.Fprintf(stdout, "\nCommands to add rules:\n")
		fmt.Fprintf(stdout, "  vortex config secret header <name> <ENV_VAR>\n")
		fmt.Fprintf(stdout, "  vortex config secret query <name> <ENV_VAR>\n")
		fmt.Fprintf(stdout, "  vortex config secret cookie <name> <ENV_VAR>\n")
		fmt.Fprintf(stdout, "  vortex config secret pattern <regex> <ENV_VAR>\n")
	}

	return nil
}
