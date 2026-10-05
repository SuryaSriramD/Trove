# Windows and Linux protected account storage

C7/C8 now have source implementations for the shared Sign in with ChatGPT
credential backend. This package does not qualify either operating system for
release or enable provider process containment on those platforms. The existing
macOS Keychain backend remains selected on macOS.

## Windows 11 x64

Credential records use current-user DPAPI with `CRYPTPROTECT_UI_FORBIDDEN`, never
machine-wide protection. A root-and-registration-specific entropy value binds
the encrypted record to its host storage location and registration. The existing
credential payload also binds host, vendor subject and client identity. Records
remain limited to 128 KiB and are never returned to the renderer.

DPAPI was selected because the accepted OAuth record size exceeds Credential
Manager's 2,560-byte blob limit. Only ciphertext is written to private host
storage outside every workspace. There is no plaintext fallback or credential
export. The native API, its output allocation, and temporary cleartext buffers
are handled directly; shell commands and PowerShell are not used.

Storage creation supplies a protected Windows DACL granting the current user and
SYSTEM access. Reads and lock acquisition verify the actual opened handle's
owner, DACL, object type and link count. Reparse points, symlink/junction ancestors,
network paths, inherited or broader ACLs, and non-regular credential files are
rejected. `LockFileEx` prevents another app instance from owning the same account
registrations. Metadata and encrypted records use private temporary files,
flushed writes and `MoveFileEx` replacement, rather than POSIX permission or
directory-sync assumptions.

## Ubuntu 24.04 x64

The Linux adapter uses the user's local `/run/user/<uid>/bus`, verifies its private
parent directory and socket ownership, and checks the connected peer UID. It does
not accept arbitrary D-Bus addresses, TCP buses, cookie authentication, or shell
activation. EXTERNAL authentication is followed by selection of the Secret
Service's unique owner, which remains pinned for all calls and prompt signals.

The backend negotiates only the Secret Service specified
`dh-ietf1024-sha256-aes128-cbc-pkcs7` session. It validates peer keys, derives the
128-bit key with HKDF-SHA256, encrypts with a fresh IV, validates bounded ciphertext
and padding, and checks the returned session identity. The algorithm is the
interoperability format of the local desktop service; it is not used for network
transport. Refusal of encrypted sessions makes the backend unavailable.

A stable application namespace and registration ID select each keyring item.
The adapter refuses duplicates, updates the existing item during refresh, creates
a default collection when needed, and keeps unrelated items outside the operation.
Unlock prompts belong to the pinned service and exact prompt object. Dismissal
and cancellation fail the request; cancellation also dismisses a prompt that is
still starting. Operations have a 45-second deadline, and cleanup is bounded.
No application file stores a plaintext credential.

Opening the manager checks capability through an empty encrypted session, without
reading stored items or showing a keyring prompt. If the local service is missing
or incompatible, the renderer receives a fixed, understandable availability
message. Only locally constructed `auth.Error` messages are exposed; raw OS,
D-Bus and credential-store errors use a generic message.

## Verification and remaining evidence

The [evidence manifest](../../evidence/completion/platform-credentials/manifest.json)
binds the checked source and target test binaries. Eighteen auth tests and two
native adapter boundary tests passed with the race detector on the development
Mac. A private real D-Bus daemon and a synthetic Secret Service exercise encrypted
wire calls, account isolation, rotation, deletion, duplicate rejection, incorrect
session refusal, first collection creation, native prompt dismissal, cancellation
and encrypted-only negotiation. Independent HMAC/HKDF and OpenSSL AES-CBC vectors
check the cryptographic encoding separately from the synthetic service.

The fixture initially used a Unix socket path longer than the host's limit and
registered new test methods while serving requests; both fixture defects were
corrected before the final passing race run. Tests also found two adapter issues:
activation must not require an already-running service to have an activation
file, and cancellation during the prompt-start call must dismiss that prompt.
Both are corrected and covered by the final regressions.

Windows amd64 and Linux amd64 auth test binaries compile. They were **not run**
on either target OS. Windows tests for DPAPI round trips, tamper/copy rejection,
ACLs, locks, metadata replacement and reparse refusal are included for that
qualification. Real Windows user-profile/DPAPI behavior, cross-user refusal,
ACL and junction behavior, crash/reboot durability, and the actual Ubuntu
GNOME/keyring unlock and cancellation flows remain unqualified. Platform provider
containment, packaging, native workers and the full release matrix remain open.
No real account, desktop keyring or stored user credential was accessed for these
checks.

## Primary API references

Checked on 5 October 2026:

- [Microsoft DPAPI protection](https://learn.microsoft.com/en-us/windows/win32/api/dpapi/nf-dpapi-cryptprotectdata) and [Credential Manager record limits](https://learn.microsoft.com/en-us/windows/win32/api/wincred/ns-wincred-credentialw).
- [Windows security descriptor creation](https://learn.microsoft.com/en-us/windows/win32/secauthz/creating-a-security-descriptor-for-a-new-object-in-c--) and [exclusive file locks](https://learn.microsoft.com/en-us/windows/win32/api/fileapi/nf-fileapi-lockfileex).
- [Secret Service encrypted algorithm](https://specifications.freedesktop.org/secret-service/latest/ch07s03.html), [service methods](https://specifications.freedesktop.org/secret-service/latest/org.freedesktop.Secret.Service.html), [collection methods](https://specifications.freedesktop.org/secret-service/latest/org.freedesktop.Secret.Collection.html) and [native prompt lifecycle](https://specifications.freedesktop.org/secret-service/latest/org.freedesktop.Secret.Prompt.html).
