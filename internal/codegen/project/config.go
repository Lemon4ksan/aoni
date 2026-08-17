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

	"github.com/lemon4ksan/aoni/internal/codegen/cache"
	"github.com/lemon4ksan/aoni/internal/codegen/parser"
)

// ConfigFileName is the primary project configuration filename.
const ConfigFileName = ".vortex.yml"

// Config represents the complete .vortex.yml configuration schema.
type Config struct {
	Version    int              `yaml:"version"`
	Defaults   DefaultsConfig   `yaml:"defaults,omitempty"`
	Contracts  []ContractConfig `yaml:"contracts"`
	Secrets    SecretsConfig    `yaml:"secrets,omitempty"`
	Lint       LintConfig       `yaml:"lint,omitempty"`
	Formatting FormattingConfig `yaml:"formatting,omitempty"`
	Ignore     []string         `yaml:"ignore,omitempty"`
	Export     ExportConfig     `yaml:"export,omitempty"`
	Coverage   CoverageConfig   `yaml:"coverage,omitempty"`

	// Runtime metadata
	RootDir    string `yaml:"-"`
	ConfigPath string `yaml:"-"`
}

// FormattingConfig controls automated code wrapping and formatting limits.
type FormattingConfig struct {
	MaxLen int `yaml:"max_len,omitempty"` // max line length threshold (default 120)
}

// UnmarshalYAML supports max_len and max-len keys with default 120.
func (f *FormattingConfig) UnmarshalYAML(value *yaml.Node) error {
	type rawFormatting struct {
		MaxLen  int `yaml:"max_len"`
		MaxLenK int `yaml:"max-len"`
	}

	var raw rawFormatting
	if err := value.Decode(&raw); err != nil {
		return err
	}

	switch {
	case raw.MaxLen > 0:
		f.MaxLen = raw.MaxLen
	case raw.MaxLenK > 0:
		f.MaxLen = raw.MaxLenK
	default:
		f.MaxLen = 120
	}

	return nil
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
	Retry   int    `yaml:"retry,omitempty"`
	Timeout string `yaml:"timeout,omitempty"`
	Harness bool   `yaml:"harness,omitempty"`
	Mock    bool   `yaml:"mock,omitempty"`
}

// SecretsConfig specifies secret detection, header/query/cookie masking, and environment mappings.
type SecretsConfig = cache.SecretsConfig

// SecretPathRule associates a URL path segment pattern with a secret variable name.
type SecretPathRule = cache.SecretPathRule

// SecretPattern associates a regular expression pattern with a secret variable name.
type SecretPattern = cache.SecretPattern

// NormalizeHeaderToEnv converts header names into clean uppercase environment variable names.
var NormalizeHeaderToEnv = cache.NormalizeHeaderToEnv

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

// FindContract locates a contract by name (e.g. "AntigravityAPI", "antigravityapi", "MakerSuiteAPI.go"), package, or file path.
func (cfg *Config) FindContract(nameOrPath string) *ContractConfig {
	if cfg == nil || len(cfg.Contracts) == 0 {
		return nil
	}

	cleanTarget := filepath.Clean(nameOrPath)
	targetLower := strings.ToLower(nameOrPath)
	trimmedName := strings.TrimSuffix(nameOrPath, ".go")
	trimmedLower := strings.ToLower(trimmedName)

	// 1. Direct name match (exact or case-insensitive, with or without .go)
	for i := range cfg.Contracts {
		c := &cfg.Contracts[i]
		if c.Name == nameOrPath || strings.ToLower(c.Name) == targetLower ||
			c.Name == trimmedName || strings.ToLower(c.Name) == trimmedLower {
			return c
		}
	}

	// 2. Direct file, dir, or package match
	for i := range cfg.Contracts {
		c := &cfg.Contracts[i]
		if c.File != "" && (filepath.Clean(c.File) == cleanTarget || strings.EqualFold(c.File, nameOrPath) ||
			filepath.Clean(c.File) == cleanTarget+".go" || strings.EqualFold(c.File, trimmedName+".go")) {
			return c
		}

		if c.Dir != "" && (filepath.Clean(c.Dir) == cleanTarget || strings.EqualFold(c.Dir, nameOrPath)) {
			return c
		}

		if c.Package != "" && (c.Package == nameOrPath || strings.EqualFold(c.Package, nameOrPath) ||
			c.Package == trimmedName || strings.EqualFold(c.Package, trimmedName)) {
			return c
		}
	}

	// 3. Match against basename or relative path from RootDir
	for i := range cfg.Contracts {
		c := &cfg.Contracts[i]
		if c.File != "" {
			fullPath := filepath.Join(cfg.RootDir, c.File)
			if filepath.Clean(fullPath) == cleanTarget || filepath.Clean(fullPath) == cleanTarget+".go" {
				return c
			}

			if filepath.Base(c.File) == nameOrPath || strings.TrimSuffix(filepath.Base(c.File), ".go") == nameOrPath ||
				strings.TrimSuffix(filepath.Base(c.File), ".go") == trimmedName {
				return c
			}
		}
	}

	return nil
}

