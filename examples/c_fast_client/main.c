/*
 * Copyright (c) 2026 Lemon4ksan All rights reserved.
 * Use of this source code is governed by a BSD-style
 * license that can be found in the LICENSE file.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include "../../include/aoni.h"

static double get_time_sec(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec * 1e-9;
}

int main(int argc, char** argv) {
    printf("====================================================\n");
    printf("   aoni C-ABI Zero-Copy Client (libaoni.so)\n");
    printf("   Engine Version: %s\n", aoni_version());
    printf("====================================================\n\n");

    // 1. Initialize fast.Client instance
    aoni_config_t cfg;
    memset(&cfg, 0, sizeof(cfg));
    cfg.max_conns_per_host = 4096;
    cfg.concurrency = 65536;
    cfg.timeout_ms = 10000;
    cfg.browser_profile = AONI_BROWSER_CHROME; // Evasion Profile: Chrome

    aoni_client_t client = aoni_client_create(&cfg);
    if (!client) {
        fprintf(stderr, "[-] Failed to initialize aoni_client\n");
        return 1;
    }
    printf("[+] aoni_client initialized successfully (uTLS Chrome Evasion enabled).\n");

    const char* target_url = "https://httpbin.org/get";
    if (argc > 1) {
        target_url = argv[1];
    }

    // 2. Execute Single Synchronous Zero-Copy Request with Headers inspection
    printf("\n[*] Executing single synchronous request to: %s\n", target_url);

    uint8_t resp_buffer[8192];
    uint8_t resp_headers[4096];
    aoni_task_t single_task;
    memset(&single_task, 0, sizeof(single_task));

    single_task.task_id = 1;
    single_task.method = "GET";
    single_task.method_len = strlen(single_task.method);
    single_task.url = (char*)target_url;
    single_task.url_len = strlen(target_url);

    const char* raw_headers = "User-Agent: libaoni-c-client/1.0\r\nAccept: application/json\r\n";
    single_task.headers_raw = (uint8_t*)raw_headers;
    single_task.headers_len = strlen(raw_headers);

    single_task.resp_buf_ptr = resp_buffer;
    single_task.resp_buf_cap = sizeof(resp_buffer);
    single_task.resp_headers_ptr = resp_headers;
    single_task.resp_headers_cap = sizeof(resp_headers);

    double t0 = get_time_sec();
    int32_t status = aoni_client_do(client, &single_task);
    double t1 = get_time_sec();

    if (status >= 200 && status < 400) {
        printf("[+] Single Request Success! Status: %d | Latency: %.2f ms | Body Bytes: %zu | Header Bytes: %zu\n",
               status, (t1 - t0) * 1000.0, single_task.resp_buf_len, single_task.resp_headers_len);
    } else {
        printf("[-] Single Request Failed: status=%d, err_code=%d\n", status, single_task.error_code);
    }

    // 3. Execute Parallel Batch Request (Amortized FFI over Go Netpoller with Bounded Workers)
    const size_t BATCH_COUNT = 50;
    printf("\n[*] Executing parallel batch of %zu requests (Amortized Netpoller FFI)...\n", BATCH_COUNT);

    aoni_task_t* batch = (aoni_task_t*)calloc(BATCH_COUNT, sizeof(aoni_task_t));
    uint8_t** batch_bufs = (uint8_t**)malloc(BATCH_COUNT * sizeof(uint8_t*));

    for (size_t i = 0; i < BATCH_COUNT; ++i) {
        batch_bufs[i] = (uint8_t*)malloc(4096);

        batch[i].task_id = i + 100;
        batch[i].method = "GET";
        batch[i].method_len = 3;
        batch[i].url = (char*)target_url;
        batch[i].url_len = strlen(target_url);
        batch[i].headers_raw = (uint8_t*)raw_headers;
        batch[i].headers_len = strlen(raw_headers);
        batch[i].resp_buf_ptr = batch_bufs[i];
        batch[i].resp_buf_cap = 4096;
    }

    double batch_t0 = get_time_sec();
    aoni_client_batch_do(client, batch, BATCH_COUNT);
    double batch_t1 = get_time_sec();

    size_t successful = 0;
    for (size_t i = 0; i < BATCH_COUNT; ++i) {
        if (batch[i].status_code >= 200 && batch[i].status_code < 400) {
            successful++;
        }
    }

    double total_time = batch_t1 - batch_t0;
    printf("[+] Batch Complete! %zu/%zu requests succeeded in %.2f ms\n",
           successful, BATCH_COUNT, total_time * 1000.0);
    printf("[+] Effective Throughput: %.2f requests/sec\n", (double)BATCH_COUNT / total_time);

    // Cleanup memory
    for (size_t i = 0; i < BATCH_COUNT; ++i) {
        free(batch_bufs[i]);
    }
    free(batch_bufs);
    free(batch);

    aoni_client_destroy(client);
    printf("\n[+] Cleaned up client. Done!\n");
    return 0;
}
