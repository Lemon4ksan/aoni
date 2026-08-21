// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package merge

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lemon4ksan/aoni/cmd/vortex/lib/ir"
)

// DeltaKind classifies the nature and impact of a semantic API change.
type DeltaKind string

const (
	DeltaAddMethod    DeltaKind = "ADD_METHOD"
	DeltaModifyMethod DeltaKind = "MODIFY_METHOD"
	DeltaDeprecate    DeltaKind = "DEPRECATE_METHOD"
	DeltaAddStruct    DeltaKind = "ADD_STRUCT"
	DeltaModifyStruct DeltaKind = "MODIFY_STRUCT"
)

// DeltaItem describes a single atomic change detected during semantic reconciliation.
type DeltaItem struct {
	Kind        DeltaKind `json:"kind"`
	Service     string    `json:"service,omitempty"`
	EntityName  string    `json:"entity_name"`
	Description string    `json:"description"`
	IsBreaking  bool      `json:"is_breaking"`
}

// MethodMergePlan defines how a specific method should be updated or added.
type MethodMergePlan struct {
	Service      string        `json:"service"`
	TargetMethod *ir.MethodIR  `json:"target_method"`
	IsNew        bool          `json:"is_new"`
	NewParams    []*ir.ParamIR `json:"new_params,omitempty"`
	DocAppends   []string      `json:"doc_appends,omitempty"`
}

// StructMergePlan defines how a DTO struct should be created or updated with new fields.
type StructMergePlan struct {
	StructName string        `json:"struct_name"`
	IsNew      bool          `json:"is_new"`
	Target     *ir.StructIR  `json:"target"`
	NewFields  []*ir.FieldIR `json:"new_fields,omitempty"`
}

// ReconcileResult contains all computed changes and merge instructions.
type ReconcileResult struct {
	Deltas        []DeltaItem       `json:"deltas"`
	MethodPlans   []MethodMergePlan `json:"method_plans"`
	StructPlans   []StructMergePlan `json:"struct_plans"`
	BreakingCount int               `json:"breaking_count"`
	AdditiveCount int               `json:"additive_count"`
	TargetRoot    *ir.RootIR        `json:"target_root"`
}

// Reconciler executes 3-way (or 2-way) in-memory AST reconciliation.
type Reconciler struct{}

// NewReconciler creates a ready-to-use Reconciler.
func NewReconciler() *Reconciler {
	return &Reconciler{}
}

// Reconcile compares OursIR (working copy) against TheirsIR (proposal branch),
// using BaseIR (common git ancestor) when available.
func (r *Reconciler) Reconcile(base, ours, theirs *ir.RootIR) (*ReconcileResult, error) {
	if ours == nil || theirs == nil {
		return nil, errors.New("ours and theirs IR must not be nil")
	}

	res := &ReconcileResult{
		Deltas:      make([]DeltaItem, 0),
		MethodPlans: make([]MethodMergePlan, 0),
		StructPlans: make([]StructMergePlan, 0),
		TargetRoot:  ours,
	}

	// 1. Reconcile Services & Methods
	oursServices := make(map[string]*ir.ServiceIR, len(ours.Services))
	for _, s := range ours.Services {
		oursServices[s.Name] = s
	}

	for _, theirSvc := range theirs.Services {
		ourSvc, exists := oursServices[theirSvc.Name]
		if !exists {
			// Entirely new service proposed
			for _, m := range theirSvc.Methods {
				res.MethodPlans = append(res.MethodPlans, MethodMergePlan{
					Service:      theirSvc.Name,
					TargetMethod: m,
					IsNew:        true,
				})

				rawPath := ""
				if m.Path != nil {
					rawPath = m.Path.RawTemplate
				}

				res.Deltas = append(res.Deltas, DeltaItem{
					Kind:        DeltaAddMethod,
					Service:     theirSvc.Name,
					EntityName:  m.Name,
					Description: fmt.Sprintf("Added new endpoint %s %s (%s)", m.HTTPMethod, rawPath, m.Name),
					IsBreaking:  false,
				})
				res.AdditiveCount++
			}

			continue
		}

		r.reconcileMethods(ourSvc, theirSvc, res)
	}

	// 2. Reconcile Structs / DTOs
	oursStructs := make(map[string]*ir.StructIR, len(ours.Structs))
	for _, st := range ours.Structs {
		oursStructs[st.Name] = st
	}

	for _, theirSt := range theirs.Structs {
		ourSt, exists := oursStructs[theirSt.Name]
		if !exists {
			// Entirely new DTO struct
			res.StructPlans = append(res.StructPlans, StructMergePlan{
				StructName: theirSt.Name,
				IsNew:      true,
				Target:     theirSt,
			})
			res.Deltas = append(res.Deltas, DeltaItem{
				Kind:        DeltaAddStruct,
				EntityName:  theirSt.Name,
				Description: fmt.Sprintf("Added new DTO struct %s (%d fields)", theirSt.Name, len(theirSt.Fields)),
				IsBreaking:  false,
			})
			res.AdditiveCount++

			continue
		}

		r.reconcileStructFields(ourSt, theirSt, res)
	}

	return res, nil
}

