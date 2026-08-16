// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package project implements workspace configuration discovery, .vortex.yml parsing, and contract status analysis.
package project

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

// ConfigFileName is the primary project configuration filename.
const ConfigFileName = ".vortex.yml"

// Config represents the complete .vortex.yml configuration schema.
type Config struct {
	Version   int              `yaml:"version"`
	Defaults  DefaultsConfig   `yaml:"defaults,omitempty"`
	Contracts []ContractConfig `yaml:"contracts"`
	Lint      LintConfig       `yaml:"lint,omitempty"`
	Ignore    []string         `yaml:"ignore,omitempty"`
	Export    ExportConfig     `yaml:"export,omitempty"`
	Coverage  CoverageConfig   `yaml:"coverage,omitempty"`

	// Runtime metadata
	RootDir    string `yaml:"-"`
	ConfigPath string `yaml:"-"`
}

// CoverageConfig controls test coverage profile analysis rules in vortex cover.
type CoverageConfig struct {
	File    string   `yaml:"file,omitempty"`
	Min     float64  `yaml:"min,omitempty"`
	Sort    string   `yaml:"sort,omitempty"` // "percent" or "name"
	Exclude []string `yaml:"exclude,omitempty"`
}

// DefaultsConfig defines global defaults for code generation.
type DefaultsConfig struct {
	Casing  string `yaml:"casing,omitempty"`
	Engine  string `yaml:"engine,omitempty"`
	Persona string `yaml:"persona,omitempty"`
	TLSSpec string `yaml:"tlsspec,omitempty"`
	Harness bool   `yaml:"harness,omitempty"`
	Mock    bool   `yaml:"mock,omitempty"`
}

// ContractConfig describes a single service contract definition within the workspace.
type ContractConfig struct {
	Name     string          `yaml:"name,omitempty"`
	Package  string          `yaml:"package,omitempty"`
	Dir      string          `yaml:"dir,omitempty"`
	File     string          `yaml:"file,omitempty"`
	Gen      string          `yaml:"gen,omitempty"`
	Models   string          `yaml:"models,omitempty"`
	Harness  string          `yaml:"harness,omitempty"`
	Mock     string          `yaml:"mock,omitempty"`
	Ignore   []string        `yaml:"ignore,omitempty"`
	Disable  []string        `yaml:"disable,omitempty"`
	Upstream *UpstreamConfig `yaml:"upstream,omitempty"`
	Plugins  []PluginConfig  `yaml:"plugins,omitempty"`
}

// UnmarshalYAML supports string scalars ("swagger.json") and explicit mapping objects.
func (u *UpstreamConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		u.Source = value.Value
		if strings.HasSuffix(u.Source, ".json") || strings.HasSuffix(u.Source, ".yaml") ||
			strings.HasSuffix(u.Source, ".yml") {
			u.Format = "openapi"
		}

		return nil
	}

	type rawUpstream UpstreamConfig

	var raw rawUpstream
	if err := value.Decode(&raw); err != nil {
		return err
	}

	*u = UpstreamConfig(raw)

	return nil
}

// UnmarshalYAML supports string scalars ("pkg/services/crit"), compact mappings, and rich configs.
func (c *ContractConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		val := value.Value
		if strings.HasSuffix(val, ".go") {
			c.File = val
			c.Dir = filepath.ToSlash(filepath.Dir(val))
		} else {
			c.Dir = val
			c.File = filepath.ToSlash(filepath.Join(val, "api.go"))
		}

		return nil
	}

	type rawContract ContractConfig

	var raw rawContract
	if err := value.Decode(&raw); err != nil {
		return err
	}

	*c = ContractConfig(raw)

	return nil
}

// UpstreamConfig links a Go contract to an external OpenAPI/Swagger schema source or proprietary dump.
type UpstreamConfig struct {
	Source   string `yaml:"source"`
	Format   string `yaml:"format,omitempty"`   // "openapi", "swagger", "json", "raw"
	Generate string `yaml:"generate,omitempty"` // Hook command to generate api.go from source dump
	Refresh  string `yaml:"refresh,omitempty"`  // Hook command to fetch latest source dump
}

