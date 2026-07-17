// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package aoni

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
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
	nextID    int64
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

// EnableInspector enables the traffic inspector dashboard at the specified address (e.g. ":8080").
func (c *Client) EnableInspector(addr string) error {
	inspector := NewTrafficInspector(addr)
	if err := inspector.Start(); err != nil {
		return err
	}

	c.defaults.Inspector = inspector
	c.rebuildChain()

	return nil
}

// Start spins up the local HTTP server in a background goroutine.
func (i *TrafficInspector) Start() error {
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

	lc := net.ListenConfig{}

	ln, err := lc.Listen(context.Background(), "tcp", i.addr)
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

// Stop terminates the inspector web server.
func (i *TrafficInspector) Stop() error {
	if i.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		return i.server.Shutdown(ctx)
	}

	return nil
}

// Capture logs a request/response pair into the inspector and broadcasts it to live clients.
func (i *TrafficInspector) Capture(req *http.Request, resp *http.Response, reqErr error, trace *TraceInfo) {
	if req == nil {
		return
	}

	i.mu.Lock()
	i.nextID++
	id := i.nextID
	i.mu.Unlock()

	capReq := CapturedRequest{
		ID:        id,
		Timestamp: time.Now(),
		Method:    req.Method,
		URL:       req.URL.String(),
	}

	reqHeaders := req.Header

	var respHeaders http.Header
	if resp != nil {
		respHeaders = resp.Header
	}

	var redactMap map[string]bool
	if cfg, ok := req.Context().Value(redactConfigCtxKey{}).(*redactConfig); ok && cfg != nil {
		redactMap = cfg.Headers
	}

	if resp != nil {
		capReq.Status = resp.StatusCode
		prefix := fmt.Sprintf("%d ", resp.StatusCode)
		capReq.StatusText = strings.TrimPrefix(resp.Status, prefix)
		capReq.ResponseSize = resp.ContentLength

		capReq.ResponseHeaders = make(map[string]string)
		for k, v := range respHeaders {
			if len(v) > 0 {
				if redactMap[strings.ToLower(k)] {
					capReq.ResponseHeaders[k] = "[REDACTED]"
				} else {
					capReq.ResponseHeaders[k] = v[0]
				}
			}
		}
	} else if reqErr != nil {
		capReq.StatusText = reqErr.Error()
	}

	capReq.RequestHeaders = make(map[string]string)
	for k, v := range reqHeaders {
		if len(v) > 0 {
			if redactMap[strings.ToLower(k)] {
				capReq.RequestHeaders[k] = "[REDACTED]"
			} else {
				capReq.RequestHeaders[k] = v[0]
			}
		}
	}

	if trace != nil {
		capReq.Duration = trace.Total
		capReq.DurationStr = trace.Total.String()
		capReq.RemoteAddr = trace.RemoteAddr

		capReq.RequestSize = trace.RequestSize
		if capReq.ResponseSize <= 0 {
			capReq.ResponseSize = trace.ResponseSize
		}

		capReq.DNSLookup = trace.DNSLookup
		capReq.TCPConn = trace.TCPConn
		capReq.TLSHandshake = trace.TLSHandshake
		capReq.ServerProcessing = trace.ServerProcessing
		capReq.ContentTransfer = trace.ContentTransfer

		if trace.JA4 != nil {
			capReq.JA4 = trace.JA4.JA4
			switch trace.JA4.Protocol {
			case "t":
				capReq.JA4Protocol = "TLS (TCP)"
			case "q":
				capReq.JA4Protocol = "QUIC (UDP)"
			case "d":
				capReq.JA4Protocol = "DTLS"
			default:
				capReq.JA4Protocol = trace.JA4.Protocol
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

				capReq.JA4Protocol += " " + ver
			}

			switch trace.JA4.SNI {
			case "d":
				capReq.JA4SNI = "Domain Name (Present)"
			case "i":
				capReq.JA4SNI = "IP Address"
			default:
				capReq.JA4SNI = "None / Hidden"
			}
		}
	}

	i.mu.Lock()

	i.requests = append(i.requests, capReq)
	if len(i.requests) > 500 {
		i.requests = i.requests[len(i.requests)-500:]
	}

	i.mu.Unlock()

	jsonData, err := json.Marshal(capReq)
	if err == nil {
		i.broadcast(string(jsonData))
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

func (i *TrafficInspector) dashboardHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(dashboardHTML))
}

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Aoni Traffic Inspector</title>
    <link href="https://fonts.googleapis.com/css2?family=JetBrains+Mono:wght@400;500;700&family=Outfit:wght@400;500;600;700&display=swap" rel="stylesheet">
    <style>
        :root {
            --bg-primary: #070a13;
            --bg-secondary: rgba(13, 20, 38, 0.7);
            --border-color: rgba(255, 255, 255, 0.06);
            --accent-purple: #8b5cf6;
            --accent-blue: #3b82f6;
            --accent-green: #10b981;
            --accent-red: #ef4444;
            --text-primary: #f3f4f6;
            --text-secondary: #9ca3af;
        }

        * {
            box-sizing: border-box;
            margin: 0;
            padding: 0;
        }

        body {
            font-family: 'Outfit', sans-serif;
            background-color: var(--bg-primary);
            color: var(--text-primary);
            height: 100vh;
            display: flex;
            flex-direction: column;
            overflow: hidden;
        }

        header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            padding: 0.75rem 1.5rem;
            background: rgba(11, 17, 33, 0.85);
            backdrop-filter: blur(12px);
            border-bottom: 1px solid var(--border-color);
            z-index: 10;
        }

        .logo-container {
            display: flex;
            align-items: center;
            gap: 0.75rem;
        }

        .logo-icon {
            width: 22px;
            height: 22px;
            background: linear-gradient(135deg, var(--accent-purple), var(--accent-blue));
            border-radius: 6px;
            box-shadow: 0 0 12px rgba(139, 92, 246, 0.4);
        }

        .logo-text {
            font-size: 1.1rem;
            font-weight: 700;
            letter-spacing: 0.5px;
            background: linear-gradient(to right, #fff, #9ca3af);
            -webkit-background-clip: text;
            -webkit-text-fill-color: transparent;
        }

        .status-badge {
            display: flex;
            align-items: center;
            gap: 0.5rem;
            font-size: 0.75rem;
            color: var(--text-secondary);
            background: rgba(255, 255, 255, 0.03);
            padding: 0.25rem 0.6rem;
            border-radius: 12px;
            border: 1px solid var(--border-color);
        }

        .status-dot {
            width: 6px;
            height: 6px;
            background-color: var(--accent-green);
            border-radius: 50%;
            box-shadow: 0 0 6px var(--accent-green);
            animation: pulse 2s infinite;
        }

        @keyframes pulse {
            0% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(16, 185, 129, 0.7); }
            70% { transform: scale(1); box-shadow: 0 0 0 4px rgba(16, 185, 129, 0); }
            100% { transform: scale(0.95); box-shadow: 0 0 0 0 rgba(16, 185, 129, 0); }
        }

        .main-container {
            display: flex;
            flex: 1;
            overflow: hidden;
        }

        .sidebar {
            width: 40%;
            border-right: 1px solid var(--border-color);
            display: flex;
            flex-direction: column;
            background: rgba(8, 12, 24, 0.4);
        }

        .sidebar-header {
            padding: 0.75rem;
            border-bottom: 1px solid var(--border-color);
            display: flex;
            gap: 0.5rem;
        }

        .search-bar {
            flex: 1;
            background: rgba(255, 255, 255, 0.02);
            border: 1px solid var(--border-color);
            border-radius: 6px;
            padding: 0.4rem 0.75rem;
            color: var(--text-primary);
            font-family: inherit;
            font-size: 0.875rem;
            outline: none;
        }

        .search-bar:focus {
            border-color: var(--accent-purple);
        }

        .btn-clear {
            background: rgba(239, 68, 68, 0.08);
            border: 1px solid rgba(239, 68, 68, 0.15);
            color: var(--accent-red);
            border-radius: 6px;
            padding: 0.4rem 0.75rem;
            cursor: pointer;
            font-family: inherit;
            font-size: 0.875rem;
            font-weight: 500;
        }

        .btn-clear:hover {
            background: rgba(239, 68, 68, 0.15);
        }

        .request-list {
            flex: 1;
            overflow-y: auto;
        }

        .request-item {
            display: flex;
            align-items: center;
            padding: 0.75rem;
            border-bottom: 1px solid var(--border-color);
            cursor: pointer;
            gap: 0.75rem;
        }

        .request-item:hover {
            background: rgba(255, 255, 255, 0.01);
        }

        .request-item.active {
            background: rgba(139, 92, 246, 0.06);
            border-left: 2px solid var(--accent-purple);
        }

        .method-badge {
            font-family: 'JetBrains Mono', monospace;
            font-weight: 700;
            font-size: 0.7rem;
            padding: 0.15rem 0.4rem;
            border-radius: 4px;
            min-width: 54px;
            text-align: center;
        }

        .method-GET { background: rgba(59, 130, 246, 0.1); color: var(--accent-blue); }
        .method-POST { background: rgba(16, 185, 129, 0.1); color: var(--accent-green); }
        .method-PUT { background: rgba(245, 158, 11, 0.1); color: #f59e0b; }
        .method-DELETE { background: rgba(239, 68, 68, 0.1); color: var(--accent-red); }

        .status-badge-item {
            font-family: 'JetBrains Mono', monospace;
            font-size: 0.8rem;
            font-weight: 600;
        }

        .status-2xx { color: var(--accent-green); }
        .status-3xx { color: #f59e0b; }
        .status-4xx, .status-5xx { color: var(--accent-red); }

        .url-text {
            flex: 1;
            font-family: 'JetBrains Mono', monospace;
            font-size: 0.75rem;
            white-space: nowrap;
            overflow: hidden;
            text-overflow: ellipsis;
        }

        .time-text {
            font-size: 0.7rem;
            color: var(--text-secondary);
        }

        .detail-panel {
            flex: 1;
            display: flex;
            flex-direction: column;
            background: rgba(8, 12, 24, 0.2);
            overflow-y: auto;
            padding: 1.5rem;
        }

        .empty-state {
            display: flex;
            flex-direction: column;
            align-items: center;
            justify-content: center;
            height: 100%;
            color: var(--text-secondary);
            gap: 1rem;
        }

        .empty-state-icon {
            width: 40px;
            height: 40px;
            opacity: 0.25;
        }

        .section-title {
            font-size: 0.95rem;
            font-weight: 600;
            margin-top: 1.25rem;
            margin-bottom: 0.75rem;
            color: var(--text-primary);
            border-bottom: 1px solid var(--border-color);
            padding-bottom: 0.4rem;
        }

        .meta-grid {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
            gap: 0.75rem;
            margin-bottom: 1rem;
        }

        .meta-card {
            background: var(--bg-secondary);
            border: 1px solid var(--border-color);
            border-radius: 6px;
            padding: 0.75rem;
        }

        .meta-label {
            font-size: 0.7rem;
            color: var(--text-secondary);
            margin-bottom: 0.2rem;
        }

        .meta-value {
            font-family: 'JetBrains Mono', monospace;
            font-size: 0.8rem;
            font-weight: 500;
            word-break: break-all;
        }

        .timeline-container {
            margin-bottom: 1.25rem;
        }

        .timeline-bar {
            display: flex;
            height: 18px;
            border-radius: 9px;
            overflow: hidden;
            background: rgba(255, 255, 255, 0.03);
            margin-bottom: 0.75rem;
        }

        .timeline-segment {
            height: 100%;
            position: relative;
            cursor: pointer;
            transition: opacity 0.2s;
        }

        .timeline-segment:hover {
            opacity: 0.8;
        }

        .segment-dns { background-color: #3b82f6; }
        .segment-tcp { background-color: #8b5cf6; }
        .segment-tls { background-color: #ec4899; }
        .segment-server { background-color: #f59e0b; }
        .segment-transfer { background-color: #10b981; }

        .timeline-legend {
            display: grid;
            grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
            gap: 0.5rem;
        }

        .legend-item {
            display: flex;
            align-items: center;
            gap: 0.4rem;
            font-size: 0.75rem;
        }

        .legend-color {
            width: 10px;
            height: 10px;
            border-radius: 2px;
        }

        .headers-container {
            display: flex;
            flex-direction: column;
            gap: 1.25rem;
        }

        .headers-table {
            width: 100%;
            border-collapse: collapse;
            font-family: 'JetBrains Mono', monospace;
            font-size: 0.75rem;
        }

        .headers-table tr {
            border-bottom: 1px solid var(--border-color);
        }

        .headers-table td {
            padding: 0.4rem 0;
            vertical-align: top;
        }

        .header-name {
            color: var(--accent-purple);
            width: 25%;
            font-weight: 500;
        }

        .header-value {
            color: var(--text-primary);
            word-break: break-all;
        }
    </style>
</head>
<body>
    <header>
        <div class="logo-container">
            <div class="logo-icon"></div>
            <div class="logo-text">AONI TRAFFIC INSPECTOR</div>
        </div>
        <div class="status-badge">
            <div class="status-dot"></div>
            <span>LIVE INTERCEPT ACTIVE</span>
        </div>
    </header>

    <div class="main-container">
        <div class="sidebar">
            <div class="sidebar-header">
                <input type="text" class="search-bar" id="search" placeholder="Filter by URL or method...">
                <button class="btn-clear" id="clearBtn">Clear</button>
            </div>
            <div class="request-list" id="reqList"></div>
        </div>

        <div class="detail-panel" id="detailPanel">
            <div class="empty-state">
                <svg class="empty-state-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <circle cx="12" cy="12" r="10"></circle>
                    <line x1="12" y1="16" x2="12" y2="12"></line>
                    <line x1="12" y1="8" x2="12.01" y2="8"></line>
                </svg>
                <span>Select a request to inspect details</span>
            </div>
        </div>
    </div>

    <script>
        var requests = [];
        var selectedId = null;

        var reqList = document.getElementById('reqList');
        var detailPanel = document.getElementById('detailPanel');
        var searchInput = document.getElementById('search');
        var clearBtn = document.getElementById('clearBtn');

        fetch('/requests')
            .then(function(res) { return res.json(); })
            .then(function(data) {
                requests = data || [];
                renderList();
            });

        var sse = new EventSource('/events');
        sse.onmessage = function(event) {
            var req = JSON.parse(event.data);
            requests.unshift(req);
            renderList();
        };

        searchInput.addEventListener('input', renderList);

        clearBtn.addEventListener('click', function() {
            fetch('/clear', { method: 'POST' }).then(function() {
                requests = [];
                selectedId = null;
                renderList();
                renderDetail();
            });
        });

        function renderList() {
            var query = searchInput.value.toLowerCase();
            var filtered = requests.filter(function(r) { 
                return r.url.toLowerCase().indexOf(query) !== -1 || 
                       r.method.toLowerCase().indexOf(query) !== -1;
            });

            reqList.innerHTML = filtered.map(function(r) {
                var activeClass = r.id === selectedId ? 'active' : '';
                var statusClass = 'status-' + Math.floor(r.status/100) + 'xx';
                return '<div class="request-item ' + activeClass + '" onclick="selectRequest(' + r.id + ')">' +
                    '<span class="method-badge method-' + r.method + '">' + r.method + '</span>' +
                    '<span class="status-badge-item ' + statusClass + '">' + (r.status || 'ERR') + '</span>' +
                    '<span class="url-text">' + escapeHTML(r.url) + '</span>' +
                    '<span class="time-text">' + (r.duration_str || '') + '</span>' +
                '</div>';
            }).join('');
        }

        function selectRequest(id) {
            selectedId = id;
            renderList();
            renderDetail();
        }

        function renderDetail() {
            var r = requests.find(function(req) { return req.id === selectedId; });
            if (!r) {
                detailPanel.innerHTML = 
                    '<div class="empty-state">' +
                        '<svg class="empty-state-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">' +
                            '<circle cx="12" cy="12" r="10"></circle>' +
                            '<line x1="12" y1="16" x2="12" y2="12"></line>' +
                            '<line x1="12" y1="8" x2="12.01" y2="8"></line>' +
                        '</svg>' +
                        '<span>Select a request to inspect details</span>' +
                    '</div>';
                return;
            }

            var dns = formatDuration(r.dns_lookup);
            var tcp = formatDuration(r.tcp_conn);
            var tls = formatDuration(r.tls_handshake);
            var processing = formatDuration(r.server_processing);
            var transfer = formatDuration(r.content_transfer);
            var total = r.duration_str || '0ms';

            var totalNs = r.dns_lookup + r.tcp_conn + r.tls_handshake + r.server_processing + r.content_transfer;
            var pct = function(ns) { 
                return totalNs > 0 ? (ns / totalNs * 100).toFixed(2) + '%' : '0%';
            };

            detailPanel.innerHTML = 
                '<div class="section-title" style="margin-top:0">Request Summary</div>' +
                '<div class="meta-grid">' +
                    '<div class="meta-card" style="grid-column: span 2">' +
                        '<div class="meta-label">Request URL</div>' +
                        '<div class="meta-value">' + escapeHTML(r.url) + '</div>' +
                    '</div>' +
                    '<div class="meta-card">' +
                        '<div class="meta-label">Method</div>' +
                        '<div class="meta-value" style="color: var(--accent-blue)">' + r.method + '</div>' +
                    '</div>' +
                    '<div class="meta-card">' +
                        '<div class="meta-label">Status</div>' +
                        '<div class="meta-value status-' + Math.floor(r.status/100) + 'xx">' + r.status + ' ' + escapeHTML(r.status_text) + '</div>' +
                    '</div>' +
                    '<div class="meta-card">' +
                        '<div class="meta-label">Total Duration</div>' +
                        '<div class="meta-value">' + total + '</div>' +
                    '</div>' +
                    '<div class="meta-card">' +
                        '<div class="meta-label">Remote Address</div>' +
                        '<div class="meta-value">' + escapeHTML(r.remote_addr || 'unknown') + '</div>' +
                    '</div>' +
                '</div>' +
                '<div class="section-title">TLS & Fingerprints</div>' +
                '<div class="meta-grid">' +
                    '<div class="meta-card" style="grid-column: span 2">' +
                        '<div class="meta-label">JA4 Fingerprint</div>' +
                        '<div class="meta-value" style="color: var(--accent-purple); font-weight: 700">' + escapeHTML(r.ja4 || 'Not generated') + '</div>' +
                    '</div>' +
                    '<div class="meta-card">' +
                        '<div class="meta-label">TLS Protocol</div>' +
                        '<div class="meta-value">' + escapeHTML(r.ja4_protocol || 'unknown') + '</div>' +
                    '</div>' +
                    '<div class="meta-card">' +
                        '<div class="meta-label">TLS Server Name (SNI)</div>' +
                        '<div class="meta-value">' + escapeHTML(r.ja4_sni || 'none') + '</div>' +
                    '</div>' +
                '</div>' +
                '<div class="section-title">Network Timings Timeline</div>' +
                '<div class="timeline-container">' +
                    '<div class="timeline-bar">' +
                        '<div class="timeline-segment segment-dns" style="width: ' + pct(r.dns_lookup) + '" title="DNS: ' + dns + '"></div>' +
                        '<div class="timeline-segment segment-tcp" style="width: ' + pct(r.tcp_conn) + '" title="TCP Conn: ' + tcp + '"></div>' +
                        '<div class="timeline-segment segment-tls" style="width: ' + pct(r.tls_handshake) + '" title="TLS: ' + tls + '"></div>' +
                        '<div class="timeline-segment segment-server" style="width: ' + pct(r.server_processing) + '" title="Server Processing: ' + processing + '"></div>' +
                        '<div class="timeline-segment segment-transfer" style="width: ' + pct(r.content_transfer) + '" title="Content Transfer: ' + transfer + '"></div>' +
                    '</div>' +
                    '<div class="timeline-legend">' +
                        '<div class="legend-item"><div class="legend-color segment-dns"></div><span>DNS: ' + dns + '</span></div>' +
                        '<div class="legend-item"><div class="legend-color segment-tcp"></div><span>TCP: ' + tcp + '</span></div>' +
                        '<div class="legend-item"><div class="legend-color segment-tls"></div><span>TLS: ' + tls + '</span></div>' +
                        '<div class="legend-item"><div class="legend-color segment-server"></div><span>Server Wait: ' + processing + '</span></div>' +
                        '<div class="legend-item"><div class="legend-color segment-transfer"></div><span>Transfer: ' + transfer + '</span></div>' +
                    '</div>' +
                '</div>' +
                '<div class="headers-container">' +
                    '<div>' +
                        '<div class="section-title">Request Headers</div>' +
                        '<table class="headers-table">' +
                            Object.entries(r.request_headers || {}).map(function(pair) {
                                return '<tr>' +
                                    '<td class="header-name">' + escapeHTML(pair[0]) + '</td>' +
                                    '<td class="header-value">' + escapeHTML(pair[1]) + '</td>' +
                                '</tr>';
                            }).join('') +
                        '</table>' +
                    '</div>' +
                    '<div>' +
                        '<div class="section-title">Response Headers</div>' +
                        '<table class="headers-table">' +
                            Object.entries(r.response_headers || {}).map(function(pair) {
                                return '<tr>' +
                                    '<td class="header-name">' + escapeHTML(pair[0]) + '</td>' +
                                    '<td class="header-value">' + escapeHTML(pair[1]) + '</td>' +
                                '</tr>';
                            }).join('') +
                        '</table>' +
                    '</div>' +
                '</div>';
        }

        function escapeHTML(str) {
            if (!str) return '';
            return str.replace(/[&<>'"]/g, function(tag) {
                return { '&': '&amp;', '<': '&lt;', '>': '&gt;', "'": '&#39;', '"': '&quot;' }[tag] || tag;
            });
        }

        function formatDuration(ns) {
            if (!ns) return '0.0ms';
            var ms = ns / 1000000;
            return ms.toFixed(1) + 'ms';
        }
    </script>
</body>
</html>
`
