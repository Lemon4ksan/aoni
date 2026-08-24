// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package gomock provides a lightweight, zero-dependency, pure-Go mock controller
// and matcher engine compatible with generated GoMock mocks.
package gomock

import (
	"fmt"
	"reflect"
	"sync"
	"testing"
)

// TestReporter is the interface for error reporting and helper annotations in tests.
type TestReporter interface {
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
	Helper()
}

type wrappedReporter struct {
	t any
}

func (w *wrappedReporter) Errorf(format string, args ...any) {
	if tr, ok := w.t.(interface{ Errorf(string, ...any) }); ok {
		tr.Errorf(format, args...)
	}
}

func (w *wrappedReporter) Fatalf(format string, args ...any) {
	if tr, ok := w.t.(interface{ Fatalf(string, ...any) }); ok {
		tr.Fatalf(format, args...)
	}
}

func (w *wrappedReporter) Helper() {
	if th, ok := w.t.(interface{ Helper() }); ok {
		th.Helper()
	}
}

func wrapReporter(t any) TestReporter {
	if tr, ok := t.(TestReporter); ok {
		return tr
	}

	return &wrappedReporter{t: t}
}

// Controller defines the mock lifecycle controller.
type Controller struct {
	T        TestReporter
	mu       sync.Mutex
	expected []*Call
	finished bool
}

// NewController creates a new mock controller.
func NewController(t any) *Controller {
	tr := wrapReporter(t)
	ctrl := &Controller{T: tr}

	if tb, ok := t.(testing.TB); ok {
		tb.Cleanup(ctrl.Finish)
	}

	return ctrl
}

// Finish verifies all expected calls were made.
func (ctrl *Controller) Finish() {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	if ctrl.finished {
		return
	}

	ctrl.finished = true

	for _, call := range ctrl.expected {
		if !call.satisfied() {
			if ctrl.T != nil {
				ctrl.T.Helper()
				ctrl.T.Errorf(
					"missing expected call: %v (expected %d times, actual %d times)",
					call,
					call.minCalls,
					call.actualCalls,
				)
			}
		}
	}
}

// Satisfied returns true if all recorded expectations have been satisfied.
func (ctrl *Controller) Satisfied() bool {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	for _, call := range ctrl.expected {
		if !call.satisfied() {
			return false
		}
	}

	return true
}

// RecordCall registers an expected method invocation.
func (ctrl *Controller) RecordCall(receiver any, method string, args ...any) *Call {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	call := newCall(ctrl, receiver, method, nil, args...)
	ctrl.expected = append(ctrl.expected, call)

	return call
}

// RecordCallWithMethodType registers an expected method invocation with reflection type info.
func (ctrl *Controller) RecordCallWithMethodType(
	receiver any,
	method string,
	methodType reflect.Type,
	args ...any,
) *Call {
	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	call := newCall(ctrl, receiver, method, methodType, args...)
	ctrl.expected = append(ctrl.expected, call)

	return call
}

// Call executes an invoked mock method against registered expectations.
func (ctrl *Controller) Call(receiver any, method string, args ...any) []any {
	var targetCall *Call

	ctrl.mu.Lock()

	for _, call := range ctrl.expected {
		if call.matches(receiver, method, args) {
			targetCall = call
			targetCall.actualCalls++
			break
		}
	}

	ctrl.mu.Unlock()

	if targetCall != nil {
		return targetCall.execute(args)
	}

	if ctrl.T != nil {
		ctrl.T.Helper()
		ctrl.T.Fatalf("unexpected call: %T.%s(%v)", receiver, method, args)
	}

	return []any{nil, nil, nil, nil}
}

// Call represents an expected mock method invocation.
type Call struct {
	ctrl        *Controller
	receiver    any
	method      string
	methodType  reflect.Type
	args        []any
	returns     []any
	action      any // func
	doAndReturn any // func
	setArgs     map[int]any
	prereq      *Call
	minCalls    int
	maxCalls    int
	actualCalls int
}

