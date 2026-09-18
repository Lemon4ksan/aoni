import os

fpath = r'd:\CodingProjects\aoni\fast\redirect.go'
with open(fpath, 'r', encoding='utf-8') as f:
    text = f.read()

text = text.replace('if c.referer != nil {\n\t\t\tzerocopy.AcquireURI() // c.referer.LastURL.Set(string(currentURI.FullURI()))\n\t\t}\n\n', '')

with open(fpath, 'w', encoding='utf-8') as f:
    f.write(text)
