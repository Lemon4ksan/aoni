// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// compare-tls-spec compares the project's hardcoded TLS ClientHello specs
// (chrome.Desktop, firefox.Desktop) against utls.HelloChrome_Auto and
// utls.HelloFirefox_Auto and reports any structural differences.
//
// Exit codes:
//
//	0 — specs match
//	1 — differences found (manual review required)
//	2 — internal error
package main

import (
	"fmt"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"

	utls "github.com/refraction-networking/utls"

	"github.com/lemon4ksan/aoni/fingerprint/profiles"
	"github.com/lemon4ksan/aoni/fingerprint/profiles/chrome"
	"github.com/lemon4ksan/aoni/fingerprint/profiles/firefox"
)

func main() {
	exitCode := 0

	autoChrome, err := utls.UTLSIdToSpec(utls.HelloChrome_Auto)
	if err != nil {
		fatalf("cannot resolve HelloChrome_Auto: %v", err)
	}

	projectChrome := specFromVariant("Chrome", chrome.Desktop)

	fmt.Println("=== Chrome TLS ClientHello Comparison ===")
	fmt.Printf("    reference : utls.HelloChrome_Auto\n")
	fmt.Printf("    project   : chrome.Desktop.HelloSpec\n\n")

	if diffs := compareSpecs(autoChrome, *projectChrome); len(diffs) > 0 {
		for _, d := range diffs {
			fmt.Println(" ", d)
		}

		exitCode = 1
	} else {
		fmt.Println("  ✓ No differences found")
	}

	autoFirefox, err := utls.UTLSIdToSpec(utls.HelloFirefox_Auto)
	if err != nil {
		fatalf("cannot resolve HelloFirefox_Auto: %v", err)
	}

	projectFirefox := specFromVariant("Firefox", firefox.Desktop)

	fmt.Println("\n=== Firefox TLS ClientHello Comparison ===")
	fmt.Printf("    reference : utls.HelloFirefox_Auto\n")
	fmt.Printf("    project   : firefox.Desktop.HelloID → UTLSIdToSpec\n\n")

	if diffs := compareSpecs(autoFirefox, *projectFirefox); len(diffs) > 0 {
		for _, d := range diffs {
			fmt.Println(" ", d)
		}

		exitCode = 1
	} else {
		fmt.Println("  ✓ No differences found")
	}

	if exitCode != 0 {
		fmt.Print("\n⚠  Differences detected. Update the HelloSpec in chrome.go / firefox.go\n")
		fmt.Print("   to match the utls auto spec before merging.\n")
	}

	os.Exit(exitCode)
}

// specFromVariant resolves a *utls.ClientHelloSpec from a *profiles.Variant.
// Chrome sets HelloSpec; Firefox sets HelloID.
func specFromVariant(name string, v *profiles.Variant) *utls.ClientHelloSpec {
	if v.HelloSpec != nil {
		return v.HelloSpec
	}

	if v.HelloID != (utls.ClientHelloID{}) {
		spec, err := utls.UTLSIdToSpec(v.HelloID)
		if err != nil {
			fatalf("cannot resolve %s HelloID: %v", name, err)
		}

		return &spec
	}

	fatalf("%s Desktop variant has neither HelloSpec nor HelloID", name)

	return nil
}

type diffCategory string

const (
	catCipher  diffCategory = "cipher  "
	catExt     diffCategory = "ext     "
	catCurve   diffCategory = "curve   "
	catSigAlg  diffCategory = "sigalg  "
	catALPN    diffCategory = "alpn    "
	catVersion diffCategory = "version "
)

