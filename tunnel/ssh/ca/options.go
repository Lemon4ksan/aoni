// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ca

import (
	"maps"
	"time"
)

// IssueConfig aggregates parameters for issuing OpenSSH certificates.
type IssueConfig struct {
	TTL             time.Duration
	Principals      []string
	Extensions      map[string]string
	CriticalOptions map[string]string
	KeyID           string
	Serial          uint64
}

// IssueOption configures certificate generation parameters.
type IssueOption func(*IssueConfig)

// DefaultIssueConfig returns production defaults for OpenSSH user certificate issuance.
func DefaultIssueConfig() IssueConfig {
	return IssueConfig{
		TTL: 8 * time.Hour,
		Extensions: map[string]string{
			"permit-agent-forwarding": "",
			"permit-pty":              "",
			"permit-user-rc":          "",
			"permit-X11-forwarding":   "",
		},
		CriticalOptions: make(map[string]string),
	}
}

// WithTTL sets the certificate validity duration starting from the moment of issuance.
func WithTTL(d time.Duration) IssueOption {
	return func(cfg *IssueConfig) {
		if d > 0 {
			cfg.TTL = d
		}
	}
}

// WithPrincipals configures authorized SSH usernames or hostnames allowed by the certificate.
func WithPrincipals(principals ...string) IssueOption {
	return func(cfg *IssueConfig) {
		cfg.Principals = append(cfg.Principals, principals...)
	}
}

// WithExtensions sets custom OpenSSH certificate extensions (e.g., "permit-pty").
func WithExtensions(exts map[string]string) IssueOption {
	return func(cfg *IssueConfig) {
		if cfg.Extensions == nil {
			cfg.Extensions = make(map[string]string)
		}

		maps.Copy(cfg.Extensions, exts)
	}
}

// WithCriticalOptions sets mandatory critical options that must be satisfied during connection.
func WithCriticalOptions(opts map[string]string) IssueOption {
	return func(cfg *IssueConfig) {
		if cfg.CriticalOptions == nil {
			cfg.CriticalOptions = make(map[string]string)
		}

		maps.Copy(cfg.CriticalOptions, opts)
	}
}

// WithKeyID sets a human-readable identifier for tracking certificate audit logs.
func WithKeyID(id string) IssueOption {
	return func(cfg *IssueConfig) {
		cfg.KeyID = id
	}
}

// WithSerial sets a unique serial number for the certificate.
func WithSerial(serial uint64) IssueOption {
	return func(cfg *IssueConfig) {
		cfg.Serial = serial
	}
}
