import os
import shutil
import re

src_dir = r'd:\CodingProjects\aoni\internal\transport'
dst_dir = r'd:\CodingProjects\foundation\encoding\base64'

if not os.path.exists(dst_dir):
    os.makedirs(dst_dir)

# Move all base64 files
files_to_move = [f for f in os.listdir(src_dir) if f.startswith('base64')]

for f in files_to_move:
    src_file = os.path.join(src_dir, f)
    dst_file = os.path.join(dst_dir, f)
    shutil.move(src_file, dst_file)
    
    # Change package name from 'transport' to 'base64'
    if dst_file.endswith('.go'):
        with open(dst_file, 'r', encoding='utf-8') as file:
            c = file.read()
        c = re.sub(r'package transport\b', 'package base64', c)
        with open(dst_file, 'w', encoding='utf-8') as file:
            file.write(c)

# Now we need to find everywhere in aoni that used the base64 functions
# They were using 	ransport.Base64... maybe? Let's check what functions were exported.
# Actually, inside oni, they might be just calling ase64Encode internally if it was unexported?
# Wait! Let's check ase64.go to see if functions are exported or not.
