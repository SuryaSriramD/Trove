"""Complete helper-tree identities shared by qualification copies and readers.

Every file is evidence, including AppleDouble sidecars and misleading dot names.
No filename exclusion is allowed to hide a changed executable or other resource.
"""
from pathlib import Path
import re
import shutil

from completion_qualification import bind_path

COPY_CONTRACT = 'complete-helper-tree-v1'


def helper_inventory(path):
    """Use the same files/directories representation as frozen candidates."""
    binding = bind_path(Path(path))
    if binding['kind'] != 'directory':
        raise ValueError('helper bundle must be a directory')
    return binding['inventory']


def inventory_changes(before, after):
    files = before['files'].keys() | after['files'].keys()
    changes = {p for p in files if before['files'].get(p) != after['files'].get(p)}
    changes.update('directory:' + p for p in set(before['directories']) ^ set(after['directories']))
    return sorted(changes)


def require_same_inventory(expected, actual):
    if (not isinstance(actual, dict) or set(actual) != {'files', 'directories'}
            or not isinstance(actual['files'], dict) or not isinstance(actual['directories'], list)
            or any(not isinstance(p, str) for p in actual['directories'])
            or any(not isinstance(p, str) or not isinstance(v, dict)
                   or set(v) != {'sha256', 'size'} or type(v['size']) is not int or v['size'] < 0
                   or not isinstance(v['sha256'], str) or not re.fullmatch('[0-9a-f]{64}', v['sha256'])
                   for p, v in actual['files'].items())):
        raise ValueError('invalid helper bundle inventory')
    if expected != actual:
        raise ValueError('helper bundle inventory differs: ' + ', '.join(inventory_changes(expected, actual)))


def copy_helper_bundle(source, snapshot):
    """Copy once, with no omissions; reject unsafe trees and concurrent changes.

    Preserve links during copying instead of following a link introduced after
    the source read. The copied inventory then rejects that link. Failed copies
    remain in their original destination for diagnosis and cannot be reused.
    """
    source, snapshot = Path(source), Path(snapshot)
    before = helper_inventory(source)
    shutil.copytree(source, snapshot, symlinks=True)
    copied = helper_inventory(snapshot)
    require_same_inventory(before, helper_inventory(source))
    require_same_inventory(before, copied)
    return {'contract': COPY_CONTRACT, 'sourceInventory': before, 'snapshotInventory': copied}


def check_helper_bundle(source, snapshot, copied):
    """Retain a failed post-run identity check instead of losing its report."""
    record = {**copied, 'sourceChanges': [], 'snapshotChanges': [], 'validationErrors': []}
    for role, path in [('source', source), ('snapshot', snapshot)]:
        try:
            actual = helper_inventory(path)
            record[role + 'Changes'] = inventory_changes(copied[role + 'Inventory'], actual)
        except (OSError, ValueError):
            record['validationErrors'].append(role + '_inventory_unreadable_or_unsafe')
    return record


def validate_helper_copy(record, expected):
    """Validate recorded copy metadata against a complete frozen inventory.

    Callers still verify their candidate and retained artifacts; this function
    does not replace those checks or grant a qualification result.
    """
    required = {'contract', 'sourceInventory', 'snapshotInventory',
                'sourceChanges', 'snapshotChanges', 'validationErrors'}
    if not isinstance(record, dict) or set(record) != required or record['contract'] != COPY_CONTRACT:
        raise ValueError('invalid helper copy contract')
    require_same_inventory(expected, record['sourceInventory'])
    require_same_inventory(expected, record['snapshotInventory'])
    if record['sourceChanges'] != [] or record['snapshotChanges'] != [] or record['validationErrors'] != []:
        raise ValueError('helper bundle changed or became unsafe')
