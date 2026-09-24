// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package ech implements TLS Encrypted Client Hello strictly conforming to draft-ietf-tls-esni-22.
//
// It provides binary wire-format decoding and encoding for ECHConfigList and ECHConfig structures,
// public name validation, cipher suite negotiation, recommended inner ClientHello padding calculation,
// and GREASE ECH generation.
package ech

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Standard TLS extension type and alert codepoints defined in draft-ietf-tls-esni-22 §11.
const (
	// ExtensionTypeEncryptedClientHello is the TLS 1.3 extension codepoint (0xfe0d) defined in draft-ietf-tls-esni-22 §11.1.
	ExtensionTypeEncryptedClientHello uint16 = 0xfe0d

	// ExtensionTypeECHOuterExtensions is the inner ClientHello extension codepoint (0xfd00) defined in draft-ietf-tls-esni-22 §11.1.
	ExtensionTypeECHOuterExtensions uint16 = 0xfd00

	// AlertECHRequired is the TLS alert codepoint (121) sent upon unaccepted ECH offerings (draft-ietf-tls-esni-22 §11.2).
	AlertECHRequired uint8 = 121

	// VersionDraft22 is the standard draft-22 ECH version codepoint (0xfe0d) defined in draft-ietf-tls-esni-22 §4.
	VersionDraft22 uint16 = 0xfe0d
)

// ClientHelloType represents the outer or inner variant of the encrypted_client_hello extension (draft-ietf-tls-esni-22 §5).
type ClientHelloType uint8

const (
	// ClientHelloOuter specifies the public outer ClientHello variant containing ciphertext (draft-ietf-tls-esni-22 §5).
	ClientHelloOuter ClientHelloType = 0

	// ClientHelloInner specifies the private inner ClientHello variant with empty payload (draft-ietf-tls-esni-22 §5).
	ClientHelloInner ClientHelloType = 1
)

// Standard HPKE KEM identifiers defined in RFC 9180 §7.1 and draft-ietf-tls-esni-22 §4.
const (
	KEM_P256_HKDF_SHA256   uint16 = 0x0010
	KEM_P384_HKDF_SHA384   uint16 = 0x0011
	KEM_P521_HKDF_SHA512   uint16 = 0x0012
	KEM_X25519_HKDF_SHA256 uint16 = 0x0020
)

// Standard HPKE KDF identifiers defined in RFC 9180 §7.2 and draft-ietf-tls-esni-22 §4.
const (
	KDF_HKDF_SHA256 uint16 = 0x0001
	KDF_HKDF_SHA384 uint16 = 0x0002
	KDF_HKDF_SHA512 uint16 = 0x0003
)

// Standard HPKE AEAD identifiers defined in RFC 9180 §7.3 and draft-ietf-tls-esni-22 §4.
const (
	AEAD_AES_128_GCM       uint16 = 0x0001
	AEAD_AES_256_GCM       uint16 = 0x0002
	AEAD_CHACHA20_POLY1305 uint16 = 0x0003
)

// Parsing and validation errors for ECH structures.
var (
	ErrTruncatedECHConfigList = errors.New("ech: truncated ECHConfigList wire format")
	ErrTruncatedECHConfig     = errors.New("ech: truncated ECHConfig wire format")
	ErrUnsupportedVersion     = errors.New("ech: unsupported ECHConfig version")
	ErrInvalidPublicName      = errors.New("ech: invalid public_name DNS syntax (draft-ietf-tls-esni-22 §4)")
	ErrMalformedCipherSuites  = errors.New("ech: malformed HPKE cipher suites payload")
	ErrEmptyConfigList        = errors.New("ech: empty ECHConfigList")
)

// CipherSuite represents an HPKE symmetric cipher suite (KDF + AEAD) per draft-ietf-tls-esni-22 §4.
type CipherSuite struct {
	KDFID  uint16
	AEADID uint16
}

// Extension represents an ECH configuration extension per draft-ietf-tls-esni-22 §4.2.
type Extension struct {
	Type uint16
	Data []byte
}

// IsMandatory reports whether the extension is mandatory (high-order bit set to 1 per draft-ietf-tls-esni-22 §4.2).
func (e Extension) IsMandatory() bool {
	return (e.Type & 0x8000) != 0
}

