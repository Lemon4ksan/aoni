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
	"strings"
	"time"

	"github.com/lemon4ksan/foundation/generic"
	"github.com/lemon4ksan/foundation/net/dns/wire"
	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint/grease"
	"github.com/lemon4ksan/aoni/fingerprint/ja4"
	"github.com/lemon4ksan/aoni/netutil"
	"github.com/lemon4ksan/aoni/netutil/cert"
)

var (
	// ErrUTLSHandshakeFailed is returned when the uTLS handshake fails.
	ErrUTLSHandshakeFailed = errors.New("aoni/netdial: uTLS handshake failed")
	// ErrCertificatePinning is returned when the certificate pinning validation fails.
	ErrCertificatePinning = errors.New("aoni/netdial: certificate pinning validation failed")
	// ErrNoCertificatesPresented is returned when no certificates are presented by the peer.
	ErrNoCertificatesPresented = errors.New("aoni/netdial: no certificates presented by peer")
	// ErrInvalidPinFormat is returned when the pin format is invalid.
	ErrInvalidPinFormat = errors.New("aoni/netdial: invalid pin format")
)

// ClientHelloSpecProvider is an interface for providing a utls.ClientHelloSpec.
type ClientHelloSpecProvider interface {
	ClientHelloSpec() (*utls.ClientHelloSpec, error)
}

// RTLSOptions holds the options for configuring a utls.UConn connection.
type RTLSOptions struct {
	HelloID            *utls.ClientHelloID
	SpecProvider       ClientHelloSpecProvider
	DNSResolver        DNSResolver
	SessionCache       utls.ClientSessionCache
	BaseTLSConfig      *tls.Config
	CertificatePins    map[string][]string
	CertCompression    []cert.CompressionAlgorithm
	ECHConfigList      []byte
	ALPNOverride       []string
	JA4Callback        func(ja4.Report)
	InsecureSkipVerify bool
	AutoECH            bool
}

// UConnWrapper wraps a utls.UConn and provides a ConnectionState method that returns the TLS connection state.
type UConnWrapper struct {
	*utls.UConn
}

