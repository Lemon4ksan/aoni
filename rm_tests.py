import os

files_to_remove = [
    r'd:\CodingProjects\aoni\tests\h2_h3_bench_test.go',
    r'd:\CodingProjects\aoni\tests\h2_h3_compat_test.go'
]

for f in files_to_remove:
    if os.path.exists(f):
        os.remove(f)
