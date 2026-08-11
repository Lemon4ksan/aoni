// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package digest implements RFC 7616 and RFC 2617 Digest Access Authentication for HTTP transactions.
package digest

import (
	"bytes"
	"crypto/md5" //nolint:gosec
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

var (
	// ErrDigestBadChallenge is returned when the server responds with a malformed Digest WWW-Authenticate challenge.
	ErrDigestBadChallenge = errors.New("aoni/digest: bad challenge")

	// ErrDigestInvalidCharset is returned when the server challenge specifies an unsupported character encoding.
	ErrDigestInvalidCharset = errors.New("aoni/digest: invalid charset")

	// ErrDigestAlgNotSupported is returned when the server requires an unsupported hash algorithm.
	ErrDigestAlgNotSupported = errors.New("aoni/digest: algorithm not supported")

	// ErrDigestQopNotSupported is returned when the server requires an unsupported quality of protection mode.
	ErrDigestQopNotSupported = errors.New("aoni/digest: qop not supported")
)

var digestHashFuncs = map[string]func() hash.Hash{
	"":                 md5.New,
	"MD5":              md5.New,
	"MD5-SESS":         md5.New,
	"SHA-256":          sha256.New,
	"SHA-256-SESS":     sha256.New,
	"SHA-512":          sha512.New,
	"SHA-512-SESS":     sha512.New,
	"SHA-512-256":      sha512.New512_256,
	"SHA-512-256-SESS": sha512.New512_256,
}

const (
	qopAuth    = "auth"
	qopAuthInt = "auth-int"
)

// Transport wraps an [http.RoundTripper] to automatically resolve HTTP 401 Digest Authentication challenges.
type Transport struct {
	Username  string
	Password  string
	Transport http.RoundTripper
}

// RoundTrip executes requests, handling HTTP 401 Digest challenge-response flows transparently.
func (dt *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	tr := dt.Transport
	if tr == nil {
		tr = http.DefaultTransport
	}

	req1 := dt.cloneReq(req, true)

	res, err := tr.RoundTrip(req1)
	if err != nil || res.StatusCode != http.StatusUnauthorized {
		return res, err
	}

	_, _ = io.Copy(io.Discard, res.Body)
	_ = res.Body.Close()

	// RFC 7616 Section 3.7: Iterate over all WWW-Authenticate challenges
	headers := res.Header.Values("WWW-Authenticate")
	if len(headers) == 0 {
		if h := res.Header.Get("WWW-Authenticate"); h != "" {
			headers = []string{h}
		}
	}

	if len(headers) == 0 {
		return nil, ErrDigestBadChallenge
	}

	var (
		cha     *digestChallenge
		lastErr error
	)

	for _, headerVal := range headers {
		c, parseErr := dt.parseChallenge(headerVal)
		if parseErr == nil {
			cha = c
			break
		}

		lastErr = parseErr
	}

	if cha == nil {
		if lastErr != nil {
			return nil, lastErr
		}

		return nil, ErrDigestBadChallenge
	}

	req2 := dt.cloneReq(req, false)

	cred, err := dt.createCredentials(cha, req2)
	if err != nil {
		return nil, err
	}

	auth, err := cred.digest(cha)
	if err != nil {
		return nil, err
	}

	req2.Header.Set("Authorization", auth)

	return tr.RoundTrip(req2)
}

func (dt *Transport) cloneReq(r *http.Request, first bool) *http.Request {
	r1 := r.Clone(r.Context())
	if first {
		r1.Body = http.NoBody
		r1.ContentLength = 0
		r1.GetBody = nil
	}

	return r1
}

func (dt *Transport) parseChallenge(input string) (*digestChallenge, error) {
	const ws = " \n\r\t"

	s := strings.Trim(input, ws)
	if !strings.HasPrefix(s, "Digest ") {
		return nil, ErrDigestBadChallenge
	}

	s = strings.Trim(s[7:], ws)
	c := &digestChallenge{}

	var sb strings.Builder

	key := ""
	quoted := false

	for _, r := range s {
		switch r {
		case '"':
			quoted = !quoted
		case ',':
			if quoted {
				sb.WriteRune(r)
			} else {
				val := strings.Trim(sb.String(), ws)
				sb.Reset()

				if err := c.setValue(key, val); err != nil {
					return nil, err
				}

				key = ""
			}

		case '=':
			if quoted {
				sb.WriteRune(r)
			} else {
				key = strings.Trim(sb.String(), ws)
				sb.Reset()
			}

		default:
			sb.WriteRune(r)
		}
	}

	key = strings.TrimSpace(key)
	if quoted || (key == "" && sb.Len() > 0) {
		return nil, ErrDigestBadChallenge
	}

	if key != "" {
		val := strings.Trim(sb.String(), ws)
		if err := c.setValue(key, val); err != nil {
			return nil, err
		}
	}

	return c, nil
}

func (dt *Transport) createCredentials(cha *digestChallenge, req *http.Request) (*digestCredentials, error) {
	cred := &digestCredentials{
		username:      dt.Username,
		password:      dt.Password,
		uri:           req.URL.RequestURI(),
		method:        req.Method,
		realm:         cha.realm,
		nonce:         cha.nonce,
		nc:            cha.nc,
		algorithm:     cha.algorithm,
		sessAlgorithm: strings.HasSuffix(strings.ToLower(cha.algorithm), "-sess"),
		opaque:        cha.opaque,
		userHash:      cha.userHash,
	}

	if err := cred.parseQop(cha); err != nil {
		return nil, err
	}

	if cred.qop == qopAuthInt {
		if err := dt.prepareBody(req); err != nil {
			return nil, fmt.Errorf("aoni: digest: failed to prepare body: %w", err)
		}

		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("aoni: digest: failed to get body: %w", err)
		}

		h := newHashFunc(cha.algorithm)
		if body != nil && body != http.NoBody {
			defer body.Close()

			if _, err := io.Copy(h, body); err != nil {
				return nil, fmt.Errorf("aoni: digest: failed to hash body: %w", err)
			}
		}

		cred.bodyHash = hex.EncodeToString(h.Sum(nil))
	}

	return cred, nil
}

