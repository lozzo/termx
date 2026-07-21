package linehist

import (
	"sync"

	vterm "github.com/muxvia/muxvia/vterm/vterm"
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

// LineCount 返回已落盘记录数（含 chunk 记录），绝对域。
func (e *Engine) LineCount() int {
	if e == nil {
		return 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.file.LineCount()
}

// VisibleLineRange 返回当前可见冷段记录区间。R439 后 ED3/ClearScrollback
// 只是不删除历史的软页边界，所以 live history/copy 继续覆盖全部已落盘
// logical lines；base 保留为绝对域起点，当前始终为 0。
func (e *Engine) VisibleLineRange() (int, int) {
	if e == nil {
		return 0, 0
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	base := e.file.Base()
	return base, e.file.LineCount() - base
}

// ApplyClearScrollbackBoundary 处理 ED3/ClearScrollback 软页边界。
// authoritative history 不因终端 clear-scrollback 被删除或隐藏；这里仅把
// assembler 的未闭合尾部封成一条 logical line，避免 clear 前后页面被继续
// 拼接，然后写入 append-only boundary 记录供 generation/cursor 失效使用。
func (e *Engine) ApplyClearScrollbackBoundary() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if line, ok := e.asm.SealOpen(); ok {
		if err := e.file.AppendLines([]Line{line}); err != nil {
			return err
		}
	}
	return e.file.AppendBoundary()
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

// SealOpenTail 把未闭合尾部强制闭合落盘，用于进程退出等边界。
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

// SealPrimaryScreenRows 在 terminal lifecycle 边界把仍停留在 primary 当前屏的
// 热段行封存为 cold logical lines。消息链路仍是同一个 Assembler：已滚出的
// open tail 与当前屏 Wrapped 行按同一规则拼接；alt 当前屏不应传入本方法。
func (e *Engine) SealPrimaryScreenRows(rows []ScreenRow) error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var batch []Line
	for _, row := range rows {
		batch = append(batch, e.asm.AppendEvictedRow(vterm.TerminalSemanticScrollOut{
			Cells:      cloneScreenRowCells(row.Cells),
			Wrapped:    row.Wrapped,
			WrappedSet: true,
		})...)
	}
	if line, ok := e.asm.SealOpen(); ok {
		batch = append(batch, line)
	}
	if len(batch) == 0 {
		return nil
	}
	return e.file.AppendLines(batch)
}

// AppendLifecycleLines 追加 core terminal lifecycle marker。它不是 PTY 输出，
// 而是 core-v2 lifecycle owner 明确写入的历史事件；调用方必须只用于
// start/exit/restart 这类用户可见 terminal 边界，不能拿来补程序正文。
func (e *Engine) AppendLifecycleLines(texts []string) error {
	if e == nil || len(texts) == 0 {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	batch := make([]Line, 0, len(texts))
	for _, text := range texts {
		batch = append(batch, Line{
			Runs:    []Run{{Text: text}},
			HardEnd: true,
		})
	}
	return e.file.AppendLines(batch)
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

func cloneScreenRowCells(cells []vterm.Cell) []vterm.Cell {
	if len(cells) == 0 {
		return nil
	}
	out := make([]vterm.Cell, len(cells))
	copy(out, cells)
	return out
}