// ResolveTargetToPath attempts to resolve a contract name, service name, or file path to an actual file on disk.
func ResolveTargetToPath(target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		return ""
	}

	// If file exists directly
	if fi, err := os.Stat(target); err == nil && !fi.IsDir() {
		return filepath.Clean(target)
	}

	// Attempt resolving via .vortex.yml config
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}

	if cfg, err := Load(cwd); err == nil && cfg != nil {
		if c := cfg.FindContract(target); c != nil && c.File != "" {
			candidate := c.File
			if !filepath.IsAbs(candidate) && cfg.RootDir != "" {
				candidate = filepath.Join(cfg.RootDir, candidate)
			}

			if _, sErr := os.Stat(candidate); sErr == nil {
				return filepath.Clean(candidate)
			}

			return c.File
		}
	}

	return ""
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
			ct.Name = strings.ToUpper(ct.Package[:1]) + ct.Package[1:]
		}

		if ct.Gen == "" && ct.File != "" {
			ct.Gen = strings.TrimSuffix(ct.File, ".go") + ".gen.go"
		}

		if ct.Models == "" && ct.Dir != "" {
			modelsPath := filepath.Join(ct.Dir, "models.gen.go")
			if _, err := os.Stat(filepath.Join(cfg.RootDir, modelsPath)); err == nil {
				ct.Models = filepath.ToSlash(modelsPath)
			}
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
	if isUserHomeOrSystemDir(startDir) {
		return &Config{
			Version:   1,
			RootDir:   startDir,
			Contracts: make([]ContractConfig, 0),
			Defaults: DefaultsConfig{
				Casing: "snake_case",
				Engine: "fast",
			},
		}, nil
	}

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
	Exclude  []string
	Match    string
	MaxDepth int // Max folder recursion depth (default: 6, -1 for unlimited)
	MaxFiles int // Max files to inspect (default: 5000)
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

	// Refuse unbounded recursive scan on bare filesystem root or user home directory without project config
	if isUserHomeOrSystemDir(rootDir) {
		return cfg, nil
	}

	maxDepth := 6
	maxFiles := 5000

	if opt.MaxDepth > 0 {
		maxDepth = opt.MaxDepth
	} else if opt.MaxDepth == -1 {
		maxDepth = 999999
	}

	if opt.MaxFiles > 0 {
		maxFiles = opt.MaxFiles
	}

	p := parser.NewParser()
	scannedCount := 0

	_ = filepath.WalkDir(rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil || d == nil {
			return filepath.SkipDir
		}

		relPath, relErr := filepath.Rel(rootDir, path)
		if relErr != nil {
			relPath = path
		}

		relSlash := filepath.ToSlash(relPath)

		// Guard: Depth Limit
		if strings.Count(relSlash, "/") > maxDepth {
			if d.IsDir() {
				return filepath.SkipDir
			}

			return nil
		}

		if d.IsDir() {
			name := d.Name()
			if isIgnoredDirectory(name) || name == "testdata" {
				return filepath.SkipDir
			}

			for _, ex := range opt.Exclude {
				if ex != "" && (matchGlob(ex, relSlash) || matchGlob(ex, name)) {
					return filepath.SkipDir
				}
			}

			return nil
		}

		scannedCount++
		if scannedCount > maxFiles {
			return filepath.SkipAll
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

		// Fast byte search pre-filter: skip heavy AST parsing if file has no directives
		if !quickCheckCandidate(path) {
			return nil
		}

		root, _ := p.ParseFile(path)
		if root == nil || len(root.Services) == 0 {
			return nil
		}

		// Consolidate multiple services in the same file into a single contract entry
		contractName := ""

		var upstream *UpstreamConfig

		if len(root.Services) == 1 {
			s := root.Services[0]
			if s.Source != "" {
				upstream = &UpstreamConfig{
					Source: s.Source,
					Format: "openapi",
				}
			}

			if s.Name == "API" || s.Name == "Events" || s.Name == "Client" || s.Name == "Service" {
				contractName = strings.ToUpper(root.PackageName[:1]) + root.PackageName[1:]
			} else {
				contractName = s.Name
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
				contractName = strings.ToUpper(root.PackageName[:1]) + root.PackageName[1:]
			}
		}

		cfg.Contracts = append(cfg.Contracts, ContractConfig{
			Name:     contractName,
			File:     filepath.ToSlash(relPath),
			Upstream: upstream,
		})

		return nil
	})

	cfg.Normalize()

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

	if err := os.MkdirAll(rootDir, 0o750); err != nil {
		return nil, fmt.Errorf("creating directory %s: %w", rootDir, err)
	}

	cfg.ConfigPath = configPath
	if err := cfg.SaveTo(configPath); err != nil {
		return nil, err
	}

	_, _ = EnsureGitignore(rootDir)
	_, _ = EnsureGitattributes(rootDir)

	return cfg, nil
}

