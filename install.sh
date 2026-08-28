#!/usr/bin/env bash
# cctui 交互式安装 / 卸载 / 更新脚本
set -euo pipefail

# Ctrl+C 只表示取消当前操作，不能被下载失败分支当成继续安装。
trap 'exit 130' INT

# ── 颜色 ─────────────────────────────────────────────────────────────
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

require_cmd() { command -v "$1" &>/dev/null || die "缺少依赖: $1。请先安装后重试。"; }

# 是否有可交互的终端可读取输入 (真正尝试打开, 避免 setsid/容器下 /dev/tty 存在却打不开)
has_tty() { { : </dev/tty; } 2>/dev/null; }

# 从终端读取一行输入; 无 tty 时返回失败, 由调用方决定默认行为
read_tty() {
  local __var="$1"; local __reply=""
  if [[ -t 0 ]]; then
    IFS= read -r __reply || return 1
  else
    has_tty || return 1
    IFS= read -r __reply </dev/tty 2>/dev/null || return 1
  fi
  printf -v "$__var" '%s' "$__reply"
}

REPO="${REPO:-aixinyin/cctui}"
MIRROR="${MIRROR:-}"
REPO_HOST="gitee.com"
if [[ -n "$MIRROR" ]]; then
  BASE_URL="${MIRROR}/https://${REPO_HOST}"
else
  BASE_URL="https://${REPO_HOST}"
fi

# ── Gitee API ────────────────────────────────────────────────────────
repo_api()     { curl -fsSL --connect-timeout 10 --max-time 30 "https://${REPO_HOST}/api/v5/repos/${REPO}${1}" 2>/dev/null; }
repo_dl()      { curl -fSL --connect-timeout 10 --max-time 300 --progress-bar -o "$2" "${BASE_URL}/${REPO}${1}"; }
get_latest() {
  local response
  response="$(repo_api "/releases/latest")" || return $?
  sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' <<<"$response"
}

# ── 系统检测 ─────────────────────────────────────────────────────────
detect_arch() {
  local a; a="$(uname -m)"
  case "$a" in x86_64|amd64) echo amd64;; aarch64|arm64) echo arm64;; armv7*|armhf) echo armv7;; i?86) echo 386;; *) die "不支持的架构: $a";; esac
}
detect_os() {
  local s; s="$(uname -s)"
  case "$s" in Linux*) echo linux;; Darwin*) echo darwin;; FreeBSD*) echo freebsd;; *) die "不支持的系统: $s";; esac
}

# ── 查找已安装 ──────────────────────────────────────────────────────
find_cctui() {
  command -v cctui 2>/dev/null && return 0
  for d in "${HOME}/.local/bin" /usr/local/bin /usr/bin; do [[ -x "$d/cctui" ]] && echo "$d/cctui" && return 0; done
  return 1
}

installed_version() {
  local p; p="$(find_cctui)" || return 1
  "$p" --version 2>/dev/null | sed 's/[^0-9.]*\([0-9]*\.[0-9]*\.[0-9]*\).*/\1/'
}

# ── 卸载 ─────────────────────────────────────────────────────────────
do_uninstall() {
  local target
  target="$(find_cctui 2>/dev/null)" || { warn "未找到已安装的 cctui"; return 0; }
  printf "确认卸载？[Y/n] "; local reply=""; read_tty reply || true; reply="${reply:-y}"; printf "\n"
  case "$reply" in [nN]*) info "已取消"; return 0;; esac
  rm -f "$target" "${target}.bak"
  ok "已卸载 cctui"
  info "配置数据 ~/.cc-switch/ 未删除, 如需清理请执行: rm -rf ~/.cc-switch"
}

# ── 下载 / 编译 / 安装 ─────────────────────────────────────────────
download_binary() {
  local tag="$1" tmp="$2" os="${3:-linux}" arch="${4:-amd64}"
  local an="cctui-${os}-${arch}.tar.gz"
  info "正在下载: ${an}"
  repo_dl "/releases/download/${tag}/${an}" "${tmp}/${an}" || return 1
  tar xzf "${tmp}/${an}" -C "$tmp" && [[ -x "$tmp/cctui" ]] || return 1
  ok "下载完成"
  return 0
}

