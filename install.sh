#!/usr/bin/env bash
# cctui 交互式安装 / 卸载脚本
# 支持 Arch Linux / Debian 系 / 通用 Linux / macOS / FreeBSD
set -euo pipefail

# ── 颜色（ANSI-C 语法确保转义正确）─────────────────────────────────
if [[ -t 1 ]]; then
  RED=$'\033[0;31m';   GREEN=$'\033[0;32m'; YELLOW=$'\033[0;33m'
  CYAN=$'\033[0;36m';  BOLD=$'\033[1m';      DIM=$'\033[2m'
  NC=$'\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; CYAN=''; BOLD=''; DIM=''; NC=''
fi

info()  { printf "${CYAN}[INFO]${NC}  %s\n" "$*"; }
ok()    { printf "${GREEN}[ OK ]${NC}  %s\n" "$*"; }
warn()  { printf "${YELLOW}[WARN]${NC}  %s\n" "$*"; }
err()   { printf "${RED}[ERR!]${NC}  %s\n" "$*" >&2; }
die()   { err "$*"; exit 1; }

# 清空终端输入缓冲（滚轮/误触产生的转义序列）
drain_stdin() {
  if [[ -t 0 ]]; then
    local saved_stty
    saved_stty="$(stty -g)"
    stty -icanon min 0 time 0 2>/dev/null
    while read -r -t 0.01 _drain 2>/dev/null; do :; done
    stty "$saved_stty" 2>/dev/null
  fi
}

# ── 依赖检查 ─────────────────────────────────────────────────────────
require_cmd() {
  command -v "$1" &>/dev/null || die "缺少依赖: $1。请先安装后重试。"
}

# ── JSON 解析（不依赖 jq）───────────────────────────────────────────
parse_json_tag() {
  local key="$1"
  sed -n 's/.*"'"$key"'"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1
}

# ── 系统检测 ─────────────────────────────────────────────────────────
detect_os() {
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64|amd64)   ARCH="amd64" ;;
    aarch64|arm64)  ARCH="arm64" ;;
    armv7*|armhf)   ARCH="armv7" ;;
    i?86)           ARCH="386"   ;;
    *)              die "不支持的架构: $ARCH" ;;
  esac

  OS="$(uname -s)"
  case "$OS" in
    Linux)   OS="linux" ;;
    Darwin)  OS="darwin" ;;
    FreeBSD) OS="freebsd" ;;
    *)       die "不支持的操作系统: $OS" ;;
  esac

  DISTRO="unknown"
  PKG_MGR=""
  if [[ -f /etc/os-release ]]; then
    . /etc/os-release
    case "${ID:-}" in
      arch|manjaro|endeavouros|cachyos)
        DISTRO="arch"
        PKG_MGR="pacman"
        ;;
      debian|ubuntu|linuxmint|pop|deepin|zorin|kali)
        DISTRO="debian"
        PKG_MGR="apt"
        ;;
      fedora|rhel|centos|rocky|alma)
        DISTRO="rhel"
        PKG_MGR="dnf"
        ;;
      opensuse*|sles)
        DISTRO="suse"
        PKG_MGR="zypper"
        ;;
      alpine)
        DISTRO="alpine"
        PKG_MGR="apk"
        ;;
      void)
        DISTRO="void"
        PKG_MGR="xbps"
        ;;
      *)
        DISTRO="${ID:-unknown}"
        ;;
    esac
  fi
}

# ── 查找已安装的 cctui ──────────────────────────────────────────────
find_installed() {
  local search_dirs=("${HOME}/.local/bin" "/usr/local/bin" "/usr/bin")
  for dir in "${search_dirs[@]}"; do
    if [[ -f "${dir}/cctui" ]]; then
      echo "${dir}/cctui"
      return 0
    fi
  done
  return 1
}

# ── 卸载 ─────────────────────────────────────────────────────────────
do_uninstall() {
  local target
  if ! target="$(find_installed)"; then
    warn "未找到已安装的 cctui"
    return 0
  fi

  info "找到 cctui: ${target}"
  drain_stdin
  printf "确认卸载？[Y/n] "
  read -r reply </dev/tty 2>/dev/null || reply="y"
  case "$reply" in
    [nN]*) info "已取消"; return 0 ;;
  esac

  rm -f "$target" "${target}.bak"
  ok "已卸载 cctui"
  info "配置数据 ~/.cc-switch/ 未删除，如需清理请执行: rm -rf ~/.cc-switch"
}

