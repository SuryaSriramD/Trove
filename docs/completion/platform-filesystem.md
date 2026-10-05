# Workspace ownership and file identity across platforms

This package removes Unix-only calls from the kernel's owner lock, selected-file ingestion and approved external indexing. It supplies native macOS/Linux primitives and a Windows 11 x64 implementation. It is a C7/C8 foundation, not a Windows/Linux release qualification.

The kernel holds one operating-system lock for its whole lifetime. Unix uses nonblocking flock on an open regular, single-link owner file. Windows uses exclusive, nonblocking LockFileEx on a disk handle and denies FILE_SHARE_DELETE on that ownership handle, preventing pathname replacement while it is held. The owner pathname is retained after release; process death releases descriptor ownership without deleting it. Symlink/reparse-point, directory, device and multiply linked owner files are rejected. Unix opens are nonblocking so a file-to-FIFO substitution cannot hang ingestion before the type check.

Selected-file handles pin the actual regular file. Unix identities preserve the existing device:inode strings. Windows identities combine volume serial and file index obtained from the open handle. External roots and indexed files now obtain identity from the descriptor used to read them, preserving approval checks after renames and refusing root replacements. Windows primitives accept local drive paths, including long paths, and reject device/UNC paths and alternate data streams. Network filesystems are not in this initial Windows boundary.

## Recorded checks

See [source and evidence manifest](../../evidence/completion/platform-filesystem/manifest.json). Actual macOS race tests cover exclusive ownership across processes, death/reacquisition, separate descriptors, hard links, symlinks, directories, FIFO nonblocking rejection, original bytes after a pathname replacement, and stable file/root identities. Affected workspace checks exercise ingestion, root confinement/renames, opening a second kernel, online backup, migration fences, recovery and owner transfer.

The same platformfs tests cross-compile for Windows amd64 and Linux amd64. These test binaries were not executed. Compilation checks platform APIs and types; it does not establish runtime lock, reparse, permission or filesystem behavior.

## Remaining platform work

The additive [Windows storage package](windows-storage.md) supplies private directory creation, read-only workspace DACL auditing and same-volume regular-file publication source code. Its Windows/Linux test binaries compile but have not run. Full SQLite/native application compilation, actual target execution, inherited permission and junction/ancestor behavior, Linux permissions, filesystem case/normalization behavior, filesystem crash recovery and backup portability remain open. Windows new-workspace creation, migration, backups, restores and transfer now explicitly stop before data writes because the recovery directory/marker protocol remains unsupported. Existing Unix directory fsync calls have not been converted to a silent Windows success. A Windows release cannot be claimed from these primitives. Physical power loss and independent clean-machine recovery require their own candidate evidence.

Microsoft documents [exclusive byte-range locking and process-exit release](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex), [descriptor-based volume/file identity](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-getfileinformationbyhandle), and [FlushFileBuffers access requirements](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-flushfilebuffers). No public API guarantee for portable directory fsync is inferred from those file guarantees.