// KeyConfig represents the HpkeKeyConfig structure per draft-ietf-tls-esni-22 §4.
type KeyConfig struct {
	ConfigID     uint8
	KEMID        uint16
	PublicKey    []byte
	CipherSuites []CipherSuite
}

// ConfigContents represents the ECHConfigContents structure per draft-ietf-tls-esni-22 §4.
type ConfigContents struct {
	KeyConfig         KeyConfig
	MaximumNameLength uint8
	PublicName        string
	Extensions        []Extension
}

// Config represents a parsed ECHConfig structure per draft-ietf-tls-esni-22 §4.
type Config struct {
	Version  uint16
	Length   uint16
	Contents ConfigContents
}

// HasUnsupportedMandatoryExtension checks if any mandatory extension in the config is not supported (draft-ietf-tls-esni-22 §4.2).
func (c *Config) HasUnsupportedMandatoryExtension(supported ...uint16) bool {
	if c == nil {
		return false
	}

	for _, ext := range c.Contents.Extensions {
		if ext.IsMandatory() {
			if !slices.Contains(supported, ext.Type) {
				return true
			}
		}
	}

	return false
}

// SupportsCipherSuite checks whether the config supports the given KDF and AEAD IDs (draft-ietf-tls-esni-22 §6.1).
func (c *Config) SupportsCipherSuite(kdfID, aeadID uint16) bool {
	if c == nil {
		return false
	}

	for _, cs := range c.Contents.KeyConfig.CipherSuites {
		if cs.KDFID == kdfID && cs.AEADID == aeadID {
			return true
		}
	}

	return false
}

// ParseConfigList decodes a sequence of ECHConfig structures from an ECHConfigList wire payload (draft-ietf-tls-esni-22 §4).
func ParseConfigList(raw []byte) ([]*Config, error) {
	if len(raw) < 4 {
		return nil, ErrTruncatedECHConfigList
	}

	totalLen := int(binary.BigEndian.Uint16(raw[0:2]))
	if len(raw) < 2+totalLen {
		return nil, ErrTruncatedECHConfigList
	}

	data := raw[2 : 2+totalLen]

	var configs []*Config

	offset := 0

	for offset < len(data) {
		cfg, consumed, err := ParseConfig(data[offset:])
		if err != nil {
			return nil, err
		}

		configs = append(configs, cfg)
		offset += consumed
	}

	if len(configs) == 0 {
		return nil, ErrEmptyConfigList
	}

	return configs, nil
}

// MarshalConfigList encodes a slice of [Config] pointers into an ECHConfigList wire format (draft-ietf-tls-esni-22 §4).
func MarshalConfigList(configs []*Config) ([]byte, error) {
	if len(configs) == 0 {
		return nil, ErrEmptyConfigList
	}

	var payload bytes.Buffer
	for _, cfg := range configs {
		b, err := cfg.Marshal()
		if err != nil {
			return nil, err
		}

		payload.Write(b)
	}

	var (
		buf    bytes.Buffer
		lenBuf [2]byte
	)
	binary.BigEndian.PutUint16(lenBuf[:], uint16(payload.Len()))
	buf.Write(lenBuf[:])
	buf.Write(payload.Bytes())

	return buf.Bytes(), nil
}

// ParseConfig decodes a single [Config] from wire format bytes, returning the parsed config and consumed bytes (draft-ietf-tls-esni-22 §4).
func ParseConfig(raw []byte) (*Config, int, error) {
	if len(raw) < 4 {
		return nil, 0, ErrTruncatedECHConfig
	}

	version := binary.BigEndian.Uint16(raw[0:2])
	length := int(binary.BigEndian.Uint16(raw[2:4]))

	if len(raw) < 4+length {
		return nil, 0, ErrTruncatedECHConfig
	}

	consumed := 4 + length
	contentsData := raw[4:consumed]

	if version != VersionDraft22 {
		return &Config{
			Version: version,
			Length:  uint16(length),
		}, consumed, nil
	}

	contents, err := parseContents(contentsData)
	if err != nil {
		return nil, 0, err
	}

	return &Config{
		Version:  version,
		Length:   uint16(length),
		Contents: contents,
	}, consumed, nil
}

