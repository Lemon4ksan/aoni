import os
import re

files = [
    r'd:\CodingProjects\aoni\scripts\gollvm_bench\parallel_pipeline_rps.go',
    r'd:\CodingProjects\aoni\scripts\gollvm_bench\parallel_rps.go',
    r'd:\CodingProjects\aoni\scripts\gollvm_bench\real_client_rps.go',
    r'd:\CodingProjects\aoni\tests\resiliency\toxiproxy_test.go'
]

for fpath in files:
    if not os.path.exists(fpath): continue
    with open(fpath, 'r', encoding='utf-8') as f:
        text = f.read()

    text = re.sub(r'var\s+completedCount\s+int64', r'var completedCount atomic.Int64', text)
    text = re.sub(r'var\s+completed\s+int64', r'var completed atomic.Int64', text)
    text = re.sub(r'var\s+success,\s+failures\s+int32', r'var success, failures atomic.Int32', text)
    
    text = re.sub(r'atomic\.AddInt64\(&(completed(?:Count)?),\s*1\)', r'\1.Add(1)', text)
    text = re.sub(r'atomic\.AddInt32\(&(success|failures),\s*1\)', r'\1.Add(1)', text)
    
    text = re.sub(r'atomic\.LoadInt64\(&(completed(?:Count)?)\)', r'\1.Load()', text)
    text = re.sub(r'atomic\.LoadInt32\(&(success|failures)\)', r'\1.Load()', text)

    with open(fpath, 'w', encoding='utf-8') as f:
        f.write(text)
