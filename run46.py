# Copyright (c) 2026 Lemon4ksan All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

import os
filepath = r'd:\CodingProjects\aoni\pipeline\prep.go'
with open(filepath, 'r', encoding='utf-8') as f:
    lines = f.readlines()

with open(filepath, 'w', encoding='utf-8') as f:
    skip = False
    for line in lines:
        if line.startswith('func convertRequestToStd(r core.Request) *http.Request {'):
            skip = True
        if skip:
            if line == '}\n':
                skip = False
            continue
        f.write(line)
print("Done")