func newCall(ctrl *Controller, receiver any, method string, methodType reflect.Type, args ...any) *Call {
	return &Call{
		ctrl:       ctrl,
		receiver:   receiver,
		method:     method,
		methodType: methodType,
		args:       args,
		minCalls:   1,
		maxCalls:   1,
		setArgs:    make(map[int]any),
	}
}

func (c *Call) satisfied() bool {
	return c.actualCalls >= c.minCalls && (c.maxCalls < 0 || c.actualCalls <= c.maxCalls)
}

func (c *Call) matches(receiver any, method string, args []any) bool {
	if c.receiver != receiver || c.method != method {
		return false
	}

	if c.maxCalls >= 0 && c.actualCalls >= c.maxCalls {
		return false
	}

	if c.prereq != nil && !c.prereq.satisfied() {
		return false
	}

	if len(c.args) != len(args) {
		return false
	}

	for i, expectedArg := range c.args {
		actualArg := args[i]
		if matcher, ok := expectedArg.(Matcher); ok {
			if !matcher.Matches(actualArg) {
				return false
			}
		} else if !reflect.DeepEqual(expectedArg, actualArg) {
			return false
		}
	}

	return true
}

func (c *Call) execute(args []any) []any {
	for idx, val := range c.setArgs {
		if idx < len(args) {
			target := reflect.ValueOf(args[idx])
			if target.Kind() == reflect.Pointer && !target.IsNil() {
				src := reflect.ValueOf(val)
				if src.Type().AssignableTo(target.Elem().Type()) {
					target.Elem().Set(src)
				}
			}
		}
	}

	if c.doAndReturn != nil {
		fnVal := reflect.ValueOf(c.doAndReturn)
		in := make([]reflect.Value, len(args))

		for i, arg := range args {
			if arg == nil {
				in[i] = reflect.Zero(fnVal.Type().In(i))
			} else {
				in[i] = reflect.ValueOf(arg)
			}
		}

		outVals := fnVal.Call(in)
		rets := make([]any, len(outVals))

		for i, out := range outVals {
			rets[i] = out.Interface()
		}

		return rets
	}

	if c.action != nil {
		fnVal := reflect.ValueOf(c.action)
		in := make([]reflect.Value, len(args))

		for i, arg := range args {
			if arg == nil {
				in[i] = reflect.Zero(fnVal.Type().In(i))
			} else {
				in[i] = reflect.ValueOf(arg)
			}
		}

		fnVal.Call(in)
	}

	if len(c.returns) > 0 {
		return c.returns
	}

	if c.methodType != nil && c.methodType.Kind() == reflect.Func {
		numOut := c.methodType.NumOut()
		if numOut > 0 {
			rets := make([]any, numOut)
			for i := 0; i < numOut; i++ {
				rets[i] = reflect.Zero(c.methodType.Out(i)).Interface()
			}

			return rets
		}
	}

	return []any{nil, nil, nil, nil}
}

// Return specifies return values for the expected call.
func (c *Call) Return(rets ...any) *Call {
	c.returns = rets
	return c
}

// Do specifies a function to execute when the call is made.
func (c *Call) Do(f any) *Call {
	c.action = f
	return c
}

// DoAndReturn specifies a function to execute and return its values.
func (c *Call) DoAndReturn(f any) *Call {
	c.doAndReturn = f
	return c
}

// Times specifies the exact number of times the call is expected.
func (c *Call) Times(n int) *Call {
	c.minCalls = n
	c.maxCalls = n

	return c
}

// AnyTimes allows the call to be executed 0 or more times.
func (c *Call) AnyTimes() *Call {
	c.minCalls = 0
	c.maxCalls = -1

	return c
}

// MinTimes sets the minimum number of expected calls.
func (c *Call) MinTimes(n int) *Call {
	c.minCalls = n
	c.maxCalls = -1

	return c
}

