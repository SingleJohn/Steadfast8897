#!/usr/bin/env bash
# 下载媒体库封面生成所需的中文字体到 internal/services/coverart/assets/fonts/。
#
# 幂等:目标文件已存在且尺寸合理(>= 1MB)直接跳过。
# 多镜像 fallback:GitHub raw → jsDelivr CDN → Gitee raw 镜像。
# 本脚本在 Linux / macOS / Windows(Git Bash) 下都可运行;
# GitHub Actions / Dockerfile / 本地开发 都调同一份,保证行为一致。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

FONT_DIR="${REPO_ROOT}/internal/services/coverart/assets/fonts"
FONT_NAME="NotoSansSC-Bold.otf"
FONT_PATH="${FONT_DIR}/${FONT_NAME}"
MIN_BYTES=$((1024 * 1024)) # 1MB:低于此值视为损坏/HTML 错误页

# 按优先级逐个尝试。失败一个则尝试下一个。
SOURCES=(
  "https://github.com/notofonts/noto-cjk/raw/main/Sans/OTF/SimplifiedChinese/NotoSansCJKsc-Bold.otf"
  "https://cdn.jsdelivr.net/gh/notofonts/noto-cjk@main/Sans/OTF/SimplifiedChinese/NotoSansCJKsc-Bold.otf"
  "https://raw.githubusercontent.com/notofonts/noto-cjk/main/Sans/OTF/SimplifiedChinese/NotoSansCJKsc-Bold.otf"
)

log() { printf '[fetch-fonts] %s\n' "$*"; }

file_size() {
  # 跨平台取文件大小(Linux: stat -c / macOS: stat -f / BusyBox: wc -c)
  if [ ! -f "$1" ]; then echo 0; return; fi
  if stat -c %s "$1" >/dev/null 2>&1; then stat -c %s "$1"; return; fi
  if stat -f %z "$1" >/dev/null 2>&1; then stat -f %z "$1"; return; fi
  wc -c < "$1" | tr -d ' '
}

is_font_ok() {
  local sz
  sz=$(file_size "$1")
  [ "${sz}" -ge "${MIN_BYTES}" ]
}

if [ -f "${FONT_PATH}" ] && is_font_ok "${FONT_PATH}"; then
  log "${FONT_NAME} 已存在($(file_size "${FONT_PATH}") bytes),跳过下载"
  exit 0
fi

mkdir -p "${FONT_DIR}"

for url in "${SOURCES[@]}"; do
  log "尝试从 ${url} 下载..."
  tmp="${FONT_PATH}.download"
  rm -f "${tmp}"
  if command -v curl >/dev/null 2>&1; then
    if curl -fsSL --retry 2 --retry-delay 2 --connect-timeout 15 --max-time 180 -o "${tmp}" "${url}"; then
      :
    else
      log "curl 失败"
      rm -f "${tmp}"
      continue
    fi
  elif command -v wget >/dev/null 2>&1; then
    if wget -q --tries=2 --timeout=120 -O "${tmp}" "${url}"; then
      :
    else
      log "wget 失败"
      rm -f "${tmp}"
      continue
    fi
  else
    echo "[fetch-fonts] ERROR: 系统没有 curl 也没有 wget" >&2
    exit 1
  fi
  if is_font_ok "${tmp}"; then
    mv "${tmp}" "${FONT_PATH}"
    log "下载成功:${FONT_PATH}($(file_size "${FONT_PATH}") bytes)"
    exit 0
  fi
  log "文件过小($(file_size "${tmp}") bytes),可能是错误页,换下一源"
  rm -f "${tmp}"
done

echo "[fetch-fonts] ERROR: 所有镜像都下载失败,请手动把 ${FONT_NAME} 放到 ${FONT_DIR}/ 后重新构建" >&2
exit 1
