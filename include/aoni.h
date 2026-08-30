/*
 * Copyright (c) 2026 Lemon4ksan All rights reserved.
 * Use of this source code is governed by a BSD-style
 * license that can be found in the LICENSE file.
 */

#ifndef AONI_H
#define AONI_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/*
 * Error Codes
 */
#define AONI_OK 0
#define AONI_ERR_NETWORK -1
#define AONI_ERR_BUFFER_OVERFLOW -2
#define AONI_ERR_TIMEOUT -3
#define AONI_ERR_INVALID_PARAM -4
#define AONI_ERR_CLIENT_NIL -5
#define AONI_ERR_OUT_OF_MEMORY -6
#define AONI_ERR_STREAM_CLOSED -7

/*
 * Browser Profile Constants
 */
#define AONI_BROWSER_NONE 0
#define AONI_BROWSER_CHROME 1
#define AONI_BROWSER_FIREFOX 2
#define AONI_BROWSER_SAFARI 3

/* Opaque handles */
typedef void *aoni_client_t;
typedef void *aoni_arena_t;
typedef void *aoni_stream_t;

/*
 * Stream Callbacks
 */
typedef void (*aoni_cb_on_open_t)(uint64_t stream_id, int32_t status_code,
                                  void *user_data);
typedef void (*aoni_cb_on_data_t)(uint64_t stream_id, const uint8_t *data,
                                  size_t len, int32_t is_binary,
                                  void *user_data);
typedef void (*aoni_cb_on_close_t)(uint64_t stream_id, int32_t code,
                                   const char *reason, void *user_data);
typedef void (*aoni_cb_on_error_t)(uint64_t stream_id, int32_t err_code,
                                   const char *message, void *user_data);

typedef struct {
  aoni_cb_on_open_t on_open;
  aoni_cb_on_data_t on_data;
  aoni_cb_on_close_t on_close;
  aoni_cb_on_error_t on_error;
} aoni_stream_callbacks_t;

/*
 * Stream Configuration (WebSocket, SSE, Streaming gRPC)
 */
typedef struct {
  uint64_t stream_id; /* User-defined stream identifier */
  const char *url; /* Target URL (e.g. "wss://..." or "https://.../stream") */
  size_t url_len;  /* Length of URL */
  const char *method; /* "GET", "POST" (NULL defaults to "GET") */
  size_t method_len;  /* Length of method */
  const uint8_t
      *headers_raw; /* Custom raw headers ("Sec-WebSocket-Protocol: v1\r\n") */
  size_t headers_len;   /* Length of headers */
  uint8_t is_websocket; /* 1 = WebSocket (RFC 6455 / RFC 8441 H2 CONNECT), 0 =
                           HTTP/SSE stream */
  uint8_t _pad[7];      /* Explicit 64-bit alignment padding */
} aoni_stream_config_t;

/*
 * Task descriptor representing an HTTP request & pre-allocated response slot.
 * All pointers are owned by caller (C/C++). aoni performs zero heap
 * allocations.
 */
