// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package netdial

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/netutil"
)

var (
	// ErrUTLSHandshakeFailed is returned when a uTLS handshake attempt fails.
	ErrUTLSHandshakeFailed = errors.New("netdial: uTLS handshake failed")

	// ErrCertificatePinning is returned when peer public keys do not match expected SHA-256 pins.
	ErrCertificatePinning = errors.New("netdial: certificate pinning validation failed")

	// ErrNoCertificatesPresented is returned when peer presents empty certificate chains during TLS handshakes.
	ErrNoCertificatesPresented = errors.New("netdial: no certificates presented by peer")

	// ErrInvalidPinFormat is returned when a certificate pin hash fails string decoding.
	ErrInvalidPinFormat = errors.New("netdial: invalid pin format")
)

// ClientHelloSpecProvider provides dynamic uTLS ClientHello specifications.
type ClientHelloSpecProvider interface {
	ClientHelloSpec() (*utls.ClientHelloSpec, error)
}

// RTLSOptions configures uTLS client handshake parameters.
type RTLSOptions struct {
	HelloID            *utls.ClientHelloID
	SpecProvider       ClientHelloSpecProvider
	SessionCache       utls.ClientSessionCache
	BaseTLSConfig      *tls.Config
	CertificatePins    map[string][]string
	ALPNOverride       []string
	JA4Callback        func(ja4.Report)
	InsecureSkipVerify bool
}

// UConnWrapper wraps a [*utls.UConn] to expose standard crypto/tls.ConnectionState
// compatibility so Go's net/http.Transport recognizes HTTP/2 ALPN negotiations.
type UConnWrapper struct {
	*utls.UConn
}

// ConnectionState implements the interface expected by net/http.Transport to extract TLS state.
func (w *UConnWrapper) ConnectionState() tls.ConnectionState {
	uState := w.UConn.ConnectionState()

	return tls.ConnectionState{
		Version:                     uState.Version,
		HandshakeComplete:           uState.HandshakeComplete,
		DidResume:                   uState.DidResume,
		CipherSuite:                 uState.CipherSuite,
		NegotiatedProtocol:          uState.NegotiatedProtocol,
		ServerName:                  uState.ServerName,
		PeerCertificates:            uState.PeerCertificates,
		VerifiedChains:              uState.VerifiedChains,
		SignedCertificateTimestamps: uState.SignedCertificateTimestamps,
		OCSPResponse:                uState.OCSPResponse,
		TLSUnique:                   uState.TLSUnique,
	}
}

// HandshakeUTLS executes a uTLS ClientHello handshake over a raw net.Conn socket.
// isolating TLS SNI from HTTP Host headers for Domain Fronting.
func HandshakeUTLS(
	ctx context.Context,
	conn net.Conn,
	host string,
	opts RTLSOptions,
) (*UConnWrapper, ja4.Report, error) {
	cleanHost, _ := netutil.CleanHostPort(host)
	sniHost := cleanHost

	isIP := net.ParseIP(sniHost) != nil
	if isIP {
		sniHost = "" // RFC 6066: IP addresses must not be sent in TLS SNI extension
	} else if sniHost == "" && opts.BaseTLSConfig != nil && opts.BaseTLSConfig.ServerName != "" {
		baseSNI, _ := netutil.CleanHostPort(opts.BaseTLSConfig.ServerName)
		if net.ParseIP(baseSNI) == nil {
			sniHost = baseSNI
		}
	}

	nextProtos := []string{"h2", "http/1.1"}
	if len(opts.ALPNOverride) > 0 {
		nextProtos = opts.ALPNOverride
	}

	uCfg := &utls.Config{
		ServerName:   sniHost,
		NextProtos:   nextProtos,
		OmitEmptyPsk: true,
	}

	if isIP {
		uCfg.InsecureServerNameToVerify = cleanHost
	}

	copyBaseTLSConfig(uCfg, opts.BaseTLSConfig)

	if opts.InsecureSkipVerify {
		uCfg.InsecureSkipVerify = true
	}

	applyUTLSPeerVerification(uCfg, opts, host)

	if opts.SessionCache != nil {
		uCfg.ClientSessionCache = opts.SessionCache
	}

	uConn, err := buildUConn(conn, uCfg, opts)
	if err != nil {
		_ = conn.Close()
		return nil, ja4.Report{}, err
	}

	if sniHost != "" {
		uConn.Extensions = ensureSNI(uConn.Extensions, sniHost)
	}

	uConn.Extensions = forceALPN(uConn.Extensions, nextProtos)

	if err := uConn.BuildHandshakeState(); err != nil {
		_ = conn.Close()
		return nil, ja4.Report{}, fmt.Errorf("%w: build handshake state: %w", ErrUTLSHandshakeFailed, err)
	}

	report := ExtractJA4FromUConn(uConn)

	if err := uConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, ja4.Report{}, fmt.Errorf("%w: %w", ErrUTLSHandshakeFailed, err)
	}

	if opts.JA4Callback != nil {
		opts.JA4Callback(report)
	}

	return &UConnWrapper{UConn: uConn}, report, nil
}

