// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package oracle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Supervisor manages the lifecycle of a local Oracle sidecar subprocess.
type Supervisor struct {
	client      *Client
	sidecarPath string
	cmd         *exec.Cmd
	mu          sync.Mutex
}

// NewSupervisor creates a supervisor for a sidecar script.
func NewSupervisor(client *Client, sidecarPath string) *Supervisor {
	return &Supervisor{
		client:      client,
		sidecarPath: sidecarPath,
	}
}

// EnsureRunning checks if the sidecar is alive, and if not, launches the sidecar script automatically.
func (c *Client) EnsureRunning(ctx context.Context, sidecarPath string) error {
	s := NewSupervisor(c, sidecarPath)
	return s.EnsureRunning(ctx)
}

// EnsureRunning verifies that the sidecar is responsive, launching it if necessary.
func (s *Supervisor) EnsureRunning(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already running
	if status, err := s.client.Status(ctx); err == nil && status != nil {
		return nil
	}

	sidecarFile := s.resolveSidecarPath()
	if sidecarFile == "" {
		return errors.New("sidecar script file not found (specify valid path to server.js / sidecar.gen.js)")
	}

	dir := filepath.Dir(sidecarFile)
	script := filepath.Base(sidecarFile)

	//nolint:gosec // Launch local Node.js sidecar supervisor
	cmd := exec.CommandContext(ctx, "node", script)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting oracle sidecar process: %w", err)
	}

	s.cmd = cmd

	// Poll until ready
	for range 30 {
		time.Sleep(350 * time.Millisecond)

		if status, err := s.client.Status(ctx); err == nil && status != nil {
			return nil
		}
	}

	return errors.New("oracle sidecar failed to report healthy status within timeout")
}

// Stop terminates the running sidecar process if managed by this supervisor.
func (s *Supervisor) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}

	return nil
}

func (s *Supervisor) resolveSidecarPath() string {
	if s.sidecarPath != "" {
		if _, err := os.Stat(s.sidecarPath); err == nil {
			return s.sidecarPath
		}
	}

	candidates := []string{
		"sidecar/sidecar.gen.js",
		"sidecar/server.js",
		"../sidecar/sidecar.gen.js",
		"../sidecar/server.js",
		"../../sidecar/sidecar.gen.js",
		"../../sidecar/server.js",
		"sidecar.gen.js",
		"server.js",
	}

	for _, cand := range candidates {
		if fi, err := os.Stat(cand); err == nil && !fi.IsDir() {
			return cand
		}
	}

	return ""
}
