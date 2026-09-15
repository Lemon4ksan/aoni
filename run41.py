# Copyright (c) 2026 Lemon4ksan All rights reserved.
# Use of this source code is governed by a BSD-style
# license that can be found in the LICENSE file.

import os
import re

filepath = r'd:\CodingProjects\aoni\pipeline\resilience.go'
with open(filepath, 'r', encoding='utf-8') as f:
    content = f.read()

# Replace Pipeline[Req, Resp] with StdHandler
content = re.sub(r'[a-zA-Z_]+\s*\*Pipeline\[Req,\s*Resp\]', 'h *StdHandler', content)
content = content.replace('func (p *Pipeline[Req, Resp])', 'func (h *StdHandler)')
content = re.sub(r'\bp\.', 'h.', content)
content = re.sub(r'(stage[a-zA-Z0-9_]+)\(p,', r'\1(h,', content)

# Now fix DispatchRequest
content = re.sub(
    r'func \(h \*StdHandler\) dispatchRequest\(req \*http\.Request, doer Doer, tx \*Tx\) \(\*http\.Response, error\) \{',
    '''// DispatchRequest executes the HTTP request handling retries, hedging, and proxy failovers.
func (h *StdHandler) DispatchRequest(req *http.Request, genericDoer core.GenericDoer[*http.Request, *http.Response], tx *Tx) (*http.Response, error) {
\tdoer, _ := genericDoer.(Doer)
\tif doer == nil {
\t\tdoer = DoerFunc(func(r *http.Request) (*http.Response, error) {
\t\t\treturn genericDoer.Do(r)
\t\t})
\t}''',
    content
)

with open(filepath, 'w', encoding='utf-8') as f:
    f.write(content)
print("Done")
