// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package reverse

import (
	"strings"
	"sync"
	"time"

	"github.com/lemon4ksan/foundation/silicon/rand"
	"golang.org/x/crypto/ssh"
)

// Tunnel holds metadata and connection channels for an active reverse SSH tunnel.
type Tunnel struct {
	ID        string
	Host      string
	Subdomain string
	Port      uint32
	SSHConn   *ssh.ServerConn
	CreatedAt time.Time
}

// Router maintains a thread-safe, in-memory routing table mapping subdomains,
// custom hosts, and TCP ports to active reverse SSH tunnel sessions.
//
// Zero-Disk Overhead:
// All routing registrations are held strictly in RAM using read-write mutexes.
type Router struct {
	rootDomain string
	tunnels    map[string]*Tunnel
	ports      map[uint32]*Tunnel
	mu         sync.RWMutex
}

// NewRouter creates a new in-memory reverse tunnel [Router] bound to rootDomain.
func NewRouter(rootDomain string) *Router {
	return &Router{
		rootDomain: strings.ToLower(strings.TrimSpace(rootDomain)),
		tunnels:    make(map[string]*Tunnel),
		ports:      make(map[uint32]*Tunnel),
	}
}

// Register binds an SSH connection to a subdomain or host, returning the created [Tunnel].
//
// Preconditions:
//   - If requestedHost is empty or "localhost", generates a random 4-character alphanumeric subdomain.
func (r *Router) Register(sshConn *ssh.ServerConn, requestedHost string, port uint32) (*Tunnel, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	host := r.resolveHost(requestedHost)
	if _, exists := r.tunnels[host]; exists {
		return nil, ErrHostAlreadyBound
	}

	subdomain := host
	if r.rootDomain != "" && strings.HasSuffix(host, "."+r.rootDomain) {
		subdomain = strings.TrimSuffix(host, "."+r.rootDomain)
	}

	connID := host
	if sshConn != nil {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					connID = host
				}
			}()

			if addr := sshConn.RemoteAddr(); addr != nil {
				connID = addr.String()
			}
		}()
	}

	tunnel := &Tunnel{
		ID:        connID,
		Host:      host,
		Subdomain: subdomain,
		Port:      port,
		SSHConn:   sshConn,
		CreatedAt: time.Now(),
	}

	r.tunnels[host] = tunnel
	if port > 0 {
		r.ports[port] = tunnel
	}

	return tunnel, nil
}

// Unregister removes a tunnel registration from the routing table.
func (r *Router) Unregister(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	cleanHost := strings.ToLower(strings.TrimSpace(host))
	if tunnel, exists := r.tunnels[cleanHost]; exists {
		delete(r.tunnels, cleanHost)

		if tunnel.Port > 0 {
			delete(r.ports, tunnel.Port)
		}
	}
}

// Lookup retrieves the active [Tunnel] registered for host.
func (r *Router) Lookup(host string) (*Tunnel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cleanHost := strings.ToLower(strings.TrimSpace(host))
	if idx := strings.IndexByte(cleanHost, ':'); idx != -1 {
		cleanHost = cleanHost[:idx]
	}

	t, ok := r.tunnels[cleanHost]

	return t, ok
}

// LookupPort retrieves the active [Tunnel] registered for TCP port.
func (r *Router) LookupPort(port uint32) (*Tunnel, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	t, ok := r.ports[port]

	return t, ok
}

func (r *Router) resolveHost(requested string) string {
	clean := strings.ToLower(strings.TrimSpace(requested))
	if clean != "" && clean != "localhost" && !strings.HasPrefix(clean, "127.") {
		if !strings.Contains(clean, ".") && r.rootDomain != "" {
			return clean + "." + r.rootDomain
		}

		return clean
	}

	sub := generateRandomSubdomain(4)
	if r.rootDomain != "" {
		return sub + "." + r.rootDomain
	}

	return sub
}

const alphaNums = "abcdefghijklmnopqrstuvwxyz0123456789"

func generateRandomSubdomain(length int) string {
	if length <= 0 {
		return ""
	}

	var buf [32]byte

	n := min(length, len(buf))

	for i := 0; i < n; i++ {
		buf[i] = alphaNums[rand.Uint32n(uint32(len(alphaNums)))]
	}

	return string(buf[:n])
}
