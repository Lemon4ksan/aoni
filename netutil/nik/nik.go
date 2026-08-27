// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package nik implements Chromium-grade Network Isolation Keys (NIK) and Network Anonymization Keys (NAK)
// for partitioning client network state (DNS cache, HTTP cache, TLS session tickets, and cookie jars)
// by top-level site and frame origin (RFC 6265bis / W3C Partitioned Network State).
package nik

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"

	asyncctx "github.com/lemon4ksan/foundation/async/context"
	"github.com/lemon4ksan/foundation/silicon/bytesconv"
)

type nikContextKey struct{}

// NetworkIsolationKey encapsulates a compound key used to partition network state in Chromium.
// It consists of the top-level site, the current frame site, and a cross-site indicator.
type NetworkIsolationKey struct {
	topFrameSite string
	frameSite    string
	isCrossSite  bool
	isTransient  bool
	transientID  string
}

// New constructs a [NetworkIsolationKey] from topFrameSite and frameSite.
// Canonicalizes origins to lowercase schemes and hostnames.
func New(topFrameSite, frameSite string) NetworkIsolationKey {
	top := canonicalizeSite(topFrameSite)
	frame := canonicalizeSite(frameSite)

	if top == "" && frame == "" {
		return NetworkIsolationKey{}
	}

	if frame == "" {
		frame = top
	}

	cross := !bytesconv.EqualFoldASCII(top, frame)

	return NetworkIsolationKey{
		topFrameSite: top,
		frameSite:    frame,
		isCrossSite:  cross,
	}
}

// NewSameSite constructs a same-site [NetworkIsolationKey] where topFrameSite equals frameSite.
func NewSameSite(site string) NetworkIsolationKey {
	s := canonicalizeSite(site)
	if s == "" {
		return NetworkIsolationKey{}
	}

	return NetworkIsolationKey{
		topFrameSite: s,
		frameSite:    s,
		isCrossSite:  false,
	}
}

// NewTransient creates an ephemeral, isolated [NetworkIsolationKey] with a unique random identifier.
// Useful for private/incognito browsing tabs or one-off tasks where state must never be shared.
func NewTransient() NetworkIsolationKey {
	var buf [16]byte

	_, _ = rand.Read(buf[:])

	id := hex.EncodeToString(buf[:])

	return NetworkIsolationKey{
		isTransient: true,
		transientID: id,
	}
}

// TopFrameSite yields the canonicalized top-level site.
func (k NetworkIsolationKey) TopFrameSite() string {
	return k.topFrameSite
}

// FrameSite yields the canonicalized frame site.
func (k NetworkIsolationKey) FrameSite() string {
	return k.frameSite
}

// IsCrossSite reports whether the frame site is cross-site relative to topFrameSite.
func (k NetworkIsolationKey) IsCrossSite() bool {
	return k.isCrossSite
}

// IsTransient reports whether the key is an ephemeral, in-memory partition.
func (k NetworkIsolationKey) IsTransient() bool {
	return k.isTransient
}

// IsEmpty reports whether the key holds no site or transient identifiers.
func (k NetworkIsolationKey) IsEmpty() bool {
	return !k.isTransient && k.topFrameSite == "" && k.frameSite == ""
}

// KeyString serializes the [NetworkIsolationKey] into a deterministic string representation
// suitable for map keys, cache lookups, and session store keys.
func (k NetworkIsolationKey) KeyString() string {
	if k.isTransient {
		return "transient:" + k.transientID
	}

	if k.IsEmpty() {
		return ""
	}

	if !k.isCrossSite {
		return k.topFrameSite
	}

	return fmt.Sprintf("%s|%s", k.topFrameSite, k.frameSite)
}

func (k NetworkIsolationKey) String() string {
	return k.KeyString()
}

// WithNIK binds a [NetworkIsolationKey] to the context.
func WithNIK(ctx context.Context, key NetworkIsolationKey) context.Context {
	return asyncctx.WithValue(ctx, nikContextKey{}, key)
}

// FromContext extracts a [NetworkIsolationKey] from the context.
func FromContext(ctx context.Context) (NetworkIsolationKey, bool) {
	if ctx == nil {
		return NetworkIsolationKey{}, false
	}

	val := ctx.Value(nikContextKey{})
	if k, ok := val.(NetworkIsolationKey); ok && !k.IsEmpty() {
		return k, true
	}

	return NetworkIsolationKey{}, false
}

// GetNIKOrEmpty retrieves the [NetworkIsolationKey] from context or returns an empty key.
func GetNIKOrEmpty(ctx context.Context) NetworkIsolationKey {
	if k, ok := FromContext(ctx); ok {
		return k
	}

	return NetworkIsolationKey{}
}

func canonicalizeSite(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.ToLower(raw)
	}

	scheme := strings.ToLower(u.Scheme)
	host := strings.ToLower(u.Hostname())
	port := u.Port()

	if port != "" && port != "80" && port != "443" {
		return fmt.Sprintf("%s://%s:%s", scheme, host, port)
	}

	return fmt.Sprintf("%s://%s", scheme, host)
}
