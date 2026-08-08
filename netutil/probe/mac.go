// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package probe

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// HardwareInfo holds resolved MAC address and IEEE OUI Vendor metadata.
type HardwareInfo struct {
	MAC    string
	Vendor string
}

// ResolveHardwareInfo resolves MAC address from local ARP table and identifies hardware vendor.
func ResolveHardwareInfo(ip string) (*HardwareInfo, error) {
	macStr, err := getMACFromARP(ip)
	if err != nil || macStr == "" {
		return nil, err
	}

	vendor := lookupOUIVendor(macStr)

	return &HardwareInfo{
		MAC:    macStr,
		Vendor: vendor,
	}, nil
}

func getMACFromARP(ip string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "arp", "-a", ip)
	} else {
		cmd = exec.CommandContext(ctx, "arp", "-n", ip)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	for _, line := range strings.Split(string(output), "\n") {
		if strings.Contains(line, ip) {
			for _, part := range strings.Fields(line) {
				cleanPart := strings.Trim(part, "()[]")
				if hw, parseErr := net.ParseMAC(cleanPart); parseErr == nil {
					return hw.String(), nil
				}
			}
		}
	}

	return "", fmt.Errorf("aoni probe: mac not found in arp table for %s", ip)
}

func lookupOUIVendor(macStr string) string {
	clean := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(macStr, ":", ""), "-", ""))
	if len(clean) < 6 {
		return "Unknown"
	}

	prefix := clean[:6]

	// Known IEEE OUI prefixes
	switch prefix {
	case "001a2b", "001e13", "002255":
		return "Cisco Systems"
	case "000c29", "005056":
		return "VMware"
	case "0242ac":
		return "Docker Container"
	case "b827eb", "dca632", "e45f01":
		return "Raspberry Pi Foundation"
	case "00cdfe", "04d3b0", "086698", "147dda", "3c0630", "f4d488":
		return "Apple"
	case "00037f", "000c42", "00138f", "002722", "04a151", "085531":
		return "MikroTik"
	default:
		return "Generic Vendor"
	}
}
