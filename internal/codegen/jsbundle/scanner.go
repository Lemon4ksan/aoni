// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package jsbundle

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Universal regular expressions for discovering RPC endpoints across standard frameworks.
var (
	reGRPCEndpoint  = regexp.MustCompile(`["'](/\$rpc/[a-zA-Z0-9_.]+(?:/[a-zA-Z0-9_]+)?)["']`)
	reTwirpEndpoint = regexp.MustCompile(`["'](/twirp/[a-zA-Z0-9_.]+(?:/[a-zA-Z0-9_]+)?)["']`)
	reRESTEndpoint  = regexp.MustCompile(
		`["'](/(?:api/v[0-9]|v[0-9](?:alpha|beta|internal)?(?::[a-zA-Z0-9_]+)?)/[a-zA-Z0-9_./-]+)["']`,
	)
	reTRPCEndpoint    = regexp.MustCompile(`["'](/trpc/[a-zA-Z0-9_.]+)["']`)
	reGraphQLEndpoint = regexp.MustCompile(`["'](/graphql(?:/[a-zA-Z0-9_]+)?)["']`)
)

// Universal regular expressions for discovering Protobuf / JSPB field accessors.
var (
	// Standard JSPB getters: MyClass.prototype.getName = function() { return jspb.Message.getField(this, 1) }
	reStandardGetter = regexp.MustCompile(
		`([a-zA-Z0-9_$.]+)\.prototype\.(?:get|is|has)?([a-zA-Z0-9_$]+)\s*=\s*function\s*\(\)\s*\{\s*return\s*jspb\.Message\.getField(?:Wrapper)?\(\s*this,\s*(?:[a-zA-Z0-9_$.]+,\s*)?(\d+)\s*\)`,
	)

	// Standard JSPB setters: MyClass.prototype.setName = function(a) { return jspb.Message.setField(this, 1, a) }
	reStandardSetter = regexp.MustCompile(
		`([a-zA-Z0-9_$.]+)\.prototype\.(?:set)?([a-zA-Z0-9_$]+)\s*=\s*function\s*\([a-zA-Z0-9_$]*\)\s*\{\s*return\s*jspb\.Message\.set(?:Wrapper)?Field\(\s*this,\s*(\d+)`,
	)

	// Minified Closure / JSPB accessors: _.Kw.prototype.yj = _.da(54, function() { return _.Il(this, _.aq, 3) })
	// or _.ry.prototype.yj = _.da(52, function() { return _.Ql(this, 7) })
	reMinifiedClosureGetter = regexp.MustCompile(
		`(?:_\.)?([a-zA-Z0-9_$]+)\.prototype\.([a-zA-Z0-9_$]+)\s*=\s*(?:[a-zA-Z0-9_$.]+\(\d+,\s*)?function\s*\(\)\s*\{\s*return\s*([a-zA-Z0-9_$.]+)\(\s*this,\s*(?:([a-zA-Z0-9_$.]+),\s*)?(\d+)\s*\)`,
	)

	// Protobuf Binary Reader switch cases: case 1: reader.readString()
	reBinaryReaderCase = regexp.MustCompile(`case\s+(\d+):\s*[^;]*?reader\.read([a-zA-Z0-9]+)\(\)`)

	// TypeScript / Babel Enum: Status[Status["ACTIVE"] = 0] = "ACTIVE" or e[e.ACTIVE = 0] = "ACTIVE"
	reTSEnumBabel = regexp.MustCompile(`\[["']([a-zA-Z0-9_$]+)["']\]\s*=\s*(\d+)`)
	reTSEnumShort = regexp.MustCompile(`\.([a-zA-Z0-9_$]+)\s*=\s*(\d+)`)
)

// ScanFile scans a single JavaScript / TypeScript file and returns discovered endpoints and data schemas.
func ScanFile(filePath string) (*ScanResult, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	return ScanBytes(data, filepath.Base(filePath)), nil
}

// ScanFiles scans multiple files matching glob patterns and aggregates their results.
func ScanFiles(patterns []string) (*ScanResult, error) {
	result := NewScanResult()

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}

		for _, match := range matches {
			res, err := ScanFile(match)
			if err == nil && res != nil {
				result.Merge(res)
			}
		}
	}

	return result, nil
}