func compareSpecs(reference, current utls.ClientHelloSpec) []string {
	var diffs []string //nolint:prealloc

	// Cipher suites (GREASE-filtered, set-comparison)
	refCiphers := filterGREASEU16(reference.CipherSuites)

	curCiphers := filterGREASEU16(current.CipherSuites)
	for _, c := range added16(refCiphers, curCiphers) {
		diffs = append(diffs, diff(catCipher, "ADDED  ", fmt.Sprintf("0x%04x  %s", c, cipherName(c))))
	}

	for _, c := range added16(curCiphers, refCiphers) {
		diffs = append(diffs, diff(catCipher, "REMOVED", fmt.Sprintf("0x%04x  %s", c, cipherName(c))))
	}

	// Extension types (GREASE and session-specific excluded, set-comparison)
	refExts := extTypeSet(reference.Extensions)

	curExts := extTypeSet(current.Extensions)
	for _, e := range addedStr(refExts, curExts) {
		diffs = append(diffs, diff(catExt, "ADDED  ", e))
	}

	for _, e := range addedStr(curExts, refExts) {
		diffs = append(diffs, diff(catExt, "REMOVED", e))
	}

	// Supported curves (GREASE-filtered)
	refCurves := filterGREASEU16(curvesFrom(reference.Extensions))
	curCurves := filterGREASEU16(curvesFrom(current.Extensions))

	for _, c := range added16(refCurves, curCurves) {
		diffs = append(diffs, diff(catCurve, "ADDED  ", fmt.Sprintf("%d  %s", c, curveName(c))))
	}

	for _, c := range added16(curCurves, refCurves) {
		diffs = append(diffs, diff(catCurve, "REMOVED", fmt.Sprintf("%d  %s", c, curveName(c))))
	}

	// Signature algorithms
	refSigs := sigsFrom(reference.Extensions)

	curSigs := sigsFrom(current.Extensions)
	for _, s := range added16(refSigs, curSigs) {
		diffs = append(diffs, diff(catSigAlg, "ADDED  ", fmt.Sprintf("0x%04x  %s", s, sigAlgName(s))))
	}

	for _, s := range added16(curSigs, refSigs) {
		diffs = append(diffs, diff(catSigAlg, "REMOVED", fmt.Sprintf("0x%04x  %s", s, sigAlgName(s))))
	}

	// ALPN protocols
	refALPN := alpnFrom(reference.Extensions)

	curALPN := alpnFrom(current.Extensions)
	for _, a := range addedStr(refALPN, curALPN) {
		diffs = append(diffs, diff(catALPN, "ADDED  ", fmt.Sprintf("%q", a)))
	}

	for _, a := range addedStr(curALPN, refALPN) {
		diffs = append(diffs, diff(catALPN, "REMOVED", fmt.Sprintf("%q", a)))
	}

	// Supported TLS versions
	refVers := versionsFrom(reference.Extensions)

	curVers := versionsFrom(current.Extensions)
	for _, v := range added16(refVers, curVers) {
		diffs = append(diffs, diff(catVersion, "ADDED  ", fmt.Sprintf("0x%04x  %s", v, tlsVersionName(v))))
	}

	for _, v := range added16(curVers, refVers) {
		diffs = append(diffs, diff(catVersion, "REMOVED", fmt.Sprintf("0x%04x  %s", v, tlsVersionName(v))))
	}

	return diffs
}

func diff(cat diffCategory, op, detail string) string {
	return fmt.Sprintf("[%s] %s  %s", cat, op, detail)
}

// extTypeSet returns a set of non-GREASE, non-session extension type names.
func extTypeSet(exts []utls.TLSExtension) []string {
	seen := make(map[string]bool)
	for _, ext := range exts {
		t := reflect.TypeOf(ext).String()
		// Exclude noise: GREASE placeholders and session-specific extensions.
		if strings.Contains(t, "GREASE") ||
			strings.Contains(t, "PreSharedKey") ||
			strings.Contains(t, "SessionTicket") {
			continue
		}

		// Normalise package prefix.
		t = strings.TrimPrefix(t, "*utls.")
		seen[t] = true
	}

	return sortedKeys(seen)
}

func curvesFrom(exts []utls.TLSExtension) []uint16 {
	for _, ext := range exts {
		if sc, ok := ext.(*utls.SupportedCurvesExtension); ok {
			out := make([]uint16, len(sc.Curves))
			for i, c := range sc.Curves {
				out[i] = uint16(c)
			}

			return out
		}
	}

	return nil
}

func sigsFrom(exts []utls.TLSExtension) []uint16 {
	for _, ext := range exts {
		if sa, ok := ext.(*utls.SignatureAlgorithmsExtension); ok {
			out := make([]uint16, len(sa.SupportedSignatureAlgorithms))
			for i, s := range sa.SupportedSignatureAlgorithms {
				out[i] = uint16(s)
			}

			return out
		}
	}

	return nil
}