// Marshal encodes the [Config] into its wire format bytes (draft-ietf-tls-esni-22 §4).
func (c *Config) Marshal() ([]byte, error) {
	if c == nil {
		return nil, errors.New("ech: nil config")
	}

	contentsBytes, err := marshalContents(c.Contents)
	if err != nil {
		return nil, err
	}

	var (
		buf bytes.Buffer
		hdr [4]byte
	)
	binary.BigEndian.PutUint16(hdr[0:2], c.Version)
	binary.BigEndian.PutUint16(hdr[2:4], uint16(len(contentsBytes)))
	buf.Write(hdr[:])
	buf.Write(contentsBytes)

	return buf.Bytes(), nil
}

// ParseBase64 decodes a base64-encoded ECHConfigList string into a slice of [*Config] (draft-ietf-tls-esni-22 §4).
func ParseBase64(raw string) ([]*Config, error) {
	cleaned := strings.TrimSpace(raw)
	if cleaned == "" {
		return nil, ErrTruncatedECHConfigList
	}

	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(cleaned)
		if err != nil {
			return nil, fmt.Errorf("ech: invalid base64: %w", err)
		}
	}

	return ParseConfigList(decoded)
}

// ValidatePublicName verifies that public_name conforms strictly to draft-ietf-tls-esni-22 §4 rules:
// - Non-empty dot-separated sequence of LDH labels (RFC 5890 §2.3.1).
// - Does not begin or end with a dot.
// - Final label does not consist of all digits or hex prefixes ("0x"/"0X") to prevent IPv4 literal interpretation.
// - No label exceeds 63 octets.
func ValidatePublicName(name string) error {
	if name == "" || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".") {
		return ErrInvalidPublicName
	}

	labels := strings.Split(name, ".")
	if len(labels) == 0 {
		return ErrInvalidPublicName
	}

	for _, l := range labels {
		if len(l) == 0 || len(l) > 63 {
			return ErrInvalidPublicName
		}

		for i := 0; i < len(l); i++ {
			c := l[i]
			isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')

			isHyphen := c == '-'
			if !isAlphaNum && !isHyphen {
				return ErrInvalidPublicName
			}

			if isHyphen && (i == 0 || i == len(l)-1) {
				return ErrInvalidPublicName
			}
		}
	}

	last := labels[len(labels)-1]
	// Check if final label is all digits (IPv4 confusion)
	allDigits := true
	for i := 0; i < len(last); i++ {
		if last[i] < '0' || last[i] > '9' {
			allDigits = false
			break
		}
	}

	if allDigits {
		return ErrInvalidPublicName
	}

	// Check if final label starts with 0x/0X
	if strings.HasPrefix(strings.ToLower(last), "0x") {
		return ErrInvalidPublicName
	}

	return nil
}

// CalculatePadding computes the recommended padding bytes for EncodedClientHelloInner (draft-ietf-tls-esni-22 §6.1.3).
// maxNameLen is ECHConfigContents.maximum_name_length.
func CalculatePadding(innerLen, sniLen int, maxNameLen uint8) int {
	padding := 0
	lMax := int(maxNameLen)

	if sniLen > 0 {
		if lMax > sniLen {
			padding += lMax - sniLen
		}
	} else {
		padding += lMax + 9
	}

	currentTotal := innerLen + padding
	n := max(31-((currentTotal-1)%32), 0)

	return padding + n
}