// PluginConfig specifies code generation targets for polyglot platforms (e.g. TypeScript, C, Swift).
type PluginConfig struct {
	Name string `yaml:"name"`
	Out  string `yaml:"out"`
}

// LintConfig controls static analysis rules and severity thresholds.
type LintConfig struct {
	Strict  bool                `yaml:"strict,omitempty"`
	Disable []string            `yaml:"disable,omitempty"`
	Ignore  []string            `yaml:"ignore,omitempty"`
	Enable  []string            `yaml:"enable,omitempty"`
	Rules   map[string]RuleOpts `yaml:"rules,omitempty"`
}

// AllIgnoredRules returns a deduplicated list of all globally ignored lint rules.
func (cfg *Config) AllIgnoredRules() []string {
	if cfg == nil {
		return nil
	}

	set := make(map[string]bool)

	var result []string

	add := func(rules []string) {
		for _, r := range rules {
			r = strings.TrimSpace(r)
			if r != "" && !set[r] {
				set[r] = true
				result = append(result, r)
			}
		}
	}

	add(cfg.Ignore)
	add(cfg.Lint.Ignore)
	add(cfg.Lint.Disable)

	return result
}

// RuleOpts defines per-rule configuration overrides.
type RuleOpts struct {
	MaxBytes int `yaml:"max_bytes,omitempty"`
}

// ExportConfig defines defaults for external OpenAPI schema exports.
type ExportConfig struct {
	OpenAPI struct {
		Out     string `yaml:"out,omitempty"`
		Version string `yaml:"version,omitempty"`
	} `yaml:"openapi,omitempty"`
}

// Normalize fills in implicit defaults for contracts based on folder conventions.
func (cfg *Config) Normalize() {
	for i := range cfg.Contracts {
		ct := &cfg.Contracts[i]
		if ct.File == "" && ct.Dir != "" {
			ct.File = filepath.ToSlash(filepath.Join(ct.Dir, "api.go"))
		}

		if ct.File != "" && ct.Dir == "" {
			ct.Dir = filepath.ToSlash(filepath.Dir(ct.File))
		}

		if ct.Package == "" && ct.Dir != "" {
			ct.Package = filepath.Base(ct.Dir)
		}

		if ct.Name == "" && ct.Package != "" {
			ct.Name = strings.ToUpper(ct.Package[:1]) + ct.Package[1:] + "API"
		}

		if ct.Gen == "" && ct.File != "" {
			ct.Gen = strings.TrimSuffix(ct.File, ".go") + ".gen.go"
		}

		if ct.Harness == "" && cfg.Defaults.Harness && ct.File != "" {
			ct.Harness = strings.TrimSuffix(ct.File, ".go") + "_harness.gen.go"
		}

		if ct.Mock == "" && cfg.Defaults.Mock && ct.File != "" {
			ct.Mock = strings.TrimSuffix(ct.File, ".go") + "_mock.gen.go"
		}
	}
}

// FindRoot traverses upward from startDir to discover the repository root.
// Search precedence: .vortex.yml -> vortex.yaml -> .git -> go.mod.
func FindRoot(startDir string) (rootDir, configPath string, err error) {
	curr, err := filepath.Abs(startDir)
	if err != nil {
		curr = startDir
	}

	var fallbackRoot string

	for {
		// 1. Check for explicit .vortex.yml or vortex.yaml
		for _, name := range []string{".vortex.yml", "vortex.yaml", ".vortex.yaml"} {
			cfgFile := filepath.Join(curr, name)
			if info, err := os.Stat(cfgFile); err == nil && !info.IsDir() {
				return curr, cfgFile, nil
			}
		}

		// 2. Remember go.mod or .git as workspace boundary
		if fallbackRoot == "" {
			for _, boundary := range []string{"go.mod", ".git"} {
				bPath := filepath.Join(curr, boundary)
				if _, err := os.Stat(bPath); err == nil {
					fallbackRoot = curr
					break
				}
			}
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}

		curr = parent
	}

	if fallbackRoot != "" {
		return fallbackRoot, "", nil
	}

	absStart, _ := filepath.Abs(startDir)

	return absStart, "", nil
}