func buildUConn(conn net.Conn, uCfg *utls.Config, opts RTLSOptions) (*utls.UConn, error) {
	if opts.SpecProvider != nil {
		spec, err := opts.SpecProvider.ClientHelloSpec()
		if err != nil {
			return nil, fmt.Errorf("%w: spec provider: %w", ErrUTLSHandshakeFailed, err)
		}

		uConn := utls.UClient(conn, uCfg, utls.HelloCustom)
		if err := uConn.ApplyPreset(spec); err != nil {
			return nil, fmt.Errorf("%w: apply preset: %w", ErrUTLSHandshakeFailed, err)
		}

		return uConn, nil
	}

	helloID := opts.HelloID
	if helloID == nil {
		helloID = &utls.HelloChrome_Auto
	}

	return utls.UClient(conn, uCfg, *helloID), nil
}

func copyBaseTLSConfig(uCfg *utls.Config, tlsCfg *tls.Config) {
	if tlsCfg == nil {
		return
	}

	uCfg.InsecureSkipVerify = tlsCfg.InsecureSkipVerify
	uCfg.RootCAs = tlsCfg.RootCAs
	uCfg.MinVersion = tlsCfg.MinVersion
	uCfg.MaxVersion = tlsCfg.MaxVersion
	uCfg.CipherSuites = tlsCfg.CipherSuites
	uCfg.VerifyPeerCertificate = tlsCfg.VerifyPeerCertificate

	if len(tlsCfg.CurvePreferences) > 0 {
		uCfg.CurvePreferences = make([]utls.CurveID, len(tlsCfg.CurvePreferences))
		for i, id := range tlsCfg.CurvePreferences {
			uCfg.CurvePreferences[i] = utls.CurveID(id)
		}
	}
}

func applyUTLSPeerVerification(uCfg *utls.Config, opts RTLSOptions, host string) {
	if uCfg.InsecureSkipVerify {
		if len(opts.CertificatePins) > 0 {
			uCfg.VerifyPeerCertificate = func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				return VerifyCertificatePins(host, opts.CertificatePins, rawCerts)
			}
		} else {
			uCfg.VerifyPeerCertificate = func(_ [][]byte, _ [][]*x509.Certificate) error {
				return nil
			}
		}
	} else if len(opts.CertificatePins) > 0 {
		originalVerify := uCfg.VerifyPeerCertificate
		uCfg.VerifyPeerCertificate = func(rawCerts [][]byte, verifiedChains [][]*x509.Certificate) error {
			if err := VerifyCertificatePins(host, opts.CertificatePins, rawCerts); err != nil {
				return err
			}

			if originalVerify != nil {
				return originalVerify(rawCerts, verifiedChains)
			}

			return nil
		}
	}
}

// VerifyCertificatePins validates raw peer X.509 certificates against expected domain SHA-256 pins.
func VerifyCertificatePins(host string, pins map[string][]string, rawCerts [][]byte) error {
	if len(rawCerts) == 0 {
		return ErrNoCertificatesPresented
	}

	var hostPins []string
	for pinDomain, domainPins := range pins {
		if matchHostPattern(host, pinDomain) {
			hostPins = append(hostPins, domainPins...)
		}
	}

	if len(hostPins) == 0 {
		return nil
	}

	expectedHashes := make([][]byte, 0, len(hostPins))
	for _, pin := range hostPins {
		hashBytes, err := parsePin(pin)
		if err != nil {
			return err
		}

		expectedHashes = append(expectedHashes, hashBytes)
	}

	for _, rawCert := range rawCerts {
		cert, err := x509.ParseCertificate(rawCert)
		if err != nil {
			return err
		}

		sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
		for _, expected := range expectedHashes {
			if bytes.Equal(sum[:], expected) {
				return nil
			}
		}
	}

	return ErrCertificatePinning
}