func parseContents(data []byte) (ConfigContents, error) {
	var c ConfigContents
	if len(data) < 7 {
		return c, ErrTruncatedECHConfig
	}

	configID := data[0]
	kemID := binary.BigEndian.Uint16(data[1:3])
	offset := 3

	if offset+2 > len(data) {
		return c, ErrTruncatedECHConfig
	}

	pubKeyLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2

	if offset+pubKeyLen > len(data) {
		return c, ErrTruncatedECHConfig
	}

	pubKey := slices.Clone(data[offset : offset+pubKeyLen])
	offset += pubKeyLen

	if offset+2 > len(data) {
		return c, ErrTruncatedECHConfig
	}

	csLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2

	if csLen < 4 || csLen%4 != 0 || offset+csLen > len(data) {
		return c, ErrMalformedCipherSuites
	}

	var cipherSuites []CipherSuite
	for i := 0; i < csLen; i += 4 {
		kdf := binary.BigEndian.Uint16(data[offset+i : offset+i+2])
		aead := binary.BigEndian.Uint16(data[offset+i+2 : offset+i+4])
		cipherSuites = append(cipherSuites, CipherSuite{KDFID: kdf, AEADID: aead})
	}

	offset += csLen

	if offset >= len(data) {
		return c, ErrTruncatedECHConfig
	}

	maxNameLen := data[offset]
	offset++

	if offset >= len(data) {
		return c, ErrTruncatedECHConfig
	}

	nameLen := int(data[offset])
	offset++

	if offset+nameLen > len(data) {
		return c, ErrTruncatedECHConfig
	}

	publicName := string(data[offset : offset+nameLen])
	offset += nameLen

	if err := ValidatePublicName(publicName); err != nil {
		return c, err
	}

	if offset+2 > len(data) {
		return c, ErrTruncatedECHConfig
	}

	extLen := int(binary.BigEndian.Uint16(data[offset : offset+2]))
	offset += 2

	if offset+extLen > len(data) {
		return c, ErrTruncatedECHConfig
	}

	var extensions []Extension

	extOffset := offset

	extEnd := offset + extLen
	for extOffset < extEnd {
		if extOffset+4 > extEnd {
			return c, ErrTruncatedECHConfig
		}

		extType := binary.BigEndian.Uint16(data[extOffset : extOffset+2])
		extDataLen := int(binary.BigEndian.Uint16(data[extOffset+2 : extOffset+4]))
		extOffset += 4

		if extOffset+extDataLen > extEnd {
			return c, ErrTruncatedECHConfig
		}

		extData := slices.Clone(data[extOffset : extOffset+extDataLen])
		extensions = append(extensions, Extension{Type: extType, Data: extData})
		extOffset += extDataLen
	}

	c.KeyConfig = KeyConfig{
		ConfigID:     configID,
		KEMID:        kemID,
		PublicKey:    pubKey,
		CipherSuites: cipherSuites,
	}
	c.MaximumNameLength = maxNameLen
	c.PublicName = publicName
	c.Extensions = extensions

	return c, nil
}

func marshalContents(c ConfigContents) ([]byte, error) {
	if err := ValidatePublicName(c.PublicName); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.WriteByte(c.KeyConfig.ConfigID)

	var kemBuf [2]byte
	binary.BigEndian.PutUint16(kemBuf[:], c.KeyConfig.KEMID)
	buf.Write(kemBuf[:])

	var pkLen [2]byte
	binary.BigEndian.PutUint16(pkLen[:], uint16(len(c.KeyConfig.PublicKey)))
	buf.Write(pkLen[:])
	buf.Write(c.KeyConfig.PublicKey)

	var csBytes bytes.Buffer
	for _, cs := range c.KeyConfig.CipherSuites {
		var csBuf [4]byte
		binary.BigEndian.PutUint16(csBuf[0:2], cs.KDFID)
		binary.BigEndian.PutUint16(csBuf[2:4], cs.AEADID)
		csBytes.Write(csBuf[:])
	}

	var csLen [2]byte
	binary.BigEndian.PutUint16(csLen[:], uint16(csBytes.Len()))
	buf.Write(csLen[:])
	buf.Write(csBytes.Bytes())

	buf.WriteByte(c.MaximumNameLength)

	buf.WriteByte(byte(len(c.PublicName)))
	buf.WriteString(c.PublicName)

	var extBytes bytes.Buffer
	for _, ext := range c.Extensions {
		var extHdr [4]byte
		binary.BigEndian.PutUint16(extHdr[0:2], ext.Type)
		binary.BigEndian.PutUint16(extHdr[2:4], uint16(len(ext.Data)))
		extBytes.Write(extHdr[:])
		extBytes.Write(ext.Data)
	}

	var extLen [2]byte
	binary.BigEndian.PutUint16(extLen[:], uint16(extBytes.Len()))
	buf.Write(extLen[:])
	buf.Write(extBytes.Bytes())

	return buf.Bytes(), nil
}