// ScanBytes analyzes JavaScript byte content in-memory.
func ScanBytes(data []byte, filename string) *ScanResult {
	res := NewScanResult()

	// 1. Scan Endpoints
	scanEndpoints(data, res)

	// 2. Scan Standard & Minified JSPB / Protobuf Accessors
	scanProtobufAccessors(data, filename, res)

	// 3. Scan Binary Deserializer Switch Cases
	scanBinaryDeserializers(data, filename, res)

	// 4. Scan Enums
	scanEnums(data, res)

	return res
}

func scanEndpoints(data []byte, res *ScanResult) {
	seen := make(map[string]bool)

	// gRPC-Web
	for _, m := range reGRPCEndpoint.FindAllSubmatch(data, -1) {
		if len(m) > 1 {
			p := string(m[1])
			if !seen[p] {
				seen[p] = true
				res.Endpoints = append(res.Endpoints, Endpoint{
					Path:       p,
					HTTPMethod: "POST",
					Protocol:   "grpc-web",
				})
			}
		}
	}

	// Twirp
	for _, m := range reTwirpEndpoint.FindAllSubmatch(data, -1) {
		if len(m) > 1 {
			p := string(m[1])
			if !seen[p] {
				seen[p] = true
				res.Endpoints = append(res.Endpoints, Endpoint{
					Path:       p,
					HTTPMethod: "POST",
					Protocol:   "twirp",
				})
			}
		}
	}

	// REST
	for _, m := range reRESTEndpoint.FindAllSubmatch(data, -1) {
		if len(m) > 1 {
			p := string(m[1])
			if !seen[p] {
				seen[p] = true
				res.Endpoints = append(res.Endpoints, Endpoint{
					Path:       p,
					HTTPMethod: "GET",
					Protocol:   "rest",
				})
			}
		}
	}

	// tRPC
	for _, m := range reTRPCEndpoint.FindAllSubmatch(data, -1) {
		if len(m) > 1 {
			p := string(m[1])
			if !seen[p] {
				seen[p] = true
				res.Endpoints = append(res.Endpoints, Endpoint{
					Path:       p,
					HTTPMethod: "POST",
					Protocol:   "trpc",
				})
			}
		}
	}

	// GraphQL
	for _, m := range reGraphQLEndpoint.FindAllSubmatch(data, -1) {
		if len(m) > 1 {
			p := string(m[1])
			if !seen[p] {
				seen[p] = true
				res.Endpoints = append(res.Endpoints, Endpoint{
					Path:       p,
					HTTPMethod: "POST",
					Protocol:   "graphql",
				})
			}
		}
	}
}

func scanProtobufAccessors(data []byte, filename string, res *ScanResult) {
	// Standard Getters
	for _, m := range reStandardGetter.FindAllSubmatch(data, -1) {
		if len(m) > 2 {
			className := strings.TrimPrefix(string(m[1]), "_.")
			if dotIdx := strings.LastIndex(className, "."); dotIdx >= 0 {
				className = className[dotIdx+1:]
			}

			fieldName := string(m[2])
			for _, pfx := range []string{"get", "Get", "is", "Is", "has", "Has"} {
				if strings.HasPrefix(fieldName, pfx) && len(fieldName) > len(pfx) {
					fieldName = fieldName[len(pfx):]
					break
				}
			}

			idx, err := strconv.Atoi(string(m[3]))
			if err == nil {
				msg := getOrCreateMessage(res, className, filename)
				msg.Fields[idx] = FieldDescriptor{
					Index:  idx,
					Name:   fieldName,
					GoType: "string", // will be refined by setter or body sampling
				}
			}
		}
	}

	// Standard Setters
	for _, m := range reStandardSetter.FindAllSubmatch(data, -1) {
		if len(m) > 2 {
			className := strings.TrimPrefix(string(m[1]), "_.")
			if dotIdx := strings.LastIndex(className, "."); dotIdx >= 0 {
				className = className[dotIdx+1:]
			}

			fieldName := string(m[2])
			for _, pfx := range []string{"set", "Set"} {
				if strings.HasPrefix(fieldName, pfx) && len(fieldName) > len(pfx) {
					fieldName = fieldName[len(pfx):]
					break
				}
			}

			idx, err := strconv.Atoi(string(m[3]))
			if err == nil {
				msg := getOrCreateMessage(res, className, filename)
				if existing, ok := msg.Fields[idx]; ok {
					if existing.Name == "" {
						existing.Name = fieldName
						msg.Fields[idx] = existing
					}
				} else {
					msg.Fields[idx] = FieldDescriptor{
						Index:  idx,
						Name:   fieldName,
						GoType: "string",
					}
				}
			}
		}
	}

	// Minified Closure Accessors
	for _, m := range reMinifiedClosureGetter.FindAllSubmatch(data, -1) {
		if len(m) > 5 {
			className := strings.TrimPrefix(string(m[1]), "_.")
			if dotIdx := strings.LastIndex(className, "."); dotIdx >= 0 {
				className = className[dotIdx+1:]
			}

			methodName := string(m[2])
			for _, pfx := range []string{"get", "Get", "set", "Set", "is", "Is", "has", "Has"} {
				if strings.HasPrefix(methodName, pfx) && len(methodName) > len(pfx) {
					methodName = methodName[len(pfx):]
					break
				}
			}

			subType := strings.TrimPrefix(string(m[4]), "_.")
			idxStr := string(m[5])

			idx, err := strconv.Atoi(idxStr)
			if err == nil && idx > 0 {
				msg := getOrCreateMessage(res, className, filename)

				isNested := subType != ""

				goType := "string"
				if isNested {
					goType = subType + "Tuple"
				}

				msg.Fields[idx] = FieldDescriptor{
					Index:      idx,
					Name:       methodName,
					GoType:     goType,
					IsNested:   isNested,
					SubMsgType: subType,
				}
			}
		}
	}
}

