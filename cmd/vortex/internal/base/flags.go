// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package base

import (
	"github.com/lemon4ksan/foundation/argkit"
)

// StringSliceFlag implements [flag.Value] for multi-valued command line arguments.
type StringSliceFlag = argkit.StringSliceFlag

// NormalizeArgs stitches back arguments that were fragmented by shell tokenizers.
var NormalizeArgs = argkit.NormalizeArgs

// ParseInterspersedFlags parses a FlagSet correctly with full POSIX semantics:
// clumping (-la), attached values (-I*.tmp), and fuzzy typo suggestions.
var ParseInterspersedFlags = argkit.ParseInterspersedFlags

// StringVar binds a string flag with optional short alias.
var StringVar = argkit.StringVar

// BoolVar binds a boolean flag with optional short alias.
var BoolVar = argkit.BoolVar

// IntVar binds an integer flag with optional short alias.
var IntVar = argkit.IntVar

// Int64Var binds an int64 flag with optional short alias.
var Int64Var = argkit.Int64Var

// Float64Var binds a float64 flag with optional short alias.
var Float64Var = argkit.Float64Var

// DurationVar binds a time.Duration flag with optional short alias.
var DurationVar = argkit.DurationVar
