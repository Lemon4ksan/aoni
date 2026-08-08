// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe

import (
	"net"
	"strings"
)

// CDNProvider represents a recognized Cloud / CDN network provider.
type CDNProvider string

const (
	CDNCloudflare CDNProvider = "Cloudflare"
	CDNAkamai     CDNProvider = "Akamai"
	CDNCloudfront CDNProvider = "AWS CloudFront"
	CDNFastly     CDNProvider = "Fastly"
	CDNUnknown    CDNProvider = "None"
)

// CheckCDN detects if an IP address belongs to known CDN or WAF edge networks.
func CheckCDN(ip net.IP) (isCDN bool, provider CDNProvider) {
	if ip == nil {
		return false, CDNUnknown
	}

	ipStr := ip.String()

	// Heuristic CIDR checks for popular CDN edge nodes
	switch {
	case isCloudflareIP(ip):
		return true, CDNCloudflare
	case isAkamaiIP(ipStr):
		return true, CDNAkamai
	case isFastlyIP(ipStr):
		return true, CDNFastly
	default:
		return false, CDNUnknown
	}
}

func isCloudflareIP(ip net.IP) bool {
	// IPv4 Cloudflare CIDR range checks (103.21.244.0/22, 104.16.0.0/13, 172.64.0.0/13, etc.)
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}

	if ip4[0] == 104 && (ip4[1] >= 16 && ip4[1] <= 31) {
		return true
	}

	if ip4[0] == 172 && (ip4[1] >= 64 && ip4[1] <= 127) {
		return true
	}

	if ip4[0] == 162 && ip4[1] == 158 {
		return true
	}

	if ip4[0] == 198 && ip4[1] == 41 {
		return true
	}

	return false
}

func isAkamaiIP(ipStr string) bool {
	return strings.HasPrefix(ipStr, "23.") || strings.HasPrefix(ipStr, "104.64.")
}

func isFastlyIP(ipStr string) bool {
	return strings.HasPrefix(ipStr, "151.101.") || strings.HasPrefix(ipStr, "199.27.")
}