func scanBinaryDeserializers(data []byte, filename string, res *ScanResult) {
	for _, m := range reBinaryReaderCase.FindAllSubmatch(data, -1) {
		if len(m) > 2 {
			idx, err := strconv.Atoi(string(m[1]))
			readMethod := string(m[2])

			if err == nil && idx > 0 {
				goType := mapBinaryReaderType(readMethod)
				msg := getOrCreateMessage(res, "BinaryReaderSchema", filename)
				msg.Fields[idx] = FieldDescriptor{
					Index:  idx,
					Name:   "Field" + strconv.Itoa(idx),
					GoType: goType,
				}
			}
		}
	}
}

func scanEnums(data []byte, res *ScanResult) {
	for _, m := range reTSEnumBabel.FindAllSubmatch(data, -1) {
		if len(m) > 2 {
			valName := string(m[1])

			valNum, err := strconv.Atoi(string(m[2]))
			if err == nil {
				enum := getOrCreateEnum(res, "GlobalEnum")
				enum.Values[valNum] = valName
			}
		}
	}

	for _, m := range reTSEnumShort.FindAllSubmatch(data, -1) {
		if len(m) > 2 {
			valName := string(m[1])

			valNum, err := strconv.Atoi(string(m[2]))
			if err == nil {
				enum := getOrCreateEnum(res, "GlobalEnum")
				enum.Values[valNum] = valName
			}
		}
	}
}

func mapBinaryReaderType(readMethod string) string {
	lower := strings.ToLower(readMethod)
	switch {
	case strings.Contains(lower, "string"):
		return "string"
	case strings.Contains(lower, "int64"),
		strings.Contains(lower, "uint64"),
		strings.Contains(lower, "int32"),
		strings.Contains(lower, "uint32"):
		return "int64"
	case strings.Contains(lower, "bool"):
		return "bool"
	case strings.Contains(lower, "float"), strings.Contains(lower, "double"):
		return "float64"
	case strings.Contains(lower, "bytes"):
		return "[]byte"
	default:
		return "any"
	}
}

func getOrCreateMessage(res *ScanResult, id, filename string) *MessageDescriptor {
	if msg, ok := res.Messages[id]; ok {
		return msg
	}

	msg := &MessageDescriptor{
		ID:        id,
		Name:      id,
		Fields:    make(map[int]FieldDescriptor),
		SourceRef: filename,
	}
	res.Messages[id] = msg

	return msg
}

func getOrCreateEnum(res *ScanResult, name string) *EnumDescriptor {
	if enum, ok := res.Enums[name]; ok {
		return enum
	}

	enum := &EnumDescriptor{
		Name:   name,
		Values: make(map[int]string),
	}
	res.Enums[name] = enum

	return enum
}
