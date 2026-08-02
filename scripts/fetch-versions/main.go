// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Release struct {
	Version string `json:"version"`
}

type FirefoxVersions struct {
	Latest string `json:"LATEST_FIREFOX_VERSION"`
}

func main() {
	client := &http.Client{Timeout: 10 * time.Second}

	// 1. Fetch Chrome Windows
	if ver := fetchChromeVersion(client, "Windows"); ver != "" {
		fmt.Printf("CHROME_WIN=%s\n", ver)
	}

	// 2. Fetch Chrome Android
	if ver := fetchChromeVersion(client, "Android"); ver != "" {
		fmt.Printf("CHROME_ANDROID=%s\n", ver)
	}

	// 3. Fetch Chrome iOS
	if ver := fetchChromeVersion(client, "iOS"); ver != "" {
		fmt.Printf("CHROME_IOS=%s\n", ver)
	}

	// 4. Fetch Firefox
	resp, err := client.Get("https://product-details.mozilla.org/1.0/firefox_versions.json") //nolint:noctx
	if err == nil {
		var ff FirefoxVersions
		if json.NewDecoder(resp.Body).Decode(&ff) == nil && ff.Latest != "" {
			fmt.Printf("FIREFOX=%s\n", ff.Latest)
		}

		_ = resp.Body.Close()
	}
}

func fetchChromeVersion(client *http.Client, platform string) string {
	req, err := http.NewRequest( //nolint:noctx
		"GET",
		"https://chromiumdash.appspot.com/fetch_releases?platform="+platform+"&channel=Stable&num=1",
		nil,
	)
	if err != nil {
		return ""
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var releases []Release
	if err := json.NewDecoder(resp.Body).Decode(&releases); err == nil && len(releases) > 0 {
		return releases[0].Version
	}

	return ""
}