// RegisterContract adds or updates a contract entry in .vortex.yml within rootDir.
func RegisterContract(rootDir string, ct ContractConfig) error {
	cfg, err := Load(rootDir)
	if err != nil {
		cfg = &Config{
			Version: 1,
			RootDir: rootDir,
			Defaults: DefaultsConfig{
				Casing: "snake_case",
				Engine: "fast",
			},
			Contracts: []ContractConfig{ct},
		}

		return cfg.Save()
	}

	ct.File = filepath.ToSlash(ct.File)
	found := false

	for i := range cfg.Contracts {
		existing := &cfg.Contracts[i]
		if existing.File == ct.File || (ct.Name != "" && strings.EqualFold(existing.Name, ct.Name)) {
			if ct.Name != "" {
				existing.Name = ct.Name
			}

			if ct.Package != "" {
				existing.Package = ct.Package
			}

			if ct.Upstream != nil {
				existing.Upstream = ct.Upstream
			}

			found = true

			break
		}
	}

	if !found {
		cfg.Contracts = append(cfg.Contracts, ct)
	}

	return cfg.Save()
}

// Save serializes the configuration back to disk at cfg.ConfigPath.
func (cfg *Config) Save() error {
	if cfg.ConfigPath == "" {
		if cfg.RootDir == "" {
			return errors.New("cannot save config: empty RootDir and ConfigPath")
		}

		cfg.ConfigPath = filepath.Join(cfg.RootDir, ConfigFileName)
	}

	return cfg.SaveTo(cfg.ConfigPath)
}

