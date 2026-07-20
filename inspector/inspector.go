// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package inspector

import (
	"github.com/lemon4ksan/aoni/option"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lemon4ksan/aoni"
)

// CapturedRequest represents a request logged by the traffic inspector.
type CapturedRequest struct {
	ID               int64             `json:"id"`
	Timestamp        time.Time         `json:"timestamp"`
	Method           string            `json:"method"`
	URL              string            `json:"url"`
	Status           int               `json:"status"`
	StatusText       string            `json:"status_text"`
	Duration         time.Duration     `json:"duration"`
	DurationStr      string            `json:"duration_str"`
	RemoteAddr       string            `json:"remote_addr"`
	RequestSize      int64             `json:"request_size"`
	ResponseSize     int64             `json:"response_size"`
	JA4              string            `json:"ja4"`
	JA4Protocol      string            `json:"ja4_protocol"`
	JA4SNI           string            `json:"ja4_sni"`
	H2Settings       string            `json:"h2_settings"`
	RequestHeaders   map[string]string `json:"request_headers"`
	ResponseHeaders  map[string]string `json:"response_headers"`
	DNSLookup        time.Duration     `json:"dns_lookup"`
	TCPConn          time.Duration     `json:"tcp_conn"`
	TLSHandshake     time.Duration     `json:"tls_handshake"`
	ServerProcessing time.Duration     `json:"server_processing"`
	ContentTransfer  time.Duration     `json:"content_transfer"`
}

// TrafficInspector holds requests history and runs the embedded web server.
type TrafficInspector struct {
	mu        sync.RWMutex
	requests  []CapturedRequest
	nextID    atomic.Int64
	clients   map[chan string]bool
	clientsMu sync.Mutex
	server    *http.Server
	addr      string
}

// NewTrafficInspector initializes a TrafficInspector on the specified address.
func NewTrafficInspector(addr string) *TrafficInspector {
	return &TrafficInspector{
		addr:    addr,
		clients: make(map[chan string]bool),
	}
}

// GetRequests returns a copy of all currently captured requests in reverse chronological order.
func (i *TrafficInspector) GetRequests() []CapturedRequest {
	i.mu.RLock()
	defer i.mu.RUnlock()

	reversed := make([]CapturedRequest, len(i.requests))
	for j := range i.requests {
		reversed[j] = i.requests[len(i.requests)-1-j]
	}

	return reversed
}

// Enable enables the traffic inspector dashboard for the client at the specified address.
// It returns a new clone of the client with the inspector registered.
func Enable(c *aoni.Client, addr string) (*aoni.Client, *TrafficInspector, error) {
	inspector := NewTrafficInspector(addr)
	if err := inspector.Serve(); err != nil {
		return nil, nil, err
	}

	return c.With(option.WithInspector(inspector)), inspector, nil
}

// Serve spins up the local HTTP server in a background goroutine.
func (i *TrafficInspector) Serve() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", i.dashboardHandler)
	mux.HandleFunc("/requests", i.requestsHandler)
	mux.HandleFunc("/events", i.sseHandler)
	mux.HandleFunc("/clear", i.clearHandler)

	i.server = &http.Server{
		Addr:              i.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ln, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", i.addr)
	if err != nil {
		return err
	}

	i.addr = ln.Addr().String()
	i.server.Addr = i.addr

	go func() {
		_ = i.server.Serve(ln)
	}()

	return nil
}

// Close terminates the inspector web server.
func (i *TrafficInspector) Close() error {
	if i.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		return i.server.Shutdown(ctx)
	}

	return nil
}

func captureHeaders(reqHeaders http.Header, redactMap map[string]struct{}) map[string]string {
	headers := make(map[string]string, len(reqHeaders))

	for k, v := range reqHeaders {
		if len(v) > 0 {
			if _, ok := redactMap[strings.ToLower(k)]; ok {
				headers[k] = "[REDACTED]"
			} else {
				headers[k] = v[0]
			}
		}
	}

	return headers
}

func getRedactMap(req *http.Request) map[string]struct{} {
	redactMap := make(map[string]struct{})
	if cfg, ok := req.Context().Value(aoni.RedactConfigCtxKey{}).(*aoni.RedactConfig); ok && cfg != nil {
		redactMap = cfg.Headers
	}

	return redactMap
}

