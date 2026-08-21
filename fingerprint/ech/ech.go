// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ech implements TLS Encrypted Client Hello strictly conforming to draft-ietf-tls-esni-22.
// Core implementation is located in [github.com/lemon4ksan/foundation/net/tls/ech].
package ech

import (
	fech "github.com/lemon4ksan/foundation/net/tls/ech"
)

// Standard TLS extension type and alert codepoints defined in draft-ietf-tls-esni-22 §11.
const (
	ExtensionTypeEncryptedClientHello = fech.ExtensionTypeEncryptedClientHello
	ExtensionTypeECHOuterExtensions   = fech.ExtensionTypeECHOuterExtensions
	AlertECHRequired                  = fech.AlertECHRequired
	VersionDraft22                    = fech.VersionDraft22
)

// ClientHelloType represents the outer or inner variant of the encrypted_client_hello extension.
type ClientHelloType = fech.ClientHelloType

const (
	ClientHelloOuter = fech.ClientHelloOuter
	ClientHelloInner = fech.ClientHelloInner
)

// Standard HPKE KEM identifiers defined in RFC 9180 §7.1 and draft-ietf-tls-esni-22 §4.
const (
	KEM_P256_HKDF_SHA256   = fech.KEM_P256_HKDF_SHA256
	KEM_P384_HKDF_SHA384   = fech.KEM_P384_HKDF_SHA384
	KEM_P521_HKDF_SHA512   = fech.KEM_P521_HKDF_SHA512
	KEM_X25519_HKDF_SHA256 = fech.KEM_X25519_HKDF_SHA256
)

// Standard HPKE KDF identifiers defined in RFC 9180 §7.2 and draft-ietf-tls-esni-22 §4.
const (
	KDF_HKDF_SHA256 = fech.KDF_HKDF_SHA256
	KDF_HKDF_SHA384 = fech.KDF_HKDF_SHA384
	KDF_HKDF_SHA512 = fech.KDF_HKDF_SHA512
)

// Standard HPKE AEAD identifiers defined in RFC 9180 §7.3 and draft-ietf-tls-esni-22 §4.
const (
	AEAD_AES_128_GCM       = fech.AEAD_AES_128_GCM
	AEAD_AES_256_GCM       = fech.AEAD_AES_256_GCM
	AEAD_CHACHA20_POLY1305 = fech.AEAD_CHACHA20_POLY1305
)

// Parsing and validation errors for ECH structures.
var (
	ErrTruncatedECHConfigList = fech.ErrTruncatedECHConfigList
	ErrTruncatedECHConfig     = fech.ErrTruncatedECHConfig
	ErrUnsupportedVersion     = fech.ErrUnsupportedVersion
	ErrInvalidPublicName      = fech.ErrInvalidPublicName
	ErrMalformedCipherSuites  = fech.ErrMalformedCipherSuites
	ErrEmptyConfigList        = fech.ErrEmptyConfigList
)

// CipherSuite represents an HPKE symmetric cipher suite (KDF + AEAD).
type CipherSuite = fech.CipherSuite

// Extension represents an ECH configuration extension.
type Extension = fech.Extension

// KeyConfig represents the HpkeKeyConfig structure.
type KeyConfig = fech.KeyConfig

// ConfigContents represents the ECHConfigContents structure.
type ConfigContents = fech.ConfigContents

// Config represents a parsed ECHConfig structure.
type Config = fech.Config

// ParseConfigList decodes a sequence of ECHConfig structures from an ECHConfigList wire payload.
func ParseConfigList(raw []byte) ([]*Config, error) {
	return fech.ParseConfigList(raw)
}

// MarshalConfigList encodes a slice of [Config] pointers into an ECHConfigList wire format.
func MarshalConfigList(configs []*Config) ([]byte, error) {
	return fech.MarshalConfigList(configs)
}

// ParseConfig decodes a single [Config] from wire format bytes.
func ParseConfig(raw []byte) (*Config, int, error) {
	return fech.ParseConfig(raw)
}

// ParseBase64 decodes a base64-encoded ECHConfigList string into a slice of [*Config].
func ParseBase64(raw string) ([]*Config, error) {
	return fech.ParseBase64(raw)
}

// ValidatePublicName verifies that public_name conforms strictly to draft-ietf-tls-esni-22 §4 rules.
func ValidatePublicName(name string) error {
	return fech.ValidatePublicName(name)
}

// CalculatePadding computes the recommended padding bytes for EncodedClientHelloInner.
func CalculatePadding(innerLen, sniLen int, maxNameLen uint8) int {
	return fech.CalculatePadding(innerLen, sniLen, maxNameLen)
}
