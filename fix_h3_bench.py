import os

for fname in os.listdir(r'd:\CodingProjects\aoni\tests'):
    if not fname.endswith('.go'): continue
    fpath = os.path.join(r'd:\CodingProjects\aoni\tests', fname)
    with open(fpath, 'r', encoding='utf-8') as f:
        text = f.read()

    text = text.replace('"github.com/lemon4ksan/mach/core/h2"', '"github.com/lemon4ksan/mach/proto/h2"')
    text = text.replace('"github.com/lemon4ksan/mach/core/h3"', '"github.com/lemon4ksan/mach/proto/h3"')

    with open(fpath, 'w', encoding='utf-8') as f:
        f.write(text)
