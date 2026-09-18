import os
import shutil
import re

src_dir = r'd:\CodingProjects\aoni\internal\transport'
dst_dir = r'd:\CodingProjects\foundation\encoding\base64'

if not os.path.exists(dst_dir):
    os.makedirs(dst_dir)

files_to_move = [f for f in os.listdir(src_dir) if f.startswith('base64')]

for f in files_to_move:
    src_file = os.path.join(src_dir, f)
    dst_file = os.path.join(dst_dir, f)
    shutil.move(src_file, dst_file)
    
    if dst_file.endswith('.go'):
        with open(dst_file, 'r', encoding='utf-8') as file:
            c = file.read()
        c = re.sub(r'package transport\b', 'package base64', c)
        with open(dst_file, 'w', encoding='utf-8') as file:
            file.write(c)

aoni_dir = r'd:\CodingProjects\aoni'
for root, _, files in os.walk(aoni_dir):
    if '.git' in root: continue
    for f in files:
        if not f.endswith('.go'): continue
        path = os.path.join(root, f)
        try:
            with open(path, 'r', encoding='utf-8') as file:
                content = file.read()
            if 'transport.Base64' in content:
                content = content.replace('transport.Base64', 'base64.Base64')
                # We also need to add the import
                # Let goimports handle it by adding a dummy import or just letting goimports find it
                # Actually, goimports will find it if we run go mod tidy and go run golang.org/x/tools/cmd/goimports@latest
            with open(path, 'w', encoding='utf-8') as file:
                file.write(content)
        except:
            pass

