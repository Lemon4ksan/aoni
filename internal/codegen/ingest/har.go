// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ingest

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// HARLog models the top-level container of a W3C HAR 1.2 archive.
type HARLog struct {
	Log struct {
		Entries []HAREntry `json:"entries"`
	} `json:"log"`
}

// HAREntry describes an individual HTTP request-response transaction in a HAR log.
type HAREntry struct {
	Request struct {
		Method      string       `json:"method"`
		URL         string       `json:"url"`
		Headers     []HARNV      `json:"headers"`
		QueryString []HARNV      `json:"queryString"`
		PostData    *HARPostData `json:"postData,omitempty"`
	} `json:"request"`
	Response struct {
		Status  int         `json:"status"`
		Headers []HARNV     `json:"headers"`
		Content *HARContent `json:"content,omitempty"`
	} `json:"response"`
}

// HARNV models a name-value pair for headers or query parameters.
type HARNV struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// HARPostData models the submitted request payload.
type HARPostData struct {
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// HARContent models the received response payload.
type HARContent struct {
	Size     int64  `json:"size"`
	MimeType string `json:"mimeType"`
	Text     string `json:"text"`
}

// HARToOpenAPI transforms one or more recorded W3C HAR 1.2 logs into an OpenAPI 3.0 specification,
// automatically unifying query parameters, status codes, and schema object properties.
func HARToOpenAPI(data []byte) (*openapi3.T, error) {
	var har HARLog
	if err := json.Unmarshal(data, &har); err != nil {
		return nil, fmt.Errorf("parsing HAR JSON: %w", err)
	}

	doc := &openapi3.T{
		OpenAPI: "3.0.3",
		Info: &openapi3.Info{
			Title:       "API Specification (Captured from Traffic)",
			Version:     "1.0.0",
			Description: "Synthesized automatically by Vortex HAR Traffic Ingestion Engine",
		},
		Paths: openapi3.NewPaths(),
		Components: &openapi3.Components{
			Schemas: make(openapi3.Schemas),
		},
	}

	for _, entry := range har.Log.Entries {
		rawURL := entry.Request.URL

		u, err := url.Parse(rawURL)
		if err != nil {
			continue
		}

		cleanPath := u.Path
		if cleanPath == "" {
			cleanPath = "/"
		}

		method := strings.ToUpper(entry.Request.Method)

		// Extract or create PathItem
		pathItem := doc.Paths.Value(cleanPath)
		if pathItem == nil {
			pathItem = &openapi3.PathItem{}
			doc.Paths.Set(cleanPath, pathItem)
		}

		var op *openapi3.Operation
		switch method {
		case "GET":
			op = pathItem.Get
		case "POST":
			op = pathItem.Post
		case "PUT":
			op = pathItem.Put
		case "DELETE":
			op = pathItem.Delete
		case "PATCH":
			op = pathItem.Patch
		}

		if op == nil {
			op = openapi3.NewOperation()
			op.OperationID = DeriveMethodNameFromRoute(method, cleanPath)

			op.Summary = fmt.Sprintf("%s %s", method, cleanPath)
			switch method {
			case "GET":
				pathItem.Get = op
			case "POST":
				pathItem.Post = op
			case "PUT":
				pathItem.Put = op
			case "DELETE":
				pathItem.Delete = op
			case "PATCH":
				pathItem.Patch = op
			}
		}

		// 1. Ingest & Union Query Parameters
		for _, q := range entry.Request.QueryString {
			if q.Name == "" {
				continue
			}

			param := op.Parameters.GetByInAndName(openapi3.ParameterInQuery, q.Name)
			if param == nil {
				param = openapi3.NewQueryParameter(q.Name).WithSchema(openapi3.NewStringSchema())
				op.AddParameter(param)
			}
		}

		// 2. Ingest & Union Request Body
		if entry.Request.PostData != nil && len(entry.Request.PostData.Text) > 0 {
			schema := inferSchemaFromJSON([]byte(entry.Request.PostData.Text))
			if schema != nil {
				if op.RequestBody == nil || op.RequestBody.Value == nil {
					reqBody := openapi3.NewRequestBody().WithContent(openapi3.NewContentWithJSONSchema(schema))
					op.RequestBody = &openapi3.RequestBodyRef{Value: reqBody}
				} else {
					existingContent := op.RequestBody.Value.Content.Get("application/json")
					if existingContent != nil && existingContent.Schema != nil && existingContent.Schema.Value != nil {
						existingContent.Schema.Value = mergeSchemas(existingContent.Schema.Value, schema)
					}
				}
			}
		}

		// 3. Ingest & Union Response Body & Status Codes
		statusStr := strconv.Itoa(entry.Response.Status)
		if entry.Response.Status == 0 {
			statusStr = "200"
		}

		statusInt, _ := strconv.Atoi(statusStr)

		respRef := op.Responses.Value(statusStr)
		if respRef == nil {
			resp := openapi3.NewResponse().WithDescription("Response " + statusStr)
			if entry.Response.Content != nil && len(entry.Response.Content.Text) > 0 {
				schema := inferSchemaFromJSON([]byte(entry.Response.Content.Text))
				if schema != nil {
					resp.WithContent(openapi3.NewContentWithJSONSchema(schema))
				}
			}

			op.AddResponse(statusInt, resp)
		} else if respRef.Value != nil && entry.Response.Content != nil && len(entry.Response.Content.Text) > 0 {
			schema := inferSchemaFromJSON([]byte(entry.Response.Content.Text))
			if schema != nil {
				existingContent := respRef.Value.Content.Get("application/json")
				if existingContent != nil && existingContent.Schema != nil && existingContent.Schema.Value != nil {
					existingContent.Schema.Value = mergeSchemas(existingContent.Schema.Value, schema)
				}
			}
		}
	}

	return doc, nil
}

func mergeSchemas(s1, s2 *openapi3.Schema) *openapi3.Schema {
	if s1 == nil {
		return s2
	}

	if s2 == nil {
		return s1
	}

	if s1.Type != nil && s2.Type != nil {
		if s1.Type.Includes("object") && s2.Type.Includes("object") {
			if s1.Properties == nil {
				s1.Properties = make(openapi3.Schemas)
			}

			for k, v := range s2.Properties {
				if existingProp, exists := s1.Properties[k]; exists && existingProp.Value != nil && v.Value != nil {
					existingProp.Value = mergeSchemas(existingProp.Value, v.Value)
				} else {
					s1.Properties[k] = v
				}
			}

			return s1
		}

		if s1.Type.Includes("array") && s2.Type.Includes("array") && s1.Items != nil && s2.Items != nil &&
			s1.Items.Value != nil &&
			s2.Items.Value != nil {
			s1.Items.Value = mergeSchemas(s1.Items.Value, s2.Items.Value)
			return s1
		}
	}

	return s1
}

func inferSchemaFromJSON(data []byte) *openapi3.Schema {
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return openapi3.NewStringSchema()
	}

	return valueToSchema(raw)
}

func valueToSchema(v any) *openapi3.Schema {
	if v == nil {
		return openapi3.NewStringSchema()
	}

	switch val := v.(type) {
	case bool:
		return openapi3.NewBoolSchema()
	case float64:
		if val == float64(int64(val)) {
			return openapi3.NewInt64Schema()
		}

		return openapi3.NewFloat64Schema()

	case string:
		return openapi3.NewStringSchema()
	case []any:
		arr := openapi3.NewArraySchema()
		if len(val) > 0 {
			arr.Items = &openapi3.SchemaRef{Value: valueToSchema(val[0])}
		} else {
			arr.Items = &openapi3.SchemaRef{Value: openapi3.NewStringSchema()}
		}

		return arr

	case map[string]any:
		obj := openapi3.NewObjectSchema()

		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}

		sort.Strings(keys)

		for _, k := range keys {
			obj.Properties[k] = &openapi3.SchemaRef{Value: valueToSchema(val[k])}
		}

		return obj

	default:
		return openapi3.NewStringSchema()
	}
}
