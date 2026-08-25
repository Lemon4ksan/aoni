/*
 * Copyright (c) 2026 Lemon4ksan All rights reserved.
 * Use of this source code is governed by a BSD-style
 * license that can be found in the LICENSE file.
 */

#ifndef AONI_H
#define AONI_H

#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

/*
 * Error Codes
 */
#define AONI_OK                  0
#define AONI_ERR_NETWORK        -1
#define AONI_ERR_BUFFER_OVERFLOW -2
#define AONI_ERR_TIMEOUT        -3
#define AONI_ERR_INVALID_PARAM  -4
#define AONI_ERR_CLIENT_NIL     -5

/*
 * Browser Profile Constants
 */
#define AONI_BROWSER_NONE       0
#define AONI_BROWSER_CHROME     1
#define AONI_BROWSER_FIREFOX    2
#define AONI_BROWSER_SAFARI     3

/*
 * Task descriptor representing an HTTP request & pre-allocated response slot.
 * All pointers are owned by caller (C/C++). aoni performs zero heap allocations.
 */
typedef struct {
    uint64_t       task_id;          /* User-defined task identifier */

    /* Request inputs (Read-only by aoni) */
    char*          method;           /* "GET", "POST", "PUT", etc. (NULL or empty defaults to "GET") */
    size_t         method_len;       /* Length of method string */
    char*          url;              /* Full request URL (e.g. "https://api.target.com/path") */
    size_t         url_len;          /* Length of URL string */
    uint8_t*       headers_raw;      /* Raw serialized headers ("Header1: Val1\r\nHeader2: Val2\r\n") */
    size_t         headers_len;      /* Length of raw headers buffer */
    uint8_t*       body_ptr;         /* Request payload pointer (NULL if no body) */
    size_t         body_len;         /* Length of request body */

    /* Response outputs (Written directly into host memory by aoni) */
    uint8_t*       resp_buf_ptr;     /* Pre-allocated response body buffer */
    size_t         resp_buf_cap;     /* Capacity of pre-allocated response body buffer */
    size_t         resp_buf_len;     /* Actual body bytes written (populated by aoni) */
    uint8_t*       resp_headers_ptr; /* Optional pre-allocated response headers buffer (NULL = ignore) */
    size_t         resp_headers_cap; /* Capacity of response headers buffer */
    size_t         resp_headers_len; /* Actual header bytes written (populated by aoni) */
    int32_t        status_code;      /* HTTP response status code (e.g. 200, 404, 500) */
    int32_t        error_code;       /* 0 = AONI_OK, <0 = Error Code */
} aoni_task_t;

/*
 * Client configuration parameters
 */
typedef struct {
    uint32_t    max_conns_per_host; /* Max keep-alive connections per target host (0 = default 4096) */
    uint32_t    concurrency;        /* Max concurrent in-flight requests (0 = default 65536) */
    uint32_t    timeout_ms;         /* Global request timeout in milliseconds (0 = default 30000) */
    uint8_t     browser_profile;    /* AONI_BROWSER_NONE, CHROME, FIREFOX, SAFARI */
    uint8_t     enable_http2;       /* 1 = Enable H2, 0 = Disable */
    uint8_t     enable_http3;       /* 1 = Enable H3, 0 = Disable */
    char*       proxy_url;          /* Optional proxy URL (e.g. "socks5://127.0.0.1:9050", NULL = direct) */
} aoni_config_t;

/* Opaque pointer to an aoni fast.Client instance */
typedef void* aoni_client_t;

/*
 * aoni_client_create initializes a new fast.Client instance with the given configuration.
 * Returns NULL on initialization failure.
 */
aoni_client_t aoni_client_create(aoni_config_t* config);

/*
 * aoni_client_destroy safely tears down connection pools and releases client resources.
 */
void aoni_client_destroy(aoni_client_t client);

/*
 * aoni_client_do executes a single synchronous HTTP request with zero heap allocations.
 * Returns status_code on success (>= 100), or negative error code on failure.
 */
int32_t aoni_client_do(aoni_client_t client, aoni_task_t* task);

/*
 * aoni_client_batch_do processes N requests concurrently across the Go Netpoller
 * within a single FFI call, achieving sub-nanosecond per-request FFI amortization.
 */
void aoni_client_batch_do(aoni_client_t client, aoni_task_t* tasks, size_t count);

/*
 * aoni_version returns the library version string.
 */
char* aoni_version(void);

#ifdef __cplusplus
}
#endif

#endif /* AONI_H */