// MaxTimes sets the maximum number of expected calls (between 0 and n calls).
func (c *Call) MaxTimes(n int) *Call {
	c.minCalls = 0
	c.maxCalls = n

	return c
}

// SetArg sets an out-parameter by pointer when the call is matched.
func (c *Call) SetArg(index int, val any) *Call {
	c.setArgs[index] = val
	return c
}

func (c *Call) String() string {
	return fmt.Sprintf("%T.%s(%v)", c.receiver, c.method, c.args)
}

// Matcher represents a call argument matcher.
type Matcher interface {
	Matches(x any) bool
	String() string
}

type anyMatcher struct{}

// Any returns a matcher matching anything.
func Any() Matcher                  { return anyMatcher{} }
func (anyMatcher) Matches(any) bool { return true }
func (anyMatcher) String() string   { return "is anything" }

type eqMatcher struct{ x any }

// Eq returns a matcher matching by deep equality.
func Eq(x any) Matcher                 { return eqMatcher{x: x} }
func (m eqMatcher) Matches(x any) bool { return reflect.DeepEqual(m.x, x) }
func (m eqMatcher) String() string     { return fmt.Sprintf("is equal to %v", m.x) }

type nilMatcher struct{}

// Nil returns a matcher matching nil pointers, slices, maps, or interfaces.
func Nil() Matcher { return nilMatcher{} }

func (nilMatcher) Matches(x any) bool {
	if x == nil {
		return true
	}

	v := reflect.ValueOf(x)
	switch v.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return v.IsNil()
	default:
		return false
	}
}
func (nilMatcher) String() string { return "is nil" }

type notMatcher struct{ m Matcher }

// Not inverts the inner matcher.
func Not(m any) Matcher {
	if matcher, ok := m.(Matcher); ok {
		return notMatcher{m: matcher}
	}

	return notMatcher{m: Eq(m)}
}
func (n notMatcher) Matches(x any) bool { return !n.m.Matches(x) }
func (n notMatcher) String() string     { return fmt.Sprintf("not(%v)", n.m) }

type assignableToTypeOfMatcher struct{ target reflect.Type }

// AssignableToTypeOf matches if arg is assignable to the target type.
func AssignableToTypeOf(x any) Matcher {
	if t, ok := x.(reflect.Type); ok {
		return assignableToTypeOfMatcher{target: t}
	}

	return assignableToTypeOfMatcher{target: reflect.TypeOf(x)}
}

func (a assignableToTypeOfMatcher) Matches(x any) bool {
	if x == nil {
		return false
	}

	return reflect.TypeOf(x).AssignableTo(a.target)
}

func (a assignableToTypeOfMatcher) String() string {
	return fmt.Sprintf("is assignable to %v", a.target)
}

type lenMatcher struct{ n int }

// Len matches if arg has length n.
func Len(n int) Matcher { return lenMatcher{n: n} }

func (l lenMatcher) Matches(x any) bool {
	if x == nil {
		return false
	}

	v := reflect.ValueOf(x)
	switch v.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == l.n
	default:
		return false
	}
}
func (l lenMatcher) String() string { return fmt.Sprintf("has length %d", l.n) }

func toCall(v any) *Call {
	if c, ok := v.(*Call); ok {
		return c
	}

	if v == nil {
		return nil
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}

	if rv.Kind() == reflect.Struct {
		field := rv.FieldByName("Call")
		if field.IsValid() {
			if c, ok := field.Interface().(*Call); ok {
				return c
			}
		}
	}

	return nil
}

// InOrder declares that the given calls must be matched in order.
func InOrder(calls ...any) {
	var callList []*Call

	for _, c := range calls {
		if cl := toCall(c); cl != nil {
			callList = append(callList, cl)
		}
	}

	for i := 1; i < len(callList); i++ {
		callList[i].prereq = callList[i-1]
	}
}