// Load loads and parses .vortex.yml from the workspace root or performs auto-discovery.
func Load(startDir string) (*Config, error) {
	rootDir, configPath, err := FindRoot(startDir)
	if err != nil {
		return nil, fmt.Errorf("discovering workspace root: %w", err)
	}

	if configPath != "" {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, fmt.Errorf("reading %s: %w", configPath, err)
		}

		var cfg Config
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", configPath, err)
		}

		cfg.RootDir = rootDir

		cfg.ConfigPath = configPath
		if cfg.Version == 0 {
			cfg.Version = 1
		}

		cfg.Normalize()

		return &cfg, nil
	}

	// Zero-Config Auto-Discovery
	return AutoDiscover(rootDir)
}

// AutoDiscoverOptions configures path filtering for workspace contract auto-discovery.
type AutoDiscoverOptions struct {
	Exclude []string
	Match   string
}

// AutoDiscover scans the workspace directory tree and builds a virtual Config.
func AutoDiscover(rootDir string, opts ...AutoDiscoverOptions) (*Config, error) {
	var opt AutoDiscoverOptions
	if len(opts) > 0 {
		opt = opts[0]
	}

	cfg := &Config{
		Version:   1,
		RootDir:   rootDir,
		Contracts: make([]ContractConfig, 0),
		Defaults: DefaultsConfig{
			Casing: "snake_case",
			Engine: "fast",
		},
	}

	p := parser.NewParser()

	_ = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil {
			return filepath.SkipDir
		}

		relPath, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			relPath = path
		}

		relSlash := filepath.ToSlash(relPath)

		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" || name == ".system_generated" ||
				name == "testdata" {
				return filepath.SkipDir
			}

			for _, ex := range opt.Exclude {
				if ex != "" && (matchGlob(ex, relSlash) || matchGlob(ex, name)) {
					return filepath.SkipDir
				}
			}

			return nil
		}

		for _, ex := range opt.Exclude {
			if ex != "" && (matchGlob(ex, relSlash) || matchGlob(ex, d.Name())) {
				return nil
			}
		}

		if opt.Match != "" && !matchGlob(opt.Match, relSlash) {
			return nil
		}

		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), ".gen.go") ||
			strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}

		root, _ := p.ParseFile(path)
		if root == nil || len(root.Services) == 0 {
			return nil
		}

		dir := filepath.Dir(relPath)
		baseName := filepath.Base(relPath)
		genName := strings.TrimSuffix(baseName, ".go") + ".gen.go"
		genPath := filepath.Join(dir, genName)

		modelsPath := ""
		if _, statErr := os.Stat(filepath.Join(rootDir, dir, "models.gen.go")); statErr == nil {
			modelsPath = filepath.Join(dir, "models.gen.go")
		}

		// Consolidate multiple services in the same file into a single contract entry
		contractName := ""

		var upstream *UpstreamConfig

		if len(root.Services) == 1 {
			contractName = root.Services[0].Name
			if root.Services[0].Source != "" {
				upstream = &UpstreamConfig{
					Source: root.Services[0].Source,
					Format: "openapi",
				}
			}
		} else {
			// Multiple services in 1 file (e.g. Steam WebAPI sub-interfaces)
			for _, s := range root.Services {
				if s.Source != "" && upstream == nil {
					upstream = &UpstreamConfig{
						Source: s.Source,
						Format: "openapi",
					}
				}

				if strings.EqualFold(s.Name, root.PackageName+"API") || strings.HasSuffix(s.Name, "API") {
					contractName = s.Name
				}
			}

			if contractName == "" || contractName == "API" {
				contractName = strings.ToUpper(root.PackageName[:1]) + root.PackageName[1:] + "API"
			}
		}

		cfg.Contracts = append(cfg.Contracts, ContractConfig{
			Name:     contractName,
			Package:  root.PackageName,
			File:     filepath.ToSlash(relPath),
			Gen:      filepath.ToSlash(genPath),
			Models:   filepath.ToSlash(modelsPath),
			Upstream: upstream,
		})

		return nil
	})

	return cfg, nil
}

