"""Publish generated artifacts only after all outputs have been prepared."""
import os
from pathlib import Path
import tempfile


def publish(outputs, *, check=False):
    changed = [Path(path) for path, data in outputs.items() if not Path(path).is_file() or Path(path).read_bytes() != data]
    if check:
        if changed:
            raise SystemExit('Generated files differ: ' + ', '.join(str(path) for path in changed))
        return
    for path in changed:
        path.parent.mkdir(parents=True, exist_ok=True)
        descriptor, temporary = tempfile.mkstemp(prefix='.' + path.name + '-', dir=path.parent)
        try:
            with os.fdopen(descriptor, 'wb') as output:
                output.write(outputs[path])
            os.chmod(temporary, path.stat().st_mode & 0o777 if path.exists() else 0o644)
            os.replace(temporary, path)
        finally:
            if os.path.exists(temporary):
                os.unlink(temporary)