# ── GitHub 请求 ──────────────────────────────────────────────────────
REPO="${REPO:-AXY520/cctui}"
MIRROR="${MIRROR:-}"
VERSION=""

github_api() {
  curl -fsSL -H "Accept: application/vnd.github.v3+json" \
    "https://api.github.com${1}" 2>/dev/null
}

github_download() {
  local path="$1" output="$2"
  local url="https://github.com${path}"
  [[ -n "$MIRROR" ]] && url="${MIRROR}/${url}"
  curl -fSL --progress-bar -o "$output" "$url"
}

# ── 获取最新版本 ─────────────────────────────────────────────────────
get_latest_version() {
  local ver
  ver="$(github_api "/repos/${REPO}/releases/latest" | parse_json_tag "tag_name" || true)"
  if [[ -z "$ver" ]]; then
    ver="$(github_api "/repos/${REPO}/tags?per_page=1" | parse_json_tag "name" || true)"
  fi
  echo "$ver"
}

# ── 下载预编译二进制 ─────────────────────────────────────────────────
download_binary() {
  local tag="$1" tmpdir="$2"
  local archive_name="cctui-${OS}-${ARCH}.tar.gz"
  local dest="${tmpdir}/${archive_name}"
  local download_path="/${REPO}/releases/download/${tag}/${archive_name}"

  info "正在下载: ${archive_name}"
  if github_download "$download_path" "$dest"; then
    tar xzf "$dest" -C "$tmpdir" 2>/dev/null || return 1
    local found
    found="$(find "$tmpdir" -name 'cctui' -type f | head -n1)"
    if [[ -n "$found" ]]; then
      cp "$found" "${tmpdir}/cctui"
      chmod +x "${tmpdir}/cctui"
      return 0
    fi
  fi
  return 1
}

# ── 从源码构建 ───────────────────────────────────────────────────────
build_from_source() {
  local tag="$1" tmpdir="$2"

  require_cmd go
  require_cmd git

  local go_ver
  go_ver="$(go version | sed 's/.*go\([0-9]*\.[0-9]*\).*/\1/')"
  local go_major="${go_ver%%.*}"
  local go_minor="${go_ver##*.}"
  if (( go_major < 1 || (go_major == 1 && go_minor < 21) )); then
    die "Go 版本过低 (${go_ver})，需要 1.21+。请升级: https://go.dev/dl/"
  fi

  local src_dir="${tmpdir}/src"
  mkdir -p "$src_dir"

  if [[ "$tag" == "main" ]]; then
    info "正在克隆仓库..."
    git clone --depth 1 "https://github.com/${REPO}.git" "$src_dir" 2>/dev/null \
      || die "克隆失败，请检查网络"
  else
    info "正在下载源码..."
    local tar_url="https://github.com/${REPO}/archive/refs/tags/${tag}.tar.gz"
    [[ -n "$MIRROR" ]] && tar_url="${MIRROR}/${tar_url}"
    curl -fSL --progress-bar -o "${tmpdir}/src.tar.gz" "$tar_url" || die "源码下载失败"
    tar xzf "${tmpdir}/src.tar.gz" -C "$src_dir" --strip-components=1
  fi

  info "正在编译 (CGO_ENABLED=0)..."
  (
    cd "$src_dir"
    export CGO_ENABLED=0
    export GOFLAGS="-buildmode=pie -trimpath -mod=readonly"
    go build -ldflags='-s -w' -o "${tmpdir}/cctui" . 2>&1
  ) || die "编译失败"
  ok "编译完成"
}

