#!/usr/bin/env bash
#
# scripts/verify.sh — 提交前一键验证。
#
#   1) go test ./internal/... -count=1
#   2) npx tsc --noEmit
#   3) npx vitest run
#   4) npx vite build          （--no-build 可跳过；会改写 web/dist 里的产物）
#
# 特性：
#   · set -euo pipefail，任一阶段失败立即停（fail-fast），后面的阶段标「未运行」。
#   · 不依赖调用者的 cwd：仓库根由脚本自身路径推导。
#   · TMPDIR 默认 $HOME/gotmp（不存在则创建）。/tmp 在很多机器上是满的，
#     go test 会因为无法创建临时目录而报出与代码无关的假失败。
#   · 只打印各阶段命令与耗时，不读取、不打印任何凭据。
#
# 用法：
#   bash scripts/verify.sh              # 全量（含构建）
#   bash scripts/verify.sh --no-build   # 跳过 vite build（只读校验）
#
set -euo pipefail

REPO_ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
WEB_DIR="$REPO_ROOT/web"

usage() {
  cat <<'USAGE'
用法: bash scripts/verify.sh [--no-build]

  --no-build   跳过最后一步 `npx vite build`（构建会改写 web/dist，只读校验时用）
  -h, --help   显示本帮助
USAGE
}

NO_BUILD=0
for arg in "$@"; do
  case "$arg" in
    --no-build) NO_BUILD=1 ;;
    -h|--help) usage; exit 0 ;;
    *)
      echo "未知参数: $arg" >&2
      usage >&2
      exit 2
      ;;
  esac
done

cd "$REPO_ROOT"

# ── TMPDIR：go test 需要可写的临时目录 ────────────────────────────────
# 故意不继承调用者的 TMPDIR：本机 /tmp 常是满的，继承会制造无关失败。
# 需要指定时用 VERIFY_TMPDIR=/path。
TMPDIR="${VERIFY_TMPDIR:-$HOME/gotmp}"
if ! mkdir -p "$TMPDIR" 2>/dev/null; then
  echo "无法创建 TMPDIR=$TMPDIR（可用 TMPDIR=/其他可写目录 覆盖）" >&2
  exit 1
fi
export TMPDIR
if [ ! -w "$TMPDIR" ]; then
  echo "TMPDIR=$TMPDIR 不可写（可用 VERIFY_TMPDIR=/其他可写目录 覆盖）" >&2
  exit 1
fi

if [ ! -d "$WEB_DIR/node_modules" ]; then
  echo "缺少 $WEB_DIR/node_modules：请先在 web/ 下安装依赖（本脚本不会自动安装）。" >&2
  exit 1
fi

echo "仓库根 : $REPO_ROOT"
echo "TMPDIR : $TMPDIR"
echo "模式   : $([ "$NO_BUILD" -eq 1 ] && echo '只读校验（--no-build）' || echo '全量（含 vite build）')"

# ── 阶段运行器 ────────────────────────────────────────────────────────
STAGE_LABELS=()
STAGE_STATES=()
STAGE_SECONDS=()
FAILED_LABEL=""
ABORTED=0

run_stage() {
  local label="$1" dir="$2"
  shift 2

  if [ "$ABORTED" -eq 1 ]; then
    STAGE_LABELS+=("$label")
    STAGE_STATES+=("跳过")
    STAGE_SECONDS+=("-")
    return 0
  fi

  echo ""
  echo "── $label ──────────────────────────────────────────────"
  echo "   cwd: $dir"
  echo "   cmd: $*"

  local start end rc=0
  start=$(date +%s)
  # 子 shell 里切换目录：失败不会污染后续阶段的 cwd。
  ( cd "$dir" && "$@" ) || rc=$?
  end=$(date +%s)

  STAGE_LABELS+=("$label")
  STAGE_SECONDS+=("$((end - start))s")

  if [ "$rc" -eq 0 ]; then
    STAGE_STATES+=("PASS")
    echo "   ↳ PASS ($((end - start))s)"
  else
    STAGE_STATES+=("FAIL")
    echo "   ↳ FAIL (退出码 $rc，耗时 $((end - start))s)"
    FAILED_LABEL="$label"
    ABORTED=1
  fi
  return 0
}

# ── 四个阶段 ──────────────────────────────────────────────────────────
run_stage "go test ./internal/..." "$REPO_ROOT" go test ./internal/... -count=1
run_stage "tsc --noEmit"          "$WEB_DIR"   npx tsc --noEmit
run_stage "vitest run"            "$WEB_DIR"   npx vitest run

if [ "$NO_BUILD" -eq 1 ]; then
  STAGE_LABELS+=("vite build")
  STAGE_STATES+=("跳过")
  STAGE_SECONDS+=("-")
else
  run_stage "vite build" "$WEB_DIR" npx vite build
fi

# ── 汇总 ──────────────────────────────────────────────────────────────
echo ""
echo "════════════════════ 结果汇总 ════════════════════"
all_ok=1
for i in "${!STAGE_LABELS[@]}"; do
  state="${STAGE_STATES[$i]}"
  printf '  %-22s %-6s %s\n' "${STAGE_LABELS[$i]}" "$state" "${STAGE_SECONDS[$i]}"
  case "$state" in
    FAIL) all_ok=0 ;;
    *) ;;
  esac
done
echo "══════════════════════════════════════════════════"

if [ "$all_ok" -eq 1 ]; then
  echo "PASS：全部阶段通过。"
  exit 0
fi

echo "FAIL：第一个失败的阶段是「$FAILED_LABEL」，后续阶段未运行。" >&2
echo "      先修这一处再重跑，别在红的基础上继续叠加改动。" >&2
exit 1
