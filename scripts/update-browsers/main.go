// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	fheader "github.com/lemon4ksan/foundation/net/http/header"
)

const (
	chromeFile  = "fingerprint/profiles/chrome/chrome.go"
	firefoxFile = "fingerprint/profiles/firefox/firefox.go"
	safariFile  = "fingerprint/profiles/safari/safari.go"
)

type versionInfo struct {
	chromeWin     string
	chromeAndroid string
	chromeIOS     string
	chromeMajor   string
	firefox       string
	firefoxMajor  string
	ios           string
	iosUA         string
	android       string
}

func main() {
	dryRun := flag.Bool("dry-run", false, "dry run (do not write changes)")

	flag.Parse()

	fmt.Println("=== Fetching Latest Browser Versions ===")

	client := &http.Client{Timeout: 15 * time.Second}

	info := fetchVersions(client)
	if info.chromeWin == "" || info.firefox == "" {
		fmt.Fprintf(os.Stderr, "ERROR: failed to fetch browser versions\n")
		os.Exit(1)
	}

	fmt.Printf("Latest Chrome Win:     %s (Major: %s)\n", info.chromeWin, info.chromeMajor)
	fmt.Printf("Latest Chrome Android: %s\n", info.chromeAndroid)
	fmt.Printf("Latest Chrome iOS:     %s\n", info.chromeIOS)
	fmt.Printf("Latest Firefox:        %s (Major/Minor: %s)\n", info.firefox, info.firefoxMajor)

	if info.ios != "" {
		fmt.Printf("Latest iOS:            %s\n", info.ios)
	}

	if info.android != "" {
		fmt.Printf("Latest Android:        %s\n", info.android)
	}

	fmt.Println()

	updated := updateChrome(info, *dryRun)

	// 1. Update Chrome

	// 2. Update Firefox
	if updateFirefox(info, *dryRun) {
		updated = true
	}

	// 3. Update Safari
	if updateSafari(info, *dryRun) {
		updated = true
	}

	writeGitHubOutput(info, updated)

	if *dryRun {
		fmt.Println("\n[DRY RUN] No files modified.")
		return
	}

	if updated {
		fmt.Println("\n=== Verifying Profiles & Tests ===")

		cmd := exec.CommandContext(context.Background(), "go", "test", "./fingerprint/profiles/...") // #nosec G204
		cmd.Stdout = os.Stdout

		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: profile tests failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Println("✓ All profile tests passed successfully!")
	} else {
		fmt.Println("All browser profiles are already up to date.")
	}
}

