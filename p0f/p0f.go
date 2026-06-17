// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package p0f

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// WindowType describes how the TCP window size is computed.
type WindowType int

const (
	// WindowNormal indicates the exact window size value.
	WindowNormal WindowType = iota
	// WindowMSS indicates window = MSS * N.
	WindowMSS
	// WindowMOD indicates window = N * rand(1, 65535/N).
	WindowMOD
	// WindowMTU indicates window = MTU * N.
	WindowMTU
	// WindowAny indicates any window size is acceptable.
	WindowAny
)

// Signature represents a parsed p0f TCP fingerprint.
type Signature struct {
	IPVersion   string     // "4", "6", "*"
	TTL         int        // initial TTL value
	HasTTLMinus bool       // true if TTL has "-" suffix (bad TTL)
	IPOptLen    int        // IP options header length in bytes
	MSS         int        // -1 = wildcard
	WindowSize  int        // window size value, -1 = wildcard
	WindowType  WindowType // how to interpret WindowSize
	WindowScale int        // window scale shift count, -1 = wildcard
	Options     []string   // TCP options layout (e.g. ["mss","sok","ts","nop","ws"])
	Quirks      []string   // IP/TCP quirks (e.g. ["df","id+"])
	Payload     string     // "0", "+", or "*"
}

// Parse parses a p0f signature string in the format:
// {ip_ver}:{ttl}:{ip_opt_len}:{mss}:{window,wscale}:{opt_layout}:{quirks}:{pay_class}
func Parse(sig string) (*Signature, error) {
	parts := strings.Split(sig, ":")
	if len(parts) != 8 {
		return nil, fmt.Errorf("p0f: signature must have 8 colon-separated fields, got %d", len(parts))
	}

	s := &Signature{}

	s.IPVersion = parts[0]
	if s.IPVersion != "4" && s.IPVersion != "6" && s.IPVersion != "*" {
		return nil, fmt.Errorf("p0f: invalid IP version %q", s.IPVersion)
	}

	// Parse TTL (may have "-" suffix for bad TTL)
	ttlStr := parts[1]
	if strings.HasSuffix(ttlStr, "-") {
		s.HasTTLMinus = true
		ttlStr = strings.TrimSuffix(ttlStr, "-")
	}

	ttl, err := strconv.Atoi(ttlStr)
	if err != nil {
		return nil, fmt.Errorf("p0f: invalid TTL %q: %w", parts[1], err)
	}

	s.TTL = ttl

	ipOptLen, err := strconv.Atoi(parts[2])
	if err != nil {
		return nil, fmt.Errorf("p0f: invalid IP option length %q: %w", parts[2], err)
	}

	s.IPOptLen = ipOptLen

	// Parse MSS (-1 = wildcard)
	if parts[3] == "*" {
		s.MSS = -1
	} else {
		mss, err := strconv.Atoi(parts[3])
		if err != nil {
			return nil, fmt.Errorf("p0f: invalid MSS %q: %w", parts[3], err)
		}

		s.MSS = mss
	}

	// Parse window,wscale
	if err := parseWindow(parts[4], s); err != nil {
		return nil, err
	}

	// Parse options
	if parts[5] != "" {
		s.Options = strings.Split(parts[5], ",")
	} else {
		s.Options = []string{}
	}

	// Parse quirks
	if parts[6] != "" {
		s.Quirks = strings.Split(parts[6], ",")
	} else {
		s.Quirks = []string{}
	}

	s.Payload = parts[7]

	return s, nil
}

