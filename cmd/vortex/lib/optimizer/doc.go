// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package optimizer normalizes and optimizes the IR prior to code emission.
//
// It performs sub-requester clustering for services spanning multiple base URLs,
// calculates stack-allocated buffer capacities for zero-alloc query and form building,
// and resolves zero-copy parameter serialization strategies.
package optimizer