build_source() {
  local tag="$1" tmp="$2"
  require_cmd go; require_cmd git
  local ver; ver="$(go version 2>/dev/null | sed 's/.*go\([0-9]*\.[0-9]*\).*/\1/')"
  ((${ver%%.*} < 1 || (${ver%%.*} == 1 && ${ver##*.} < 21))) && die "需要 Go 1.21+，当前: $ver"
  local sd="${tmp}/src"; mkdir -p "$sd"
  if [[ "$tag" == main ]]; then
    git clone --depth 1 "https://${REPO_HOST}/${REPO}.git" "$sd"
  else
    local tu="https://${REPO_HOST}/${REPO}/archive/refs/tags/${tag}.tar.gz"
    [[ -n "$MIRROR" ]] && tu="${MIRROR}/${tu}"
    curl -fSL --connect-timeout 10 --max-time 300 --progress-bar -o "${tmp}/src.tar.gz" "$tu" || die "源码下载失败"
    tar xzf "${tmp}/src.tar.gz" -C "$sd" --strip-components=1
  fi
  (cd "$sd"; export CGO_ENABLED=0 GOFLAGS="-buildmode=pie -trimpath -mod=readonly";
   go build -ldflags="-s -w -X main.version=${tag#v}" -o "${tmp}/cctui" .)
}

install_cctui() {
  local ver="$1" src="$2"
  local dir; dir="${INSTALL_DIR:-}"
  if [[ -z "$dir" ]]; then
    [[ -d "${HOME}/.local/bin" && -w "${HOME}/.local/bin" ]] && dir="${HOME}/.local/bin" || {
      [[ -w /usr/local/bin ]] && dir=/usr/local/bin || { dir="${HOME}/.local/bin"; mkdir -p "$dir"; }
    }
  fi
  mkdir -p "$dir"
  [[ -f "${dir}/cctui" ]] && cp "${dir}/cctui" "${dir}/cctui.bak"
  cp "$src" "${dir}/cctui" && chmod +x "${dir}/cctui"
  INSTALL_DIR="$dir"
  ok "已安装到 ${dir}/cctui"
}

# ── 安装流程 ─────────────────────────────────────────────────────────
do_install() {
  local os arch ver tmp
  os="$(detect_os)"; arch="$(detect_arch)"; ver="${1:-latest}"
  [[ "$ver" == "latest" ]] && { ver="$(get_latest)"; [[ -z "$ver" ]] && die "无法获取最新版本"; }
  info "版本: ${ver} (${os}/${arch})"
  tmp="$(mktemp -d)"; trap "rm -rf '$tmp'" RETURN
  if download_binary "$ver" "$tmp" "$os" "$arch"; then
    install_cctui "$ver" "${tmp}/cctui"
  else
    local status=$?
    ((status == 130)) && exit 130
    warn "预编译二进制不可用, 尝试源码编译..."
    build_source "$ver" "$tmp"
    install_cctui "$ver" "${tmp}/cctui"
  fi
}

# ── 注册快捷唤起 ────────────────────────────────────────────────────
do_register_alias() {
  local dir="$INSTALL_DIR"
  [[ -z "$dir" || ! -f "$dir/cctui" ]] && { warn "cctui 未安装，请先安装"; return; }
  info "正在注册快捷命令 cctui ..."
  local shell_rc=""
  case "${SHELL:-bash}" in
    */zsh)  shell_rc="${ZDOTDIR:-$HOME}/.zshrc" ;;
    */bash) shell_rc="$HOME/.bashrc" ;;
    */fish) shell_rc="$HOME/.config/fish/config.fish" ;;
    *)      shell_rc="$HOME/.profile" ;;
  esac

  # 检查 PATH 是否已包含安装目录
  if [[ ":$PATH:" != *":${dir}:"* ]]; then
    info "PATH 中不包含 ${dir}, 尝试添加到 ${shell_rc}"
    if [[ "${SHELL##*/}" == "fish" ]]; then
      echo "set -gx PATH \"$dir\" \$PATH" >> "$shell_rc"
    else
      echo "export PATH=\"\$HOME/.local/bin:\$PATH\"" >> "$shell_rc"
    fi
    ok "快捷命令注册完成，请执行 source ${shell_rc} 或重启终端后即可使用 cctui 命令"
  else
    ok "快捷命令已就绪，直接输入 cctui 即可启动"
  fi
}