func parseWindow(field string, s *Signature) error {
	if field == "" {
		return errors.New("p0f: empty window field")
	}

	// Handle wildcard: *,-1
	if field == "*,-1" {
		s.WindowType = WindowAny
		s.WindowSize = -1
		s.WindowScale = -1

		return nil
	}

	parts := strings.Split(field, ",")
	if len(parts) != 2 {
		return fmt.Errorf("p0f: invalid window field %q: expected two comma-separated values", field)
	}

	// Parse window scale
	scale, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("p0f: invalid window scale %q: %w", parts[1], err)
	}

	s.WindowScale = scale

	// Parse window size (may be mss*20, 8192, etc.)
	wsStr := parts[0]
	switch {
	case strings.HasPrefix(wsStr, "mss*"):
		s.WindowType = WindowMSS

		multiplier, err := strconv.Atoi(strings.TrimPrefix(wsStr, "mss*"))
		if err != nil {
			return fmt.Errorf("p0f: invalid MSS multiplier %q: %w", wsStr, err)
		}

		s.WindowSize = multiplier

	case strings.HasPrefix(wsStr, "mtu*"):
		s.WindowType = WindowMTU

		multiplier, err := strconv.Atoi(strings.TrimPrefix(wsStr, "mtu*"))
		if err != nil {
			return fmt.Errorf("p0f: invalid MTU multiplier %q: %w", wsStr, err)
		}

		s.WindowSize = multiplier

	default:
		ws, err := strconv.Atoi(wsStr)
		if err != nil {
			return fmt.Errorf("p0f: invalid window size %q: %w", wsStr, err)
		}

		s.WindowType = WindowNormal
		s.WindowSize = ws
	}

	return nil
}

// MustParse parses a p0f signature string and panics on error.
func MustParse(sig string) *Signature {
	s, err := Parse(sig)
	if err != nil {
		panic(err)
	}

	return s
}

// String reconstructs the p0f signature string.
func (s *Signature) String() string {
	ttlStr := strconv.Itoa(s.TTL)
	if s.HasTTLMinus {
		ttlStr += "-"
	}

	mssStr := "*"
	if s.MSS != -1 {
		mssStr = strconv.Itoa(s.MSS)
	}

	var windowStr string
	switch s.WindowType {
	case WindowAny:
		windowStr = "*,-1"
	case WindowMSS:
		windowStr = fmt.Sprintf("mss*%d,%d", s.WindowSize, s.WindowScale)
	default:
		windowStr = fmt.Sprintf("%d,%d", s.WindowSize, s.WindowScale)
	}

	return fmt.Sprintf("%s:%s:%d:%s:%s:%s:%s:%s",
		s.IPVersion,
		ttlStr,
		s.IPOptLen,
		mssStr,
		windowStr,
		strings.Join(s.Options, ","),
		strings.Join(s.Quirks, ","),
		s.Payload,
	)
}

// Predefined p0f signatures for common OS versions.
var (
	Linux311  = MustParse("*:64:0:*:mss*20,10:mss,sok,ts,nop,ws:df,id+:0")
	Linux3x   = MustParse("*:64:0:*:mss*10,0:mss,sok,ts,nop,ws:df,id+:0")
	Linux26   = MustParse("*:64:0:*:mss*4,7:mss,sok,ts,nop,ws:df,id+:0")
	Linux24   = MustParse("*:64:0:*:mss*4,0:mss,sok,ts,nop,ws:df,id+:0")
	WindowsXP = MustParse("*:128:0:*:16384,0:mss,nop,nop,sok:df,id+:0")
	Windows7  = MustParse("*:128:0:*:8192,8:mss,nop,ws,nop,nop,sok:df,id+:0")
	Windows10 = MustParse("*:128:0:*:8192,2:mss,nop,ws,nop,nop,sok:df,id+:0")
	MacOS     = MustParse("*:64:0:*:65535,6:mss,sok,ts,nop,ws:df+:0")
	Android   = MustParse("*:64:0:*:mss*44,1:mss,sok,ts,nop,ws:df,id+:0")
	IOS       = MustParse("*:64:0:*:65535,3:mss,nop,ws,sok,ts:df,id+:0")
	Nmap      = MustParse("*:64-:0:265:512,0:mss,sok,ts:ack+:0")
)
