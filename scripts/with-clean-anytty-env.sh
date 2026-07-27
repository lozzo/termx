#!/usr/bin/env bash

set -euo pipefail

if [[ $# -eq 0 ]]; then
  echo "usage: $0 COMMAND [ARG ...]" >&2
  exit 2
fi

unset_args=()
while IFS='=' read -r name _; do
  case "$name" in
    ANYTTY*) unset_args+=(-u "$name") ;;
  esac
done < <(env)

# 测试必须只使用自身显式设置的 ANYTTY 变量，不能继承调用终端或远程会话状态。
if [[ ${#unset_args[@]} -eq 0 ]]; then
  exec env "$@"
fi
exec env "${unset_args[@]}" "$@"
