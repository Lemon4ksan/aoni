// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package client

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// HostConfig holds options parsed from an OpenSSH config file for a specific host alias.
type HostConfig struct {
	Host                  string
	HostName              string
	User                  string
	Port                  uint
	IdentityFiles         []string
	ProxyJump             string
	ProxyCommand          string
	StrictHostKeyChecking string
	UserKnownHostsFile    string
}

// SSHConfig represents a parsed OpenSSH config file containing host entries.
type SSHConfig struct {
	Hosts map[string]*HostConfig
}

// ParseSSHConfigFile loads and parses an OpenSSH config file from path.
// If path is empty, defaults to ~/.ssh/config.
func ParseSSHConfigFile(path string) (*SSHConfig, error) {
	target := path
	if target == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}

		target = filepath.Join(home, ".ssh", "config")
	}

	f, err := os.Open(target)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ParseSSHConfig(f)
}

// ParseSSHConfig parses OpenSSH config directives from reader.
func ParseSSHConfig(r io.Reader) (*SSHConfig, error) {
	cfg := &SSHConfig{Hosts: make(map[string]*HostConfig)}
	scanner := bufio.NewScanner(r)

	var current *HostConfig

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		key := strings.ToLower(fields[0])
		val := strings.Join(fields[1:], " ")

		switch key {
		case "host":
			current = &HostConfig{
				Host: fields[1],
			}
			cfg.Hosts[fields[1]] = current

		default:
			if current == nil {
				current = &HostConfig{Host: "*"}
				cfg.Hosts["*"] = current
			}

			switch key {
			case "hostname":
				current.HostName = val
			case "user":
				current.User = val
			case "port":
				if p, err := strconv.ParseUint(val, 10, 32); err == nil {
					current.Port = uint(p)
				}
			case "identityfile":
				current.IdentityFiles = append(current.IdentityFiles, val)
			case "proxyjump":
				current.ProxyJump = val
			case "proxycommand":
				current.ProxyCommand = val
			case "stricthostkeychecking":
				current.StrictHostKeyChecking = val
			case "userknownhostsfile":
				current.UserKnownHostsFile = val
			}
		}
	}

	return cfg, scanner.Err()
}

// GetHost retrieves the HostConfig matching alias, falling back to wildcard '*' settings.
func (cfg *SSHConfig) GetHost(alias string) *HostConfig {
	if h, ok := cfg.Hosts[alias]; ok {
		return h
	}

	wildcard, hasWildcard := cfg.Hosts["*"]
	res := &HostConfig{
		Host:     alias,
		HostName: alias,
	}

	if hasWildcard {
		res.User = wildcard.User
		res.Port = wildcard.Port
		res.IdentityFiles = wildcard.IdentityFiles
	}

	return res
}

// NewClientFromConfig creates an SSH Client loading options automatically from ~/.ssh/config for alias.
func NewClientFromConfig(ctx context.Context, alias string, opts ...Option) (*Client, error) {
	sshCfg, err := ParseSSHConfigFile("")
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("aoni ssh config: read config: %w", err)
	}

	hostCfg := &HostConfig{Host: alias, HostName: alias}
	if sshCfg != nil {
		hostCfg = sshCfg.GetHost(alias)
	}

	targetAddr := hostCfg.HostName
	if targetAddr == "" {
		targetAddr = alias
	}

	var mergedOpts []Option

	if hostCfg.Port > 0 {
		mergedOpts = append(mergedOpts, WithPort(hostCfg.Port))
	}

	for _, idFile := range hostCfg.IdentityFiles {
		expanded := expandHome(idFile)
		if _, err := os.Stat(expanded); err == nil {
			mergedOpts = append(mergedOpts, WithKeyFile(expanded, ""))
		}
	}

	if strings.EqualFold(hostCfg.StrictHostKeyChecking, "no") {
		mergedOpts = append(mergedOpts, WithInsecureIgnoreHostKey())
	}

	if hostCfg.UserKnownHostsFile != "" {
		mergedOpts = append(mergedOpts, WithKnownHosts(expandHome(hostCfg.UserKnownHostsFile)))
	}

	mergedOpts = append(mergedOpts, opts...)

	return New(ctx, hostCfg.User, targetAddr, mergedOpts...)
}

func expandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}

	return path
}
