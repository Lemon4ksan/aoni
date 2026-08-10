// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package p0f provides passive OS TCP/IP stack fingerprint spoofing via socket options.
package p0f

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrInvalidP0fSignature is returned when a p0f signature string violates the 8-field spec format.
var ErrInvalidP0fSignature = errors.New("aoni/p0f: invalid p0f signature string")

// Error describes a parsing or parsing validation error for a specific signature field.
type Error struct {
	Field string
	Val   string
	Err   error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}

	return "aoni/p0f: field " + e.Field + " (" + e.Val + "): " + e.Err.Error()
}

func (e *Error) Unwrap() error { return e.Err }

// WindowType describes how TCP Window Size is computed for the fingerprint.
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

// Predefined p0f TCP signatures for common operating systems.
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

// Signature represents a parsed p0f TCP/IP stack fingerprint.
type Signature struct {
	IPVersion   string
	TTL         int
	HasTTLMinus bool
	IPOptLen    int
	MSS         int
	WindowSize  int
	WindowType  WindowType
	WindowScale int
	Options     []string
	Quirks      []string
	Payload     string
}

// String reconstructs the 8-field p0f signature string format.
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
		windowStr = "mss*" + strconv.Itoa(s.WindowSize) + "," + strconv.Itoa(s.WindowScale)
	case WindowMTU:
		windowStr = "mtu*" + strconv.Itoa(s.WindowSize) + "," + strconv.Itoa(s.WindowScale)
	default:
		windowStr = strconv.Itoa(s.WindowSize) + "," + strconv.Itoa(s.WindowScale)
	}

	return s.IPVersion + ":" + ttlStr + ":" + strconv.Itoa(
		s.IPOptLen,
	) + ":" + mssStr + ":" + windowStr + ":" + strings.Join(
		s.Options,
		",",
	) + ":" + strings.Join(
		s.Quirks,
		",",
	) + ":" + s.Payload
}

// Parse parses an 8-field p0f signature string.
func Parse(sig string) (*Signature, error) {
	parts := strings.Split(sig, ":")
	if len(parts) != 8 {
		return nil, ErrInvalidP0fSignature
	}

	s := &Signature{}

	s.IPVersion = parts[0]
	if s.IPVersion != "4" && s.IPVersion != "6" && s.IPVersion != "*" {
		return nil, fmt.Errorf("p0f: invalid IP version %q", s.IPVersion)
	}

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

	if parts[3] == "*" {
		s.MSS = -1
	} else {
		mss, err := strconv.Atoi(parts[3])
		if err != nil {
			return nil, fmt.Errorf("p0f: invalid MSS %q: %w", parts[3], err)
		}

		s.MSS = mss
	}

	if err := parseWindow(parts[4], s); err != nil {
		return nil, err
	}

	if parts[5] != "" {
		s.Options = strings.Split(parts[5], ",")
	} else {
		s.Options = []string{}
	}

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

	if field == "*,-1" {
		s.WindowType = WindowAny
		s.WindowSize = -1
		s.WindowScale = -1

		return nil
	}

	parts := strings.Split(field, ",")
	if len(parts) != 2 {
		return fmt.Errorf("p0f: invalid window field %q", field)
	}

	scale, err := strconv.Atoi(parts[1])
	if err != nil {
		return fmt.Errorf("p0f: invalid window scale %q: %w", parts[1], err)
	}

	s.WindowScale = scale

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
