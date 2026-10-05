"""Pinned offline PDF resource inventory shared by build and package validation."""
import re
from pathlib import Path
from desktop_package import safe_relative, no_link, digest, inventory, binary_target

RUNTIMES={'windows':'webview2-pdfjs-6.3.289-v1','linux':'webkitgtk-2.52.6-pdfjs-6.3.289-v1'}
MIN_WEBVIEW2='141.0.3537.50'
LINUX_UNAVAILABLE='Linux PDF text extraction/export unavailable until whole-process containment is implemented'
EXTENSIONS={'.js','.mjs','.css','.json','.woff','.woff2','.ttf','.otf','.pfb','.bcmap','.icc','.wasm','.png','.jpg','.jpeg','.gif','.webp','.svg'}
ENTRIES={'print.html','extract.html','validate.html'}
def asset_path(name):
    safe_relative(name)
    return bool(re.fullmatch(r'[A-Za-z0-9/._-]{1,512}',name)) and (name in ENTRIES or (name.startswith(('assets/','pdfjs-assets/')) and Path(name).suffix in EXTENSIONS))
def assets(root):
    entries=inventory(root);result={}
    for entry in entries:
        name=entry['path'];size=entry['sizeBytes']
        if not asset_path(name) or size>64<<20:raise ValueError('Invalid document asset')
        result[name]={'sha256':entry['sha256'],'size':size}
    if not ENTRIES.issubset(result) or len(result)>4096 or sum(x['size'] for x in result.values())>128<<20:raise ValueError('Document asset bounds/entries differ')
    return result

def validate(root,target,manifest):
    os_name=target.split('-')[0];suffix='.exe' if os_name=='windows' else ''
    helper=root/('document-worker'+suffix)
    if manifest.get('schemaVersion')!=1 or manifest.get('name')!='document-worker' or manifest.get('os')!=os_name or manifest.get('architecture')!='amd64' or manifest.get('runtime')!=RUNTIMES[os_name] or manifest.get('testOnly') is not False:
        raise ValueError('Document production identity differs')
    if helper.stat().st_size>128<<20 or digest(helper)!=manifest.get('sha256') or not binary_target(helper,target) or (os_name=='linux' and not helper.stat().st_mode&0o111):raise ValueError('Document helper hash/target differs')
    if os_name=='windows' and manifest.get('minimumWebView2')!=MIN_WEBVIEW2:raise ValueError('Document WebView2 minimum differs')
    if os_name=='linux':
        extension=root/'document-web-extension.so'
        if extension.stat().st_size>16<<20 or digest(extension)!=manifest.get('webExtensionSha256') or not binary_target(extension,target):raise ValueError('Document policy extension differs')
    if assets(root/'document-assets')!=manifest.get('assets'):raise ValueError('Document resource inventory differs')