func (r *Reconciler) reconcileMethods(ourSvc, theirSvc *ir.ServiceIR, res *ReconcileResult) {
	ourMethodsByRoute := make(map[string]*ir.MethodIR)
	ourMethodsByName := make(map[string]*ir.MethodIR)

	for _, m := range ourSvc.Methods {
		rawPath := ""
		if m.Path != nil {
			rawPath = m.Path.RawTemplate
		}

		key := fmt.Sprintf("%s:%s", strings.ToUpper(m.HTTPMethod), normalizeRoute(rawPath))
		ourMethodsByRoute[key] = m
		ourMethodsByName[m.Name] = m
	}

	for _, theirMethod := range theirSvc.Methods {
		theirRawPath := ""
		if theirMethod.Path != nil {
			theirRawPath = theirMethod.Path.RawTemplate
		}

		theirKey := fmt.Sprintf("%s:%s", strings.ToUpper(theirMethod.HTTPMethod), normalizeRoute(theirRawPath))

		ourMethod, exists := ourMethodsByRoute[theirKey]
		if !exists {
			ourMethod, exists = ourMethodsByName[theirMethod.Name]
		}

		if !exists {
			// New method in existing service
			res.MethodPlans = append(res.MethodPlans, MethodMergePlan{
				Service:      ourSvc.Name,
				TargetMethod: theirMethod,
				IsNew:        true,
			})
			res.Deltas = append(res.Deltas, DeltaItem{
				Kind:       DeltaAddMethod,
				Service:    ourSvc.Name,
				EntityName: theirMethod.Name,
				Description: fmt.Sprintf(
					"Added method %s (%s %s)",
					theirMethod.Name,
					theirMethod.HTTPMethod,
					theirRawPath,
				),
				IsBreaking: false,
			})
			res.AdditiveCount++

			continue
		}

		// Method exists: check for additive parameter deltas
		ourParams := make(map[string]bool)
		for _, p := range ourMethod.Params {
			ourParams[strings.ToLower(p.GoName)] = true
			if p.WireKey != "" {
				ourParams[strings.ToLower(p.WireKey)] = true
			}
		}

		var newParams []*ir.ParamIR
		for _, tp := range theirMethod.Params {
			nameMatched := ourParams[strings.ToLower(tp.GoName)]

			wireMatched := tp.WireKey != "" && ourParams[strings.ToLower(tp.WireKey)]
			if !nameMatched && !wireMatched {
				newParams = append(newParams, tp)
			}
		}

		if len(newParams) > 0 {
			res.MethodPlans = append(res.MethodPlans, MethodMergePlan{
				Service:      ourSvc.Name,
				TargetMethod: ourMethod,
				IsNew:        false,
				NewParams:    newParams,
			})
			for _, np := range newParams {
				res.Deltas = append(res.Deltas, DeltaItem{
					Kind:        DeltaModifyMethod,
					Service:     ourSvc.Name,
					EntityName:  fmt.Sprintf("%s.%s", ourSvc.Name, ourMethod.Name),
					Description: fmt.Sprintf("Added param %q (%s)", np.GoName, np.GoType.Name),
					IsBreaking:  false,
				})
				res.AdditiveCount++
			}
		}
	}
}

func (r *Reconciler) reconcileStructFields(ourSt, theirSt *ir.StructIR, res *ReconcileResult) {
	ourFieldsByName := make(map[string]bool)
	ourFieldsByWire := make(map[string]bool)

	for _, f := range ourSt.Fields {
		ourFieldsByName[strings.ToLower(f.GoName)] = true
		if f.WireName != "" {
			ourFieldsByWire[strings.ToLower(f.WireName)] = true
		}
	}

	var newFields []*ir.FieldIR
	for _, tf := range theirSt.Fields {
		matched := ourFieldsByName[strings.ToLower(tf.GoName)]
		if !matched && tf.WireName != "" && ourFieldsByWire[strings.ToLower(tf.WireName)] {
			matched = true
		}

		if !matched {
			newFields = append(newFields, tf)
		}
	}

	if len(newFields) > 0 {
		res.StructPlans = append(res.StructPlans, StructMergePlan{
			StructName: ourSt.Name,
			IsNew:      false,
			Target:     ourSt,
			NewFields:  newFields,
		})
		for _, nf := range newFields {
			res.Deltas = append(res.Deltas, DeltaItem{
				Kind:        DeltaModifyStruct,
				EntityName:  fmt.Sprintf("%s.%s", ourSt.Name, nf.GoName),
				Description: fmt.Sprintf("Added field %s (%s)", nf.GoName, nf.Type.Name),
				IsBreaking:  false,
			})
			res.AdditiveCount++
		}
	}
}

func normalizeRoute(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "/")

	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			parts[i] = "{param}"
		}
	}

	return strings.Join(parts, "/")
}
