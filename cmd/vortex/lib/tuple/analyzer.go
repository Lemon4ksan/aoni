// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tuple

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// TupleIndexReport holds statistical and type analysis for a specific tuple index.
type TupleIndexReport struct {
	Index        int      `json:"index"`
	TotalSamples int      `json:"total_samples"`
	NonNilCount  int      `json:"non_nil_count"`
	Occupancy    float64  `json:"occupancy_rate"`
	InferredType string   `json:"inferred_type"`
	DefaultName  string   `json:"default_name"`
	SampleValues []string `json:"sample_values"`
	IsReserved   bool     `json:"is_reserved"`
}

// TupleAnalysisReport contains multi-sample statistical analysis across all indices of a tuple struct.
type TupleAnalysisReport struct {
	StructName   string             `json:"struct_name"`
	Endpoint     string             `json:"endpoint"`
	TotalSamples int                `json:"total_samples"`
	Indices      []TupleIndexReport `json:"indices"`
}

// AnalyzeTupleSamples computes multi-sample occupancy metrics and detects sparse/reserved indices.
func AnalyzeTupleSamples(
	structName, endpoint string,
	samples [][]any,
	discoveredNames ...map[int]string,
) *TupleAnalysisReport {
	report := &TupleAnalysisReport{
		StructName:   structName,
		Endpoint:     endpoint,
		TotalSamples: len(samples),
	}

	if len(samples) == 0 {
		return report
	}

	var maskNames map[int]string
	if len(discoveredNames) > 0 && discoveredNames[0] != nil {
		maskNames = discoveredNames[0]
	}

	maxLen := 0
	for _, s := range samples {
		if len(s) > maxLen {
			maxLen = len(s)
		}
	}

	for idx := 0; idx < maxLen; idx++ {
		idxReport := TupleIndexReport{
			Index:        idx,
			TotalSamples: len(samples),
		}

		typeCounts := make(map[string]int)

		var rawSamples []any

		for _, s := range samples {
			if idx < len(s) && s[idx] != nil {
				idxReport.NonNilCount++
				tStr := classifyType(s[idx])
				typeCounts[tStr]++

				if len(rawSamples) < 3 {
					rawSamples = append(rawSamples, s[idx])
				}
			}
		}

		if len(samples) > 0 {
			idxReport.Occupancy = float64(idxReport.NonNilCount) / float64(len(samples))
		}

		for _, v := range rawSamples {
			b, _ := json.Marshal(v)

			sVal := string(b)
			if len(sVal) > 45 {
				sVal = sVal[:42] + "..."
			}

			idxReport.SampleValues = append(idxReport.SampleValues, sVal)
		}

		// Check if FieldMask discovered a semantic name for this index
		switch {
		case maskNames != nil && maskNames[idx] != "":
			rawMask := maskNames[idx]
			idxReport.InferredType = resolveDominantType(typeCounts)
			idxReport.DefaultName = SnakeToPascal(rawMask)
			idxReport.SampleValues = append(
				[]string{fmt.Sprintf("[update_mask: %q]", rawMask)},
				idxReport.SampleValues...,
			)

		case idxReport.NonNilCount == 0:
			idxReport.InferredType = "any"
			idxReport.IsReserved = true
			idxReport.DefaultName = fmt.Sprintf("Reserved%d", idx)

		default:
			idxReport.InferredType = resolveDominantType(typeCounts)
			if strings.HasPrefix(idxReport.InferredType, "[]") {
				idxReport.DefaultName = fmt.Sprintf("List%d", idx)
			} else {
				idxReport.DefaultName = fmt.Sprintf("Field%d", idx)
			}
		}

		report.Indices = append(report.Indices, idxReport)
	}

	return report
}

func SnakeToPascal(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == '.'
	})

	var sb strings.Builder
	for _, p := range parts {
		if len(p) > 0 {
			sb.WriteString(strings.ToUpper(p[:1]))
			sb.WriteString(strings.ToLower(p[1:]))
		}
	}

	return sb.String()
}

func classifyType(val any) string {
	switch v := val.(type) {
	case string:
		return "string"

	case bool:
		return "bool"

	case float64:
		if math.Floor(v) == v {
			return "int64"
		}

		return "float64"

	case []any:
		if len(v) == 0 {
			return "[]any"
		}

		elemType := classifyType(v[0])
		switch elemType {
		case "string":
			return "[]string"

		case "int64":
			return "[]int64"

		default:
			return "[]" + elemType
		}

	case map[string]any:
		return "map[string]any"

	default:
		return "any"
	}
}

func resolveDominantType(counts map[string]int) string {
	if len(counts) == 0 {
		return "any"
	}

	bestType := "any"

	bestCount := -1
	for t, c := range counts {
		if c > bestCount {
			bestCount = c
			bestType = t
		}
	}

	return bestType
}

