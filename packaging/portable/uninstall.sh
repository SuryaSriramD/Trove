#!/bin/sh
set -eu
[ "$(uname -s)" = Linux ] || exit 1
[ "$(id -u)" -ne 0 ] || { echo 'Uninstall as the installing desktop user.' >&2; exit 1; }
data=${XDG_DATA_HOME:-"$HOME/.local/share"}
case "$data" in /*) ;; *) exit 1;; esac
app_dir="$data/Trove/app"
desktop="$data/applications/local.trove.desktop.desktop"
path=$app_dir
while [ "$path" != / ]; do [ ! -L "$path" ] || exit 1; path=$(dirname -- "$path"); done
path=$desktop
while [ "$path" != / ]; do [ ! -L "$path" ] || exit 1; path=$(dirname -- "$path"); done
[ -f "$app_dir/.trove-install" ] && [ -f "$app_dir/desktop-manifest.json" ] || { echo 'No owned Trove installation found.' >&2; exit 1; }
[ -z "$(find "$app_dir" ! -type f ! -type d -print)" ] || { echo 'Installation contains symlinks; preserved for inspection.' >&2; exit 1; }
expected=$(sha256sum "$app_dir/desktop-manifest.json" | cut -d ' ' -f 1)
actual=$(cat "$app_dir/.trove-install")
wanted=$(printf 'Trove local.trove.desktop install-v1\n%s\n%s' "$expected" "$app_dir")
[ "$actual" = "$wanted" ] || { echo 'Installation ownership differs; nothing removed.' >&2; exit 1; }
# Check every checksum path before passing it to any filesystem tool.
while IFS= read -r entry; do
 checksum=${entry%% *}
 relative=${entry#*  }
 [ "${#checksum}" -eq 64 ] && [ "$entry" = "$checksum  $relative" ] || exit 1
 case "$checksum" in *[!a-f0-9]*) exit 1;; esac
 case "$relative" in ''|/*|../*|*/../*|*/..|..|./*|*/./*|*/.|*\\*) exit 1;; esac
 printf '%s\n' "$relative" | LC_ALL=C grep -Eq '^[A-Za-z0-9_@+.,() /=-]+$' || exit 1
done < "$app_dir/files.sha256"
(cd "$app_dir" && sha256sum --check --strict --status files.sha256) || { echo 'Installation changed; preserved for inspection.' >&2; exit 1; }
# Refuse unexpected user files before deleting the owned application directory.
find "$app_dir" -type f | while IFS= read -r file; do
 relative=${file#"$app_dir/"}
 case "$relative" in .trove-install|.trove-registration|files.sha256) continue;; esac
 grep -F "  $relative" "$app_dir/files.sha256" | grep -Fx "$(sha256sum "$file" | cut -d ' ' -f 1)  $relative" >/dev/null || exit 1
done
find "$app_dir" -mindepth 1 -type d | while IFS= read -r directory; do
 relative=${directory#"$app_dir/"}
 awk -v prefix="$relative/" 'index(substr($0,67),prefix)==1 { found=1 } END { exit !found }' "$app_dir/files.sha256" || { echo 'Unexpected directory preserved.' >&2; exit 1; }
done
if [ -e "$desktop" ]; then
 cmp -s "$desktop" "$app_dir/.trove-registration" || { echo 'Desktop registration belongs to another installation; preserved.' >&2; exit 1; }
 rm -- "$desktop"
fi
rm -rf -- "$app_dir"
update-desktop-database "$data/applications"
printf 'Trove application removed. Workspaces, model caches, credentials and settings preserved.\n'
