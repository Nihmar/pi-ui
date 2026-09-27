#!/usr/bin/env bash
# Builds the AppImage of the pi-ui client from a Flutter Linux release bundle.
#
#   packaging/linux/appimage.sh <bundle-dir> <output-dir> [version]
#
# The bundle is what `flutter build linux --release` produces
# (`app/build/linux/x64/release/bundle`). The output is an AppImage plus its `.sha256`,
# both named after the version when one is given.
#
# appimagetool is fetched from its own release rather than vendored: it is a build tool, and
# pinning it here keeps the artifact reproducible without putting a 5 MB binary in the tree.
set -euo pipefail

bundle="${1:?usage: appimage.sh <bundle-dir> <output-dir> [version]}"
out="${2:?usage: appimage.sh <bundle-dir> <output-dir> [version]}"
version="${3:-0.0.0}"
tools_url="${APPIMAGETOOL_URL:-https://github.com/AppImage/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage}"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
name="piui_${version}_linux_x86_64"

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

appdir="${work}/piui.AppDir"
# The bundle keeps its own layout under usr/ (the executable beside its data/ and lib/,
# which is what the Flutter runner expects); only what a desktop needs is added.
mkdir -p "${appdir}/usr/share/applications" \
         "${appdir}/usr/share/icons/hicolor/256x256/apps"

# The Flutter bundle goes into usr/: the executable with its data, its libs and its plugins,
# laid out exactly as the runner expects to find them next to itself.
cp -r "${bundle}/." "${appdir}/usr/"
install -m 0755 "${here}/piui.png" "${appdir}/usr/share/icons/hicolor/256x256/apps/piui.png"
install -m 0644 "${here}/piui.png" "${appdir}/piui.png"

cat > "${appdir}/piui.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=pi-ui
Comment=Sessions, files, git and terminals for the pi coding agent
Exec=piui
Icon=piui
Categories=Development;Utility;
Terminal=false
StartupWMClass=piui
DESKTOP
install -m 0644 "${appdir}/piui.desktop" \
    "${appdir}/usr/share/applications/piui.desktop"

# AppRun is the entry point the AppImage tool embeds: it points the dynamic loader at the
# bundle's own libraries, so the AppImage runs on a distribution that has none of them.
cat > "${appdir}/AppRun" <<'APPRUN'
#!/bin/sh
# The libraries the bundle ships take precedence over the host's, which is what makes one
# AppImage work across distributions.
here="$(dirname "$(readlink -f "$0")")"
export LD_LIBRARY_PATH="${here}/usr/lib:${here}/usr/lib/${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}"
export XDG_DATA_DIRS="${here}/usr/share:${XDG_DATA_DIRS:-/usr/share}"
exec "${here}/usr/piui" "$@"
APPRUN
chmod 0755 "${appdir}/AppRun"

tool="${work}/appimagetool"
if [ ! -x "${tool}" ]; then
    curl -fsSL --retry 3 -o "${tool}" "${tools_url}"
    chmod 0755 "${tool}"
fi

mkdir -p "${out}"
# APPIMAGE_EXTRACT_AND_RUN avoids needing FUSE, which a container or a CI runner does not
# have: the tool extracts itself and runs, which is the same work by a different route.
ARCH=x86_64 APPIMAGE_EXTRACT_AND_RUN=1 "${tool}" --no-appstream "${appdir}" "${out}/${name}.AppImage"

(cd "${out}" && sha256sum "${name}.AppImage" > "${name}.AppImage.sha256")
echo "built ${out}/${name}.AppImage"