// ConnectionState returns the TLS connection state of the wrapped UConn.
func (w *UConnWrapper) ConnectionState() tls.ConnectionState {
	uState := w.UConn.ConnectionState()

	return tls.ConnectionState{
		Version:                     uState.Version,
		HandshakeComplete:           true,
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
func HandshakeUTLS(
	ctx context.Context,
	conn net.Conn,
	host string,
	opts RTLSOptions,
) (*UConnWrapper, ja4.Report, error) {
	cleanHost, _ := netutil.CleanHostPort(host)
	sniHost := cleanHost

	if net.ParseIP(cleanHost) != nil {
		sniHost = ""
		if opts.BaseTLSConfig != nil && opts.BaseTLSConfig.ServerName != "" {
			baseSNI, _ := netutil.CleanHostPort(opts.BaseTLSConfig.ServerName)
			if net.ParseIP(baseSNI) == nil {
				sniHost = baseSNI
			}
		}
	}

	nextProtos := []string{"h2", "http/1.1"}
	if len(opts.ALPNOverride) > 0 {
		nextProtos = opts.ALPNOverride
	}

	uCfg := &utls.Config{
		ServerName:   generic.Coalesce(sniHost, cleanHost),
		NextProtos:   nextProtos,
		OmitEmptyPsk: true,
	}

	if net.ParseIP(cleanHost) != nil {
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

	echConfig := opts.ECHConfigList
	if len(echConfig) == 0 && opts.AutoECH && opts.DNSResolver != nil && cleanHost != "" {
		echConfig = resolveECHViaDNS(ctx, opts.DNSResolver, cleanHost)
	}

	if len(echConfig) > 0 {
		uCfg.EncryptedClientHelloConfigList = echConfig
	}

	uConn, err := buildUConn(conn, uCfg, opts)
	if err != nil {
		_ = conn.Close()
		return nil, ja4.Report{}, err
	}

	if len(opts.CertCompression) > 0 {
		applyCertCompression(uConn, opts.CertCompression)
	}

	if err := uConn.BuildHandshakeState(); err != nil {
		_ = conn.Close()
		return nil, ja4.Report{}, fmt.Errorf("%w: build handshake state: %w", ErrUTLSHandshakeFailed, err)
	}

	uConn.Extensions = removeECHExtensions(uConn.Extensions, len(echConfig) > 0)

	if len(opts.ALPNOverride) > 0 {
		for i, ext := range uConn.Extensions {
			if alpnExt, ok := ext.(*utls.ALPNExtension); ok {
				alpnExt.AlpnProtocols = opts.ALPNOverride
				uConn.Extensions[i] = alpnExt
				break
			}
		}
	}

	uConn.ClientHelloID = utls.HelloCustom
	if err := uConn.BuildHandshakeState(); err != nil {
		_ = conn.Close()
		return nil, ja4.Report{}, fmt.Errorf("%w: rebuild handshake state: %w", ErrUTLSHandshakeFailed, err)
	}

	report := ExtractJA4FromUConn(uConn)

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	}

	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	if err := uConn.HandshakeContext(ctx); err != nil {
		_ = conn.Close()
		return nil, ja4.Report{}, fmt.Errorf("%w: %w", ErrUTLSHandshakeFailed, err)
	}

	if opts.JA4Callback != nil {
		opts.JA4Callback(report)
	}

	return &UConnWrapper{UConn: uConn}, report, nil
}

func resolveECHViaDNS(ctx context.Context, resolver DNSResolver, host string) []byte {
	type extendedResolver interface {
		LookupWireRecord(ctx context.Context, host string, qtype uint16) ([]byte, error)
	}

	if ext, ok := resolver.(extendedResolver); ok {
		msg, err := ext.LookupWireRecord(ctx, host, wire.TypeHTTPS)
		if err == nil {
			ech, _ := wire.ExtractECHFromHTTPSResponse(msg, 0)
			return ech
		}
	}

	return nil
}

func applyCertCompression(uConn *utls.UConn, algos []cert.CompressionAlgorithm) {
	if len(algos) == 0 {
		return
	}

	utlsAlgos := make([]utls.CertCompressionAlgo, len(algos))
	for i, a := range algos {
		utlsAlgos[i] = cert.ToUTLS(a)
	}

	for _, ext := range uConn.Extensions {
		if compExt, ok := ext.(*utls.UtlsCompressCertExtension); ok {
			compExt.Algorithms = utlsAlgos
			return
		}
	}

	uConn.Extensions = append(uConn.Extensions, &utls.UtlsCompressCertExtension{
		Algorithms: utlsAlgos,
	})
}

func isECHExtension(ext utls.TLSExtension) bool {
	switch ext.(type) {
	case *utls.GREASEEncryptedClientHelloExtension,
		*utls.UnimplementedECHExtension,
		utls.EncryptedClientHelloExtension:
		return true
	default:
		return false
	}
}

func removeECHExtensions(exts []utls.TLSExtension, keepECH bool) []utls.TLSExtension {
	if len(exts) == 0 {
		return exts
	}

	filtered := make([]utls.TLSExtension, 0, len(exts))
	for _, ext := range exts {
		if ext == nil {
			continue
		}

		if isECHExtension(ext) && !keepECH {
			continue
		}

		filtered = append(filtered, ext)
	}

	return filtered
}

func buildUConn(conn net.Conn, uCfg *utls.Config, opts RTLSOptions) (*utls.UConn, error) {
	if opts.SpecProvider != nil {
		spec, err := opts.SpecProvider.ClientHelloSpec()
		if err == nil && spec != nil {
			uConn := utls.UClient(conn, uCfg, utls.HelloCustom)
			if err := uConn.ApplyPreset(spec); err == nil {
				return uConn, nil
			}
		}
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

// VerifyCertificatePins verifies that the given raw certificates match the specified pins for the given host.
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
	lower := strings.ToLower(pin)

	if strings.HasPrefix(lower, "pin-sha256=") {
		pin = strings.TrimPrefix(pin, "pin-sha256=")
	} else if strings.HasPrefix(lower, "sha256/") {
		pin = pin[7:]
	}

	pin = strings.Trim(pin, "\"")

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

// ExtractJA4FromUConn extracts a JA4 report from a utls.UConn connection.
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
		CipherCount: len(grease.Filter(hello.CipherSuites)),
		ExtCount:    len(grease.Filter(extensions)),
		SNI:         sniStr,
		ALPN:        alpnToken,
	}

	if len(fingerprint) >= 4 {
		report.Version = fingerprint[1:3]
	}

	return report
}
