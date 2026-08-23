#!/bin/bash
# Copyright (c) 2026 Lemon4ksan All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.


set -e

echo "Generating CA key and certificate:"
openssl req -x509 -sha256 -nodes -days 3650 -newkey rsa:2048 \
  -keyout ca.key -out ca.pem \
  -subj "/O=quic-go Certificate Authority/"

echo "Generating CSR"
openssl req -out cert.csr -new -newkey rsa:2048 -nodes -keyout priv.key \
  -subj "/O=quic-go/"

echo "Sign certificate:"
openssl x509 -req -sha256 -days 3650 -in cert.csr  -out cert.pem \
  -CA ca.pem -CAkey ca.key -CAcreateserial \
  -extfile <(printf "subjectAltName=DNS:localhost")

# debug output the certificate
openssl x509 -noout -text -in cert.pem

# we don't need the CA key, the serial number and the CSR any more
rm ca.key cert.csr ca.srl

