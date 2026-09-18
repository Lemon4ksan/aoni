import os

fpath = r'd:\CodingProjects\aoni\tests\h2_h3_bench_test.go'
with open(fpath, 'r', encoding='utf-8') as f:
    text = f.read()

text = text.replace('"github.com/lemon4ksan/mach/core/h2"', '"github.com/lemon4ksan/mach/proto/h2"')

with open(fpath, 'w', encoding='utf-8') as f:
    f.write(text)
