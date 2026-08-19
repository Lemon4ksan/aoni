# Release Notes

## v0.6.1 (since v0.6.0)

### Bug Fixes & Network Stack Refinements

* **Native uTLS Dialing on Custom HTTPS Ports** (`fast/client.go`, `fast/dial.go`) - Configured `DialTLSContext` in `applyCustomDialer` and `fast.Client.With()`, allowing `fasthttp.Client` to natively route all HTTPS requests across arbitrary non-443 ports (such as `httptest.NewTLSServer` dynamic ports or custom HTTPS endpoints) through uTLS without dropping to unencrypted TCP.

* **RFC 6066 TLS SNI IP Compliance** (`dial.go`, `fast/dial.go`, `netutil/netdial/utls.go`) - Restored strict RFC 6066 Section 3 compliance for TLS Server Name Indication (SNI) by omitting literal IP address strings (e.g. `"184.51.234.105"`) from the `ServerName` field in ClientHello extensions, preventing connection drops on WAFs (Akamai, Cloudflare).

* **RFC 6265 Single-Header Cookie Formatting & `MaxAge` Handling** (`fast/client.go`, `cookie/cookie.go`, `cookie/jar.go`) - Restored single `Cookie:` header field serialization in `applyCookies`, formatting all request cookies into a single semicolon-delimited string (`Cookie: name1=val1; name2=val2`). Added `MaxAge` mapping to `Cookie` struct (`Export`/`Import`), implemented instant deletion for `MaxAge < 0` in `PersistentJar.SetCookies`, and normalized domain names to canonical lowercase (`strings.ToLower`) per RFC 6265 Section 5.1.2 & 5.2.2.

* **HTTP/1.1 `Host` Header Wire Reordering & `Connection: TE` Handling** (`internal/h1/h1.go`, `fast/client.go`) - Mapped the `:authority` pseudo-header to the HTTP/1.1 `Host` header field during wire header reordering, placing `Host:` at position #2 (immediately after the request line) and eliminating 43-second timeouts / WAF drops caused by tail-placed `Host` headers. Added `ensureConnectionTE` in `executeFastHTTP` to append `TE` to the `Connection` header whenever a `TE` header is present in HTTP/1.1 requests per RFC 9112 Section 7.4.

* **Deflate Decompression Bug Fix & RFC 9110 Content Header Scrubbing** (`fast/client.go`) - Fixed `decompressFastResponse` by separating `gzip` and `deflate` stream decompression, configuring `zlib.NewReader` and `flate.NewReader` fallbacks for `deflate` payloads per RFC 9110 Section 8.4.1.2. Refined `applyRedirectMethodAndBody` to purge all representation/content headers (`Content-Type`, `Content-Length`, `Content-Encoding`, `Content-Language`, `Content-Location`, `Digest`) when switching methods to `GET` on 301, 302, and 303 redirects per RFC 9110 Section 15.4.

* **TRACE Method Sensitive Header Stripping** (`fast/client.go`) - Introduced `sanitizeTraceHeaders` to automatically strip sensitive authentication and session headers (`Authorization`, `Proxy-Authorization`, `Cookie`) on `TRACE` requests per RFC 9110 Section 9.3.8.

* **HTTP/2 SETTINGS Frame & Browser Profile Alignment** (`fast/h2engine`, `fast/client.go`) - Aligned `h2engine` default frame size to 16 KB (`defaultDataFrameSize`) and propagated `Fingerprint.H2Settings` from browser profiles (`ChromeSettings`, `FirefoxSettings`) down to `h2engine.Conn`, eliminating H2 SETTINGS fingerprint mismatch drops on WAFs.

* **HTTP/3 Hop-by-Hop Filtering & Reserved H2 SETTINGS Rejection** (`fast/h3engine/qpack.go`, `fast/h3engine/frames.go`, `fast/h3engine/conn.go`) - Implemented `isForbiddenH3Header` to filter out connection-specific headers (`Connection`, `Keep-Alive`, `Proxy-Connection`, `Transfer-Encoding`, `Upgrade`) in HTTP/3 requests per RFC 9114 Section 4.2. Added `DecodeSettings` to reject reserved HTTP/2 SETTINGS IDs (`0x00`, `0x02`, `0x03`, `0x04`, `0x05`) with `H3_SETTINGS_ERROR` (`0x0109`) and enforced `SETTINGS` as the mandatory first frame on control streams (`H3_MISSING_SETTINGS` `0x010a`) per RFC 9114 Section 7.2.4.1 & 6.2.1.

