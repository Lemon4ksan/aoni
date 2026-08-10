// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package fingerprint

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

var ErrInvalidECHConfig = errors.New("aoni/fingerprint: invalid base64 ech config")

// ParseECHConfigBase64 decodes a base64-encoded ECHConfigList string.
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