func (dt *Transport) prepareBody(req *http.Request) error {
	if req.GetBody != nil {
		return nil
	}

	if req.Body == nil || req.Body == http.NoBody {
		req.GetBody = func() (io.ReadCloser, error) {
			return http.NoBody, nil
		}

		return nil
	}

	b, err := io.ReadAll(req.Body)
	if err != nil {
		return fmt.Errorf("aoni: digest: failed to read body: %w", err)
	}

	_ = req.Body.Close()
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(b)), nil
	}
	req.Body = io.NopCloser(bytes.NewReader(b))

	return nil
}

type digestChallenge struct {
	realm     string
	domain    string
	nonce     string
	opaque    string
	stale     string
	algorithm string
	qop       []string
	nc        int
	userHash  string
}

func (dc *digestChallenge) isQopSupported(qop string) bool {
	return slices.Contains(dc.qop, qop)
}

func (dc *digestChallenge) setValue(k, v string) error {
	switch k {
	case "realm":
		dc.realm = v
	case "domain":
		dc.domain = v
	case "nonce":
		dc.nonce = v
	case "opaque":
		dc.opaque = v
	case "stale":
		dc.stale = v
	case "algorithm":
		dc.algorithm = v
	case "qop":
		if v != "" {
			dc.qop = strings.Split(v, ",")
		}
	case "charset":
		if strings.ToUpper(v) != "UTF-8" {
			return ErrDigestInvalidCharset
		}
	case "nc":
		nc, err := strconv.ParseInt(v, 16, 32)
		if err != nil {
			return fmt.Errorf("aoni: digest: invalid nc: %w", err)
		}

		dc.nc = int(nc)

	case "userhash":
		dc.userHash = v
	default:
		return ErrDigestBadChallenge
	}

	return nil
}

type digestCredentials struct {
	username      string
	password      string
	userHash      string
	method        string
	uri           string
	realm         string
	nonce         string
	algorithm     string
	sessAlgorithm bool
	cnonce        string
	opaque        string
	qop           string
	nc            int
	response      string
	bodyHash      string
}

