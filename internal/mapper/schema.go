// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package mapper provides zero-allocation reflection schema caching and struct field mapping.
package mapper

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// FieldPlan describes a pre-computed struct field index and tag mapping.
type FieldPlan struct {
	Index    int
	Name     string
	TagValue string
	Omit     bool
}

// SchemaCache caches reflection field plans by reflect.Type to eliminate runtime reflection overhead.
type SchemaCache struct {
	cache sync.Map // map[reflect.Type][]FieldPlan
}

// DefaultSchemaCache is the global default schema plan cache.
var DefaultSchemaCache = &SchemaCache{}

// GetPlans returns or computes the cached [FieldPlan] slice for type t and tagKey.
func (s *SchemaCache) GetPlans(t reflect.Type, tagKey string) []FieldPlan {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return nil
	}

	key := fmt.Sprintf("%s:%s", t.PkgPath()+"."+t.Name(), tagKey)
	if cached, ok := s.cache.Load(key); ok {
		return cached.([]FieldPlan)
	}

	plans := make([]FieldPlan, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		tagVal := field.Tag.Get(tagKey)
		if tagVal == "-" {
			continue
		}

		name := field.Name
		omit := false

		if tagVal != "" {
			parts := strings.Split(tagVal, ",")
			if len(parts) > 0 && parts[0] != "" {
				name = parts[0]
			}

			for _, p := range parts[1:] {
				if p == "omitempty" {
					omit = true
				}
			}
		}

		plans = append(plans, FieldPlan{
			Index:    i,
			Name:     name,
			TagValue: tagVal,
			Omit:     omit,
		})
	}

	s.cache.Store(key, plans)

	return plans
}

// MapStructToMap extracts key-value string mappings from val using cached FieldPlans.
func (s *SchemaCache) MapStructToMap(val any, tagKey string) map[string]string {
	if val == nil {
		return nil
	}

	v := reflect.ValueOf(val)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}

		v = v.Elem()
	}

	if v.Kind() != reflect.Struct {
		return nil
	}

	plans := s.GetPlans(v.Type(), tagKey)
	if len(plans) == 0 {
		return nil
	}

	result := make(map[string]string, len(plans))
	for _, plan := range plans {
		fv := v.Field(plan.Index)
		if plan.Omit && fv.IsZero() {
			continue
		}

		result[plan.Name] = fmt.Sprint(fv.Interface())
	}

	return result
}
