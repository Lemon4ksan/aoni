# Copyright (c) 2026 Lemon4ksan All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

import os
import re
filepath = r'd:\CodingProjects\aoni\pipeline\prep.go'
with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

content = re.sub(r'func convertRequestToStd\(r core\.Request\) \*http\.Request \{[\s\S]*?\}\n', '', content)
with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)
print("Done")
