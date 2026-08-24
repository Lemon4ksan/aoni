// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe

import (
	"net"
	"net/netip"
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

	if addr, ok := netip.AddrFromSlice(ip); ok {
		return CheckCDNAddr(addr.Unmap())
	}

	return false, CDNUnknown
}

// CheckCDNAddr detects if a [netip.Addr] belongs to known CDN or WAF edge networks with 0 heap allocations.
func CheckCDNAddr(addr netip.Addr) (isCDN bool, provider CDNProvider) {
	if !addr.IsValid() {
		return false, CDNUnknown
	}

	addr = addr.Unmap()
	if addr.Is4() {
		b := addr.As4()

		// Cloudflare IPv4 CIDRs: 104.16.0.0/13, 172.64.0.0/13, 162.158.0.0/15, 198.41.128.0/17, 103.21.244.0/22
		if (b[0] == 104 && (b[1] >= 16 && b[1] <= 31)) ||
			(b[0] == 172 && (b[1] >= 64 && b[1] <= 127)) ||
			(b[0] == 162 && (b[1] == 158 || b[1] == 159)) ||
			(b[0] == 198 && b[1] == 41) ||
			(b[0] == 103 && b[1] == 21 && (b[2] >= 244 && b[2] <= 247)) {
			return true, CDNCloudflare
		}

		// Akamai IPv4: 23.0.0.0/8, 104.64.0.0/10
		if b[0] == 23 || (b[0] == 104 && (b[1] >= 64 && b[1] <= 127)) {
			return true, CDNAkamai
		}

		// Fastly IPv4: 151.101.0.0/16, 199.27.72.0/21
		if (b[0] == 151 && b[1] == 101) || (b[0] == 199 && b[1] == 27) {
			return true, CDNFastly
		}
	}

	return false, CDNUnknown
}
