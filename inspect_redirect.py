import os
import re

fpath = r'd:\CodingProjects\aoni\fast\redirect.go'
with open(fpath, 'r', encoding='utf-8') as f:
    text = f.read()

# fix referer != nil. Usually it's req.Header.Referer() != nil. referer is a function now.
# Wait, let's see how it's used. Let's just look at line 102.
