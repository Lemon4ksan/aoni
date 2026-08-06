// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cert

import utls "github.com/refraction-networking/utls"

// CompressionAlgorythm specifies a certificate compression algorithm defined in RFC 8879.
type CompressionAlgorythm uint16

const (
	CertCompressionZlib CompressionAlgorythm = 1
	CompressionBrotli   CompressionAlgorythm = 2
	CompressionZstd     CompressionAlgorythm = 3
)

// ToUTLS maps the compression algorithm to its corresponding uTLS representation.
func (a CompressionAlgorythm) ToUTLS() utls.CertCompressionAlgo {
	switch a {
	case CertCompressionZlib:
		return utls.CertCompressionZlib
	case CompressionZstd:
		return utls.CertCompressionZstd
	default:
		return utls.CertCompressionBrotli
	}
}
