// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package traffic

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lemon4ksan/aoni/cmd/vortex/internal/base"
	"github.com/lemon4ksan/aoni/cmd/vortex/internal/inspector"
	"github.com/lemon4ksan/aoni/cmd/vortex/internal/text"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/cache"
	"github.com/lemon4ksan/aoni/cmd/vortex/lib/project"
)

func (c *Cmd) getRootDir() string {
	cwd, _ := os.Getwd()
	if root, _, err := project.FindRoot(cwd); err == nil && root != "" {
		return root
	}

	return cwd
}

func formatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}

	if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024.0)
	}

	return fmt.Sprintf("%.2f MB", float64(b)/(1024.0*1024.0))
}

func (c *Cmd) runList(_ context.Context, _ []string, stdout, _ io.Writer) error {
	rootDir := c.getRootDir()

	list, err := cache.ListTraffic(rootDir)
	if err != nil {
		return fmt.Errorf("listing cache: %w", err)
	}

	if len(list) == 0 {
		fmt.Fprintf(
			stdout,
			"No traffic captures stored in .vortex/cache/traffic (use 'vortex traffic store <file.har>')\n",
		)

		return nil
	}

	doc := text.NewDocument().
		Title("⚡", "Vortex Traffic Cache (.vortex/cache/traffic)")
	defer doc.Release()

	headers := []string{"ID", "ORIGINAL FILE", "DOMAINS", "ENDPOINTS", "RAW -> GZ", "SANITIZED", "DATE"}
	rows := make([][]string, 0, len(list))

	for _, e := range list {
		originsStr := strings.Join(e.Origins, ", ")
		if len(originsStr) > 28 {
			originsStr = originsStr[:25] + "..."
		}

		origFile := e.OriginalFile
		if len(origFile) > 28 {
			origFile = origFile[:25] + "..."
		}

		sizeRatio := fmt.Sprintf("%s -> %s", formatBytes(e.SizeBytes), formatBytes(e.CompressedBytes))

		sanStr := "✔ yes"
		if !e.Sanitized {
			sanStr = "raw"
		}

		rows = append(rows, []string{
			e.ID,
			origFile,
			originsStr,
			strconv.Itoa(e.EndpointCount),
			sizeRatio,
			sanStr,
			e.StoredAt.Format("2006-01-02 15:04"),
		})
	}

	doc.Table(headers, rows...)
	doc.Field("Total Sessions", strconv.Itoa(len(list)))

	return doc.RenderTo(stdout, text.DefaultTerminalRenderer)
}

