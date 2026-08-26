// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	fheader "github.com/lemon4ksan/foundation/net/http/header"
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

	// 5. Fetch iOS
	if iosVer := fetchIOSVersion(client); iosVer != "" {
		fmt.Printf("IOS=%s\n", iosVer)
	}

	// 6. Fetch Android
	if androidVer := fetchAndroidVersion(client); androidVer != "" {
		fmt.Printf("ANDROID=%s\n", androidVer)
	}
}

type ipswResponse struct {
	Firmwares []struct {
		Version string `json:"version"`
		Signed  bool   `json:"signed"`
	} `json:"firmwares"`
}

func fetchIOSVersion(client *http.Client) string {
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

	var ipsw ipswResponse
	if err := json.NewDecoder(resp.Body).Decode(&ipsw); err == nil {
		for _, fw := range ipsw.Firmwares {
			if fw.Signed && fw.Version != "" {
				return fw.Version
			}
		}
	}

	return ""
}

type wikiResponse struct {
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

func fetchAndroidVersion(client *http.Client) string {
	req, err := http.NewRequest(fheader.MethodGet, "https://www.wikidata.org/w/api.php?action=wbgetentities&format=json&ids=Q94&props=claims", nil) //nolint:noctx
	if err != nil {
		return ""
	}
	req.Header.Set(fheader.UserAgent, "Mozilla/5.0")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	var wiki wikiResponse
	if err := json.NewDecoder(resp.Body).Decode(&wiki); err == nil {
		for _, claim := range wiki.Entities.Q94.Claims.P348 {
			if claim.Rank == "preferred" && claim.Mainsnak.DataValue.Value != "" {
				val := claim.Mainsnak.DataValue.Value
				parts := strings.Split(val, ".")
				return parts[0]
			}
		}
	}

	return ""
}

func fetchChromeVersion(client *http.Client, platform string) string {
	req, err := http.NewRequest( //nolint:noctx
		fheader.MethodGet,
		"https://chromiumdash.appspot.com/fetch_releases?platform="+platform+"&channel=Stable&num=1",
		nil,
	)
	if err != nil {
		return ""
	}

	req.Header.Set(fheader.UserAgent, "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

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
