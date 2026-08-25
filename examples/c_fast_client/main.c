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
    printf("   aoni Silicon C-ABI Engine (libaoni.so)\n");
    printf("   Engine Version: %s (Off-Heap Enabled)\n", aoni_version());
    printf("====================================================\n\n");

    // 1. Initialize fast.Client instance
    aoni_config_t cfg;
    memset(&cfg, 0, sizeof(cfg));
    cfg.max_conns_per_host = 4096;
    cfg.concurrency = 65536;
    cfg.timeout_ms = 10000;
    cfg.browser_profile = AONI_BROWSER_CHROME; // uTLS Chrome Profile

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

    // ---------------------------------------------------------
    // Mode 1: Synchronous Pre-Allocated Buffer (Caller-Owned)
    // ---------------------------------------------------------
    printf("\n[1] Mode 1: Pre-Allocated Buffer Request to: %s\n", target_url);

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
        printf("[+] Mode 1 Success! Status: %d | Latency: %.2f ms | Body: %zu B | Headers: %zu B\n",
               status, (t1 - t0) * 1000.0, single_task.resp_buf_len, single_task.resp_headers_len);
    } else {
        printf("[-] Mode 1 Failed: status=%d, err_code=%d\n", status, single_task.error_code);
    }

    // ---------------------------------------------------------
    // Mode 2: Dynamic Off-Heap Auto-Allocation (0% Go GC)
    // ---------------------------------------------------------
    printf("\n[2] Mode 2: Dynamic Off-Heap Auto-Allocation (resp_buf_ptr = NULL)...\n");

    aoni_task_t auto_task;
    memset(&auto_task, 0, sizeof(auto_task));

    auto_task.task_id = 2;
    auto_task.url = (char*)target_url;
    auto_task.url_len = strlen(target_url);
    auto_task.resp_buf_ptr = NULL; // Asks libaoni to auto-allocate in OS page memory
    auto_task.resp_buf_cap = 0;

    double t2 = get_time_sec();
    int32_t auto_status = aoni_client_do(client, &auto_task);
    double t3 = get_time_sec();

    if (auto_status >= 200 && auto_status < 400) {
        printf("[+] Mode 2 Success! Status: %d | Latency: %.2f ms | Allocated in Off-Heap: %zu B at %p\n",
               auto_status, (t3 - t2) * 1000.0, auto_task.resp_buf_len, (void*)auto_task.resp_buf_ptr);
        // Safely free the off-heap page buffer
        aoni_task_free(&auto_task);
        printf("[+] Mode 2 Off-Heap Buffer safely released back to OS kernel.\n");
    } else {
        printf("[-] Mode 2 Failed: status=%d, err_code=%d\n", auto_status, auto_task.error_code);
    }

    // ---------------------------------------------------------
    // Mode 3: Off-Heap Arena Batching (Single-cycle O(1) Reset)
    // ---------------------------------------------------------
    const size_t BATCH_COUNT = 50;
    const size_t ARENA_SIZE = 16 * 1024 * 1024; // 16 MB OS Virtual Page
    printf("\n[3] Mode 3: Parallel Batch in 16MB Off-Heap Arena (%zu requests)...\n", BATCH_COUNT);

    aoni_arena_t arena = aoni_arena_create(ARENA_SIZE);
    if (!arena) {
        fprintf(stderr, "[-] Failed to allocate off-heap arena\n");
        return 1;
    }
    printf("[+] Allocated %zu MB Off-Heap Virtual Arena (100%% GC-invisible).\n", ARENA_SIZE / (1024 * 1024));

    aoni_task_t* batch = (aoni_task_t*)calloc(BATCH_COUNT, sizeof(aoni_task_t));

    for (size_t i = 0; i < BATCH_COUNT; ++i) {
        batch[i].task_id = i + 100;
        batch[i].method = "GET";
        batch[i].method_len = 3;
        batch[i].url = (char*)target_url;
        batch[i].url_len = strlen(target_url);
        batch[i].headers_raw = (uint8_t*)raw_headers;
        batch[i].headers_len = strlen(raw_headers);
        batch[i].arena = arena; // Route placement directly into the shared OS arena
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
    printf("[+] Batch Complete! %zu/%zu succeeded in %.2f ms (Throughput: %.2f req/sec)\n",
           successful, BATCH_COUNT, total_time * 1000.0, (double)BATCH_COUNT / total_time);

    // O(1) 1-cycle reset of entire arena without de-allocating OS pages
    aoni_arena_reset(arena);
    printf("[+] Arena reset in 1 CPU cycle.\n");

    // Clean up arena and client
    aoni_arena_destroy(arena);
    free(batch);

    aoni_client_destroy(client);
    printf("\n[+] Cleaned up client & off-heap resources. Done!\n");
    return 0;
}