func alpnFrom(exts []utls.TLSExtension) []string {
	for _, ext := range exts {
		if a, ok := ext.(*utls.ALPNExtension); ok {
			return a.AlpnProtocols
		}
	}

	return nil
}

func versionsFrom(exts []utls.TLSExtension) []uint16 {
	for _, ext := range exts {
		if sv, ok := ext.(*utls.SupportedVersionsExtension); ok {
			return filterGREASEU16(sv.Versions)
		}
	}

	return nil
}

// isGREASE reports whether v is a TLS GREASE value (RFC 8701).
func isGREASE(v uint16) bool {
	lo := v & 0xff
	return lo == v>>8 && lo&0x0f == 0x0a
}

func filterGREASEU16(in []uint16) []uint16 {
	out := make([]uint16, 0, len(in))
	for _, v := range in {
		if !isGREASE(v) {
			out = append(out, v)
		}
	}

	return out
}

// added16 returns elements in 'a' that are not in 'b' (a − b), sorted.
func added16(a, b []uint16) []uint16 {
	bSet := make(map[uint16]bool, len(b))
	for _, v := range b {
		bSet[v] = true
	}

	var out []uint16
	for _, v := range a {
		if !bSet[v] {
			out = append(out, v)
		}
	}

	slices.Sort(out)

	return out
}

// addedStr returns elements in 'a' that are not in 'b' (a − b), sorted.
func addedStr(a, b []string) []string {
	bSet := make(map[string]bool, len(b))
	for _, v := range b {
		bSet[v] = true
	}

	var out []string
	for _, v := range a {
		if !bSet[v] {
			out = append(out, v)
		}
	}

	sort.Strings(out)

	return out
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return keys
}

func cipherName(c uint16) string {
	names := map[uint16]string{
		0x1301: "TLS_AES_128_GCM_SHA256",
		0x1302: "TLS_AES_256_GCM_SHA384",
		0x1303: "TLS_CHACHA20_POLY1305_SHA256",
		0xc02b: "TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
		0xc02f: "TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		0xc02c: "TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
		0xc030: "TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
		0xcca9: "TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305",
		0xcca8: "TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305",
		0xc013: "TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA",
		0xc014: "TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA",
		0x009c: "TLS_RSA_WITH_AES_128_GCM_SHA256",
		0x009d: "TLS_RSA_WITH_AES_256_GCM_SHA384",
		0x002f: "TLS_RSA_WITH_AES_128_CBC_SHA",
		0x0035: "TLS_RSA_WITH_AES_256_CBC_SHA",
	}
	if n, ok := names[c]; ok {
		return n
	}

	return "unknown"
}

func curveName(c uint16) string {
	names := map[uint16]string{
		0x001d: "X25519",
		0x001e: "X448",
		0x0017: "CurveP256",
		0x0018: "CurveP384",
		0x0019: "CurveP521",
		0x4588: "X25519MLKEM768",
		0x11ec: "X25519Kyber768Draft00",
	}
	if n, ok := names[c]; ok {
		return n
	}

	return "unknown"
}

func sigAlgName(s uint16) string {
	names := map[uint16]string{
		0x0403: "ECDSAWithP256AndSHA256",
		0x0804: "PSSWithSHA256",
		0x0401: "PKCS1WithSHA256",
		0x0503: "ECDSAWithP384AndSHA384",
		0x0805: "PSSWithSHA384",
		0x0501: "PKCS1WithSHA384",
		0x0806: "PSSWithSHA512",
		0x0601: "PKCS1WithSHA512",
		0x0201: "PKCS1WithSHA1",
		0x0203: "ECDSAWithSHA1",
	}
	if n, ok := names[s]; ok {
		return n
	}

	return "unknown"
}

func tlsVersionName(v uint16) string {
	names := map[uint16]string{
		0x0304: "TLS 1.3",
		0x0303: "TLS 1.2",
		0x0302: "TLS 1.1",
		0x0301: "TLS 1.0",
	}
	if n, ok := names[v]; ok {
		return n
	}

	return "unknown"
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(2)
}
