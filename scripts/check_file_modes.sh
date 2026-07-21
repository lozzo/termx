#!/bin/sh
set -eu

bad=$(
	# 只检查当前维护范围内的普通文件；真正脚本允许保留可执行位。
	git ls-files -s \
		.gitignore AGENTS.md Makefile README.md go.work go.work.sum package.json package-lock.json workflow.md \
		clients \
		cmd/muxvia \
		core \
		docs \
		fixtures \
		internal \
		private/cloud \
		proto \
		remote \
		scripts \
		shared \
		testkit \
		tui \
		vterm |
	awk '
		$1 == "100755" {
			path = $4
			if (path ~ /\.(go|md|mod|sum|proto|json|txt|yaml|yml|png|jpe?g|gif|webp|svg|ico)$/ ||
			    path ~ /(^|\/)(AGENTS|README|Makefile|LICENSE|NOTICE|DCO)$/ ||
			    path ~ /(^|\/)\.gitignore$/ ||
			    path ~ /(^|\/)go\.work(\.sum)?$/ ||
			    path ~ /(^|\/)workflow\.md$/) {
				print path
			}
		}
	'
)

if [ -n "$bad" ]; then
	printf '%s\n' "unexpected executable bit on regular source/config files:"
	printf '%s\n' "$bad"
	exit 1
fi
