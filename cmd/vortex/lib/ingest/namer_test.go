// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ingest_test

import (
	"testing"

	"github.com/lemon4ksan/foundation/testkit/require"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ingest"
)

func TestSanitizeMethodName(t *testing.T) {
	tests := []struct {
		name        string
		opID        string
		httpMethod  string
		path        string
		serviceName string
		expected    string
	}{
		{
			name:        "Ugly Swagger IGet verb prefix",
			opID:        "GetIgetCurrenciesV1",
			httpMethod:  "GET",
			path:        "/IGetCurrencies/v1",
			serviceName: "API",
			expected:    "GetCurrencies",
		},
		{
			name:        "Ugly Swagger IGetPricesV4",
			opID:        "GetIgetPricesV4",
			httpMethod:  "GET",
			path:        "/IGetPrices/v4",
			serviceName: "API",
			expected:    "GetPrices",
		},
		{
			name:        "OpenAPI with trailing REST verb",
			opID:        "api_v1_classifieds_search_alerts_get",
			httpMethod:  "GET",
			path:        "/api/v1/classifieds/search/alerts",
			serviceName: "ClassifiedsAPI",
			expected:    "APIV1ClassifiedsSearchAlerts",
		},
		{
			name:        "Service name prefix stripping",
			opID:        "AccountService_GetBalance",
			httpMethod:  "GET",
			path:        "/user/balance",
			serviceName: "AccountService",
			expected:    "GetBalance",
		},
		{
			name:        "Derive from route when opID is empty",
			opID:        "",
			httpMethod:  "GET",
			path:        "/api/v2/items/{id}/pricing",
			serviceName: "CatalogAPI",
			expected:    "GetItemsByIDPricing",
		},
		{
			name:        "Delete route without opID",
			opID:        "",
			httpMethod:  "DELETE",
			path:        "/cart/{id}",
			serviceName: "CartAPI",
			expected:    "DeleteCartByID",
		},
		{
			name:        "PascalCase with common initialisms",
			opID:        "get_sku_http_info",
			httpMethod:  "GET",
			path:        "/sku/info",
			serviceName: "ItemsAPI",
			expected:    "GetSKUHTTPInfo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ingest.SanitizeMethodName(tt.opID, tt.httpMethod, tt.path, tt.serviceName)
			require.Equal(t, tt.expected, result)
		})
	}
}
