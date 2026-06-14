#!/usr/bin/env bash
# cctui 一键安装 / 卸载脚本
# 支持 Linux / macOS / FreeBSD / Windows(Git Bash)
# 优先下载 GitHub Releases 预编译二进制，无预编译时从源码构建
set -euo pipefail

# ── 颜色（非 TTY 自动关闭）──────────────────────────────────────────
if [[ -t 1 ]]; then
  RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[0;33m'
  CYAN='\033[0;36m'; BOLD='\033[1m'; NC='\033[0m'
else
  RED=''; GREEN=''; YELLOW=''; CYAN=''; BOLD=''; NC=''
fi

info()  { printf "${CYAN}[INFO]${NC}  %s\n" "$*"; }
ok()    { printf "${GREEN}[OK]${NC}    %s\n" "$*"; }
warn()  { printf "${YELLOW}[WARN]${NC}  %s\n" "$*"; }
err()   { printf "${RED}[ERR]${NC}   %s\n" "$*" >&2; }
die()   { err "$*"; exit 1; }

# ── JSON 解析（不依赖 jq）───────────────────────────────────────────
parse_json_tag() {
  local key="$1"
  sed -n 's/.*"'"$key"'"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1
}

# ── 依赖检查 ─────────────────────────────────────────────────────────
require_cmd() {
  command -v "$1" &>/dev/null || die "缺少依赖: $1。请先安装后重试。"
}

# ── 参数解析 ─────────────────────────────────────────────────────────
REPO="${REPO:-AXY520/cctui}"
VERSION="${VERSION:-}"
INSTALL_DIR=""
MIRROR="${MIRROR:-}"
FORCE_BUILD="false"
SKIP_CONFIRM="false"
UNINSTALL="false"

usage() {
  cat <<'EOF'
cctui 安装脚本

用法:
  bash install.sh [选项]

选项:
  --version <ver>     安装指定版本 (例如 v0.1.0)，默认最新
  --dir <path>        安装目录，默认 ~/.local/bin 或 /usr/local/bin
  --mirror <url>      GitHub 镜像前缀，例如 https://ghfast.top
  --build             强制从源码编译（需要 Go 1.21+）
  --repo <owner/repo> 覆盖仓库地址，默认 AXY520/cctui
  --uninstall         卸载 cctui
  -y, --yes           跳过确认提示
  -h, --help          显示帮助

环境变量:
  REPO                同 --repo
  VERSION             同 --version
  MIRROR              同 --mirror

示例:
  bash install.sh                              # 安装最新版
  bash install.sh --version v0.1.0             # 安装指定版本
  bash install.sh --mirror https://ghfast.top  # 使用镜像加速
  bash install.sh --build                      # 从源码编译
  bash install.sh --uninstall                  # 卸载
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)     VERSION="$2"; shift 2 ;;
    --dir)         INSTALL_DIR="$2"; shift 2 ;;
    --mirror)      MIRROR="${2%/}"; shift 2 ;;
    --build)       FORCE_BUILD="true"; shift ;;
    --repo)        REPO="$2"; shift 2 ;;
    --uninstall)   UNINSTALL="true"; shift ;;
    -y|--yes)      SKIP_CONFIRM="true"; shift ;;
    -h|--help)     usage; exit 0 ;;
    *)             die "未知参数: $1（使用 -h 查看帮助）" ;;
  esac
done

# ── 卸载流程 ─────────────────────────────────────────────────────────
if [[ "$UNINSTALL" == "true" ]]; then
  # 在可能的目录中查找
  SEARCH_DIRS=("${HOME}/.local/bin" "/usr/local/bin" "/usr/bin")
  found=""
  for dir in "${SEARCH_DIRS[@]}"; do
    if [[ -f "${dir}/cctui" ]]; then
      found="${dir}/cctui"
      break
    fi
  done

  if [[ -z "$found" ]]; then
    warn "未找到已安装的 cctui"
    exit 0
  fi

  info "找到 cctui: ${found}"
  if [[ "$SKIP_CONFIRM" != "true" ]]; then
    printf "确认卸载？[Y/n] "
    read -r reply </dev/tty 2>/dev/null || read -r reply
    case "$reply" in
      [nN]*) info "已取消"; exit 0 ;;
    esac
  fi

  rm -f "$found"
  # 清理备份
  rm -f "${found}.bak"
  ok "已卸载 cctui ($found)"
  info "配置数据 ~/.cc-switch/ 未删除，如需清理请手动执行: rm -rf ~/.cc-switch"
  exit 0
fi

# ── 依赖检查 ─────────────────────────────────────────────────────────
require_cmd curl
require_cmd uname
require_cmd tar

# ── 系统检测 ─────────────────────────────────────────────────────────
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
  Linux)       OS="linux" ;;
  Darwin)      OS="darwin" ;;
  FreeBSD)     OS="freebsd" ;;
  MINGW*|MSYS*|CYGWIN*) OS="windows" ;;
  *)           die "不支持的操作系统: $OS" ;;
