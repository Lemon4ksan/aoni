// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package core

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
	"github.com/lemon4ksan/aoni/internal/codegen/cache"
	codeparser "github.com/lemon4ksan/aoni/internal/codegen/parser"
	"github.com/lemon4ksan/aoni/internal/tui"
)

// CmdSmoke performs rapid live connectivity and schema smoke checks on API contracts.
type CmdSmoke struct{}

func (c *CmdSmoke) Name() string      { return "smoke" }
func (c *CmdSmoke) Aliases() []string { return []string{"probe", "ping"} }
func (c *CmdSmoke) Synopsis() string {
	return "Rapidly probe live endpoints using contract secrets and render latency table"
}

func (c *CmdSmoke) Usage() string {
	return "vortex smoke [file.go|contract] [--timeout=5s] [--all] [flags]"
}

type smokeResult struct {
	Service    string
	Method     string
	HTTPVerb   string
	Endpoint   string
	StatusCode int
	Latency    time.Duration
	TLSVersion string
	Err        error
}

func (c *CmdSmoke) Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("smoke", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		timeoutFlag = fs.Duration("timeout", 5*time.Second, "Per-endpoint probe timeout")
		allFlag     = fs.Bool("all", false, "Probe all methods including POST/PUT (default: only safe GET/HEAD)")
		dirFlag     = fs.String("dir", "", "Target workspace directory (default: current root)")
	)

	fs.Usage = func() {
		fmt.Fprintf(stderr, "vortex smoke — Rapid Live API Probe & Connectivity Checker\n\n")
		fmt.Fprintf(stderr, "Usage:\n")
		fmt.Fprintf(stderr, "  vortex smoke [contract.go] [--timeout=5s] [--all]\n\n")
		fmt.Fprintf(stderr, "Examples:\n")
		fmt.Fprintf(
			stderr,
			"  vortex smoke                                  # Probe safe GET/HEAD endpoints across workspace\n",
		)
		fmt.Fprintf(stderr, "  vortex smoke pkg/api/user.go                  # Probe specific contract endpoints\n")
		fmt.Fprintf(stderr, "  vortex smoke pkg/api/api.go --all             # Probe all RPC and REST endpoints\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
	}

	posArgs, err := base.ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	rt, _ := base.NewRuntime(*dirFlag)

	targetFiles := rt.CollectFiles(posArgs)
	if len(targetFiles) == 0 {
		return fmt.Errorf("no contract files found in %s", rt.RootDir)
	}

	rootDir := rt.RootDir
	vault, _, _ := cache.LoadSecrets(rootDir)

	var results []smokeResult

	p := codeparser.NewParser()

	fmt.Fprintf(stdout, "Probing live endpoints across %d contract file(s)...\n\n", len(targetFiles))

	httpClient := &http.Client{
		Timeout: *timeoutFlag,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false}, // #nosec G402
		},
	}

	for _, fPath := range targetFiles {
		root, err := p.ParseFile(fPath)
		if err != nil || len(root.Services) == 0 {
			continue
		}

		for _, svc := range root.Services {
			baseURL := svc.BaseURL
			if baseURL == "" {
				baseURL = "https://127.0.0.1"
			}

			for _, m := range svc.Methods {
				httpVerb := strings.ToUpper(m.HTTPMethod)
				if httpVerb == "" {
					httpVerb = "GET"
				}

				isSafe := httpVerb == "GET" || httpVerb == "HEAD" ||
					strings.HasPrefix(m.Name, "Get") || strings.HasPrefix(m.Name, "List") ||
					strings.HasPrefix(m.Name, "Fetch") || strings.HasPrefix(m.Name, "Check")

				if !isSafe && !*allFlag {
					continue
				}

				rawPath := ""
				if m.Path != nil {
					rawPath = m.Path.RawTemplate
				}

				// Interpolate path with secrets
				resolvedPath := rawPath
				if vault != nil {
					for k, s := range vault.Secrets {
						resolvedPath = strings.ReplaceAll(resolvedPath, "{"+k+"}", s.Value)
						resolvedPath = strings.ReplaceAll(resolvedPath, "${"+k+"}", s.Value)
					}
				}

				targetURL := baseURL
				if !strings.HasSuffix(targetURL, "/") && !strings.HasPrefix(resolvedPath, "/") && resolvedPath != "" {
					targetURL += "/"
				}

				targetURL += resolvedPath

				// Create request
				reqCtx, cancel := context.WithTimeout(ctx, *timeoutFlag)

				req, err := http.NewRequestWithContext(reqCtx, httpVerb, targetURL, nil)
				if err != nil {
					cancel()

					results = append(results, smokeResult{
						Service:  svc.Name,
						Method:   m.Name,
						HTTPVerb: httpVerb,
						Endpoint: targetURL,
						Err:      err,
					})

					continue
				}

				for _, h := range svc.Headers {
					val := h.StaticValue
					if vault != nil {
						for k, s := range vault.Secrets {
							val = strings.ReplaceAll(val, "${"+k+"}", s.Value)
						}
					}

					req.Header.Set(h.Key, val)
				}

				// Apply method headers
				for _, h := range m.Headers {
					val := h.StaticValue
					if vault != nil {
						for k, s := range vault.Secrets {
							val = strings.ReplaceAll(val, "${"+k+"}", s.Value)
						}
					}

					req.Header.Set(h.Key, val)
				}

				start := time.Now()
				resp, err := httpClient.Do(req)
				latency := time.Since(start)

				cancel()

				tlsVer := "TLS 1.3"

				statusCode := 0
				if err == nil {
					statusCode = resp.StatusCode
					if resp.TLS != nil {
						switch resp.TLS.Version {
						case tls.VersionTLS13:
							tlsVer = "TLS 1.3"
						case tls.VersionTLS12:
							tlsVer = "TLS 1.2"
						default:
							tlsVer = "TLS"
						}
					}

					_ = resp.Body.Close()
				}

				u, _ := url.Parse(targetURL)

				displayPath := targetURL
				if u != nil {
					displayPath = u.Path
					if u.RawQuery != "" {
						displayPath += "?" + u.RawQuery
					}
				}

				results = append(results, smokeResult{
					Service:    svc.Name,
					Method:     m.Name,
					HTTPVerb:   httpVerb,
					Endpoint:   displayPath,
					StatusCode: statusCode,
					Latency:    latency,
					TLSVersion: tlsVer,
					Err:        err,
				})
			}
		}
	}

	if len(results) == 0 {
		fmt.Fprintf(stdout, "No testable GET/HEAD endpoints found. Pass --all to include state-modifying requests.\n")
		return nil
	}

	// Render TUI Table
	tbl := tui.NewTable("SERVICE", "VERB", "ENDPOINT", "STATUS", "LATENCY", "TLS")
	for _, r := range results {
		var statusStr string
		switch {
		case r.Err != nil:
			statusStr = "ERR: " + r.Err.Error()
			if len(statusStr) > 25 {
				statusStr = statusStr[:22] + "..."
			}

		case r.StatusCode >= 200 && r.StatusCode < 300:
			statusStr = fmt.Sprintf("%d OK", r.StatusCode)

		case r.StatusCode >= 400:
			statusStr = strconv.Itoa(r.StatusCode)

		default:
			statusStr = fmt.Sprintf("HTTP %d", r.StatusCode)
		}

		latStr := fmt.Sprintf("%d ms", r.Latency.Milliseconds())
		tbl.AddRow(r.Service, r.HTTPVerb, r.Endpoint, statusStr, latStr, r.TLSVersion)
	}

	_ = tbl.Render(stdout)

	return nil
}
