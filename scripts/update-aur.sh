#!/usr/bin/env bash

set -euo pipefail

usage() {
  cat <<'EOF'
用法:
  scripts/update-aur.sh --tag <tag> --aur-dir <dir> [options]

选项:
  --tag <tag>                  Git tag，例如 v0.1.0
  --aur-dir <dir>              AUR 仓库目录
  --pkgname <name>             包名，默认 cctui
  --pkgdesc <desc>             pkgdesc
  --source-repo-url <url>      上游仓库 URL，默认 https://github.com/manateelazycat/cctui
  --archive-url <url>          覆盖源码 tarball URL
  --archive-dir-name <name>    覆盖源码解压目录名
  --validate                   用 makepkg 校验 .SRCINFO
  -h, --help                   显示帮助
EOF
}

pkgname="cctui"
pkgdesc="Terminal UI tool to manage and switch Claude, Codex, and Gemini providers"
source_repo_url="https://github.com/manateelazycat/cctui"
tag=""
aur_dir=""
archive_url=""
archive_dir_name=""
validate="false"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --tag)
      tag="${2:-}"
      shift 2
      ;;
    --aur-dir)
      aur_dir="${2:-}"
      shift 2
      ;;
    --pkgname)
      pkgname="${2:-}"
      shift 2
      ;;
    --pkgdesc)
      pkgdesc="${2:-}"
      shift 2
      ;;
    --source-repo-url)
      source_repo_url="${2:-}"
      shift 2
      ;;
    --archive-url)
      archive_url="${2:-}"
      shift 2
      ;;
    --archive-dir-name)
      archive_dir_name="${2:-}"
      shift 2
      ;;
    --validate)
      validate="true"
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "未知参数: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [[ -z "$tag" || -z "$aur_dir" ]]; then
  usage >&2
  exit 1
fi

tag="${tag#refs/tags/}"
pkgver="${tag#v}"
pkgrel="1"

if [[ -z "$archive_url" ]]; then
  archive_url="${source_repo_url}/archive/refs/tags/${tag}.tar.gz"
fi

if [[ -z "$archive_dir_name" ]]; then
  archive_dir_name="${pkgname}-${pkgver}"
fi

mkdir -p "$aur_dir"

tmp_archive="$(mktemp)"
tmp_srcinfo="$(mktemp)"
cleanup() {
  rm -f "$tmp_archive" "$tmp_srcinfo"
}
trap cleanup EXIT

curl -fL "$archive_url" -o "$tmp_archive"
sha256="$(sha256sum "$tmp_archive" | awk '{print $1}')"
source_name="${pkgname}-${pkgver}.tar.gz"

cat > "${aur_dir}/PKGBUILD" <<EOF
pkgname=${pkgname}
pkgver=${pkgver}
pkgrel=${pkgrel}
pkgdesc='${pkgdesc}'
arch=('x86_64' 'aarch64')
url='${source_repo_url}'
license=('custom')
makedepends=('go')
source=('${source_name}::${archive_url}')
sha256sums=('${sha256}')

build() {
  cd "\${srcdir}/${archive_dir_name}"
  export CGO_ENABLED=0
  export GOFLAGS='-buildmode=pie -trimpath -mod=readonly -modcacherw'
  go build -ldflags='-s -w' -o ${pkgname} .
}

package() {
  cd "\${srcdir}/${archive_dir_name}"
  install -Dm755 ${pkgname} "\${pkgdir}/usr/bin/${pkgname}"
  install -Dm644 README.md "\${pkgdir}/usr/share/doc/${pkgname}/README.md"
}
EOF

cat > "${aur_dir}/.SRCINFO" <<EOF
pkgbase = ${pkgname}
	pkgdesc = ${pkgdesc}
	pkgver = ${pkgver}
	pkgrel = ${pkgrel}
	url = ${source_repo_url}
	arch = x86_64
	arch = aarch64
	license = custom
	makedepends = go
	source = ${source_name}::${archive_url}
	sha256sums = ${sha256}

pkgname = ${pkgname}
EOF

if [[ "$validate" == "true" ]]; then
  if ! command -v makepkg >/dev/null 2>&1; then
    echo "makepkg 不存在，无法执行 --validate" >&2
    exit 1
  fi

  (
    cd "$aur_dir"
    makepkg --printsrcinfo > "$tmp_srcinfo"
  )

  if ! diff -u "$tmp_srcinfo" "${aur_dir}/.SRCINFO"; then
    echo ".SRCINFO 与 makepkg --printsrcinfo 输出不一致" >&2
    exit 1
  fi
fi

echo "已更新 AUR 文件:"
echo "  ${aur_dir}/PKGBUILD"
echo "  ${aur_dir}/.SRCINFO"
echo "pkgver=${pkgver}"
echo "sha256=${sha256}"
