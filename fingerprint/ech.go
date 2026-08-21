// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fingerprint

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/lemon4ksan/aoni/fingerprint/ech"
)

// Standard TLS extension type and alert codepoints for Encrypted Client Hello (draft-ietf-tls-esni-22).
const (
	// ExtensionTypeEncryptedClientHello is the TLS 1.3 extension codepoint (0xfe0d) defined in draft-ietf-tls-esni-22 §11.1.
	ExtensionTypeEncryptedClientHello = ech.ExtensionTypeEncryptedClientHello

	// ExtensionTypeECHOuterExtensions is the inner ClientHello extension codepoint (0xfd00) defined in draft-ietf-tls-esni-22 §11.1.
	ExtensionTypeECHOuterExtensions = ech.ExtensionTypeECHOuterExtensions

	// AlertECHRequired is the TLS alert codepoint (121) sent upon unaccepted ECH offerings (draft-ietf-tls-esni-22 §11.2).
	AlertECHRequired = ech.AlertECHRequired
)

// ErrInvalidECHConfig indicates that the provided ECH configuration payload is malformed or invalid.
var ErrInvalidECHConfig = errors.New("aoni/fingerprint: invalid base64 ech config")

// ParseECHConfigBase64 decodes a base64-encoded ECHConfigList string (draft-ietf-tls-esni-22 §4).
func ParseECHConfigBase64(raw string) ([]byte, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return nil, ErrInvalidECHConfig
	}

	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err == nil {
		return decoded, nil
	}

	decoded, err = base64.RawStdEncoding.DecodeString(cleaned)
	if err == nil {
		return decoded, nil
	}

	return nil, fmt.Errorf("%w: %w", ErrInvalidECHConfig, err)
}
