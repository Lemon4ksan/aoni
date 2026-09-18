import os

aoni_dir = r'd:\CodingProjects\aoni'

for root, _, files in os.walk(aoni_dir):
    if '.git' in root or 'vendor' in root: continue
    for f in files:
        if not f.endswith('.go'): continue
        path = os.path.join(root, f)
        
        with open(path, 'r', encoding='utf-8') as file:
            content = file.read()
            
        if '"github.com/lemon4ksan/aoni/ext' in content:
            content = content.replace('"github.com/lemon4ksan/aoni/ext', '"github.com/lemon4ksan/aoni/x')
            with open(path, 'w', encoding='utf-8') as file:
                file.write(content)
