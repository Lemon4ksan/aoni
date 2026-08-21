// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package svcb implements Service Binding (SVCB) and HTTPS Resource Records strictly conforming to RFC 9460.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/dns/svcb].
package svcb

import (
	"net"

	fsvcb "github.com/lemon4ksan/foundation/net/dns/svcb"
)

// Standard DNS Resource Record Types defined in RFC 9460 §14.1 & §14.2.
const (
	TypeSVCB  = fsvcb.TypeSVCB
	TypeHTTPS = fsvcb.TypeHTTPS
)

// SvcParamKey represents a registered Service Parameter Key defined in RFC 9460 §14.3.2.
type SvcParamKey = fsvcb.SvcParamKey

// Registered SvcParamKey constants defined in RFC 9460 §14.3.2 (Table 1).
const (
	ParamMandatory     = fsvcb.ParamMandatory
	ParamALPN          = fsvcb.ParamALPN
	ParamNoDefaultALPN = fsvcb.ParamNoDefaultALPN
	ParamPort          = fsvcb.ParamPort
	ParamIPv4Hint      = fsvcb.ParamIPv4Hint
	ParamECH           = fsvcb.ParamECH
	ParamIPv6Hint      = fsvcb.ParamIPv6Hint
	ParamInvalidKey    = fsvcb.ParamInvalidKey
)

// ParseParamKey parses a presentation SvcParamKey string into [SvcParamKey].
func ParseParamKey(name string) (SvcParamKey, error) {
	return fsvcb.ParseParamKey(name)
}

// Standard parsing and validation errors for SVCB/HTTPS resource records.
var (
	ErrTruncatedRDATA     = fsvcb.ErrTruncatedRDATA
	ErrMalformedDomain    = fsvcb.ErrMalformedDomain
	ErrUnsortedParamKeys  = fsvcb.ErrUnsortedParamKeys
	ErrDuplicateParamKey  = fsvcb.ErrDuplicateParamKey
	ErrMalformedParam     = fsvcb.ErrMalformedParam
	ErrIncompatibleRecord = fsvcb.ErrIncompatibleRecord
)

// Record represents a parsed SVCB (Type 64) or HTTPS (Type 65) Resource Record (RFC 9460 §2).
type Record = fsvcb.Record

// ParseRDATA parses the RDATA wire format payload into a [*Record] (RFC 9460 §2.2).
func ParseRDATA(rdata []byte) (*Record, error) {
	return fsvcb.ParseRDATA(rdata)
}

// BuildHTTPSQueryName builds the canonical DNS query name for an HTTPS origin (RFC 9460 §9.1).
func BuildHTTPSQueryName(origin string, port uint16) string {
	return fsvcb.BuildHTTPSQueryName(origin, port)
}

// BuildSVCBQueryName builds the canonical DNS query name using Attrleaf Port Prefix Naming (RFC 9460 §2.3).
func BuildSVCBQueryName(scheme, service string, port uint16) string {
	return fsvcb.BuildSVCBQueryName(scheme, service, port)
}

// EncodeALPN encodes a list of ALPN protocol names into the wire-format SvcParamValue (RFC 9460 §7.1.1).
func EncodeALPN(alpns []string) []byte {
	return fsvcb.EncodeALPN(alpns)
}

// EncodePort encodes a 16-bit port number into the wire-format SvcParamValue (RFC 9460 §7.2).
func EncodePort(port uint16) []byte {
	return fsvcb.EncodePort(port)
}

// EncodeIPv4Hints encodes a slice of IPv4 addresses into the wire-format SvcParamValue (RFC 9460 §7.3).
func EncodeIPv4Hints(ips []net.IP) []byte {
	return fsvcb.EncodeIPv4Hints(ips)
}

// EncodeIPv6Hints encodes a slice of IPv6 addresses into the wire-format SvcParamValue (RFC 9460 §7.3).
func EncodeIPv6Hints(ips []net.IP) []byte {
	return fsvcb.EncodeIPv6Hints(ips)
}

// ParseResponseRecords parses all Answer resource records of expectedType from a DNS response payload.
func ParseResponseRecords(dnsMsg []byte, expectedType uint16) ([]*Record, error) {
	return fsvcb.ParseResponseRecords(dnsMsg, expectedType)
}
