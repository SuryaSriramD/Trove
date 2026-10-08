"""Retain private qualification inputs without changing a closed workspace."""
from contextlib import contextmanager
import fcntl
import hashlib
import os
from pathlib import Path
import shutil
import stat


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        if not stat.S_ISDIR(os.fstat(fd).st_mode):
            raise ValueError('expected a directory for synchronization')
        os.fsync(fd)
    finally:
        os.close(fd)


def private_directory(path):
    path.mkdir(mode=0o700, exist_ok=False)
    info = path.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise ValueError('qualification directory must be private and owned')
    sync_directory(path)
    sync_directory(path.parent)
    return path


def inventory(root):
    """Bind paths and bytes without including private filenames in the report."""
    if root.is_symlink() or not root.is_dir():
        raise ValueError('input must be an existing unlinked directory')
    h = hashlib.sha256()
    count = size = 0
    for path in sorted(root.rglob('*')):
        info = path.lstat()
        if stat.S_ISLNK(info.st_mode) or not (stat.S_ISREG(info.st_mode) or stat.S_ISDIR(info.st_mode)):
            raise ValueError('input contains a link or special object')
        name = path.relative_to(root).as_posix().encode()
        h.update(len(name).to_bytes(8, 'big') + name + (b'd' if path.is_dir() else b'f'))
        if path.is_file():
            file_hash = hashlib.sha256()
            with path.open('rb') as source:
                while chunk := source.read(1024 * 1024):
                    file_hash.update(chunk)
            h.update(info.st_size.to_bytes(8, 'big') + file_hash.digest())
            count += 1
            size += info.st_size
    return {'sha256': h.hexdigest(), 'files': count, 'bytes': size}


@contextmanager
def owner_lock(source):
    if source.is_symlink() or not source.is_dir():
        raise ValueError('fixture root must be an unlinked directory')
    fd = os.open(source / '.owner.lock', os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as lock:
        info = os.fstat(lock.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != os.getuid():
            raise ValueError('fixture owner lock is unsafe')
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield


def sync_tree(root):
    """Called outside measured intervals, while the workspace owner lock is held."""
    directories = [root]
    for path in sorted(root.rglob('*')):
        info = path.lstat()
        if stat.S_ISDIR(info.st_mode):
            directories.append(path)
        elif stat.S_ISREG(info.st_mode):
            fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
            try:
                os.fsync(fd)
            finally:
                os.close(fd)
        else:
            raise ValueError('input contains a link or special object')
    for path in reversed(directories):
        sync_directory(path)
    sync_directory(root.parent)


def clone_closed_workspace(source, destination):
    with owner_lock(source):
        before = inventory(source)
        shutil.copytree(source, destination)
        if before != inventory(source) or inventory(destination) != before:
            raise ValueError('fixture changed during copy')
        sync_tree(destination)
        return before


def retain_closed_workspace(source):
    with owner_lock(source):
        before = inventory(source)
        sync_tree(source)
        if inventory(source) != before:
            raise ValueError('fixture changed during retention')
        return {'path': str(source), 'inventory': before, 'ownerLockHeld': True,
                'unchangedDuringRetention': True, 'fsynced': True}