esac

info "系统: ${BOLD}${OS}/${ARCH}${NC}"

# ── 版本号比较 ───────────────────────────────────────────────────────
# 返回 0 如果 $1 > $2，1 如果 $1 < $2，2 如果相等
version_gt() {
  local IFS=.
  local -a a=($1) b=($2)
  local i
  for ((i=0; i<${#a[@]} || i<${#b[@]}; i++)); do
    local x="${a[i]:-0}" y="${b[i]:-0}"
    if ((x > y)); then return 0; fi
    if ((x < y)); then return 1; fi
  done
  return 2
}

# ── 检查已安装版本 ───────────────────────────────────────────────────
INSTALLED_VERSION=""
if command -v cctui &>/dev/null; then
  INSTALLED_VERSION="$(cctui --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -n1 || true)"
fi

# ── GitHub 请求辅助 ──────────────────────────────────────────────────
github_api() {
  local path="$1"
  curl -fsSL -H "Accept: application/vnd.github.v3+json" \
    "https://api.github.com${path}" 2>/dev/null
}

github_download() {
  local path="$1" output="$2"
  local url="https://github.com${path}"
  # 镜像只替换域名下载部分，不碰 API
  [[ -n "$MIRROR" ]] && url="${MIRROR}/${url}"
  curl -fSL --progress-bar -o "$output" "$url"
}

# ── 获取最新版本 ─────────────────────────────────────────────────────
if [[ -z "$VERSION" ]]; then
  info "正在查询最新版本..."
  VERSION="$(github_api "/repos/${REPO}/releases/latest" | parse_json_tag "tag_name" || true)"
  if [[ -z "$VERSION" ]]; then
    VERSION="$(github_api "/repos/${REPO}/tags?per_page=1" | parse_json_tag "name" || true)"
  fi
  if [[ -z "$VERSION" ]]; then
    if [[ "$FORCE_BUILD" == "true" ]] && command -v go &>/dev/null; then
      warn "无法获取最新版本，将从 main 分支源码构建"
      VERSION="main"
    else
      die "无法获取最新版本，请检查网络或用 --version 手动指定"
    fi
  fi
fi

info "目标版本: ${BOLD}${VERSION}${NC}"

# ── 版本重复检查 ─────────────────────────────────────────────────────
if [[ -n "$INSTALLED_VERSION" ]]; then
  target_clean="${VERSION#v}"
  if [[ "$INSTALLED_VERSION" == "$target_clean" ]] && [[ "$FORCE_BUILD" != "true" ]]; then
    ok "cctui ${INSTALLED_VERSION} 已安装，无需重复安装"
    exit 0
  fi
  info "当前已安装: ${INSTALLED_VERSION}，将升级到 ${target_clean}"
fi

# ── 安装目录选择 ─────────────────────────────────────────────────────
if [[ -z "$INSTALL_DIR" ]]; then
  if [[ -d "${HOME}/.local/bin" ]] && [[ -w "${HOME}/.local/bin" ]]; then
    INSTALL_DIR="${HOME}/.local/bin"
  elif [[ -w "/usr/local/bin" ]]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "$INSTALL_DIR"
  fi
fi
mkdir -p "$INSTALL_DIR"

info "安装目录: ${INSTALL_DIR}"

# ── 临时目录 & 清理 ─────────────────────────────────────────────────
TMPDIR="$(mktemp -d)"
cleanup() { rm -rf "$TMPDIR"; }
trap cleanup EXIT INT TERM

# ── 下载预编译二进制 ─────────────────────────────────────────────────
BINARY_NAME="cctui"
[[ "$OS" == "windows" ]] && BINARY_NAME="cctui.exe"

download_binary() {
  local tag="$1"
  local archive_name="cctui-${OS}-${ARCH}.tar.gz"
  [[ "$OS" == "windows" ]] && archive_name="cctui-${OS}-${ARCH}.zip"
  local download_path="/${REPO}/releases/download/${tag}/${archive_name}"
  local dest="${TMPDIR}/${archive_name}"

  info "正在下载: ${archive_name}"
  if github_download "$download_path" "$dest"; then
    info "正在解压..."
    if [[ "$OS" == "windows" ]]; then
      unzip -qo "$dest" -d "$TMPDIR" 2>/dev/null || return 1
    else
      tar xzf "$dest" -C "$TMPDIR" 2>/dev/null || return 1
    fi
    local found
    found="$(find "$TMPDIR" -name "$BINARY_NAME" -type f | head -n1)"
    if [[ -n "$found" ]]; then
      cp "$found" "${TMPDIR}/${BINARY_NAME}"
      chmod +x "${TMPDIR}/${BINARY_NAME}"
      return 0
    fi
  fi
  return 1
}

# ── 从源码构建 ───────────────────────────────────────────────────────
build_from_source() {
  local tag="$1"

  require_cmd go
  require_cmd git

  local go_ver
  go_ver="$(go version | sed 's/.*go\([0-9]*\.[0-9]*\).*/\1/')"
  local go_major="${go_ver%%.*}"
  local go_minor="${go_ver##*.}"
  if (( go_major < 1 || (go_major == 1 && go_minor < 21) )); then
    die "Go 版本过低 (${go_ver})，需要 1.21+。请升级: https://go.dev/dl/"
  fi

  info "Go ${go_ver} ✓"

  local src_dir="${TMPDIR}/src"
  mkdir -p "$src_dir"

  if [[ "$tag" == "main" ]]; then
    info "正在克隆仓库..."
    git clone --depth 1 "https://github.com/${REPO}.git" "$src_dir" 2>/dev/null \
      || die "克隆失败，请检查网络或使用 --mirror 参数"
  else
    info "正在下载源码..."
    local tar_url="https://github.com/${REPO}/archive/refs/tags/${tag}.tar.gz"
    [[ -n "$MIRROR" ]] && tar_url="${MIRROR}/${tar_url}"
    local tar_dest="${TMPDIR}/src.tar.gz"
    curl -fSL --progress-bar -o "$tar_dest" "$tar_url" || die "源码下载失败"
    tar xzf "$tar_dest" -C "$src_dir" --strip-components=1
  fi

  info "正在编译 (CGO_ENABLED=0, 请耐心等待)..."
  (
    cd "$src_dir"
    export CGO_ENABLED=0
    export GOFLAGS="-buildmode=pie -trimpath -mod=readonly"
    go build -ldflags='-s -w' -o "${TMPDIR}/${BINARY_NAME}" . 2>&1
  ) || die "编译失败"

  ok "编译完成"
}

# ── 主安装流程 ───────────────────────────────────────────────────────
installed_via=""

if [[ "$FORCE_BUILD" == "true" ]]; then
  info "强制从源码编译模式"
  build_from_source "$VERSION"
  installed_via="source"
else
  if download_binary "$VERSION"; then
    installed_via="binary"
  else
    warn "预编译二进制不可用，回退到源码编译..."
    build_from_source "$VERSION"
    installed_via="source"
  fi
fi

# ── 确认安装（从 /dev/tty 读取，兼容管道模式）──────────────────────
if [[ "$SKIP_CONFIRM" != "true" ]]; then
  printf "\n${BOLD}即将安装:${NC}\n"
  printf "  二进制: %s/%s\n" "$INSTALL_DIR" "$BINARY_NAME"
  printf "  来源:   %s\n" "$([[ $installed_via == binary ]] && echo '预编译二进制' || echo '源码编译')"
  printf "\n确认安装？[Y/n] "
  if ! read -r reply </dev/tty 2>/dev/null; then
    # 管道模式无法交互，默认继续
    reply="y"
    printf "y (自动确认)\n"
  fi
  case "$reply" in
    [nN]*) info "已取消安装"; exit 0 ;;
  esac
fi

# ── 安装 ─────────────────────────────────────────────────────────────
info "正在安装到 ${INSTALL_DIR}/${BINARY_NAME}..."

if [[ -f "${INSTALL_DIR}/${BINARY_NAME}" ]]; then
  BACKUP="${INSTALL_DIR}/${BINARY_NAME}.bak"
  cp "${INSTALL_DIR}/${BINARY_NAME}" "$BACKUP"
  info "已备份旧版本到 ${BACKUP}"
fi

cp "${TMPDIR}/${BINARY_NAME}" "${INSTALL_DIR}/${BINARY_NAME}"
chmod +x "${INSTALL_DIR}/${BINARY_NAME}"

ok "安装完成！"

# ── PATH 检查 & 提示 ────────────────────────────────────────────────
if command -v cctui &>/dev/null; then
  ok "cctui 已在 PATH 中"
else
  warn "cctui 不在 PATH 中"
  printf "\n请将以下内容添加到你的 shell 配置文件:\n\n"

  SHELL_NAME="$(basename "${SHELL:-bash}")"
  case "$SHELL_NAME" in
    zsh)  PROFILE="~/.zshrc" ;;
    bash) PROFILE="~/.bashrc" ;;
    fish) PROFILE="~/.config/fish/config.fish" ;;
    *)    PROFILE="~/.profile" ;;
  esac

  if [[ "$SHELL_NAME" == "fish" ]]; then
    printf "  ${CYAN}set -gx PATH %s \$PATH${NC}\n\n" "$INSTALL_DIR"
  else
    printf "  ${CYAN}export PATH=\"%s:\$PATH\"${NC}\n\n" "$INSTALL_DIR"
  fi
  printf "然后运行: ${CYAN}source %s${NC}\n" "$PROFILE"
fi

printf "\n${GREEN}${BOLD}🎉 cctui 安装成功！${NC}\n"
printf "运行 ${CYAN}cctui${NC} 启动 TUI 界面\n"
printf "卸载: ${CYAN}bash install.sh --uninstall${NC}\n"
printf "文档: https://github.com/%s\n" "$REPO"
