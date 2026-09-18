import os

fpath = r'd:\CodingProjects\aoni\fast\redirect.go'
with open(fpath, 'r', encoding='utf-8') as f:
    text = f.read()

text = text.replace('fastReq.Header.Del(header.Referer)', 'fastReq.Header.Del("Referer")')
text = text.replace('bytesconv.S2B(header.Referer)', 'bytesconv.S2B("Referer")')

with open(fpath, 'w', encoding='utf-8') as f:
    f.write(text)