func writeGitHubOutput(info versionInfo, updated bool) {
	ghOutput := os.Getenv("GITHUB_OUTPUT")
	if ghOutput == "" {
		return
	}

	f, err := os.OpenFile(ghOutput, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()

	fmt.Fprintf(f, "updated=%t\n", updated)
	fmt.Fprintf(f, "chrome_version=%s\n", info.chromeMajor)
	fmt.Fprintf(f, "chrome_win_version=%s\n", info.chromeWin)
	fmt.Fprintf(f, "chrome_android_version=%s\n", info.chromeAndroid)
	fmt.Fprintf(f, "chrome_ios_version=%s\n", info.chromeIOS)
	fmt.Fprintf(f, "firefox_version=%s\n", info.firefox)
	fmt.Fprintf(f, "ios_version=%s\n", info.ios)
	fmt.Fprintf(f, "android_version=%s\n", info.android)
}

func fetchVersions(client *http.Client) versionInfo {
	var info versionInfo

	info.chromeWin = fetchChrome(client, "Windows")
	info.chromeAndroid = fetchChrome(client, "Android")

	info.chromeIOS = fetchChrome(client, "iOS")
	if info.chromeWin != "" {
		parts := strings.Split(info.chromeWin, ".")
		info.chromeMajor = parts[0]
	}

	info.firefox = fetchFirefox(client)
	if info.firefox != "" {
		parts := strings.Split(info.firefox, ".")
		if len(parts) >= 2 {
			info.firefoxMajor = parts[0] + "." + parts[1]
		} else {
			info.firefoxMajor = info.firefox
		}
	}

	info.ios = fetchIOS(client)
	if info.ios != "" {
		info.iosUA = strings.ReplaceAll(info.ios, ".", "_")
	}

	info.android = fetchAndroid(client)

	return info
}

func fetchChrome(client *http.Client, platform string) string {
	url := fmt.Sprintf("https://chromiumdash.appspot.com/fetch_releases?platform=%s&channel=Stable&num=1", platform)

	req, err := http.NewRequest(fheader.MethodGet, url, nil) //nolint:noctx
	if err != nil {
		return ""
	}

	req.Header.Set(fheader.UserAgent, "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var releases []struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&releases); err == nil && len(releases) > 0 {
		return releases[0].Version
	}

	return ""
}

func fetchFirefox(client *http.Client) string {
	resp, err := client.Get("https://product-details.mozilla.org/1.0/firefox_versions.json") //nolint:noctx
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var ff struct {
		Latest string `json:"LATEST_FIREFOX_VERSION"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ff); err == nil {
		return ff.Latest
	}

	return ""
}

func fetchIOS(client *http.Client) string {
	req, err := http.NewRequest(fheader.MethodGet, "https://api.ipsw.me/v4/device/iPhone16,2", nil) //nolint:noctx
	if err != nil {
		return ""
	}

	req.Header.Set(fheader.UserAgent, "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var ipsw struct {
		Firmwares []struct {
			Version string `json:"version"`
			Signed  bool   `json:"signed"`
		} `json:"firmwares"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ipsw); err == nil {
		for _, fw := range ipsw.Firmwares {
			if fw.Signed && fw.Version != "" {
				return fw.Version
			}
		}
	}

	return ""
}

func fetchAndroid(client *http.Client) string {
	req, err := http.NewRequestWithContext(
		context.Background(),
		fheader.MethodGet,
		"https://www.wikidata.org/w/api.php?action=wbgetentities&format=json&ids=Q94&props=claims",
		nil,
	)
	if err != nil {
		return ""
	}

	req.Header.Set(fheader.UserAgent, "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var wiki struct {
		Entities struct {
			Q94 struct {
				Claims struct {
					P348 []struct {
						Rank     string `json:"rank"`
						Mainsnak struct {
							DataValue struct {
								Value string `json:"value"`
							} `json:"datavalue"`
						} `json:"mainsnak"`
					} `json:"P348"`
				} `json:"claims"`
			} `json:"Q94"`
		} `json:"entities"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wiki); err == nil {
		for _, claim := range wiki.Entities.Q94.Claims.P348 {
			if claim.Rank == "preferred" && claim.Mainsnak.DataValue.Value != "" {
				parts := strings.Split(claim.Mainsnak.DataValue.Value, ".")
				return parts[0]
			}
		}
	}

	return ""
}

func updateChrome(info versionInfo, dryRun bool) bool {
	contentBytes, err := os.ReadFile(chromeFile)
	if err != nil {
		return false
	}

	content := string(contentBytes)
	original := content

	// Extract current major
	reSec := regexp.MustCompile(`"Google Chrome";v="(\d+)"`)

	m := reSec.FindStringSubmatch(content)
	if len(m) < 2 {
		return false
	}

	currMajor := m[1]

	if currMajor != info.chromeMajor {
		fmt.Printf("Updating Chrome: %s -> %s\n", currMajor, info.chromeMajor)
		content = strings.ReplaceAll(
			content,
			`"Google Chrome";v="`+currMajor+`"`,
			`"Google Chrome";v="`+info.chromeMajor+`"`,
		)
		content = strings.ReplaceAll(content, `"Chromium";v="`+currMajor+`"`, `"Chromium";v="`+info.chromeMajor+`"`)
		content = strings.ReplaceAll(content, `Chrome/`+currMajor+`.0.0.0`, `Chrome/`+info.chromeMajor+`.0.0.0`)
	}

	// Update Android UA
	reAndroid := regexp.MustCompile(`Chrome/\d+\.0\.\d+\.\d+ Mobile`)
	if info.chromeAndroid != "" {
		content = reAndroid.ReplaceAllString(content, `Chrome/`+info.chromeAndroid+` Mobile`)
	}

	// Update iOS UA
	reIOS := regexp.MustCompile(`CriOS/\d+\.0\.\d+\.\d+ Mobile`)
	if info.chromeIOS != "" {
		content = reIOS.ReplaceAllString(content, `CriOS/`+info.chromeIOS+` Mobile`)
	}

	if content != original {
		if !dryRun {
			if err := os.WriteFile(chromeFile, []byte(content), 0o600); err != nil { // #nosec G703
				fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", chromeFile, err)
				return false
			}
		}

		return true
	}

	return false
}

func updateFirefox(info versionInfo, dryRun bool) bool {
	contentBytes, err := os.ReadFile(firefoxFile)
	if err != nil {
		return false
	}

	content := string(contentBytes)
	original := content

	// Update rv: and Firefox/
	reFF := regexp.MustCompile(`Firefox/(\d+\.\d+)`)

	m := reFF.FindStringSubmatch(content)
	if len(m) >= 2 {
		currFF := m[1]
		if currFF != info.firefoxMajor {
			fmt.Printf("Updating Firefox: %s -> %s\n", currFF, info.firefoxMajor)
			content = strings.ReplaceAll(content, `rv:`+currFF, `rv:`+info.firefoxMajor)
			content = strings.ReplaceAll(content, `Firefox/`+currFF, `Firefox/`+info.firefoxMajor)
			content = strings.ReplaceAll(content, `FxiOS/`+currFF, `FxiOS/`+info.firefoxMajor)
		}
	}

	// Update Android OS version if available
	if info.android != "" {
		reAnd := regexp.MustCompile(`Android \d+;`)
		content = reAnd.ReplaceAllString(content, `Android `+info.android+`;`)
	}

	if content != original {
		if !dryRun {
			if err := os.WriteFile(firefoxFile, []byte(content), 0o600); err != nil { // #nosec G703
				fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", firefoxFile, err)
				return false
			}
		}

		return true
	}

	return false
}

func updateSafari(info versionInfo, dryRun bool) bool {
	contentBytes, err := os.ReadFile(safariFile)
	if err != nil {
		return false
	}

	content := string(contentBytes)
	original := content

	if info.ios != "" {
		parts := strings.Split(info.ios, ".")
		if len(parts) >= 2 {
			safariVer := parts[0] + "." + parts[1]
			reSaf := regexp.MustCompile(`Version/\d+\.\d+`)
			content = reSaf.ReplaceAllString(content, `Version/`+safariVer)
		}
	}

	if content != original {
		if !dryRun {
			if err := os.WriteFile(safariFile, []byte(content), 0o600); err != nil { // #nosec G703
				fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", safariFile, err)
				return false
			}
		}

		return true
	}

	return false
}
