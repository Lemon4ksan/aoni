// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package base

import (
	"flag"
	"strings"
)

// StringSliceFlag implements [flag.Value] for multi-valued command line arguments.
type StringSliceFlag []string

func (s *StringSliceFlag) String() string {
	if s == nil {
		return ""
	}

	return strings.Join(*s, ",")
}

func (s *StringSliceFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// NormalizeArgs stitches back arguments that were fragmented by shell tokenizers (e.g. PowerShell splitting -out=pkg/api and .go).
func NormalizeArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	result := make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]
		for i+1 < len(args) && strings.HasPrefix(args[i+1], ".") && !strings.Contains(args[i+1], "/") && !strings.Contains(args[i+1], "\\") {
			// Orphaned suffix like .go, .json, .har, .yaml
			arg = strings.Join([]string{arg, args[i+1]}, "")
			i++
		}

		result = append(result, arg)
	}

	return result
}

// ParseInterspersedFlags parses a FlagSet correctly even when flags and positional arguments
// are freely mixed together (e.g. `vortex diff file.go --strict`). It returns the ordered positional arguments.
func ParseInterspersedFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	args = NormalizeArgs(args)

	var (
		flagArgs []string
		posArgs  []string
	)

	type boolFlag interface {
		IsBoolFlag() bool
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			posArgs = append(posArgs, args[i+1:]...)
			break
		}

		if strings.HasPrefix(arg, "-") && arg != "-" {
			// Extract flag name (strip leading - or --)
			cleanArg := strings.TrimLeft(arg, "-")
			flagName := cleanArg
			hasEqual := false

			if eqIdx := strings.Index(cleanArg, "="); eqIdx != -1 {
				flagName = cleanArg[:eqIdx]
				hasEqual = true
			}

			fl := fs.Lookup(flagName)
			if fl == nil || hasEqual {
				// Flag not found in FlagSet or already has =val attached
				flagArgs = append(flagArgs, arg)
				continue
			}

			// Check if boolean flag
			if bf, ok := fl.Value.(boolFlag); ok && bf.IsBoolFlag() {
				flagArgs = append(flagArgs, arg)
			} else {
				flagArgs = append(flagArgs, arg)
				// Consume next argument as flag value if available and not a flag
				if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
					i++
					flagArgs = append(flagArgs, args[i])
				}
			}
		} else {
			posArgs = append(posArgs, arg)
		}
	}

	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}

	return posArgs, nil
}

// StringVar binds a string flag with optional short alias.
func StringVar(fs *flag.FlagSet, p *string, name, short, value, usage string) {
	fs.StringVar(p, name, value, usage)

	if short != "" && short != name {
		fs.StringVar(p, short, value, usage)
	}
}

// BoolVar binds a boolean flag with optional short alias.
func BoolVar(fs *flag.FlagSet, p *bool, name, short string, value bool, usage string) {
	fs.BoolVar(p, name, value, usage)

	if short != "" && short != name {
		fs.BoolVar(p, short, value, usage)
	}
}

// IntVar binds an integer flag with optional short alias.
func IntVar(fs *flag.FlagSet, p *int, name, short string, value int, usage string) {
	fs.IntVar(p, name, value, usage)

	if short != "" && short != name {
		fs.IntVar(p, short, value, usage)
	}
}
