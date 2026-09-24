// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package dns provides secure, resilient, and high-performance DNS resolution strategies,
// IDNA2008 label classification, RFC 5452 anti-spoofing security, and RFC 8767/2308/8020 caching.
package dns

import (
	"strings"
	"unicode/utf8"

	"github.com/lemon4ksan/mach/proto/dns/wire"
	"golang.org/x/net/idna"
)

// IDNA2008 Constants defined in RFC 5890.
const (
	// ACEPrefix is the ASCII-Compatible Encoding prefix for IDNA A-labels (RFC 5890 §2.3.2.5).
	ACEPrefix = "xn--"

	// MaxPunycodePayloadLength is the maximum allowed length of the Punycode part in an A-label (RFC 5890 §2.3.2.1).
	MaxPunycodePayloadLength = 59
)

// LabelType classifies a DNS label according to the IDNA2008 taxonomy in RFC 5890 §2.3.
type LabelType int

const (
	// LabelTypeUnknown represents an unclassified label.
	LabelTypeUnknown LabelType = iota

	// LabelTypeNRLDH represents a standard Non-Reserved LDH (letters, digits, hyphen) label (RFC 5890 §2.3.2.2).
	LabelTypeNRLDH

	// LabelTypeALabel represents an IDNA-valid ASCII-Compatible Encoded label starting with "xn--" (RFC 5890 §2.3.2.1).
	LabelTypeALabel

	// LabelTypeULabel represents an IDNA-valid Unicode NFC label with non-ASCII characters (RFC 5890 §2.3.2.1).
	LabelTypeULabel

	// LabelTypeRLDH represents a Reserved LDH label with "--" in 3rd/4th positions, but not starting with "xn--" (RFC 5890 §2.3.1).
	LabelTypeRLDH

	// LabelTypeFakeALabel represents an XN-label starting with "xn--" whose payload is NOT valid Punycode (RFC 5890 §2.3.1).
	LabelTypeFakeALabel

	// LabelTypeNonLDH represents a label that does not conform to the LDH syntax (e.g., underscore labels "_tcp", leading/trailing hyphen, or symbols) (RFC 5890 §2.3.1).
	LabelTypeNonLDH
)

// String returns the human-readable name of the RFC 5890 label type.
func (t LabelType) String() string {
	switch t {
	case LabelTypeNRLDH:
		return "NR-LDH"
	case LabelTypeALabel:
		return "A-label"
	case LabelTypeULabel:
		return "U-label"
	case LabelTypeRLDH:
		return "R-LDH"
	case LabelTypeFakeALabel:
		return "Fake A-label"
	case LabelTypeNonLDH:
		return "Non-LDH"
	default:
		return "Unknown"
	}
}

// IsLDH reports whether label conforms to the classical LDH (letters, digits, hyphen) syntax per RFC 5890 §2.3.1.
//
// Rules:
// - Length between 1 and 63 octets.
// - Contains only ASCII letters ('A'-'Z', 'a'-'z'), digits ('0'-'9'), and hyphen ('-').
// - Hyphen must not appear at the first or last position.
func IsLDH(label string) bool {
	n := len(label)
	if n == 0 || n > wire.MaxLabelLength {
		return false
	}

	if label[0] == '-' || label[n-1] == '-' {
		return false
	}

	for i := range n {
		c := label[i]
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}

	return true
}

// ClassifyLabel categorizes a single DNS label according to the RFC 5890 §2.3 taxonomy.
func ClassifyLabel(label string) LabelType {
	if len(label) == 0 || len(label) > wire.MaxLabelLength {
		return LabelTypeNonLDH
	}

	// Check if label contains non-ASCII Unicode characters
	hasNonASCII := false
	for i := range len(label) {
		if label[i] >= 0x80 {
			hasNonASCII = true
			break
		}
	}

	if hasNonASCII {
		if !utf8.ValidString(label) {
			return LabelTypeNonLDH
		}

		// Validate U-label via IDNA2008 conversion
		if _, err := idna.Lookup.ToASCII(label); err == nil {
			return LabelTypeULabel
		}

		return LabelTypeNonLDH
	}

	// ASCII Label
	if !IsLDH(label) {
		return LabelTypeNonLDH
	}

	// Check for Reserved LDH (R-LDH): "--" in 3rd and 4th position (index 2 and 3)
	if len(label) >= 4 && label[2] == '-' && label[3] == '-' {
		lower := strings.ToLower(label[:4])
		if lower == ACEPrefix {
			// XN-label: check if payload is valid Punycode
			if _, err := idna.Lookup.ToUnicode(label); err == nil {
				return LabelTypeALabel
			}

			return LabelTypeFakeALabel
		}

		return LabelTypeRLDH
	}

	return LabelTypeNRLDH
}

// IsIDN reports whether a domain name is an Internationalized Domain Name (IDN)
// by containing at least one A-label or U-label per RFC 5890 §2.3.2.3.
func IsIDN(domain string) bool {
	domain = strings.TrimSuffix(domain, ".")
	if len(domain) == 0 {
		return false
	}

	for label := range strings.SplitSeq(domain, ".") {
		ltype := ClassifyLabel(label)
		if ltype == LabelTypeALabel || ltype == LabelTypeULabel {
			return true
		}
	}

	return false
}

// EqualFoldASCII reports whether s1 and s2, interpreted as DNS labels or domain names,
// are equal under strict ASCII case-insensitivity per RFC 4343 §3.
//
// Specification Rules (RFC 4343 §3):
//   - ASCII letters (0x41-0x5A / 'A'-'Z' and 0x61-0x7A / 'a'-'z') match case-insensitively.
//   - Non-ASCII octets (0x00-0x20 and 0x7F-0xFF) are treated as raw binary octets and MUST match exactly
//     without Latin/ISO-8859 accent folding.
//   - Trailing dots are ignored during comparison (e.g., "example.com." equals "EXAMPLE.COM").
func EqualFoldASCII(s1, s2 string) bool {
	s1 = strings.TrimSuffix(s1, ".")
	s2 = strings.TrimSuffix(s2, ".")

	if len(s1) != len(s2) {
		return false
	}

	for i := range len(s1) {
		c1 := s1[i]
		c2 := s2[i]

		if c1 == c2 {
			continue
		}

		// Fold ASCII uppercase to lowercase (0x41-0x5A -> 0x61-0x7A)
		if c1 >= 'A' && c1 <= 'Z' {
			c1 += 'a' - 'A'
		}

		if c2 >= 'A' && c2 <= 'Z' {
			c2 += 'a' - 'A'
		}

		if c1 != c2 {
			return false
		}
	}

	return true
}

// CanonicalDomainName returns the canonical, case-folded lowercase representation of a domain name
// per RFC 4343 §4 & §6 and RFC 4034 §6.
//
// Converts ASCII letters to lowercase while preserving raw octets and trailing root dots.
func CanonicalDomainName(domain string) string {
	var sb strings.Builder
	sb.Grow(len(domain))

	for i := range len(domain) {
		c := domain[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}

		sb.WriteByte(c)
	}

	return sb.String()
}
