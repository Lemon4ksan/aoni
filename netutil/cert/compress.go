// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cert

import utls "github.com/refraction-networking/utls"

// CompressionAlgorithm specifies a certificate compression algorithm defined in RFC 8879.
type CompressionAlgorithm uint16

const (
	CertCompressionZlib CompressionAlgorithm = 1
	CompressionBrotli   CompressionAlgorithm = 2
	CompressionZstd     CompressionAlgorithm = 3
)

// ToUTLS maps the compression algorithm to its corresponding uTLS representation.
func (a CompressionAlgorithm) ToUTLS() utls.CertCompressionAlgo {
	switch a {
	case CertCompressionZlib:
		return utls.CertCompressionZlib
	case CompressionZstd:
		return utls.CertCompressionZstd
	default:
		return utls.CertCompressionBrotli
	}
}