// RenderTable renders a neutral, clean terminal table of the tuple index analysis.
func (r *TupleAnalysisReport) RenderTable() string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "⚡ Vortex Tuple Saliency Analysis (%s across %d sample entries)\n\n",
		r.StructName, r.TotalSamples)

	fmt.Fprintf(&sb, "  %-6s %-12s %-12s %-24s %s\n",
		"INDEX", "OCCUPANCY", "TYPE", "STATUS / NAME", "SAMPLE VALUES FROM TRAFFIC")
	sb.WriteString("  ─────────────────────────────────────────────────────────────────────────────────────────────\n")

	for _, idx := range r.Indices {
		occStr := fmt.Sprintf("%3.0f%% (%2d)", idx.Occupancy*100, idx.NonNilCount)

		sampleDesc := "<always nil / unused>"
		if len(idx.SampleValues) > 0 {
			sampleDesc = strings.Join(idx.SampleValues, ", ")
		}

		nameStr := idx.DefaultName
		if idx.IsReserved {
			nameStr = fmt.Sprintf("Reserved%d (nil)", idx.Index)
		}

		fmt.Fprintf(&sb, "  [%2d]   %-12s %-12s %-24s %s\n",
			idx.Index, occStr, idx.InferredType, nameStr, sampleDesc)
	}

	sb.WriteString(
		"\nTip: Use `vortex ast rename --type=" + r.StructName + " --field=<Index> --to=<Name>` to assign semantic names.\n",
	)

	return sb.String()
}

// ExtractHARResponses extracts all JSON/Protobuf array payloads for specific endpoints from a HAR file.
func ExtractHARResponses(harData []byte, endpointFilter string) map[string][][]any {
	var har struct {
		Log struct {
			Entries []struct {
				Request struct {
					URL string `json:"url"`
				} `json:"request"`
				Response struct {
					Content struct {
						Text string `json:"text"`
					} `json:"content"`
				} `json:"response"`
			} `json:"entries"`
		} `json:"log"`
	}

	if err := json.Unmarshal(harData, &har); err != nil {
		return nil
	}

	results := make(map[string][][]any)

	for _, entry := range har.Log.Entries {
		url := entry.Request.URL
		if endpointFilter != "" && !strings.Contains(strings.ToLower(url), strings.ToLower(endpointFilter)) {
			continue
		}

		text := strings.TrimPrefix(entry.Response.Content.Text, ")]}'\n")

		var parsed any
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			continue
		}

		if rootArr, ok := parsed.([]any); ok && len(rootArr) > 0 {
			var tupleList [][]any

			// Case 1: Wrapped list of tuples (e.g. ListModels: [ [ [model1], [model2] ] ])
			if len(rootArr) == 1 {
				if innerArr, ok := rootArr[0].([]any); ok && len(innerArr) > 0 {
					allSlices := true
					for _, item := range innerArr {
						if _, isSlice := item.([]any); !isSlice {
							allSlices = false
							break
						}
					}

					if allSlices {
						for _, item := range innerArr {
							if tupleElem, ok := item.([]any); ok {
								tupleList = append(tupleList, tupleElem)
							}
						}
					} else {
						// Single tuple wrapped in [ [...] ]
						tupleList = append(tupleList, innerArr)
					}
				}
			} else {
				// Case 2: Single tuple at root level [ val0, val1, val2, ... ]
				tupleList = append(tupleList, rootArr)
			}

			if len(tupleList) > 0 {
				results[url] = append(results[url], tupleList...)
			}
		}
	}

	return results
}

// ExtractFieldMasks scans HAR request payloads for Google protobuf update masks ([ [entity...], [ ["path"] ] ]).
func ExtractFieldMasks(harData []byte) map[string]map[int]string {
	var har struct {
		Log struct {
			Entries []struct {
				Request struct {
					URL      string `json:"url"`
					PostData *struct {
						Text string `json:"text"`
					} `json:"postData"`
				} `json:"request"`
			} `json:"entries"`
		} `json:"log"`
	}

	if err := json.Unmarshal(harData, &har); err != nil {
		return nil
	}

	results := make(map[string]map[int]string)

	for _, entry := range har.Log.Entries {
		if entry.Request.PostData == nil || entry.Request.PostData.Text == "" {
			continue
		}

		text := strings.TrimPrefix(entry.Request.PostData.Text, ")]}'\n")

		var parsed any
		if err := json.Unmarshal([]byte(text), &parsed); err != nil {
			continue
		}

		rootArr, ok := parsed.([]any)
		if !ok || len(rootArr) < 2 {
			continue
		}

		entityArr, isEntity := rootArr[0].([]any)
		if !isEntity {
			continue
		}

		var maskPaths []string
		if maskArr, isMask := rootArr[1].([]any); isMask {
			for _, m := range maskArr {
				if pathStr, isStr := m.(string); isStr {
					maskPaths = append(maskPaths, pathStr)
				} else if subMask, isSub := m.([]any); isSub {
					for _, sub := range subMask {
						if subStr, isSubStr := sub.(string); isSubStr {
							maskPaths = append(maskPaths, subStr)
						}
					}
				}
			}
		}

		if len(maskPaths) == 0 {
			continue
		}

		url := entry.Request.URL
		if results[url] == nil {
			results[url] = make(map[int]string)
		}

		var nonNilIndices []int
		for i, v := range entityArr {
			if v != nil {
				nonNilIndices = append(nonNilIndices, i)
			}
		}

		if len(nonNilIndices) == 1 && len(maskPaths) == 1 {
			results[url][nonNilIndices[0]] = maskPaths[0]
		}
	}

	return results
}
