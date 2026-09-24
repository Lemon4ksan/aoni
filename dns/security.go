// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dns

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrSpoofedID indicates a DNS response transaction ID mismatch (RFC 5452 §9.1).
	ErrSpoofedID = errors.New("aoni/dns: query transaction ID mismatch (potential forgery / RFC 5452)")

	// ErrSpoofedQName indicates a DNS response question name mismatch (RFC 5452 §9.1).
	ErrSpoofedQName = errors.New("aoni/dns: query question name mismatch (potential forgery / RFC 5452)")

	// ErrSpoofedQType indicates a DNS response question type mismatch (RFC 5452 §9.1).
	ErrSpoofedQType = errors.New("aoni/dns: query question type mismatch (potential forgery / RFC 5452)")

	// ErrSpoofedQClass indicates a DNS response question class mismatch (RFC 5452 §9.1).
	ErrSpoofedQClass = errors.New("aoni/dns: query question class mismatch (potential forgery / RFC 5452)")
)

// GenerateQueryID generates an unpredictable, cryptographically random 16-bit DNS Query ID
// utilizing the full 0-65535 ID space per RFC 5452 §9.2 and RFC 4086.
func GenerateQueryID() uint16 {
	var b [2]byte

	_, _ = rand.Read(b[:])

	return binary.BigEndian.Uint16(b[:])
}

// ValidateQueryMatch strictly verifies that an incoming DNS response matches the original query's
// Question attributes per RFC 5452 §9.1 (Query Matching Rules).
//
// A mismatch in any attribute causes the response to be rejected as invalid/spoofed.
func ValidateQueryMatch(
	expectedID, actualID uint16,
	expectedQName, actualQName string,
	expectedQType, actualQType, expectedQClass, actualQClass uint16,
) error {
	if expectedID != actualID {
		return fmt.Errorf("%w: got %d, expected %d", ErrSpoofedID, actualID, expectedID)
	}

	expClean := strings.TrimSuffix(strings.ToLower(expectedQName), ".")

	actClean := strings.TrimSuffix(strings.ToLower(actualQName), ".")
	if expClean != actClean {
		return fmt.Errorf("%w: got %q, expected %q", ErrSpoofedQName, actualQName, expectedQName)
	}

	if expectedQType != actualQType {
		return fmt.Errorf("%w: got type %d, expected %d", ErrSpoofedQType, actualQType, expectedQType)
	}

	if expectedQClass != actualQClass {
		return fmt.Errorf("%w: got class %d, expected %d", ErrSpoofedQClass, actualQClass, expectedQClass)
	}

	return nil
}