# ── 显示信息面板 ──────────────────────────────────────────────────
show_panel() {
  local ver="${1:-unknown}"
  local os; os="$(detect_os)"; local arch; arch="$(detect_arch)"
  local installed="未安装"; local path=""; local iv=""
  iv="$(installed_version 2>/dev/null)" || true
  path="$(find_cctui 2>/dev/null)" || true
  [[ -n "$iv" ]] && installed="$iv"
  [[ -z "$iv" && -n "$path" ]] && installed="已安装(版本未知)"
  printf "\n  ${BOLD}┌──────────────────────────────────────┐${NC}\n"
  printf "  ${BOLD}│${NC}  ${CYAN}${BOLD}cctui 安装管理器${NC}                   ${BOLD}│${NC}\n"
  printf "  ${BOLD}├──────────────────────────────────────┤${NC}\n"
  printf "  ${BOLD}│${NC}  系统:     ${BOLD}%-25s${NC}${BOLD}│${NC}\n" "${os}/${arch}"
  printf "  ${BOLD}│${NC}  已安装:   ${BOLD}%-25s${NC}${BOLD}│${NC}\n" "$installed"
  printf "  ${BOLD}│${NC}  最新版:   ${BOLD}%-25s${NC}${BOLD}│${NC}\n" "$ver"
  printf "  ${BOLD}│${NC}  路径:     ${DIM}%-25s${NC}${BOLD}│${NC}\n" "${path:-${INSTALL_DIR:-${HOME}/.local/bin}}"
  printf "  ${BOLD}└──────────────────────────────────────┘${NC}\n"
}

# ── 主菜单 ───────────────────────────────────────────────────────────
main() {
  require_cmd curl; require_cmd uname
  local action="${1:-}"

  # 参数模式
  case "$action" in
    -i|--install)
      do_install "$2"
      do_register_alias
      printf "\n${GREEN}🎉 完成！${NC} 运行 ${CYAN}cctui${NC} 启动\n"
      return
      ;;
    -u|--uninstall)
      do_uninstall
      return
      ;;
    --help|-h)
      echo "用法: bash install.sh [-i <version>] [-u]"
      echo "       bash install.sh       # 交互式菜单"
      echo "环境变量: REPO, MIRROR"
      return
      ;;
  esac

  local latest
  if latest="$(get_latest)"; then
    :
  else
    local status=$?
    ((status == 130)) && exit 130
    latest="获取失败"
  fi
  show_panel "$latest"

  local msg=""
  local has_update="n"
  local iv; iv="$(installed_version 2>/dev/null)" || true
  if [[ -n "$iv" && "$latest" != "获取失败" && "${iv#v}" != "${latest#v}" ]]; then
    msg="发现新版本！"
    has_update=y
  fi

  # 计算可用选项
  local opts=()
  local installed="n"
  find_cctui &>/dev/null && installed="y"

  if [[ "$installed" == "y" ]]; then
    [[ "$has_update" == "y" ]] && opts+=("升级")
    opts+=("重新安装" "卸载")
  else
    opts+=("安装")
  fi
  opts+=("退出")

  printf "${msg:+  ${YELLOW}%s${NC}\n}" "$msg"
  printf "\n"
  local i=1
  for opt in "${opts[@]}"; do
    printf "  ${BOLD}[%d]${NC} %s\n" "$i" "$opt"
    ((i++))
  done
  printf "\n请选择操作 [1-%d]: " "${#opts[@]}"

  # 管道执行是公开的一键安装入口（curl | bash），不应等待 /dev/tty 输入。
  # 直接执行 bash install.sh 仍保留交互菜单。
  local choice=""
  if [[ ! -t 0 ]]; then
    choice=1
    printf "1\n"
    info "检测到管道执行, 自动执行: ${opts[0]}"
    info "如需指定操作可用: bash install.sh -i <版本>  或  -u 卸载"
  elif ! read_tty choice; then
    choice=1
    printf "1\n"
    warn "未检测到可交互终端, 默认执行: ${opts[0]}"
    info "如需指定操作可用: bash install.sh -i <版本>  或  -u 卸载"
  fi
  choice="${choice:-1}"

  # 如果没指定安装参数，默认就当前最新版本
  case "${opts[$((choice-1))]:-}" in
    "安装"|"升级"|"重新安装")
      do_install "$latest"
      do_register_alias
      printf "\n${GREEN}🎉 完成！${NC} 运行 ${CYAN}cctui${NC} 启动\n"
      ;;
    "卸载")
      do_uninstall
      ;;
    "退出")
      info "已退出"
      ;;
    *)
      die "无效选择"
      ;;
  esac
}

main "$@"