func matchHostPattern(host, pinDomain string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	pinDomain = strings.ToLower(strings.TrimSpace(pinDomain))

	if host == pinDomain {
		return true
	}

	base, isWildcard := strings.CutPrefix(pinDomain, "*.")
	if !isWildcard {
		return false
	}

	if host == base {
		return true
	}

	suffix := "." + base
	if !strings.HasSuffix(host, suffix) {
		return false
	}

	prefix := strings.TrimSuffix(host, suffix)

	return !strings.Contains(prefix, ".")
}

func parsePin(pin string) ([]byte, error) {
	pin = strings.TrimSpace(pin)
	if strings.HasPrefix(strings.ToLower(pin), "sha256/") {
		pin = pin[7:]
	}

	if b, err := base64.StdEncoding.DecodeString(pin); err == nil && len(b) == 32 {
		return b, nil
	}

	if b, err := base64.RawStdEncoding.DecodeString(pin); err == nil && len(b) == 32 {
		return b, nil
	}

	if b, err := hex.DecodeString(pin); err == nil && len(b) == 32 {
		return b, nil
	}

	return nil, ErrInvalidPinFormat
}

// ExtractJA4FromUConn analyzes completed handshakes and extracts pure-Go [ja4.Report] signatures.
func ExtractJA4FromUConn(uConn *utls.UConn) ja4.Report {
	hello := uConn.HandshakeState.Hello

	var extensions, sigAlgorithms []uint16
	if len(hello.Raw) > 0 {
		extensions, sigAlgorithms = ja4.ParseExtensionsFromRaw(hello.Raw)
	}

	sni := hello.ServerName != ""
	fingerprint := ja4.ComputeJA4(
		hello.CipherSuites,
		extensions,
		hello.SupportedVersions,
		sni,
		hello.AlpnProtocols,
		sigAlgorithms,
	)

	sniStr := "i"
	if sni {
		sniStr = "d"
	}

	alpnToken := "00"
	if len(hello.AlpnProtocols) > 0 {
		first := hello.AlpnProtocols[0]
		if len(first) == 2 {
			alpnToken = first
		} else if len(first) > 0 {
			alpnToken = string([]byte{first[0], first[len(first)-1]})
		}
	}

	report := ja4.Report{
		JA4:         fingerprint,
		Protocol:    "t",
		CipherCount: len(ja4.FilterGREASE(hello.CipherSuites)),
		ExtCount:    len(ja4.FilterGREASE(extensions)),
		SNI:         sniStr,
		ALPN:        alpnToken,
	}

	if len(fingerprint) >= 4 {
		report.Version = fingerprint[1:3]
	}

	return report
}

func ensureSNI(extensions []utls.TLSExtension, serverName string) []utls.TLSExtension {
	if serverName == "" {
		return extensions
	}

	found := false
	filtered := make([]utls.TLSExtension, 0, len(extensions))

	for _, ext := range extensions {
		if _, ok := ext.(*utls.SNIExtension); ok {
			filtered = append(filtered, &utls.SNIExtension{ServerName: serverName})
			found = true
		} else {
			filtered = append(filtered, ext)
		}
	}

	if !found {
		filtered = append(filtered, &utls.SNIExtension{ServerName: serverName})
	}

	return filtered
}

func forceALPN(extensions []utls.TLSExtension, protos []string) []utls.TLSExtension {
	found := false
	filtered := make([]utls.TLSExtension, 0, len(extensions))

	for _, ext := range extensions {
		switch e := ext.(type) {
		case *utls.ALPNExtension:
			filtered = append(filtered, &utls.ALPNExtension{AlpnProtocols: protos})
			found = true
		case *utls.ApplicationSettingsExtension:
			if slices.Contains(protos, "h2") {
				filtered = append(filtered, e)
			}
		default:
			filtered = append(filtered, e)
		}
	}

	if !found {
		filtered = append(filtered, &utls.ALPNExtension{AlpnProtocols: protos})
	}

	return filtered
}