func matchGlob(pattern, target string) bool {
	pattern = filepath.ToSlash(pattern)
	target = filepath.ToSlash(target)

	if pattern == target || pattern == "*" {
		return true
	}

	if strings.HasSuffix(pattern, "/**") {
		prefix := strings.TrimSuffix(pattern, "/**")
		return target == prefix || strings.HasPrefix(target, prefix+"/")
	}

	if strings.HasPrefix(pattern, "**/") {
		suffix := strings.TrimPrefix(pattern, "**/")
		return target == suffix || strings.HasSuffix(target, "/"+suffix)
	}

	if matched, err := filepath.Match(pattern, target); err == nil && matched {
		return true
	}

	return strings.Contains(target, strings.Trim(pattern, "*"))
}

// Init creates a fresh .vortex.yml file in rootDir based on auto-discovered workspace contracts.
func Init(rootDir string, force bool, opts ...AutoDiscoverOptions) (*Config, error) {
	configPath := filepath.Join(rootDir, ConfigFileName)
	if !force {
		if _, err := os.Stat(configPath); err == nil {
			return nil, errors.New(".vortex.yml already exists (use --force to overwrite)")
		}
	}

	cfg, err := AutoDiscover(rootDir, opts...)
	if err != nil {
		return nil, fmt.Errorf("auto-discovering services: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshaling configuration: %w", err)
	}

	header := []byte(
		"# .vortex.yml — Vortex API Guardian Workspace Configuration\n# Documentation: https://github.com/lemon4ksan/aoni\n\n",
	)
	header = append(header, data...)

	if err := os.MkdirAll(rootDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating directory %s: %w", rootDir, err)
	}

	if err := os.WriteFile(configPath, header, 0o600); err != nil {
		return nil, fmt.Errorf("writing %s: %w", configPath, err)
	}

	cfg.ConfigPath = configPath

	_, _ = EnsureGitignore(rootDir)

	return cfg, nil
}

// GitignoreVortexBlock contains standard ignore patterns for Vortex test harnesses, mocks, and diagnostics.
const GitignoreVortexBlock = `
# Vortex Test Artifacts & Diagnostics
*_mock.gen.go
*_harness.gen.go
*_harness_test.go
*.prof
*.sarif
.vortex/
`

// EnsureGitignore ensures that .gitignore in rootDir includes Vortex ignore patterns.
func EnsureGitignore(rootDir string) (bool, error) {
	giPath := filepath.Join(rootDir, ".gitignore")

	data, err := os.ReadFile(giPath)
	if os.IsNotExist(err) {
		content := strings.TrimPrefix(GitignoreVortexBlock, "\n")
		if writeErr := os.WriteFile(giPath, []byte(content), 0o600); writeErr != nil {
			return false, fmt.Errorf("creating .gitignore: %w", writeErr)
		}

		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("reading .gitignore: %w", err)
	}

	content := string(data)
	if strings.Contains(content, "*_mock.gen.go") || strings.Contains(content, "*_harness.gen.go") {
		return false, nil
	}

	var newContent bytes.Buffer
	newContent.Write(data)

	if len(data) > 0 && !strings.HasSuffix(content, "\n") {
		newContent.WriteString("\n")
	}

	newContent.WriteString(GitignoreVortexBlock)

	if writeErr := os.WriteFile(giPath, newContent.Bytes(), 0o600); writeErr != nil {
		return false, fmt.Errorf("updating .gitignore: %w", writeErr)
	}

	return true, nil
}

