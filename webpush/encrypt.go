// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webpush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	// MaxPayloadSize is the maximum permitted plaintext size in bytes for a single WebPush record (RFC 8291 §4).
	MaxPayloadSize = 3993

	// DefaultRecordSize is the standard record size (4096 octets) defined in RFC 8188 & RFC 8291 §4.
	DefaultRecordSize = 4096

	// HeaderLength is the fixed size of an RFC 8291 aes128gcm content coding header (86 octets).
	HeaderLength = 86
)

// EncryptConfig allows overriding encryption parameters for testing or specialized payloads.
type EncryptConfig struct {
	Salt       []byte
	SenderPriv *ecdh.PrivateKey
	RecordSize uint32
}

// Encrypt encrypts plaintext according to RFC 8291 (Message Encryption for Web Push) using ECE aes128gcm.
func Encrypt(plaintext []byte, sub *Subscription, cfg *EncryptConfig) ([]byte, error) {
	if sub == nil || sub.Keys.P256DH == "" || sub.Keys.Auth == "" {
		return nil, ErrInvalidSubscription
	}

	if len(plaintext) > MaxPayloadSize {
		return nil, ErrPayloadTooLarge
	}

	uaPubBytes, err := decodeBase64URL(sub.Keys.P256DH)
	if err != nil || len(uaPubBytes) != 65 || uaPubBytes[0] != 0x04 {
		return nil, ErrInvalidP256DHKey
	}

	authSecret, err := decodeBase64URL(sub.Keys.Auth)
	if err != nil || len(authSecret) != 16 {
		return nil, ErrInvalidAuthSecret
	}

	uaPub, err := ecdh.P256().NewPublicKey(uaPubBytes)
	if err != nil {
		return nil, fmt.Errorf("aoni/webpush: invalid user agent public key: %w", err)
	}

	return encryptWithKeys(plaintext, uaPub, authSecret, cfg)
}

func encryptWithKeys(plaintext []byte, uaPub *ecdh.PublicKey, authSecret []byte, cfg *EncryptConfig) ([]byte, error) {
	var asPriv *ecdh.PrivateKey
	var err error

	if cfg != nil && cfg.SenderPriv != nil {
		asPriv = cfg.SenderPriv
	} else {
		asPriv, err = ecdh.P256().GenerateKey(rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("aoni/webpush: failed to generate ephemeral ECDH key: %w", err)
		}
	}

	asPubBytes := asPriv.PublicKey().Bytes()

	salt := make([]byte, 16)
	if cfg != nil && len(cfg.Salt) == 16 {
		copy(salt, cfg.Salt)
	} else {
		if _, err := io.ReadFull(rand.Reader, salt); err != nil {
			return nil, err
		}
	}

	rs := uint32(DefaultRecordSize)
	if cfg != nil && cfg.RecordSize > 0 {
		rs = cfg.RecordSize
	}

	// 1. Derive shared secret: ECDH(as_private, ua_public)
	ecdhSecret, err := asPriv.ECDH(uaPub)
	if err != nil {
		return nil, fmt.Errorf("aoni/webpush: ECDH calculation failed: %w", err)
	}

	uaPubBytes := uaPub.Bytes()

	// 2. HKDF-Extract(salt=auth_secret, IKM=ecdh_secret) -> PRK_key
	prkKey := hmacSHA256(authSecret, ecdhSecret)

	// 3. HKDF-Expand(PRK_key, key_info, L_key=32) -> IKM
	keyInfo := bytes.Join([][]byte{
		[]byte("WebPush: info\x00"),
		uaPubBytes,
		asPubBytes,
		{0x01},
	}, nil)
	ikm := hmacSHA256(prkKey, keyInfo)

	// 4. HKDF-Extract(salt, IKM) -> PRK
	prk := hmacSHA256(salt, ikm)

	// 5. HKDF-Expand(PRK, cek_info, L_cek=16) -> CEK
	cekInfo := []byte("Content-Encoding: aes128gcm\x00\x01")
	cek := hmacSHA256(prk, cekInfo)[:16]

	// 6. HKDF-Expand(PRK, nonce_info, L_nonce=12) -> NONCE
	nonceInfo := []byte("Content-Encoding: nonce\x00\x01")
	nonce := hmacSHA256(prk, nonceInfo)[:12]

	// 7. Construct ECE header: salt(16) + rs(4) + idlen(1) + keyid(65) = 86 octets
	header := make([]byte, HeaderLength)
	copy(header[0:16], salt)
	binary.BigEndian.PutUint32(header[16:20], rs)
	header[20] = byte(len(asPubBytes))
	copy(header[21:86], asPubBytes)

	// 8. Plaintext with delimiter 0x02
	padded := make([]byte, len(plaintext)+1)
	copy(padded, plaintext)
	padded[len(plaintext)] = 0x02

	// 9. AES-128-GCM encryption
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, padded, nil)

	// Return Header || Ciphertext
	return append(header, ciphertext...), nil
}

// Decrypt decrypts an RFC 8291 WebPush payload using the recipient's ECDH private key and auth secret.
func Decrypt(payload []byte, uaPriv *ecdh.PrivateKey, authSecret []byte) ([]byte, error) {
	if len(payload) < HeaderLength+16 { // Header(86) + GCM tag(16)
		return nil, ErrDecryptionFailed
	}

	if uaPriv == nil || len(authSecret) != 16 {
		return nil, ErrInvalidSubscription
	}

	salt := payload[0:16]
	idLen := int(payload[20])
	if idLen != 65 || len(payload) < 21+idLen {
		return nil, ErrDecryptionFailed
	}

	asPubBytes := payload[21 : 21+idLen]
	ciphertext := payload[HeaderLength:]

	asPub, err := ecdh.P256().NewPublicKey(asPubBytes)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	// 1. ECDH(ua_private, as_public)
	ecdhSecret, err := uaPriv.ECDH(asPub)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	uaPubBytes := uaPriv.PublicKey().Bytes()

	// 2. HKDF-Extract(salt=auth_secret, IKM=ecdh_secret) -> PRK_key
	prkKey := hmacSHA256(authSecret, ecdhSecret)

	// 3. HKDF-Expand(PRK_key, key_info, L_key=32) -> IKM
	keyInfo := bytes.Join([][]byte{
		[]byte("WebPush: info\x00"),
		uaPubBytes,
		asPubBytes,
		{0x01},
	}, nil)
	ikm := hmacSHA256(prkKey, keyInfo)

	// 4. HKDF-Extract(salt, IKM) -> PRK
	prk := hmacSHA256(salt, ikm)

	// 5. Derive CEK and NONCE
	cekInfo := []byte("Content-Encoding: aes128gcm\x00\x01")
	cek := hmacSHA256(prk, cekInfo)[:16]

	nonceInfo := []byte("Content-Encoding: nonce\x00\x01")
	nonce := hmacSHA256(prk, nonceInfo)[:12]

	// 6. AES-128-GCM decryption
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	plaintextPadded, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	// Strip trailing padding and check 0x02 delimiter
	lastIdx := len(plaintextPadded) - 1
	for lastIdx >= 0 && plaintextPadded[lastIdx] == 0x00 {
		lastIdx--
	}

	if lastIdx < 0 || plaintextPadded[lastIdx] != 0x02 {
		return nil, ErrDecryptionFailed
	}

	return plaintextPadded[:lastIdx], nil
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}