// SaveTo writes the compact configuration to a specific file path.
func (cfg *Config) SaveTo(filePath string) error {
	compactCfg := *cfg
	compactCfg.Contracts = make([]ContractConfig, len(cfg.Contracts))

	for i, c := range cfg.Contracts {
		compactCfg.Contracts[i] = ContractConfig{
			Name:     c.Name,
			File:     c.File,
			Upstream: c.Upstream,
			Plugins:  c.Plugins,
			Ignore:   c.Ignore,
			Disable:  c.Disable,
		}
	}

	data, err := yaml.Marshal(&compactCfg)
	if err != nil {
		return fmt.Errorf("marshaling configuration: %w", err)
	}

	header := []byte(
		"# .vortex.yml — Vortex API Guardian Workspace Configuration\n# Documentation: https://github.com/lemon4ksan/aoni\n\n",
	)
	header = append(header, data...)

	if err := os.WriteFile(filePath, header, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", filePath, err)
	}

	return nil
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

// GitattributesVortexBlock contains standard attributes for Go line endings and GitHub PR diff collapsing.
const GitattributesVortexBlock = `
# Go & Protocol Buffers LF Normalization
* text=auto eol=lf
*.go text eol=lf
*.proto text eol=lf

# GitHub Linguist — Collapse generated code in PR diffs and exclude from stats
*.gen.go linguist-generated=true
*_mock.gen.go linguist-generated=true
*_harness.gen.go linguist-generated=true
*.pb.go linguist-generated=true
*_aoni.pb.go linguist-generated=true
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

// EnsureGitattributes ensures that .gitattributes in rootDir configures LF endings and PR diff collapsing.
func EnsureGitattributes(rootDir string) (bool, error) {
	gaPath := filepath.Join(rootDir, ".gitattributes")

	data, err := os.ReadFile(gaPath)
	if os.IsNotExist(err) {
		content := strings.TrimPrefix(GitattributesVortexBlock, "\n")
		if writeErr := os.WriteFile(gaPath, []byte(content), 0o600); writeErr != nil {
			return false, fmt.Errorf("creating .gitattributes: %w", writeErr)
		}

		return true, nil
	} else if err != nil {
		return false, fmt.Errorf("reading .gitattributes: %w", err)
	}

	content := string(data)
	if strings.Contains(content, "linguist-generated=true") || strings.Contains(content, "*.gen.go") {
		return false, nil
	}

	var newContent bytes.Buffer
	newContent.Write(data)

	if len(data) > 0 && !strings.HasSuffix(content, "\n") {
		newContent.WriteString("\n")
	}

	newContent.WriteString(GitattributesVortexBlock)

	if writeErr := os.WriteFile(gaPath, newContent.Bytes(), 0o600); writeErr != nil {
		return false, fmt.Errorf("updating .gitattributes: %w", writeErr)
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

func isSystemRoot(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}

	clean := filepath.Clean(abs)

	// Unix root "/" or empty
	if clean == "/" || clean == "\\" || clean == "." {
		return clean == "/" || clean == "\\"
	}

	// Windows volume root: "C:\", "D:\", "C:", etc.
	vol := filepath.VolumeName(clean)
	if vol != "" && (clean == vol || clean == vol+`\` || clean == vol+`/`) {
		return true
	}

	return false
}

func isIgnoredDirectory(name string) bool {
	if strings.HasPrefix(name, ".") && name != "." && name != ".." {
		return true
	}

	switch strings.ToLower(name) {
	case "vendor", "node_modules", "testdata", "appdata", "windows",
		"program files", "program files (x86)", "$recycle.bin", "system volume information",
		"tmp", "temp", "dist", "build", "out", "bin", "obj", "desktop", "documents",
		"downloads", "pictures", "videos", "music", "onedrive", "virtualbox vms", "scoop":
		return true
	default:
		return false
	}
}

func isUserHomeOrSystemDir(path string) bool {
	if isSystemRoot(path) {
		return true
	}

	clean := filepath.Clean(path)

	home, err := os.UserHomeDir()
	if err == nil && home != "" && clean == filepath.Clean(home) {
		return true
	}

	return false
}

func quickCheckCandidate(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()

	buf := make([]byte, 65536)

	n, err := f.Read(buf)
	if err != nil && n == 0 {
		return false
	}

	content := buf[:n]

	return bytes.Contains(content, []byte("@aoni:service")) ||
		bytes.Contains(content, []byte("@service")) ||
		bytes.Contains(content, []byte("@aoni:endpoint")) ||
		bytes.Contains(content, []byte("@aoni:mirror")) ||
		bytes.Contains(content, []byte("@mirror"))
}
