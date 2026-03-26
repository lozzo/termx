package tui

import (
	// 通过副作用导入注册根视图 renderer，避免运行入口只起 Bubble Tea
	// 但没有任何页面渲染器接线，导致界面看起来像“卡住”。
	_ "github.com/lozzow/termx/tui/render/workbench"
)
