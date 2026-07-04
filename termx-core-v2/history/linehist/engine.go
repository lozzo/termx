package linehist

import (
	"sync"

	vterm "github.com/lozzow/termx/termx-vterm/vterm"
)

// Engine 组合拼装器与文件 backend，是 logical-line 历史的落盘入口。
// 它只消费 vterm 事务的 EvictedRows（seal-on-eviction 信号），
// 不读 raw PTY、不读 live snapshot、不持有第二份屏幕状态。
type Engine struct {
	mu   sync.Mutex
	asm  *Assembler
	file *LineFile
}

// NewEngine 用已打开的 LineFile 创建引擎。文件里已有的记录即恢复出的
// 冷历史；未闭合尾部不跨进程恢复（重启时旧行的续写上下文已不存在，
// Close 会把它按硬结束落盘）。
func NewEngine(file *LineFile) *Engine {
	return &Engine{asm: NewAssembler(), file: file}
}

// ApplyEvictedRows 按事务顺序消费滚出的物理行，把拼装完成的 logical line
// 批量落盘。滚出行的完整性（空行保留、alt 归因、ring 关闭）由 vterm
// EvictedRows 保证，这里不再做任何过滤或对账。
func (e *Engine) ApplyEvictedRows(rows []vterm.TerminalSemanticScrollOut) error {
	if e == nil || len(rows) == 0 {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var batch []Line
	for _, row := range rows {
		batch = append(batch, e.asm.AppendEvictedRow(row)...)
	}
	return e.file.AppendLines(batch)
}

// LineCount 返回已落盘记录数（含 chunk 记录）。
func (e *Engine) LineCount() int {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.file.LineCount()
}

// Lines 按绝对序号分页读取已落盘记录。
func (e *Engine) Lines(start int, end int) ([]Line, error) {
	if e == nil {
		return nil, nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.file.Lines(start, end)
}

// OpenTail 返回未闭合尾部（已滚出但还没等到硬换行的续行内容）。
// 投影层把它拼在冷段之后、emulator 当前屏之前，作为热段一部分。
func (e *Engine) OpenTail() []Run {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.asm.Open()
}

// SealOpenTail 把未闭合尾部强制闭合落盘，用于 ED3/RIS/进程退出等边界。
func (e *Engine) SealOpenTail() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	line, ok := e.asm.SealOpen()
	if !ok {
		return nil
	}
	return e.file.AppendLines([]Line{line})
}

// Close 把未闭合尾部按硬结束落盘后关闭文件：重启后旧行的续写上下文
// 已不存在，把已滚出的内容留在内存里只会丢数据。
func (e *Engine) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if line, ok := e.asm.SealOpen(); ok {
		if err := e.file.AppendLines([]Line{line}); err != nil {
			_ = e.file.Close()
			return err
		}
	}
	return e.file.Close()
}