# ── 安装到目录 ───────────────────────────────────────────────────────
install_binary() {
  local src="$1"
  local install_dir=""

  # 选择安装目录
  if [[ -w "/usr/local/bin" ]]; then
    install_dir="/usr/local/bin"
  elif [[ -d "${HOME}/.local/bin" ]] && [[ -w "${HOME}/.local/bin" ]]; then
    install_dir="${HOME}/.local/bin"
  else
    install_dir="${HOME}/.local/bin"
    mkdir -p "$install_dir"
  fi

  info "安装目录: ${install_dir}"

  # 备份旧版本
  if [[ -f "${install_dir}/cctui" ]]; then
    cp "${install_dir}/cctui" "${install_dir}/cctui.bak"
    info "已备份旧版本"
  fi

  cp "$src" "${install_dir}/cctui"
  chmod +x "${install_dir}/cctui"

  ok "安装完成！"

  # PATH 检查
  if command -v cctui &>/dev/null; then
    ok "cctui 已在 PATH 中"
  else
    warn "cctui 不在 PATH 中"
    local shell_name profile
    shell_name="$(basename "${SHELL:-bash}")"
    case "$shell_name" in
      zsh)  profile="~/.zshrc" ;;
      bash) profile="~/.bashrc" ;;
      fish) profile="~/.config/fish/config.fish" ;;
      *)    profile="~/.profile" ;;
    esac
    printf "\n请将以下内容添加到 %s:\n\n" "$profile"
    if [[ "$shell_name" == "fish" ]]; then
      printf "  ${CYAN}set -gx PATH %s \$PATH${NC}\n" "$install_dir"
    else
      printf "  ${CYAN}export PATH=\"%s:\$PATH\"${NC}\n" "$install_dir"
    fi
  fi
}

# ── 执行安装 ─────────────────────────────────────────────────────────
do_install() {
  local version="$1"

  # 检查重复安装
  if command -v cctui &>/dev/null; then
    local installed_ver
    installed_ver="$(cctui --version 2>/dev/null | sed 's/[^0-9]*\([0-9]*\.[0-9]*\.[0-9]*\).*/\1/' | head -n1 || true)"
    local target_clean="${version#v}"
    if [[ "$installed_ver" == "$target_clean" ]]; then
      ok "cctui ${installed_ver} 已是最新，无需重复安装"
      return 0
    fi
    if [[ -n "$installed_ver" ]]; then
      info "当前已安装: ${installed_ver}，将升级到 ${target_clean}"
    fi
  fi

  local tmpdir
  tmpdir="$(mktemp -d)"
  trap "rm -rf '$tmpdir'" RETURN

  # 优先预编译，失败回退源码
  if download_binary "$version" "$tmpdir"; then
    ok "预编译二进制下载成功"
    install_binary "${tmpdir}/cctui"
  else
    warn "预编译二进制不可用 (系统: ${OS}/${ARCH})"
    drain_stdin
    printf "是否从源码编译？需要 ${BOLD}Go 1.21+${NC} 和 ${BOLD}git${NC} [y/N] "
    local reply
    if ! read -r reply </dev/tty 2>/dev/null; then
      reply="y"
      printf "y (自动确认)\n"
    fi
    case "$reply" in
      [yY]*) ;;
      *) info "已取消"; return 0 ;;
    esac
    build_from_source "$version" "$tmpdir"
    install_binary "${tmpdir}/cctui"
  fi
}

# ── 信息面板 ─────────────────────────────────────────────────────────
show_info() {
  local latest_ver="${1:-unknown}"
  local installed_ver="未安装"
  local installed_path=""
  if command -v cctui &>/dev/null; then
    installed_ver="$(cctui --version 2>/dev/null | sed 's/[^0-9]*\([0-9]*\.[0-9]*\.[0-9]*\).*/\1/' | head -n1 || true)"
    installed_ver="${installed_ver:-已安装(版本未知)}"
    installed_path="$(command -v cctui)"
  elif installed_path="$(find_installed 2>/dev/null)"; then
    installed_ver="已安装(不在 PATH)"
  fi

  local distro_display="${DISTRO}"
  case "$DISTRO" in
    arch)   distro_display="Arch Linux" ;;
    debian) distro_display="Debian/Ubuntu" ;;
    rhel)   distro_display="Fedora/RHEL" ;;
    suse)   distro_display="openSUSE" ;;
  esac

  printf "\n"
  printf "  ${BOLD}╔══════════════════════════════════════╗${NC}\n"
  printf "  ${BOLD}║${NC}         ${CYAN}${BOLD}cctui 安装管理器${NC}             ${BOLD}║${NC}\n"
  printf "  ${BOLD}╠══════════════════════════════════════╣${NC}\n"
  printf "  ${BOLD}║${NC}  系统:     ${BOLD}%-24s${NC} ${BOLD}║${NC}\n" "${OS}/${ARCH}"
  printf "  ${BOLD}║${NC}  发行版:   ${BOLD}%-24s${NC} ${BOLD}║${NC}\n" "${distro_display}"
  printf "  ${BOLD}║${NC}  已安装:   ${BOLD}%-24s${NC} ${BOLD}║${NC}\n" "${installed_ver}"
  printf "  ${BOLD}║${NC}  最新版:   ${BOLD}%-24s${NC} ${BOLD}║${NC}\n" "${latest_ver}"
  [[ -n "$installed_path" ]] && \
  printf "  ${BOLD}║${NC}  路径:     ${DIM}%-24s${NC} ${BOLD}║${NC}\n" "${installed_path}"
  printf "  ${BOLD}╚══════════════════════════════════════╝${NC}\n"
  printf "\n"
}

