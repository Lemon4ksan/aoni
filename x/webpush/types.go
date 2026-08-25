// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package webpush implements the Web Push protocol (RFC 8030),
// Message Encryption for Web Push (RFC 8291 ECE aes128gcm), and
// Voluntary Application Server Identification (RFC 8292 VAPID)
// for direct delivery of push notifications to Apple APNs, Google FCM, and Mozilla Push Service.
package webpush

import (
	"errors"
	"time"
)

// Standard HTTP Header names and Content Encodings for WebPush.
const (
	// HeaderTTL specifies the message retention duration in seconds (RFC 8030 §5.2).
	HeaderTTL = "TTL"

	// HeaderUrgency specifies the message urgency level (RFC 8030 §5.3).
	HeaderUrgency = "Urgency"

	// HeaderTopic specifies the correlation topic for message replacement (RFC 8030 §5.4).
	HeaderTopic = "Topic"

	// HeaderContentEncoding specifies the payload content encoding (RFC 8291 §4).
	HeaderContentEncoding = "Content-Encoding"

	// HeaderAuthorization specifies the VAPID authorization header (RFC 8292 §3).
	HeaderAuthorization = "Authorization"

	// ContentEncodingAES128GCM is the mandatory content encoding for encrypted push messages (RFC 8291).
	ContentEncodingAES128GCM = "aes128gcm"
)

// Common WebPush errors.
var (
	ErrInvalidSubscription = errors.New("aoni/webpush: invalid subscription endpoint or keys")
	ErrInvalidP256DHKey    = errors.New("aoni/webpush: invalid P-256 public key (p256dh)")
	ErrInvalidAuthSecret   = errors.New("aoni/webpush: invalid authentication secret (auth)")
	ErrInvalidVAPIDKeys    = errors.New("aoni/webpush: invalid VAPID keys")
	ErrPayloadTooLarge     = errors.New("aoni/webpush: plaintext payload exceeds maximum size (3993 bytes)")
	ErrDecryptionFailed    = errors.New("aoni/webpush: failed to decrypt message payload")
	ErrPushRejected        = errors.New("aoni/webpush: push service rejected message")
)

// Urgency defines the delivery urgency hint for battery and network optimization (RFC 8030 §5.3).
type Urgency string

const (
	// UrgencyVeryLow is for advertisements and non-critical updates on power and Wi-Fi.
	UrgencyVeryLow Urgency = "very-low"

	// UrgencyLow is for topic updates on either power or Wi-Fi.
	UrgencyLow Urgency = "low"

	// UrgencyNormal is for standard chat, email, or calendar alerts (default).
	UrgencyNormal Urgency = "normal"

	// UrgencyHigh is for time-sensitive alerts or incoming calls on low battery.
	UrgencyHigh Urgency = "high"
)

// Subscription represents the W3C PushSubscription JSON object provided by the user agent.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     Keys   `json:"keys"`
}

// Keys encapsulates the cryptographic key parameters associated with a [Subscription].
type Keys struct {
	// P256DH contains the base64url-encoded uncompressed P-256 public key of the user agent.
	P256DH string `json:"p256dh"`

	// Auth contains the base64url-encoded 16-byte authentication secret of the user agent.
	Auth string `json:"auth"`
}

// Message encapsulates the configuration and payload of an outbound WebPush notification.
type Message struct {
	Payload []byte
	TTL     time.Duration
	Urgency Urgency
	Topic   string
	VAPID   *VAPIDConfig
}

// VAPIDConfig specifies Voluntary Application Server Identification parameters (RFC 8292).
type VAPIDConfig struct {
	Keys    *VAPIDKeys
	Subject string // e.g. "mailto:admin@example.com" or "https://example.com"
	TTL     time.Duration
}
