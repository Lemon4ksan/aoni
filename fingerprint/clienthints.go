// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fingerprint

import (
	"strings"

	"github.com/lemon4ksan/aoni/fingerprint/profiles"
)

// ClientHints holds W3C High-Entropy and Low-Entropy Client Hints attributes.
type ClientHints struct {
	Brand           string
	FullVersion     string
	FullVersionList string
	Mobile          string
	Platform        string
	PlatformVersion string
	Architecture    string
	Bitness         string
	Model           string
	FormFactors     string
	WoW64           string
}

// BuildClientHintsForOS generates a realistic set of High-Entropy Client Hints
// matching the provided User-Agent string and target OS key.
func BuildClientHintsForOS(ua string, os profiles.OSKey) ClientHints {
	fullVersion := extractChromeVersion(ua)
	majorVersion := extractMajorVersion(fullVersion)

	hints := ClientHints{
		Brand:           `"Not_A Brand";v="8", "Chromium";v="` + majorVersion + `", "Google Chrome";v="` + majorVersion + `"`,
		FullVersion:     fullVersion,
		FullVersionList: `"Not_A Brand";v="8.0.0.0", "Chromium";v="` + fullVersion + `", "Google Chrome";v="` + fullVersion + `"`,
		Mobile:          os.Mobile(),
		FormFactors:     resolveFormFactor(os),
	}

	populateOSDetails(&hints, os)

	return hints
}

// ApplyHeaders injects non-empty Client Hints attributes into request headers.
func (ch ClientHints) ApplyHeaders(setHeader func(key, val string)) {
	if setHeader == nil {
		return
	}

	setHeader("Sec-CH-UA", ch.Brand)
	setHeader("Sec-CH-UA-Mobile", ch.Mobile)

	if ch.Platform != "" {
		setHeader("Sec-CH-UA-Platform", ch.Platform)
	}

	if ch.FullVersionList != "" {
		setHeader("Sec-CH-UA-Full-Version-List", ch.FullVersionList)
	}

	if ch.PlatformVersion != "" {
		setHeader("Sec-CH-UA-Platform-Version", ch.PlatformVersion)
	}

	if ch.Architecture != "" {
		setHeader("Sec-CH-UA-Arch", ch.Architecture)
	}

	if ch.Bitness != "" {
		setHeader("Sec-CH-UA-Bitness", ch.Bitness)
	}

	if ch.Model != "" {
		setHeader("Sec-CH-UA-Model", ch.Model)
	}

	if ch.FormFactors != "" {
		setHeader("Sec-CH-UA-Form-Factors", ch.FormFactors)
	}
}

// extractChromeVersion extracts the full version token (e.g. "120.0.6099.109") from a User-Agent without regex heap allocations.
func extractChromeVersion(ua string) string {
	const prefix = "Chrome/"

	idx := strings.Index(ua, prefix)
	if idx >= 0 {
		rest := ua[idx+len(prefix):]

		end := strings.IndexAny(rest, " ;/()")
		if end >= 0 {
			rest = rest[:end]
		}

		if len(rest) > 0 {
			return rest
		}
	}

	return "120.0.6099.109"
}

// extractMajorVersion extracts the leading major version number from a semver string.
func extractMajorVersion(fullVersion string) string {
	major, _, _ := strings.Cut(fullVersion, ".")
	if major == "" {
		return "120"
	}

	return major
}

// resolveFormFactor reports whether the target OS corresponds to a Mobile or Desktop form factor.
func resolveFormFactor(os profiles.OSKey) string {
	if os.IsMobile() {
		return `"Mobile"`
	}

	return `"Desktop"`
}

// populateOSDetails fills platform, platform version, architecture, bitness, and model hints for the target OS.
func populateOSDetails(ch *ClientHints, os profiles.OSKey) {
	switch os {
	case profiles.Windows:
		ch.Platform = `"Windows"`
		ch.PlatformVersion = `"15.0.0"`
		ch.Architecture = `"x86"`
		ch.Bitness = `"64"`
		ch.Model = `""`

	case profiles.MacOS:
		ch.Platform = `"macOS"`
		ch.PlatformVersion = `"14.2.1"`
		ch.Architecture = `"arm"`
		ch.Bitness = `"64"`
		ch.Model = `""`

	case profiles.Linux:
		ch.Platform = `"Linux"`
		ch.PlatformVersion = `"6.5.0"`
		ch.Architecture = `"x86"`
		ch.Bitness = `"64"`
		ch.Model = `""`

	case profiles.Android:
		ch.Platform = `"Android"`
		ch.PlatformVersion = `"14.0.0"`
		ch.Architecture = `"arm"`
		ch.Bitness = `"64"`
		ch.Model = `"Pixel 8"`

	case profiles.IOS:
		ch.Platform = `"iOS"`
		ch.PlatformVersion = `"17.2.0"`
		ch.Architecture = `"arm"`
		ch.Bitness = `"64"`
		ch.Model = `"iPhone"`
	}
}
