# Copyright (c) 2026 Lemon4ksan All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

import os

filepath = r'd:\CodingProjects\aoni\pipeline\prep.go'
with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

content = content.replace('\t"bytes"\n', '')
content = content.replace('\t"github.com/lemon4ksan/foundation/silicon/bytesconv"\n', '')
content = content.replace('\t"github.com/lemon4ksan/mach/client/h1"\n', '')

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)
