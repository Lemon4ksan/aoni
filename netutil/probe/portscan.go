// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Top20Ports lists the most common TCP service ports for fast, low-overhead probing.
var Top20Ports = []int{
	80, 443, 22, 21, 25, 3389, 110, 445, 139, 143,
	53, 135, 3306, 8080, 1723, 111, 995, 993, 5900, 8443,
}

// OpenPortResult holds metadata, service guesses, and banners discovered for an open port.
type OpenPortResult struct {
	Port    int
	Service string
	Banner  string
	RTT     time.Duration
}

// ScanPorts performs a fast, concurrent TCP connect scan against target, grabbing service banners.
//
// Rootless Execution:
// Uses unprivileged TCP socket connections with a worker pool, avoiding the need for
// raw sockets or elevated system privileges.
func ScanPorts(
	ctx context.Context,
	target string,
	ports []int,
	timeout time.Duration,
	workers int,
) ([]OpenPortResult, error) {
	if len(ports) == 0 {
		ports = Top20Ports
	}

	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	if workers <= 0 {
		workers = 25
	}

	ipAddr, err := net.ResolveIPAddr("ip", target)
	if err != nil {
		return nil, fmt.Errorf("aoni/probe: resolve ip failed: %w", err)
	}

	results := make([]OpenPortResult, 0, len(ports))

	var mu sync.Mutex

	portCh := make(chan int, len(ports))
	for _, p := range ports {
		portCh <- p
	}

	close(portCh)

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)

		go func() {
			defer wg.Done()

			for p := range portCh {
				select {
				case <-ctx.Done():
					return
				default:
				}

				if res, open := probePort(ctx, ipAddr.IP.String(), p, timeout); open {
					mu.Lock()

					results = append(results, res)
					mu.Unlock()
				}
			}
		}()
	}

	wg.Wait()

	return results, nil
}

// probePort attempts a TCP dial on target ip:port and reads service banner on success.
func probePort(ctx context.Context, ip string, port int, timeout time.Duration) (OpenPortResult, bool) {
	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	dialer := &net.Dialer{Timeout: timeout}

	start := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	rtt := time.Since(start)

	if err != nil {
		return OpenPortResult{}, false
	}

	defer conn.Close()

	banner, service := grabBannerAndService(conn, port)

	return OpenPortResult{
		Port:    port,
		Service: service,
		Banner:  banner,
		RTT:     rtt,
	}, true
}

// grabBannerAndService reads initial handshake banner bytes from open connection.
func grabBannerAndService(conn net.Conn, port int) (banner, service string) {
	service = guessServiceByPort(port)

	_ = conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 256)

	n, err := conn.Read(buf)
	if err == nil && n > 0 {
		rawBanner := strings.TrimSpace(string(buf[:n]))
		banner = sanitizeBanner(rawBanner)

		if detected := detectServiceFromBanner(rawBanner); detected != "" {
			service = detected
		}
	}

	return banner, service
}

// guessServiceByPort maps standard IANA port numbers to known service names.
func guessServiceByPort(port int) string {
	switch port {
	case 80, 8080, 8000:
		return "http"
	case 443, 8443:
		return "https"
	case 22, 2222:
		return "ssh"
	case 21:
		return "ftp"
	case 25, 587, 465:
		return "smtp"
	case 3306:
		return "mysql"
	case 5432:
		return "postgresql"
	case 6379:
		return "redis"
	case 27017:
		return "mongodb"
	default:
		return "unknown"
	}
}

// detectServiceFromBanner inspects banner string heuristics for identifiable protocol greetings.
func detectServiceFromBanner(banner string) string {
	lower := strings.ToLower(banner)
	switch {
	case strings.HasPrefix(lower, "ssh-"):
		return "ssh"
	case strings.HasPrefix(lower, "220") || strings.Contains(lower, "ftp"):
		return "ftp"
	case strings.HasPrefix(lower, "http/"), strings.Contains(lower, "html"):
		return "http"
	case strings.Contains(lower, "mysql"):
		return "mysql"
	case strings.Contains(lower, "redis"):
		return "redis"
	default:
		return ""
	}
}

// sanitizeBanner filters non-printable ASCII characters from service greetings.
func sanitizeBanner(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))

	for _, r := range s {
		if r >= 32 && r <= 126 {
			sb.WriteRune(r)
		} else {
			sb.WriteByte(' ')
		}
	}

	return strings.TrimSpace(sb.String())
}
