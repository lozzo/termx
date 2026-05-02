#!/usr/bin/env bash
set -euo pipefail

require_in_file() {
  local file="$1"
  local needle="$2"
  if ! grep -Fq "$needle" "$file"; then
    printf 'missing required text in %s: %s\n' "$file" "$needle" >&2
    return 1
  fi
}

reject_in_file() {
  local file="$1"
  local needle="$2"
  if grep -Fq "$needle" "$file"; then
    printf 'forbidden stale text in %s: %s\n' "$file" "$needle" >&2
    return 1
  fi
}

require_workflow_detail_fields() {
  local id="$1"
  local file="docs/remote-rebuild/WORKFLOW.md"
  local heading
  heading="$(grep -n "^### ${id} " "$file" | head -n 1 | cut -d: -f1)"
  if [[ -z "$heading" ]]; then
    printf 'missing workflow detail section for todo %s\n' "$id" >&2
    return 1
  fi

  local next_heading
  next_heading="$(tail -n +"$((heading + 1))" "$file" | grep -n '^### ' | head -n 1 | cut -d: -f1 || true)"
  local end
  if [[ -z "$next_heading" ]]; then
    end="$(wc -l < "$file")"
  else
    end="$((heading + next_heading - 1))"
  fi

  local block
  block="$(sed -n "${heading},${end}p" "$file")"
  local field
  for field in \
    "状态：" \
    "父条目：" \
    "来源：" \
    "目标：" \
    "范围：" \
    "非目标：" \
    "外部依赖：" \
    "mock 策略：" \
    "先写的失败测试：" \
    "预期失败结果：" \
    "实现摘要：" \
    "重构摘要：" \
    "运行命令：" \
    "测试结果：" \
    "subagent review：" \
    "review 发现：" \
    "review 后修复：" \
    "新增派生条目：" \
    "deferred human items：" \
    "剩余风险：" \
    "下一步：" \
    "commit："; do
    if ! grep -Fq "$field" <<<"$block"; then
      printf 'todo %s missing required field: %s\n' "$id" "$field" >&2
      return 1
    fi
  done
}

require_in_file AGENTS.md "## Current Task: Remote Web / Hub / Agent Buildout"
require_in_file AGENTS.md "web-control/AGENTS.md"
require_in_file AGENTS.md "termx-hub/AGENTS.md"
require_in_file AGENTS.md "docs/remote-rebuild/WORKFLOW.md"
require_in_file AGENTS.md "mock / fake / local stub"
require_in_file AGENTS.md "free/public_p2p 是否能拿到 TURN credentials"
require_in_file AGENTS.md "HTTP/WebSocket 是否被错误包装成 terminal/file/api/events 的运行时 transport"
require_in_file AGENTS.md "relay 是否被抽成第四种客户端 transport/path"
require_in_file AGENTS.md "SQLite transaction、TTL cleanup、quota/session limit 是否是真行为"

require_in_file docs/remote-rebuild/WORKFLOW.md "Remote Web / Hub / Agent Buildout"
require_in_file docs/remote-rebuild/WORKFLOW.md "| 0 |"
require_in_file docs/remote-rebuild/WORKFLOW.md "| 11 |"
require_in_file docs/remote-rebuild/WORKFLOW.md "deferred_external"
require_in_file docs/remote-rebuild/WORKFLOW.md "subagent review"
require_in_file docs/remote-rebuild/WORKFLOW.md "local / public_p2p / managed"
reject_in_file docs/remote-rebuild/WORKFLOW.md "Start P4-A"
reject_in_file docs/remote-rebuild/WORKFLOW.md "Current phase: P3 embedded local web first"
reject_in_file docs/remote-rebuild/WORKFLOW.md "Active todo: P4-A"

for id in 0 1 2 3 4 5 6 7 8 9 10 11; do
  require_workflow_detail_fields "$id"
done

require_in_file web-control/AGENTS.md "Web Control Plane"
require_in_file web-control/AGENTS.md "Mock providers must stay behind interfaces"
require_in_file termx-hub/AGENTS.md "must not be a terminal/file/api/events HTTP or WebSocket runtime proxy"
require_in_file termx-hub/AGENTS.md "Relay is not a fourth client transport"

printf 'remote rebuild workflow rules check passed\n'
