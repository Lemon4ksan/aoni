// Copyright (c) 2026 Lemon4ksan All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package geoip provides fast, zero-dependency lookup for MMDB GeoIP2 and GeoLite2 databases.
package geoip

import (
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"
)

// Metadata holds extracted geolocation, ASN, and IP classification metadata.
type Metadata struct {
	Country   string
	ASN       uint
	ISP       string
	UsageType string // "DataCenter", "Residential", "Mobile"
}

// DB provides fast lookup for MMDB GeoIP2 and GeoLite2 databases.
type DB struct {
	reader *maxminddb.Reader
}

// Open loads a GeoIP2 MMDB database file into memory.
func Open(path string) (*DB, error) {
	r, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}

	return &DB{reader: r}, nil
}

// OpenBytes instantiates a Reader directly from an in-memory byte slice.
func OpenBytes(b []byte) (*DB, error) {
	r, err := maxminddb.OpenBytes(b)
	if err != nil {
		return nil, err
	}

	return &DB{reader: r}, nil
}

// Lookup queries metadata for target IP using netip.Addr.
func (db *DB) Lookup(ip netip.Addr) (*Metadata, error) {
	if db == nil || db.reader == nil {
		return nil, nil
	}

	record := db.reader.Lookup(ip)

	var result struct {
		Country struct {
			ISOCode string `maxminddb:"iso_code"`
		} `maxminddb:"country"`
		ASN                 uint   `maxminddb:"autonomous_system_number"`
		ASNOrg              string `maxminddb:"autonomous_system_organization"`
		AutonomousSystemNum uint   `maxminddb:"autonomous_system_number"`
		AutonomousSystemOrg string `maxminddb:"autonomous_system_organization"`
		ISP                 string `maxminddb:"isp"`
		UserType            string `maxminddb:"user_type"`
		IsHostingProvider   bool   `maxminddb:"is_hosting_provider"`
	}

	if err := record.Decode(&result); err != nil {
		return nil, err
	}

	asn := result.ASN
	if asn == 0 {
		asn = result.AutonomousSystemNum
	}

	isp := result.ISP
	if isp == "" {
		isp = result.ASNOrg
	}

	if isp == "" {
		isp = result.AutonomousSystemOrg
	}

	usage := "Residential"
	if result.IsHostingProvider || result.UserType == "hosting" || result.UserType == "business" {
		usage = "DataCenter"
	} else if result.UserType == "cellular" {
		usage = "Mobile"
	}

	return &Metadata{
		Country:   result.Country.ISOCode,
		ASN:       asn,
		ISP:       isp,
		UsageType: usage,
	}, nil
}

// Close closes the underlying MMDB reader.
func (db *DB) Close() error {
	if db != nil && db.reader != nil {
		return db.reader.Close()
	}

	return nil
}