# ── 主菜单 ───────────────────────────────────────────────────────────
main() {
  require_cmd curl
  require_cmd uname

  detect_os

  local latest_version
  latest_version="$(get_latest_version)"
  [[ -z "$latest_version" ]] && latest_version="无法获取"

  show_info "$latest_version"

  if [[ "$latest_version" == "无法获取" ]]; then
    warn "无法获取最新版本，源码编译模式需要网络"
    printf "\n"
    printf "  ${BOLD}[1]${NC} 从源码编译安装 (需要 Go 1.21+)\n"
    printf "  ${BOLD}[2]${NC} 卸载 cctui\n"
    printf "  ${BOLD}[0]${NC} 退出\n"
  else
    printf "\n"
    printf "  ${BOLD}[1]${NC} 安装 / 升级到 ${latest_version}\n"
    printf "  ${BOLD}[2]${NC} 卸载 cctui\n"
    printf "  ${BOLD}[0]${NC} 退出\n"
  fi

  drain_stdin
  printf "\n请选择操作 [0-2]: "
  local choice
  if ! read -r choice </dev/tty 2>/dev/null; then
    # 管道模式：自动安装
    choice="1"
    printf "1 (自动选择安装)\n"
  fi

  case "$choice" in
    1)
      printf "\n"
      if [[ -n "$latest_version" ]]; then
        do_install "$latest_version"
      else
        do_install "main"
      fi
      ;;
    2)
      printf "\n"
      do_uninstall
      ;;
    0)
      info "已退出"
      exit 0
      ;;
    *)
      die "无效选择: $choice"
      ;;
  esac

  local action_result="done"
  printf "\n${GREEN}${BOLD}\U0001f389 完成！${NC}\n"
  if command -v cctui &>/dev/null; then
    printf "运行 ${CYAN}cctui${NC} 启动 TUI 界面\n"
  fi
  printf "文档: https://github.com/%s\n" "$REPO"
}

# ── 支持命令行参数覆盖（跳过菜单）──────────────────────────────────
ACTION="${1:-}"
case "$ACTION" in
  --install|-i)
    shift || true
    VERSION="${1:-}"
    require_cmd curl; require_cmd uname
    detect_os
    [[ -z "$VERSION" ]] && VERSION="$(get_latest_version)"
    [[ -z "$VERSION" ]] && die "无法获取版本"
    do_install "$VERSION"
    ;;
  --uninstall|-u)
    do_uninstall
    ;;
  --help|-h)
    cat <<'EOF'
cctui 安装脚本

用法:
  bash install.sh            # 交互式菜单
  bash install.sh -i [ver]   # 直接安装 (默认最新版)
  bash install.sh -u         # 直接卸载

环境变量:
  REPO    覆盖仓库地址 (默认 AXY520/cctui)
  MIRROR  GitHub 镜像前缀，例如 https://ghfast.top

示例:
  bash install.sh                           # 菜单模式
  bash install.sh -i v0.2.0                 # 安装指定版本
  MIRROR=https://ghfast.top bash install.sh # 使用镜像
EOF
    ;;
  "")
    main
    ;;
  *)
    die "未知参数: $ACTION（使用 --help 查看帮助）"
    ;;
esac