* **HTTP Caching Invalidation & Dynamic TTL Calculation** (`pipeline.go`, `config.go`) - Implemented `invalidateCache` to automatically purge cached `GET` responses upon receiving successful non-error status codes for unsafe methods (`POST`, `PUT`, `DELETE`, `PATCH`) per RFC 9111 Section 4.4. Added `parseFreshnessLifetime` to dynamically derive response TTLs from `Cache-Control` (`max-age`, `s-maxage`) or `Expires` headers, added `CachedAt` timestamps to `CachedResponse`, and generated the mandatory `Age` header when serving responses from cache per RFC 9111 Section 4.2.1 & 5.1.

* **RFC 8187 File Parameter Unescaping & Windows Device Protection** (`netutil/sanitize.go`) - Added `decodeRFC8187` in `ExtractSanitizedFilename` to support percent-encoding unescaping (`url.PathUnescape`), language tags (`UTF-8'en'`), and ISO-8859-1 fallback decoding for `filename*` parameters. Added `isWindowsReservedDeviceName` to block reserved system device names (`CON`, `PRN`, `AUX`, `NUL`, `COM1-9`, `LPT1-9`) and control characters in `SanitizeFileName` per RFC 6266 Section 4.3 & RFC 8187.

* **HTTP Digest Auth Case-Insensitivity & `userhash` Preserving Fix** (`netutil/digest/digest.go`) - Normalized algorithm names to uppercase (`strings.ToUpper`), fixed destructive `dc.username` mutation in `String()` when `userhash=true` (preserving `ha1()` across retries), and added iteration over all `WWW-Authenticate` header lines to select the first supported challenge per RFC 7616 Section 3.4.4 & 3.7.

* **WebSocket Control Frame Validation & SWAR Mask Acceleration** (`realtime/ws/wsconn.go`) - Enforced 125-byte payload limit on control frames (`Ping`, `Pong`, `Close`), validated reserved RSV bits (`0x70`) returning `ErrReservedBitsSet`, and accelerated client XOR frame masking with 64-bit SWAR block processing (`applyFastMask`) per RFC 6455 Section 5.2 & 5.5.

* **HTTP/1.1 Fallback on HTTP/2 Stream Resets** (`fast/client.go`) - Allowed seamless fallback to `executeFastHTTP` (HTTP/1.1 over uTLS) when an HTTP/2 stream or connection is reset by the peer, even when a browser profile (`BrowserID != BrowserNone`) is configured.

* **Redirect Header Preservation & Double-TLS Prevention** (`fast/client.go`) - Applied request modifiers only on the initial request (`redirectsFollowed == 0`) to prevent overwriting `sec-fetch-site` and `Referer` headers on redirect iterations, and restored Double-TLS prevention in `executeFastHTTP`.

* **`sync.Pool` Memory Leak Prevention** (`fast/bridge.go`, `std_adapter.go`) - Wrapped response body streams in `responseBodyCloser` so that invoking `http.Response.Body.Close()` properly triggers `aoni.Response.Close()`, properly recycling pooled `fast.Request` and `fast.Response` objects back into `sync.Pool`.

* **Safe `fasthttp.Client` Cloning & Custom Dialer Preservation** (`fast/client.go`) - Refactored `fast.Client.With()` to clone `fasthttp.Client` fields explicitly, eliminating `copylocks` lint errors on embedded `noCopy` mutexes and preserving custom user/test dialers (`c.engine.Dial`).

* **HTTP Proxy `CONNECT` Tunneling & Direct IP SSRF Guard** (`netutil/netdial/netdial.go`) - Updated `dialHTTPProxy` to pass `Method: "CONNECT"` to `http.ReadResponse` (preventing hangs on bodyless 2xx CONNECT responses) and added direct IP SSRF checks in `DialDirectTCP`.

### Tools & Fingerprint Updates

* **OpenAPI Client Code Generator CLI** (`cmd/openapi`) - Introduced a standalone CLI tool (`cmd/openapi`) for generating type-safe, zero-allocation `aoni` API clients from OpenAPI 2.0 / 3.0 specifications.

* **Updated Browser Profiles & Version Script** (`fingerprint/profiles`, `scripts`) - Updated Chrome (v131) and Firefox (v153) profile User-Agent and Sec-CH-UA strings, and refactored `update-browser-versions.sh` to use an embedded Go fetcher (`scripts/fetch-versions/main.go`) and Chromium Dash API for cross-platform execution without `jq`.

* **Tests & Benchmarks** (`fast/bench_test.go`, `tests`) - Added comprehensive benchmarks (`bench_test.go`) and integration test coverage (`fast_modules_integration_test.go`, `client_compat_test.go`) validating uTLS evasion, anti-DPI, telemetry, and bridge compatibility.