// FilterContext filters contracts relevant to targetDir.
// If targetDir is the rootDir, returns all contracts unless specific target names are given.
func (c *Config) FilterContext(targetDir string, targetNames ...string) []ContractConfig {
	if len(targetNames) > 0 {
		var filtered []ContractConfig
		for _, name := range targetNames {
			for _, ct := range c.Contracts {
				if strings.EqualFold(ct.Name, name) || strings.EqualFold(ct.Package, name) {
					filtered = append(filtered, ct)
				}
			}
		}

		if len(filtered) > 0 {
			return filtered
		}
	}

	if targetDir == "" || targetDir == "." || filepath.Clean(targetDir) == filepath.Clean(c.RootDir) {
		return c.Contracts
	}

	relTarget, err := filepath.Rel(c.RootDir, targetDir)
	if err != nil {
		relTarget = targetDir
	}

	relTarget = filepath.ToSlash(relTarget)

	var matched []ContractConfig
	for _, ct := range c.Contracts {
		contractDir := filepath.ToSlash(filepath.Dir(ct.File))
		if contractDir == relTarget || strings.HasPrefix(contractDir, relTarget+"/") ||
			strings.HasPrefix(relTarget, contractDir+"/") {
			matched = append(matched, ct)
		}
	}

	if len(matched) == 0 {
		return c.Contracts
	}

	return matched
}

// WorkConfigFileName is the primary multi-repo workspace configuration file.
const (
	WorkConfigFileName     = ".vortex.work"
	AltWorkConfigFileName  = "vortex.work"
	YamlWorkConfigFileName = ".vortex.work.yml"
)

// WorkConfig represents a multi-repo workspace orchestrator (.vortex.work).
type WorkConfig struct {
	Version    int      `yaml:"version"`
	Workspaces []string `yaml:"workspaces"`

	// Runtime metadata
	WorkDir  string    `yaml:"-"`
	WorkPath string    `yaml:"-"`
	Projects []*Config `yaml:"-"`
}

// FindWorkRoot traverses upward looking for .vortex.work or vortex.work.
func FindWorkRoot(startDir string) (workDir, workPath string, err error) {
	curr, err := filepath.Abs(startDir)
	if err != nil {
		curr = startDir
	}

	for {
		for _, name := range []string{WorkConfigFileName, AltWorkConfigFileName, YamlWorkConfigFileName} {
			filePath := filepath.Join(curr, name)
			if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
				return curr, filePath, nil
			}
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}

		curr = parent
	}

	return "", "", errors.New("no .vortex.work workspace file found in hierarchy")
}

// LoadWork loads the .vortex.work configuration and resolves all child workspace configs.
func LoadWork(workDir string) (*WorkConfig, error) {
	_, workPath, err := FindWorkRoot(workDir)
	if err != nil {
		// Try direct path in workDir
		for _, name := range []string{WorkConfigFileName, AltWorkConfigFileName, YamlWorkConfigFileName} {
			fp := filepath.Join(workDir, name)
			if info, statErr := os.Stat(fp); statErr == nil && !info.IsDir() {
				workPath = fp
				break
			}
		}

		if workPath == "" {
			return nil, err
		}
	}

	data, err := os.ReadFile(workPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", workPath, err)
	}

	var wc WorkConfig
	if err := yaml.Unmarshal(data, &wc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", workPath, err)
	}

	wc.WorkDir = filepath.Dir(workPath)
	wc.WorkPath = workPath

	for _, ws := range wc.Workspaces {
		wsPath := filepath.Join(wc.WorkDir, filepath.FromSlash(ws))
		if cfg, lErr := Load(wsPath); lErr == nil && cfg != nil {
			wc.Projects = append(wc.Projects, cfg)
		}
	}

	return &wc, nil
}

// SaveWork writes a WorkConfig to disk.
func SaveWork(filePath string, wc *WorkConfig) error {
	data, err := yaml.Marshal(wc)
	if err != nil {
		return fmt.Errorf("marshal work config: %w", err)
	}

	return os.WriteFile(filePath, data, 0o600)
}

// AutoDiscoverWorkspaces scans subdirectories of parentDir to find directories containing .vortex.yml.
func AutoDiscoverWorkspaces(parentDir string) ([]string, error) {
	entries, err := os.ReadDir(parentDir)
	if err != nil {
		return nil, err
	}

	var discovered []string

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		subDir := filepath.Join(parentDir, entry.Name())
		for _, cfgName := range []string{ConfigFileName, "vortex.yaml", ".vortex.yaml"} {
			if _, err := os.Stat(filepath.Join(subDir, cfgName)); err == nil {
				discovered = append(discovered, "./"+entry.Name())
				break
			}
		}
	}

	return discovered, nil
}