typedef struct {
  uint64_t task_id; /* User-defined task identifier */

  /* Request inputs (Read-only by aoni, const-correct) */
  const char *
      method; /* "GET", "POST", "PUT", etc. (NULL or empty defaults to "GET") */
  size_t method_len; /* Length of method string */
  const char *url;   /* Full request URL (e.g. "https://api.target.com/path") */
  size_t url_len;    /* Length of URL string */
  const uint8_t *headers_raw; /* Raw serialized headers ("Header1:
                                 Val1\r\nHeader2: Val2\r\n") */
  size_t headers_len;         /* Length of raw headers buffer */
  const uint8_t *body_ptr;    /* Request payload pointer (NULL if no body) */
  size_t body_len;            /* Length of request body */

  /* Response outputs */
  uint8_t *
      resp_buf_ptr;    /* Response body pointer:
                        *  - If non-NULL on input: aoni copies into caller's
                        * pre-allocated buffer.
                        *  - If NULL on input: aoni auto-allocates via offheap
                        * (free via aoni_task_free).
                        *  - If arena is set: allocated sequentially from arena. */
  size_t resp_buf_cap; /* Capacity of pre-allocated response body buffer */
  size_t resp_buf_len; /* Actual body bytes written (populated by aoni) */

  uint8_t
      *resp_headers_ptr; /* Optional response headers buffer (NULL = ignore) */
  size_t resp_headers_cap; /* Capacity of response headers buffer */
  size_t resp_headers_len; /* Actual header bytes written (populated by aoni) */

  int32_t status_code; /* HTTP response status code (e.g. 200, 404, 500) */
  int32_t error_code;  /* 0 = AONI_OK, <0 = Error Code */

  /* High-Resolution Performance & Diagnostics (0.28 ns silicon monotonic clock)
   */
  uint64_t dns_time_ns;   /* DNS lookup duration in nanoseconds */
  uint64_t tls_time_ns;   /* TLS handshake duration in nanoseconds */
  uint64_t ttfb_ns;       /* Time To First Byte in nanoseconds */
  uint64_t total_time_ns; /* Total request roundtrip time in nanoseconds */

  /* Advanced Memory & Arena placement */
  aoni_arena_t arena; /* Optional Off-Heap Arena for zero-alloc bump placement
                         (NULL = disabled) */
  void *_internal_handle; /* Private internal handle for offheap tracking */
} aoni_task_t;

/*
 * Client configuration parameters
 */
typedef struct {
  uint32_t max_conns_per_host; /* Max keep-alive connections per target host (0
                                  = default 4096) */
  uint32_t
      concurrency; /* Max concurrent in-flight requests (0 = default 65536) */
  uint32_t timeout_ms; /* Global request timeout in milliseconds (0 = default
                          30000) */
  uint8_t browser_profile; /* AONI_BROWSER_NONE, CHROME, FIREFOX, SAFARI */
  uint8_t enable_http2;    /* 1 = Enable H2, 0 = Disable */
  uint8_t enable_http3;    /* 1 = Enable H3, 0 = Disable */
  uint8_t _pad[1]; /* Explicit alignment padding (x86_64 8-byte boundary) */
  const char *proxy_url; /* Optional proxy URL (e.g. "socks5://127.0.0.1:9050",
                            NULL = direct) */
} aoni_config_t;

/*
 * Client Lifecycle
 */
aoni_client_t aoni_client_create(aoni_config_t *config);
void aoni_client_destroy(aoni_client_t client);

/*
 * One-Shot Request Execution
 */
int32_t aoni_client_do(aoni_client_t client, aoni_task_t *task);
void aoni_client_batch_do(aoni_client_t client, aoni_task_t *tasks,
                          size_t count);
int32_t aoni_client_pipeline_do(aoni_client_t client, aoni_task_t *tasks,
                                size_t count);

/*
 * Full-Duplex Stream Transport (WebSockets, SSE, Streaming gRPC)
 */
aoni_stream_t aoni_stream_connect(aoni_client_t client,
                                  aoni_stream_config_t *config,
                                  aoni_stream_callbacks_t *callbacks,
                                  void *user_data);
int32_t aoni_stream_send(aoni_stream_t stream, uint8_t *data, size_t len,
                         int32_t is_binary);
void aoni_stream_close(aoni_stream_t stream, int32_t code, char *reason);

/*
 * Off-Heap Arena & Memory Management (0% Go GC Overhead)
 */
aoni_arena_t aoni_arena_create(size_t size_bytes);
void aoni_arena_reset(aoni_arena_t arena);
void aoni_arena_destroy(aoni_arena_t arena);
void aoni_task_free(aoni_task_t *task);
void aoni_free(void *ptr);

/*
 * Engine Version
 */
char *aoni_version(void);

#ifdef __cplusplus
}
#endif

#endif /* AONI_H */
