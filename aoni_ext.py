import os
import shutil

ext_dir = r'd:\CodingProjects\aoni\ext'
if not os.path.exists(ext_dir):
    os.makedirs(ext_dir)

packages_to_move = ['tunnel', 'realtime', 'codec', 'fingerprint', 'telemetry']

for pkg in packages_to_move:
    src = os.path.join(r'd:\CodingProjects\aoni', pkg)
    dst = os.path.join(ext_dir, pkg)
    if os.path.exists(src):
        os.system(f'git mv {src} {dst}')

def patch_file(fpath):
    with open(fpath, 'r', encoding='utf-8') as f:
        text = f.read()

    new_text = text
    for pkg in packages_to_move:
        # Replace imports: "github.com/lemon4ksan/aoni/pkg" -> "github.com/lemon4ksan/aoni/ext/pkg"
        # Also handle subpackages like "github.com/lemon4ksan/aoni/pkg/sub"
        new_text = new_text.replace(f'"github.com/lemon4ksan/aoni/{pkg}', f'"github.com/lemon4ksan/aoni/ext/{pkg}')

    if new_text != text:
        with open(fpath, 'w', encoding='utf-8') as f:
            f.write(new_text)

for root, _, files in os.walk(r'd:\CodingProjects\aoni'):
    for f in files:
        if f.endswith('.go'):
            patch_file(os.path.join(root, f))
