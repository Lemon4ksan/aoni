// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package webpush

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/mod"
)

// MessageOption defines functional modifiers for outbound push messages.
type MessageOption func(*Message)

// WithTTL sets the message Time-To-Live retention duration (RFC 8030 §5.2).
func WithTTL(ttl time.Duration) MessageOption {
	return func(m *Message) {
		m.TTL = ttl
	}
}

// WithUrgency sets the message delivery urgency level (RFC 8030 §5.3).
func WithUrgency(urgency Urgency) MessageOption {
	return func(m *Message) {
		m.Urgency = urgency
	}
}

// WithTopic sets the correlation topic for message replacement (RFC 8030 §5.4).
func WithTopic(topic string) MessageOption {
	return func(m *Message) {
		m.Topic = topic
	}
}

// WithVAPID overrides the VAPID configuration for this specific push message.
func WithVAPID(vapid *VAPIDConfig) MessageOption {
	return func(m *Message) {
		m.VAPID = vapid
	}
}

// Client provides high-throughput, zero-allocation WebPush notification delivery to Apple APNs, Google FCM, and Mozilla Push Services.
//
// Encrypts payloads via ECDH over curve P-256 and HKDF-SHA256 (RFC 8291) with RFC 8292 VAPID JWT authentication.
//
// # Example
//
//	wpClient := webpush.NewClient(client, vapidConfig)
//	resp, err := wpClient.Send(ctx, subscription, &webpush.Message{
//	    Payload: []byte(`{"title":"Hello"}`),
//	})
type Client struct {
	httpClient *aoni.Client
	vapid      *VAPIDConfig
}

// NewClient instantiates a new WebPush [Client] backed by an [aoni.Client] and optional default VAPID configuration.
func NewClient(httpClient *aoni.Client, defaultVAPID *VAPIDConfig) *Client {
	if httpClient == nil {
		httpClient = aoni.NewClient(nil)
	}

	return &Client{
		httpClient: httpClient,
		vapid:      defaultVAPID,
	}
}

// Send sends an encrypted WebPush notification to the designated subscriber per RFC 8030 and RFC 8291.
//
// # RFC Compliance
//
// Conforms to RFC 8030 (Generic Event Delivery Using HTTP Push), RFC 8291 (Message Encryption for Web Push),
// and RFC 8292 (Voluntary Application Server Identification: VAPID).
func (c *Client) Send(ctx context.Context, sub *Subscription, msg *Message) (*http.Response, error) {
	if sub == nil || sub.Endpoint == "" {
		return nil, ErrInvalidSubscription
	}

	if msg == nil {
		msg = &Message{}
	}

	var (
		ciphertext []byte
		err        error
	)

	if len(msg.Payload) > 0 {
		ciphertext, err = Encrypt(msg.Payload, sub, nil)
		if err != nil {
			return nil, fmt.Errorf("aoni/webpush: encryption error: %w", err)
		}
	}

	ttl := msg.TTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}

	ttlSeconds := int64(ttl.Seconds())
	if ttlSeconds < 0 {
		ttlSeconds = 0
	}

	urgency := msg.Urgency
	if urgency == "" {
		urgency = UrgencyNormal
	}

	vapidCfg := msg.VAPID
	if vapidCfg == nil {
		vapidCfg = c.vapid
	}

	var mods []aoni.RequestModifier

	mods = append(mods,
		mod.WithHeader(HeaderTTL, strconv.FormatInt(ttlSeconds, 10)),
		mod.WithHeader(HeaderUrgency, string(urgency)),
	)

	if msg.Topic != "" {
		mods = append(mods, mod.WithHeader(HeaderTopic, msg.Topic))
	}

	if len(ciphertext) > 0 {
		mods = append(mods,
			mod.WithHeader(HeaderContentEncoding, ContentEncodingAES128GCM),
			mod.WithBodyBytes(ciphertext),
		)
	}

	if vapidCfg != nil && vapidCfg.Keys != nil {
		authVal, err := vapidCfg.Keys.AuthorizationHeader(sub.Endpoint, vapidCfg.Subject, vapidCfg.TTL)
		if err != nil {
			return nil, fmt.Errorf("aoni/webpush: VAPID signing error: %w", err)
		}

		mods = append(mods, mod.WithHeader(HeaderAuthorization, authVal))
	}

	resp, err := c.httpClient.Request(ctx, http.MethodPost, sub.Endpoint, mods...)
	if err != nil {
		return nil, err
	}

	// 201 Created (standard accepted) or 202 Accepted (with receipts) are successful deliveries
	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusAccepted ||
		resp.StatusCode == http.StatusOK {
		return resp, nil
	}

	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	return resp, fmt.Errorf("%w: status %d: %s", ErrPushRejected, resp.StatusCode, string(body))
}

// SendJSON marshals payload into JSON and delivers the encrypted WebPush notification.
func (c *Client) SendJSON(
	ctx context.Context,
	sub *Subscription,
	payload any,
	opts ...MessageOption,
) (*http.Response, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("aoni/webpush: failed to marshal JSON payload: %w", err)
	}

	msg := &Message{
		Payload: data,
	}

	for _, opt := range opts {
		if opt != nil {
			opt(msg)
		}
	}

	return c.Send(ctx, sub, msg)
}