func (dc *digestCredentials) parseQop(cha *digestChallenge) error {
	if len(cha.qop) == 0 {
		return nil
	}

	if cha.isQopSupported(qopAuth) {
		dc.qop = qopAuth
		return nil
	}

	if cha.isQopSupported(qopAuthInt) {
		dc.qop = qopAuthInt
		return nil
	}

	return ErrDigestQopNotSupported
}

func (dc *digestCredentials) h(data string) string {
	h := newHashFunc(dc.algorithm)
	_, _ = h.Write([]byte(data))

	return hex.EncodeToString(h.Sum(nil))
}

func (dc *digestCredentials) digest(cha *digestChallenge) (string, error) {
	normAlg := strings.ToUpper(strings.TrimSpace(dc.algorithm))
	if normAlg == "" {
		normAlg = "MD5"
	}

	if _, ok := digestHashFuncs[normAlg]; !ok {
		return "", ErrDigestAlgNotSupported
	}

	if err := dc.parseQop(cha); err != nil {
		return "", err
	}

	dc.nc++

	b := make([]byte, 16)
	_, _ = io.ReadFull(rand.Reader, b)
	dc.cnonce = hex.EncodeToString(b)

	ha1 := dc.ha1()
	ha2 := dc.ha2()

	var resp string
	switch dc.qop {
	case "":
		resp = fmt.Sprintf("%s:%s:%s", ha1, dc.nonce, ha2)
	case qopAuth, qopAuthInt:
		resp = fmt.Sprintf("%s:%s:%08x:%s:%s:%s", ha1, dc.nonce, dc.nc, dc.cnonce, dc.qop, ha2)
	}

	dc.response = dc.h(resp)

	return "Digest " + dc.String(), nil
}

func (dc *digestCredentials) ha1() string {
	a1 := dc.h(fmt.Sprintf("%s:%s:%s", dc.username, dc.realm, dc.password))
	if dc.sessAlgorithm {
		return dc.h(fmt.Sprintf("%s:%s:%s", a1, dc.nonce, dc.cnonce))
	}

	return a1
}

func (dc *digestCredentials) ha2() string {
	if dc.qop == qopAuthInt {
		return dc.h(fmt.Sprintf("%s:%s:%s", dc.method, dc.uri, dc.bodyHash))
	}

	return dc.h(fmt.Sprintf("%s:%s", dc.method, dc.uri))
}

func (dc *digestCredentials) String() string {
	sl := make([]string, 0, 10)

	// RFC 7616 Section 3.4.4: Compute userhash without mutating the original dc.username
	displayUsername := dc.username
	if dc.userHash == "true" {
		displayUsername = dc.h(fmt.Sprintf("%s:%s", dc.username, dc.realm))
	}

	sl = append(sl, fmt.Sprintf(`username="%s"`, displayUsername))
	sl = append(sl, fmt.Sprintf(`realm="%s"`, dc.realm))
	sl = append(sl, fmt.Sprintf(`nonce="%s"`, dc.nonce))
	sl = append(sl, fmt.Sprintf(`uri="%s"`, dc.uri))

	if dc.algorithm != "" {
		sl = append(sl, "algorithm="+dc.algorithm)
	}

	if dc.opaque != "" {
		sl = append(sl, fmt.Sprintf(`opaque="%s"`, dc.opaque))
	}

	if dc.qop != "" {
		sl = append(sl, "qop="+dc.qop)
		sl = append(sl, fmt.Sprintf("nc=%08x", dc.nc))
		sl = append(sl, fmt.Sprintf(`cnonce="%s"`, dc.cnonce))
	}

	if dc.userHash == "true" {
		sl = append(sl, "userhash="+dc.userHash)
	}

	sl = append(sl, fmt.Sprintf(`response="%s"`, dc.response))

	return strings.Join(sl, ", ")
}

func newHashFunc(algorithm string) hash.Hash {
	normAlg := strings.ToUpper(strings.TrimSpace(algorithm))

	hf := digestHashFuncs[normAlg]
	if hf == nil {
		hf = md5.New
	}

	h := hf()
	h.Reset()

	return h
}