type rawHAREntry struct {
	StartedDateTime string `json:"startedDateTime"`
	Request         struct {
		Method  string `json:"method"`
		URL     string `json:"url"`
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
		PostData struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"postData"`
	} `json:"request"`
	Response struct {
		Status     int    `json:"status"`
		StatusText string `json:"statusText"`
		Headers    []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
		Content struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
		} `json:"content"`
	} `json:"response"`
	Time float64 `json:"time"`
}

type rawHARLog struct {
	Log struct {
		Entries []rawHAREntry `json:"entries"`
	} `json:"log"`
}

func (c *Cmd) runShow(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("traffic inspect", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		entryIdx int
		filter   string
		allFlag  bool
		uiFlag   bool
		port     int
	)

	base.IntVar(fs, &entryIdx, "entry", "", -1, "Inspect specific entry index (0-based) in full detail")
	base.StringVar(fs, &filter, "filter", "", "", "Filter entries by endpoint or URL substring")
	base.BoolVar(fs, &allFlag, "all", "", false, "Include static assets (scripts, styles, images)")
	base.BoolVar(fs, &uiFlag, "ui", "", false, "Launch interactive web inspector dashboard")
	base.IntVar(fs, &port, "port", "", 8999, "Port for web dashboard (used with -ui)")

	posArgs, err := base.ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	if len(posArgs) == 0 {
		return errors.New("missing session ID or HAR file (e.g. 'vortex traffic inspect <session_name|file.har>')")
	}

	target := posArgs[0]
	if entryIdx == -1 && len(posArgs) > 1 {
		if idx, err := strconv.Atoi(posArgs[1]); err == nil {
			entryIdx = idx
		}
	}

	rootDir := c.getRootDir()

	var (
		data      []byte
		sessionID string
	)

	// Check if target is a local file on disk
	cleanTarget := filepath.Clean(target)
	if _, err := os.Stat(cleanTarget); err == nil {
		content, readErr := os.ReadFile(cleanTarget) //nolint:gosec
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", target, readErr)
		}

		data = content
		sessionID = filepath.Base(target)
	} else {
		trafficData, entry, getErr := cache.GetTraffic(rootDir, target)
		if getErr != nil {
			return getErr
		}

		data = trafficData
		sessionID = entry.ID
	}

	var har rawHARLog
	if err := json.Unmarshal(data, &har); err != nil {
		return fmt.Errorf("parsing HAR payload: %w", err)
	}

	entries := har.Log.Entries
	if len(entries) == 0 {
		fmt.Fprintf(stdout, "⚡ Traffic Session %s: 0 captured requests found\n", sessionID)

		return nil
	}

	if uiFlag {
		return c.runWebUI(ctx, sessionID, entries, port, stdout)
	}

	// Mode 1: Single entry detail inspection
	if entryIdx >= 0 {
		if entryIdx >= len(entries) {
			return fmt.Errorf("entry index #%d out of range (total entries: %d)", entryIdx, len(entries))
		}

		e := entries[entryIdx]
		fmt.Fprintf(stdout, "⚡ Traffic Entry #%d in %s\n", entryIdx, sessionID)
		fmt.Fprintf(stdout, "  Method:       %s\n", e.Request.Method)
		fmt.Fprintf(stdout, "  URL:          %s\n", e.Request.URL)
		fmt.Fprintf(stdout, "  Status:       %d %s\n", e.Response.Status, e.Response.StatusText)

		if e.Time > 0 {
			fmt.Fprintf(stdout, "  Duration:     %.0f ms\n", e.Time)
		}

		if e.Request.PostData.MimeType != "" {
			fmt.Fprintf(stdout, "  Req MIME:     %s\n", e.Request.PostData.MimeType)
		}

		if e.Response.Content.MimeType != "" {
			fmt.Fprintf(stdout, "  Resp MIME:    %s\n", e.Response.Content.MimeType)
		}

		// Request Body
		fmt.Fprintf(stdout, "\n▶ Request Body:\n")

		reqText := e.Request.PostData.Text
		if reqText == "" {
			fmt.Fprintf(stdout, "  (empty)\n")
		} else {
			cleanReq := strings.TrimPrefix(reqText, ")]}'\n")

			var parsedReq any
			if err := json.Unmarshal([]byte(cleanReq), &parsedReq); err == nil {
				pretty, _ := json.MarshalIndent(parsedReq, "  ", "  ")
				fmt.Fprintf(stdout, "  %s\n", string(pretty))
			} else {
				lines := strings.Split(reqText, "\n")
				if len(lines) > 50 {
					lines = append(lines[:50], fmt.Sprintf("... (%d lines truncated)", len(lines)-50))
				}

				for _, line := range lines {
					fmt.Fprintf(stdout, "  %s\n", line)
				}
			}
		}

		// Response Body
		fmt.Fprintf(stdout, "\n◀ Response Body:\n")

		respText := e.Response.Content.Text
		if respText == "" {
			fmt.Fprintf(stdout, "  (empty)\n")
		} else {
			cleanResp := strings.TrimPrefix(respText, ")]}'\n")

			var parsedResp any
			if err := json.Unmarshal([]byte(cleanResp), &parsedResp); err == nil {
				pretty, _ := json.MarshalIndent(parsedResp, "  ", "  ")
				fmt.Fprintf(stdout, "  %s\n", string(pretty))
			} else {
				lines := strings.Split(respText, "\n")
				if len(lines) > 50 {
					lines = append(lines[:50], fmt.Sprintf("... (%d lines truncated)", len(lines)-50))
				}

				for _, line := range lines {
					fmt.Fprintf(stdout, "  %s\n", line)
				}
			}
		}

		return nil
	}

	// Mode 2: Summary table of entries
	doc := text.NewDocument().
		Title("⚡", fmt.Sprintf("Traffic Session: %s (%d total entries)", sessionID, len(entries)))
	defer doc.Release()

	headers := []string{"ENTRY", "METHOD", "ENDPOINT / URL", "STATUS", "REQ PREVIEW", "RESP PREVIEW"}
	rows := make([][]string, 0, len(entries))

	displayed := 0
	for i, e := range entries {
		url := e.Request.URL
		if !allFlag {
			low := strings.ToLower(url)
			if strings.Contains(low, ".js") || strings.Contains(low, ".css") || strings.Contains(low, ".png") ||
				strings.Contains(low, ".svg") || strings.Contains(low, ".woff") || strings.Contains(low, ".ico") {
				continue
			}
		}

		if filter != "" && !strings.Contains(strings.ToLower(url), strings.ToLower(filter)) {
			continue
		}

		displayed++

		reqSnippet := strings.ReplaceAll(e.Request.PostData.Text, "\n", " ")
		if len(reqSnippet) > 35 {
			reqSnippet = reqSnippet[:32] + "..."
		}

		if reqSnippet == "" {
			reqSnippet = "-"
		}

		respSnippet := strings.TrimPrefix(e.Response.Content.Text, ")]}'\n")

		respSnippet = strings.ReplaceAll(respSnippet, "\n", " ")
		if len(respSnippet) > 35 {
			respSnippet = respSnippet[:32] + "..."
		}

		if respSnippet == "" {
			respSnippet = "-"
		}

		dispURL := url
		if len(dispURL) > 55 {
			dispURL = dispURL[:52] + "..."
		}

		statusStr := strconv.Itoa(e.Response.Status)
		if e.Response.Status == 200 {
			statusStr = "200 OK"
		}

		rows = append(rows, []string{
			fmt.Sprintf("#%d", i),
			e.Request.Method,
			dispURL,
			statusStr,
			reqSnippet,
			respSnippet,
		})
	}

	doc.Table(headers, rows...)
	doc.Field("Displayed", fmt.Sprintf("%d of %d request(s)", displayed, len(entries)))
	doc.Info("Tip", fmt.Sprintf("Run 'vortex traffic inspect %s --entry=<N>' to view full JSON payload", sessionID))

	return doc.RenderTo(stdout, text.DefaultTerminalRenderer)
}

func (c *Cmd) runMove(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return c.executeStore(ctx, "traffic move", args, true, stdout, stderr)
}

func (c *Cmd) runStore(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return c.executeStore(ctx, "traffic store", args, false, stdout, stderr)
}

func (c *Cmd) executeStore(
	_ context.Context,
	cmdName string,
	args []string,
	defaultMove bool,
	stdout, stderr io.Writer,
) error {
	fs := flag.NewFlagSet(cmdName, flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		moveFlag     bool
		sanitizeFlag bool
	)

	base.BoolVar(
		fs,
		&moveFlag,
		"move",
		"",
		defaultMove,
		"Move original file into cache (deletes source file on success)",
	)
	base.BoolVar(fs, &sanitizeFlag, "sanitize", "", true, "Sanitize tokens and credentials before storing")

	files, err := base.ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	if len(files) == 0 {
		return fmt.Errorf("no HAR files specified (e.g. 'vortex %s session.har')", cmdName)
	}

	rootDir := c.getRootDir()
	vault, vaultPath, _ := cache.LoadSecrets(rootDir)

	storedCount := 0

	for _, rawPath := range files {
		for _, fPath := range strings.Split(rawPath, ",") {
			fPath = strings.TrimSpace(fPath)
			if fPath == "" {
				continue
			}

			data, err := os.ReadFile(fPath)
			if err != nil {
				fmt.Fprintf(stderr, "⚠️  Failed reading %s: %v\n", fPath, err)
				continue
			}

			entry, secrets, err := cache.StoreTraffic(rootDir, fPath, data, moveFlag, sanitizeFlag)
			if err != nil {
				fmt.Fprintf(stderr, "⚠️  Failed caching %s: %v\n", fPath, err)
				continue
			}

			// Store extracted secrets in vault
			for k, s := range secrets {
				vault.SetWithTarget(k, s.Value, entry.ID, s.Header, s.Query, s.Cookie)
			}

			action := "Stored"
			if moveFlag {
				action = "Moved"
			}

			fmt.Fprintf(
				stdout,
				"✔ %s %s in cache as %s (%s -> %s, %d endpoints)\n",
				action,
				fPath,
				entry.ID,
				formatBytes(entry.SizeBytes),
				formatBytes(entry.CompressedBytes),
				entry.EndpointCount,
			)

			storedCount++
		}
	}

	if len(vault.Secrets) > 0 {
		_ = vault.Save(vaultPath)
		fmt.Fprintf(stdout, "✔ Updated secrets vault with %d credential(s)\n", len(vault.Secrets))
	}

	return nil
}

func (c *Cmd) runSanitize(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("traffic sanitize", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var outFile string
	base.StringVar(fs, &outFile, "out", "", "sanitized.har", "Output sanitized HAR file path")

	posArgs, err := base.ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	if len(posArgs) == 0 {
		return errors.New("missing input HAR file (e.g. 'vortex traffic sanitize session.har -out=safe.har')")
	}

	srcFile := posArgs[0]

	data, err := os.ReadFile(srcFile)
	if err != nil {
		return fmt.Errorf("reading source HAR: %w", err)
	}

	sanitized, secrets, err := cache.SanitizeHAR(data)
	if err != nil {
		return fmt.Errorf("sanitizing HAR: %w", err)
	}

	if err := os.WriteFile(outFile, sanitized, 0o600); err != nil {
		return fmt.Errorf("writing sanitized HAR: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Exported Git-safe sanitized HAR to %s (%d bytes, %d secrets masked)\n",
		outFile, len(sanitized), len(secrets))

	return nil
}

func (c *Cmd) runExport(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("traffic export", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var outFile string
	base.StringVar(fs, &outFile, "out", "", "restored.har", "Output uncompressed HAR file path")

	posArgs, err := base.ParseInterspersedFlags(fs, args)
	if err != nil {
		return err
	}

	if len(posArgs) == 0 {
		return errors.New(
			"missing session ID or hash prefix (e.g. 'vortex traffic export session_name -out=restored.har')",
		)
	}

	rootDir := c.getRootDir()

	data, entry, err := cache.GetTraffic(rootDir, posArgs[0])
	if err != nil {
		return err
	}

	// Auto-decompress any compressed/encoded entries on export for clean human readability
	if cleaned, _, err := cache.SanitizeHAR(data); err == nil && len(cleaned) > 0 {
		data = cleaned
	}

	if err := os.WriteFile(outFile, data, 0o600); err != nil {
		return fmt.Errorf("writing restored HAR: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Restored %s to %s (%s uncompressed)\n", entry.ID, outFile, formatBytes(int64(len(data))))

	return nil
}

func (c *Cmd) runSecrets(_ context.Context, args []string, stdout, stderr io.Writer) error {
	rootDir := c.getRootDir()

	vault, vaultPath, err := cache.LoadSecrets(rootDir)
	if err != nil {
		return fmt.Errorf("loading secrets vault: %w", err)
	}

	if len(args) == 0 || args[0] == "list" || args[0] == "ls" {
		secrets := vault.All()
		if len(secrets) == 0 {
			fmt.Fprintf(stdout, "No credentials in local vault .vortex/cache/secrets.json\n")
			return nil
		}

		doc := text.NewDocument().
			Title("🔑", "Vortex Local Credentials Vault (.vortex/cache/secrets.json)")
		defer doc.Release()

		headers := []string{"KEY", "MASKED VALUE", "ORIGIN", "UPDATED"}
		rows := make([][]string, 0, len(secrets))

		for _, s := range secrets {
			origin := s.Origin
			if origin == "" {
				origin = "manual"
			}

			rows = append(rows, []string{
				s.Key,
				s.Masked,
				origin,
				s.UpdatedAt.Format("2006-01-02 15:04"),
			})
		}

		doc.Table(headers, rows...)
		doc.Info("Auto-injection", "Use 'option.FromVortexCache()' in client to auto-inject credentials at runtime.")

		return doc.RenderTo(stdout, text.DefaultTerminalRenderer)
	}

	sub := strings.ToLower(args[0])
	switch sub {
	case "get":
		if len(args) < 2 {
			return errors.New("missing secret key name (e.g. 'vortex traffic secrets get AUTH_TOKEN')")
		}

		val, ok := vault.Get(args[1])
		if !ok {
			return fmt.Errorf("secret %q not found in vault", args[1])
		}

		fmt.Fprintln(stdout, val)

		return nil

	case "set":
		if len(args) < 3 {
			return errors.New("usage: vortex traffic secrets set <KEY> <VALUE>")
		}

		vault.Set(args[1], args[2], "manual")

		if err := vault.Save(vaultPath); err != nil {
			return fmt.Errorf("saving vault: %w", err)
		}

		fmt.Fprintf(stdout, "✔ Saved %s to local secrets vault\n", args[1])

		return nil

	case "clear", "purge":
		vault.Clear()

		if err := vault.Save(vaultPath); err != nil {
			return fmt.Errorf("saving vault: %w", err)
		}

		fmt.Fprintf(stdout, "✔ Cleared all credentials from local vault\n")

		return nil

	default:
		return fmt.Errorf("unknown secrets action %q (use list, get, set, clear)", sub)
	}
}

func (c *Cmd) runDelete(_ context.Context, args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return errors.New("missing session ID or hash to delete (e.g. `vortex traffic rm aistudio.chat_with_settings`)")
	}

	rootDir := c.getRootDir()
	for _, id := range args {
		deleted, err := cache.DeleteTrafficSession(rootDir, id)
		if err != nil {
			return fmt.Errorf("deleting session %q: %w", id, err)
		}

		if deleted {
			fmt.Fprintf(stdout, "✔ Deleted cached traffic session %q\n", id)
		} else {
			fmt.Fprintf(stdout, "⚠️ Session %q not found in cache\n", id)
		}
	}

	return nil
}

func (c *Cmd) runPrune(_ context.Context, args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("traffic prune", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		allFlag       bool
		olderThanFlag string
	)

	base.BoolVar(fs, &allFlag, "all", "", false, "Remove all cached traffic sessions")
	base.StringVar(fs, &olderThanFlag, "older-than", "", "", "Remove sessions older than duration (e.g. 720h, 30d)")

	if _, err := base.ParseInterspersedFlags(fs, args); err != nil {
		return err
	}

	var dur time.Duration
	if olderThanFlag != "" {
		s := olderThanFlag
		if strings.HasSuffix(s, "d") {
			daysStr := strings.TrimSuffix(s, "d")

			var days int

			_, _ = fmt.Sscanf(daysStr, "%d", &days)
			dur = time.Duration(days) * 24 * time.Hour
		} else {
			var parseErr error

			dur, parseErr = time.ParseDuration(s)
			if parseErr != nil {
				return fmt.Errorf("invalid duration format %q: %w", s, parseErr)
			}
		}
	}

	rootDir := c.getRootDir()

	removed, err := cache.PruneTraffic(rootDir, dur, allFlag)
	if err != nil {
		return fmt.Errorf("pruning cache: %w", err)
	}

	fmt.Fprintf(stdout, "✔ Removed %d cached traffic session(s)\n", removed)

	return nil
}

func (c *Cmd) runWebUI(ctx context.Context, sessionID string, entries []rawHAREntry, port int, stdout io.Writer) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)

	ti := inspector.NewTrafficInspector(addr)
	if err := ti.Serve(); err != nil {
		return fmt.Errorf("starting web inspector: %w", err)
	}

	defer ti.Close()

	for idx, e := range entries {
		startedTime, _ := time.Parse(time.RFC3339Nano, e.StartedDateTime)
		if startedTime.IsZero() {
			startedTime, _ = time.Parse(time.RFC3339, e.StartedDateTime)
		}

		if startedTime.IsZero() {
			startedTime = time.Now()
		}

		headersMap := make(map[string]string, len(e.Request.Headers))
		for _, h := range e.Request.Headers {
			headersMap[h.Name] = h.Value
		}

		respHeadersMap := make(map[string]string, len(e.Response.Headers))
		for _, h := range e.Response.Headers {
			respHeadersMap[h.Name] = h.Value
		}

		duration := time.Duration(e.Time * float64(time.Millisecond))

		capReq := inspector.CapturedRequest{
			ID:              int64(idx + 1),
			Timestamp:       startedTime,
			Method:          e.Request.Method,
			URL:             e.Request.URL,
			Status:          e.Response.Status,
			StatusText:      e.Response.StatusText,
			Duration:        duration,
			DurationStr:     fmt.Sprintf("%.1fms", e.Time),
			RequestHeaders:  headersMap,
			ResponseHeaders: respHeadersMap,
			RequestBody:     e.Request.PostData.Text,
			ResponseBody:    e.Response.Content.Text,
		}
		ti.AddCapturedRequest(capReq)
	}

	dashboardURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	fmt.Fprintf(stdout, "\n⚡ Vortex Web Inspector active for session: %s (%d requests)\n", sessionID, len(entries))
	fmt.Fprintf(stdout, "   Dashboard: %s\n", dashboardURL)
	fmt.Fprintf(stdout, "   Press Ctrl+C to stop.\n\n")

	if ctx != nil {
		<-ctx.Done()
	}

	return nil
}
