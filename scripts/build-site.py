#!/usr/bin/env python3
"""Build the static website; GitHub Actions supplies the repository identifier."""
import json
import os
from pathlib import Path
import re
import shutil

root = Path(__file__).resolve().parent.parent
output = root / '.build' / 'site'
repository = os.environ.get('GITHUB_REPOSITORY', '')
if repository and not re.fullmatch(r'[\w.-]+/[\w.-]+', repository):
    raise SystemExit('GITHUB_REPOSITORY must use owner/repository format')
output.mkdir(parents=True, exist_ok=True)
for name in ['index.html', 'style.css', 'main.js', 'translations.js', 'icon.svg']:
    shutil.copyfile(root / 'site' / name, output / name)
shutil.rmtree(output / 'screenshots', ignore_errors=True)
shutil.copytree(root / 'site' / 'screenshots', output / 'screenshots')
url = f'https://github.com/{repository}' if repository else ''
(output / 'config.js').write_text('window.SSH_GATEWAY_REPOSITORY = ' + json.dumps(url) + ';\n')
(output / '.nojekyll').touch()
print(f'Website built: {output}')
