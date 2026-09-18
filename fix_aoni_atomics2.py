import os
import re

fpath = r'd:\CodingProjects\aoni\tests\resiliency\toxiproxy_test.go'
with open(fpath, 'r', encoding='utf-8') as f:
    text = f.read()

text = text.replace('success == 0', 'success.Load() == 0')
text = text.replace('failures == 0', 'failures.Load() == 0')

with open(fpath, 'w', encoding='utf-8') as f:
    f.write(text)
