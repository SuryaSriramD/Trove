#!/bin/sh
set -eu
[ "$(uname -s)" = Linux ] && [ "$(uname -m)" = x86_64 ] || { echo 'Requires Ubuntu 24.04 x64.' >&2; exit 1; }
. /etc/os-release
[ "$ID" = ubuntu ] && [ "$VERSION_ID" = 24.04 ] || exit 1
[ "$(id -u)" -ne 0 ] || { echo 'Install as your desktop user, not root.' >&2; exit 1; }
source_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd -P)
data=${XDG_DATA_HOME:-"$HOME/.local/share"}
case "$data" in /*) ;; *) echo 'XDG_DATA_HOME must be absolute.' >&2; exit 1;; esac
app_dir="$data/Trove/app"
desktop="$data/applications/local.trove.desktop.desktop"
# Refuse links in every existing parent; never traverse a user-selected link.
check_parents() {
 path=$1
 while [ "$path" != / ]; do
  [ ! -L "$path" ] || { echo 'Refusing symlink install path.' >&2; exit 1; }
  path=$(dirname -- "$path")
 done
}
check_parents "$app_dir"; check_parents "$desktop"
[ ! -e "$app_dir" ] && [ ! -e "$desktop" ] || { echo 'Existing installation or registration preserved. Uninstall its owning installation first.' >&2; exit 1; }
if printf '%s' "$data" | LC_ALL=C grep -q '[[:cntrl:]]'; then exit 1; fi
for tool in sha256sum update-desktop-database xdg-mime; do command -v "$tool" >/dev/null || { echo "Missing OS prerequisite: $tool" >&2; exit 1; }; done
[ -z "$(xdg-mime query default x-scheme-handler/trove)" ] || { echo 'Existing trove URL handler preserved.' >&2; exit 1; }
[ -z "$(find "$source_dir" -type l -print)" ] || { echo 'Package contains symlinks.' >&2; exit 1; }
[ -z "$(find "$source_dir" ! -type f ! -type d -print)" ] || exit 1
# Check every checksum path before passing it to any filesystem tool.
while IFS= read -r entry; do
 checksum=${entry%% *}
 relative=${entry#*  }
 [ "${#checksum}" -eq 64 ] && [ "$entry" = "$checksum  $relative" ] || exit 1
 case "$checksum" in *[!a-f0-9]*) exit 1;; esac
 case "$relative" in ''|/*|../*|*/../*|*/..|..|./*|*/./*|*/.|*\\*) exit 1;; esac
 printf '%s\n' "$relative" | LC_ALL=C grep -Eq '^[A-Za-z0-9_@+.,() /=-]+$' || exit 1
done < "$source_dir/files.sha256"
(cd "$source_dir" && sha256sum --check --strict --status files.sha256) || { echo 'Package hash verification failed.' >&2; exit 1; }
grep -Fx '  "appId": "local.trove.desktop",' "$source_dir/desktop-manifest.json" >/dev/null
grep -Fx '  "target": "linux-amd64",' "$source_dir/desktop-manifest.json" >/dev/null
# Reject extra unverified files instead of copying them into the installation.
find "$source_dir" -type f | while IFS= read -r file; do
 relative=${file#"$source_dir/"}
 [ "$relative" = files.sha256 ] && continue
 grep -Fx "$(sha256sum "$file" | cut -d ' ' -f 1)  $relative" "$source_dir/files.sha256" >/dev/null || exit 1
done
# WebKitGTK is an OS prerequisite. Installation never changes apt packages.
expected=$(sed -n 's/^  "requiredWebKitGTKPackage": "\([^"]*\)",*$/\1/p' "$source_dir/desktop-manifest.json")
[ -n "$expected" ] && [ "$(dpkg-query -W -f='${Version}' libwebkit2gtk-4.1-0)" = "$expected" ] || { echo 'WebKitGTK differs from this candidate; install its reviewed prerequisite set or rebuild and qualify the updated set.' >&2; exit 1; }
reviewed_hash=$(sha256sum "$source_dir/desktop-manifest.json" | cut -d ' ' -f 1)
mkdir -p -- "$data/Trove" "$data/applications"
stage=$(mktemp -d "$data/Trove/.install-XXXXXXXX")
trap 'if [ -n "$stage" ]; then rm -rf -- "$stage"; fi' EXIT HUP INT TERM
cp -R "$source_dir/." "$stage/"
[ -z "$(find "$stage" ! -type f ! -type d -print)" ] || exit 1
(cd "$stage" && sha256sum --check --strict --status files.sha256)
hash=$(sha256sum "$stage/desktop-manifest.json" | cut -d ' ' -f 1)
[ "$hash" = "$reviewed_hash" ] || { echo 'Package changed during installation.' >&2; exit 1; }
printf 'Trove local.trove.desktop install-v1\n%s\n%s\n' "$hash" "$app_dir" > "$stage/.trove-install"
# Desktop Entry Exec escaping is stricter than shell quoting.
escaped=$(printf '%s' "$app_dir/bin/Trove" | sed 's/\\/\\\\/g; s/"/\\"/g; s/`/\\`/g; s/\$/\\$/g; s/%/%%/g')
cat > "$stage/.trove-registration" <<ENTRY
[Desktop Entry]
Type=Application
Name=Trove
Exec="$escaped" %u
Terminal=false
Categories=Office;
MimeType=x-scheme-handler/trove;
X-Trove-Install=$hash
ENTRY
[ ! -e "$app_dir" ] && [ ! -L "$app_dir" ] && [ ! -e "$desktop" ] && [ ! -L "$desktop" ] || exit 1
mv -- "$stage" "$app_dir"; stage=''
# noclobber protects an existing registration, including a race with another app.
(set -C; cat "$app_dir/.trove-registration" > "$desktop")
update-desktop-database "$data/applications"
printf 'Installed %s. Workspaces and model caches remain separate.\n' "$app_dir"