// Capture logs a request/response pair into the inspector and broadcasts it to live clients.
func (i *TrafficInspector) Capture(req *http.Request, resp *http.Response, reqErr error, trace *aoni.TraceInfo) {
	if req == nil {
		return
	}

	capReq := CapturedRequest{
		ID:        i.nextID.Add(1),
		Timestamp: time.Now(),
		Method:    req.Method,
		URL:       req.URL.String(),
	}

	redactMap := getRedactMap(req)
	capReq.RequestHeaders = captureHeaders(req.Header, redactMap)

	if resp != nil {
		capReq.Status = resp.StatusCode
		prefix := fmt.Sprintf("%d ", resp.StatusCode)
		capReq.StatusText = strings.TrimPrefix(resp.Status, prefix)
		capReq.ResponseSize = resp.ContentLength
		capReq.ResponseHeaders = captureHeaders(resp.Header, redactMap)
	} else if reqErr != nil {
		capReq.StatusText = reqErr.Error()
	}

	if trace != nil {
		applyTraceToCapturedRequest(&capReq, trace)
	}

	if trace != nil {
		applyTraceToCapturedRequest(&capReq, trace)
	}

	i.saveAndBroadcast(capReq)
}

func (i *TrafficInspector) saveAndBroadcast(req CapturedRequest) {
	i.mu.Lock()

	i.requests = append(i.requests, req)
	if len(i.requests) > 500 {
		i.requests = i.requests[len(i.requests)-500:]
	}

	i.mu.Unlock()

	jsonData, err := json.Marshal(req)
	if err == nil {
		i.broadcast(string(jsonData))
	}
}

func applyTraceToCapturedRequest(req *CapturedRequest, trace *aoni.TraceInfo) {
	req.Duration = trace.Total
	req.DurationStr = trace.Total.String()
	req.RemoteAddr = trace.RemoteAddr

	req.RequestSize = trace.RequestSize
	if req.ResponseSize <= 0 {
		req.ResponseSize = trace.ResponseSize
	}

	req.DNSLookup = trace.DNSLookup
	req.TCPConn = trace.TCPConn
	req.TLSHandshake = trace.TLSHandshake
	req.ServerProcessing = trace.ServerProcessing
	req.ContentTransfer = trace.ContentTransfer

	if trace.JA4 != nil {
		req.JA4 = trace.JA4.JA4
		switch trace.JA4.Protocol {
		case "t":
			req.JA4Protocol = "TLS (TCP)"
		case "q":
			req.JA4Protocol = "QUIC (UDP)"
		case "d":
			req.JA4Protocol = "DTLS"
		default:
			req.JA4Protocol = trace.JA4.Protocol
		}

		if trace.JA4.Version != "" {
			var ver string
			switch trace.JA4.Version {
			case "13":
				ver = "1.3"
			case "12":
				ver = "1.2"
			case "11":
				ver = "1.1"
			case "10":
				ver = "1.0"
			default:
				ver = trace.JA4.Version
			}

			req.JA4Protocol += " " + ver
		}

		switch trace.JA4.SNI {
		case "d":
			req.JA4SNI = "Domain Name (Present)"
		case "i":
			req.JA4SNI = "IP Address"
		default:
			req.JA4SNI = "None / Hidden"
		}
	}
}

func (i *TrafficInspector) broadcast(msg string) {
	i.clientsMu.Lock()
	defer i.clientsMu.Unlock()

	for ch := range i.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (i *TrafficInspector) sseHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan string, 10)

	i.clientsMu.Lock()
	i.clients[ch] = true
	i.clientsMu.Unlock()

	defer func() {
		i.clientsMu.Lock()
		delete(i.clients, ch)
		i.clientsMu.Unlock()
		close(ch)
	}()

	for {
		select {
		case msg := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (i *TrafficInspector) requestsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	i.mu.RLock()
	defer i.mu.RUnlock()

	reversed := make([]CapturedRequest, len(i.requests))
	for j := range i.requests {
		reversed[j] = i.requests[len(i.requests)-1-j]
	}

	_ = json.NewEncoder(w).Encode(reversed)
}

func (i *TrafficInspector) clearHandler(w http.ResponseWriter, r *http.Request) {
	i.mu.Lock()
	i.requests = nil
	i.mu.Unlock()
	w.WriteHeader(http.StatusOK)
}

//go:embed dashboard.html
var dashboardHTML []byte

func (i *TrafficInspector) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(dashboardHTML)
}
