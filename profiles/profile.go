// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package profiles

import (
	"sync"

	utls "github.com/refraction-networking/utls"
)

// OSKey represents the operating system key for the browser profile.
type OSKey int

// Possible OSKey values for the browser profile.
const (
	Windows OSKey = iota + 1
	MacOS
	Linux
	Android
	IOS
)

// IsMobile returns true if the OSKey is a mobile device.
func (k OSKey) IsMobile() bool { return k == Android || k == IOS }

// Mobile returns the mobile string representation of the OSKey.
func (k OSKey) Mobile() string {
	if k.IsMobile() {
		return "?1"
	}

	return "?0"
}

// HeaderEntry represents a single header entry for the browser profile.
type HeaderEntry struct {
	Name  string
	Value string
}

// H2Settings represents the settings for the HTTP/2 protocol.
type H2Settings struct {
	HeaderTableSize      uint32
	EnablePush           uint32
	MaxConcurrentStreams uint32
	InitialWindowSize    uint32
	MaxFrameSize         uint32
	MaxHeaderListSize    uint32
	ConnectionFlow       uint32
	InitialStreamID      uint32
	PriorityStreamDep    uint32
	PriorityExclusive    bool
	PriorityWeight       uint8
}

// H3Settings represents the settings for the HTTP/3 protocol.
type H3Settings struct {
	QpackMaxTableCapacity uint64
	MaxFieldSectionSize   uint64
	QpackBlockedStreams   uint64
	EnableConnectProtocol uint64
	H3Datagram            uint64
	SettingsH3Datagram    uint64
	EnableWebtransport    uint64
}

// Variant represents a single browser variant with its associated settings.
type Variant struct {
	HelloSpec     *utls.ClientHelloSpec
	HelloID       utls.ClientHelloID
	BoundaryFunc  func() string
	ConfigureH2   func(*H2Settings)
	ConfigureH3   func(*H3Settings)
	BuildHeaders  func(OSKey) []HeaderEntry
	InsertHeaders func(headers map[string]string, method string)
}

// Header names used in the browser profile.
const (
	ACCEPT                    = "accept"
	ACCEPT_ENCODING           = "accept-encoding"
	ACCEPT_LANGUAGE           = "accept-language"
	AUTHORIZATION             = "authorization"
	CACHE_CONTROL             = "cache-control"
	CONTENT_LENGTH            = "content-length"
	CONTENT_TYPE              = "content-type"
	COOKIE                    = "cookie"
	ORIGIN                    = "origin"
	PRIORITY                  = "priority"
	PRAGMA                    = "pragma"
	REFERER                   = "referer"
	SEC_CH_UA                 = "sec-ch-ua"
	SEC_CH_UA_MOBILE          = "sec-ch-ua-mobile"
	SEC_CH_UA_PLATFORM        = "sec-ch-ua-platform"
	SEC_FETCH_DEST            = "sec-fetch-dest"
	SEC_FETCH_MODE            = "sec-fetch-mode"
	SEC_FETCH_SITE            = "sec-fetch-site"
	SEC_FETCH_USER            = "sec-fetch-user"
	UPGRADE_INSECURE_REQUESTS = "upgrade-insecure-requests"
	USER_AGENT                = "user-agent"
)

// HeaderCache caches the order and enums of headers for desktop and mobile devices.
type HeaderCache struct {
	desktopOrder map[string][]string
	mobileOrder  map[string][]string

	desktopEnums map[string]map[string]int
	mobileEnums  map[string]map[string]int

	onceDesktop sync.Once
	onceMobile  sync.Once
}

// NewHeaderCache creates a new HeaderCache with the given desktop and mobile header orders.
func NewHeaderCache(desktop, mobile map[string][]string) *HeaderCache {
	return &HeaderCache{desktopOrder: desktop, mobileOrder: mobile}
}

// SortByOrder sorts the given headers according to the cached order and enums for the specified method and device type.
func (hc *HeaderCache) SortByOrder(headers []HeaderEntry, method string, isMobile bool) []HeaderEntry {
	hc.buildEnums(isMobile)

	var enums map[string]map[string]int
	if isMobile {
		enums = hc.mobileEnums
	} else {
		enums = hc.desktopEnums
	}

	order, ok := enums[method]
	if !ok {
		order = enums["GET"]
	}

	result := make([]HeaderEntry, len(headers))
	copy(result, headers)

	for i := range result {
		for j := i + 1; j < len(result); j++ {
			posI, okI := order[result[i].Name]
			posJ, okJ := order[result[j].Name]

			if !okI {
				posI = 999
			}

			if !okJ {
				posJ = 999
			}

			if posI > posJ {
				result[i], result[j] = result[j], result[i]
			}
		}
	}

	return result
}

func (hc *HeaderCache) buildEnums(isMobile bool) {
	if isMobile {
		hc.onceMobile.Do(func() {
			hc.mobileEnums = buildHeaderEnums(hc.mobileOrder)
		})
	} else {
		hc.onceDesktop.Do(func() {
			hc.desktopEnums = buildHeaderEnums(hc.desktopOrder)
		})
	}
}

// Enums returns the cached enums for the specified device type.
func (hc *HeaderCache) Enums(isMobile bool) map[string]map[string]int {
	hc.buildEnums(isMobile)

	if isMobile {
		return hc.mobileEnums
	}

	return hc.desktopEnums
}

func buildHeaderEnums(order map[string][]string) map[string]map[string]int {
	enums := make(map[string]map[string]int)
	for method, headers := range order {
		enum := make(map[string]int, len(headers))
		for i, h := range headers {
			enum[h] = i
		}

		enums[method] = enum
	}

	return enums
}
