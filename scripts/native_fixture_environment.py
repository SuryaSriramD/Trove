"""Private filesystem state for owned native fixture children, not an OS sandbox.

HOME controls Go's Darwin UserConfigDir/UserCacheDir; TMPDIR controls TempDir.
It does not virtualize the macOS Keychain, UserNotifications or SMAppService;
Cocoa preference and WebKit storage isolation is also not established.
Never exercise account login, notification permission/delivery or login-item
registration from a fixture relying on this filesystem isolation alone.
"""
import os
from pathlib import Path
import stat

from qualification_fixtures import private_directory

OS_GLOBAL_LIMITATION = ('Private HOME/TMPDIR isolate filesystem state only; macOS Keychain, '
                       'UserNotifications, login-item registration and bundle identity remain OS-global; '
                       'Cocoa preferences and WebKit storage isolation is not established. This fixture does not '
                       'authorize account login, notification permission/delivery or login-item registration.')


def native_fixture_environment(root, workspace, models=None):
    """Create a fresh owned environment; never read or modify the parent environ."""
    root = Path(root).absolute()
    root = root.parent.resolve(strict=True) / root.name
    # Exclusive mkdir rejects existing homes, dangling links and reused runs.
    private_directory(root)
    home = private_directory(root / 'home')
    temporary = private_directory(root / 'temporary')
    library = private_directory(home / 'Library')
    config = private_directory(library / 'Application Support')
    cache = private_directory(library / 'Caches')
    xdg_config = private_directory(home / '.config')
    xdg_cache = private_directory(home / '.cache')
    empty_models = private_directory(root / 'empty-models')
    workspace = Path(workspace).absolute()
    if workspace.is_symlink():
        raise ValueError('fixture workspace cannot be a link')
    workspace = workspace.parent.resolve(strict=True) / workspace.name
    if models is not None and Path(models).is_symlink():
        raise ValueError('selected model root cannot be a link')
    model_root = Path(models).resolve(strict=True) if models is not None else empty_models
    if not model_root.is_dir():
        raise ValueError('selected model root must be a directory')
    # Fixed allowlist: no credentials, provider config, inherited TROVE/DYLD,
    # proxy settings, XDG paths, or Python injection settings reach the child.
    env = {'PATH': '/usr/bin:/bin:/usr/sbin:/sbin', 'LANG': 'en_US.UTF-8',
           'HOME': str(home), 'TMPDIR': str(temporary) + os.sep,
           'TMP': str(temporary), 'TEMP': str(temporary),
           'XDG_CONFIG_HOME': str(xdg_config), 'XDG_CACHE_HOME': str(xdg_cache),
           'TROVE_WORKSPACE': str(workspace), 'TROVE_MODEL_ROOT': str(model_root)}
    receipt = {'root': str(root), 'home': str(home), 'temporary': str(temporary),
               'darwinUserConfigDir': str(config), 'darwinUserCacheDir': str(cache),
               'workspace': str(workspace), 'modelRoot': str(model_root),
               'externalModelRootExplicitlySelected': models is not None,
               'environmentKeys': sorted(env), 'parentEnvironmentInherited': False,
               'osGlobalServicesIsolated': False, 'limitation': OS_GLOBAL_LIMITATION}
    validate_native_fixture_environment(env, receipt)
    return env, receipt


def validate_native_fixture_environment(env, receipt):
    """Reject linked or non-private runtime directories before each launch."""
    for key in ('root', 'home', 'temporary', 'darwinUserConfigDir', 'darwinUserCacheDir'):
        path = Path(receipt[key])
        info = path.lstat()
        if (not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid()
                or info.st_mode & 0o077 or path.resolve(strict=True) != path):
            raise ValueError('native fixture directory must be unlinked, owned and private')
    if (env['HOME'] != receipt['home'] or Path(env['TMPDIR']) != Path(receipt['temporary'])
            or env['TROVE_WORKSPACE'] != receipt['workspace']):
        raise ValueError('native fixture environment differs from its private paths')
