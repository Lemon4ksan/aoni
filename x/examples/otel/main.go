// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lemon4ksan/aoni"
	"github.com/lemon4ksan/aoni/fluent"
	"github.com/lemon4ksan/aoni/mod"
	"github.com/lemon4ksan/aoni/option"
	"github.com/lemon4ksan/aoni/x/otel"
)

type ItemResponse struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

func main() {
	ctx := context.Background()

	// 1. Initialize Zero-Dependency OpenTelemetry Tracer targeting local OTel Collector / Tempo / Jaeger
	tracer := otel.NewTracer("gman-bot-service",
		otel.WithTracerServiceName("g-man-bot"),
		otel.WithExporter(otel.NewOTLPHTTPExporter("http://localhost:4318")),
	)
	defer tracer.Shutdown(ctx)

	// 2. Configure aoni.Client with OTel Middleware
	client := aoni.NewClient(nil,
		option.WithBaseURL("https://jsonplaceholder.typicode.com"),
		option.WithTimeout(10*time.Second),
		option.WithMiddleware(otel.NewMiddleware(
			otel.WithTracer(tracer),
			otel.WithTraceEvents(true),
			otel.WithCustomAttributes(func(req aoni.Request) []otel.Attribute {
				return []otel.Attribute{
					otel.StringAttr("bot.id", "bot-01"),
					otel.StringAttr("proxy.pool", "datacenter-eu"),
				}
			}),
		)),
	)

	// 3. Start a business trace span
	ctx, span := tracer.Start(ctx, "TradeService.FetchInventoryItem",
		otel.WithSpanKind(otel.SpanKindInternal),
		otel.WithAttributes(
			otel.StringAttr("steam.account_id", "76561198000000000"),
			otel.StringAttr("steam.trade_offer_id", "849201"),
		),
	)
	defer span.End()

	// 4. Outgoing request will automatically become a child span and inject W3C traceparent header
	item, resp, err := fluent.FetchTo[ItemResponse](ctx, client, "GET", "/todos/{id}",
		mod.WithVar("id", 1),
	)
	if err != nil {
		span.RecordError(err)
		fmt.Printf("Error fetching item: %v\n", err)
		return
	}

	span.SetStatus(otel.StatusOk, "item fetched successfully")
	fmt.Printf("Successfully fetched: %s (Status: %d, TraceID: %s)\n",
		item.Title, resp.StatusCode, otel.TraceIDFromContext(ctx))
}
