package uiinput

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

type Style struct {
	Foreground string
	Background string
	Bold       bool
	Underline  bool
}

type RenderConfig struct {
	Prompt           string
	Width            int
	PromptStyle      Style
	TextStyle        Style
	PlaceholderStyle Style
	CompletionStyle  Style
}

type State struct {
	value       string
	cursor      int
	placeholder string
	completion  string
	initialized bool
}

func New() State {
	var state State
	state.ensure()
	return state
}

func FromLegacy(value string, cursorPos int, cursorSet bool, placeholder string) State {
	state := New()
	state.ResetFromLegacy(value, cursorPos, cursorSet, placeholder)
	return state
}

func (s State) Initialized() bool {
	return s.initialized
}

func (s State) Value() string {
	if !s.initialized {
		return ""
	}
	return s.value
}

func (s State) Position() int {
	if !s.initialized {
		return 0
	}
	return s.cursor
}

func (s State) Placeholder() string {
	if !s.initialized {
		return ""
	}
	return s.placeholder
}

func (s *State) SetPlaceholder(placeholder string) {
	if s == nil {
		return
	}
	s.ensure()
	s.placeholder = placeholder
}

func (s State) Completion() string {
	if !s.initialized {
		return ""
	}
	return s.completion
}

func (s *State) SetCompletion(completion string) {
	if s == nil {
		return
	}
	s.ensure()
	s.completion = sanitizeText(completion)
}

func (s *State) ClearCompletion() {
	if s == nil || !s.initialized {
		return
	}
	s.completion = ""
}

func (s *State) SetValue(value string) {
	if s == nil {
		return
	}
	s.ensure()
	s.value = sanitizeText(value)
	s.cursor = clamp(s.cursor, 0, len([]rune(s.value)))
}

func (s *State) SetCursor(cursorPos int) {
	if s == nil {
		return
	}
	s.ensure()
	s.cursor = clamp(cursorPos, 0, len([]rune(s.value)))
}

func (s *State) EnsureFromLegacy(value string, cursorPos int, cursorSet bool, placeholder string) {
	if s == nil || s.initialized {
		return
	}
	s.ResetFromLegacy(value, cursorPos, cursorSet, placeholder)
}

func (s *State) ResetFromLegacy(value string, cursorPos int, cursorSet bool, placeholder string) {
	if s == nil {
		return
	}
	s.ensure()
	s.placeholder = placeholder
	s.value = sanitizeText(value)
	s.completion = ""
	if cursorSet {
		s.cursor = clamp(cursorPos, 0, len([]rune(s.value)))
	} else {
		s.cursor = len([]rune(s.value))
	}
}

func (s *State) Clear() {
	if s == nil {
		return
	}
	*s = State{}
}

func (s *State) HandleKey(msg tea.KeyMsg) bool {
	if s == nil {
		return false
	}
	s.ensure()
	beforeValue := s.value
	beforeCursor := s.cursor
	switch msg.Type {
	case tea.KeyRunes:
		s.insertRunes(msg.Runes)
	case tea.KeySpace:
		s.insertRunes([]rune{' '})
	case tea.KeyBackspace:
		s.deleteRuneBeforeCursor()
	case tea.KeyDelete:
		s.deleteRuneAtCursor()
	case tea.KeyLeft:
		s.cursor = clamp(s.cursor-1, 0, len([]rune(s.value)))
	case tea.KeyRight:
		s.cursor = clamp(s.cursor+1, 0, len([]rune(s.value)))
	case tea.KeyHome:
		s.cursor = 0
	case tea.KeyEnd:
		s.cursor = len([]rune(s.value))
	default:
		return false
	}
	return beforeValue != s.value || beforeCursor != s.cursor
}

func HandlesKey(msg tea.KeyMsg) bool {
	switch msg.Type {
	case tea.KeyRunes, tea.KeySpace, tea.KeyBackspace, tea.KeyDelete, tea.KeyLeft, tea.KeyRight, tea.KeyHome, tea.KeyEnd:
		return true
	default:
		return false
	}
}

func (s State) Render(cfg RenderConfig) string {
	prompt := cfg.PromptStyle.style().Render(cfg.Prompt)
	valueStyle := cfg.TextStyle.style()
	placeholderStyle := cfg.PlaceholderStyle.style()
	completionStyle := cfg.CompletionStyle.style()

	content := s.Value()
	style := valueStyle
	if content == "" && strings.TrimSpace(s.Placeholder()) != "" {
		content = s.Placeholder()
		style = placeholderStyle
	}
	if content == "" {
		completionStyle = placeholderStyle
	}

	if content == s.Placeholder() && s.Value() == "" {
		visible := content
		padding := 0
		if cfg.Width > 0 {
			visible = s.visibleContent(content, cfg.Width)
			padding = maxInt(0, cfg.Width-xansi.StringWidth(visible))
		}
		rendered := style.Render(visible)
		if padding > 0 {
			rendered += style.Render(strings.Repeat(" ", padding))
		}
		return prompt + rendered
	}

	runes := []rune(s.value)
	cursor := clamp(s.cursor, 0, len(runes))
	start := viewportStart(runes, cursor, cfg.Width)
	before := string(runes[start:cursor])
	after := string(runes[cursor:])
	rendered, padding := renderInputSegments([]inputSegment{
		{content: before, style: valueStyle},
		{content: s.completion, style: completionStyle},
		{content: after, style: valueStyle},
	}, cfg.Width)
	if padding > 0 {
		rendered += valueStyle.Render(strings.Repeat(" ", padding))
	}
	return prompt + rendered
}

func (s State) CursorCellOffset(cfg RenderConfig) int {
	if !s.initialized || s.value == "" {
		return 0
	}
	runes := []rune(s.value)
	cursor := clamp(s.cursor, 0, len(runes))
	start := viewportStart(runes, cursor, cfg.Width)
	offset := runesDisplayWidth(runes[start:cursor])
	if cfg.Width > 0 && offset >= cfg.Width {
		return maxInt(0, cfg.Width-1)
	}
	return offset
}

func (s *State) SetCursorByCell(cell int, cfg RenderConfig) bool {
	if s == nil {
		return false
	}
	s.ensure()
	if cell < 0 {
		cell = 0
	}
	current := s.cursor
	bestPos := current
	bestOffset := 0
	bestDistance := -1
	runes := []rune(s.value)
	for pos := 0; pos <= len(runes); pos++ {
		probe := *s
		probe.cursor = pos
		offset := probe.CursorCellOffset(cfg)
		distance := abs(offset - cell)
		if bestDistance >= 0 && distance > bestDistance {
			continue
		}
		if bestDistance < 0 || distance < bestDistance || (distance == bestDistance && offset <= cell && offset >= bestOffset) {
			bestPos = pos
			bestOffset = offset
			bestDistance = distance
		}
	}
	if current == bestPos {
		return false
	}
	s.cursor = bestPos
	return true
}

func PromptWidth(prompt string) int {
	return xansi.StringWidth(prompt)
}

func ValueWidth(totalWidth int, prompt string) int {
	return maxInt(0, totalWidth-PromptWidth(prompt))
}

func LegacyCursor(value string, cursorPos int, cursorSet bool) int {
	runes := []rune(value)
	if !cursorSet {
		return len(runes)
	}
	if cursorPos < 0 {
		return 0
	}
	if cursorPos > len(runes) {
		return len(runes)
	}
	return cursorPos
}

func (s *State) ensure() {
	if s == nil || s.initialized {
		return
	}
	s.initialized = true
}

func (s State) visibleContent(content string, width int) string {
	if width <= 0 {
		return content
	}
	runes := []rune(content)
	if len(runes) == 0 {
		return ""
	}
	cursor := 0
	if content == s.value {
		cursor = clamp(s.cursor, 0, len(runes))
	}
	start := viewportStart(runes, cursor, width)
	end := viewportEnd(runes, start, width)
	if start >= len(runes) || end <= start {
		return ""
	}
	return string(runes[start:end])
}

type inputSegment struct {
	content string
	style   lipgloss.Style
}

func renderInputSegments(segments []inputSegment, width int) (string, int) {
	if width <= 0 {
		var builder strings.Builder
		for _, segment := range segments {
			if segment.content == "" {
				continue
			}
			builder.WriteString(segment.style.Render(segment.content))
		}
		return builder.String(), 0
	}
	remaining := width
	var builder strings.Builder
	for _, segment := range segments {
		if segment.content == "" || remaining <= 0 {
			continue
		}
		visible, used := takeDisplayWidth(segment.content, remaining)
		if used <= 0 {
			continue
		}
		builder.WriteString(segment.style.Render(visible))
		remaining -= used
	}
	return builder.String(), remaining
}

func takeDisplayWidth(content string, limit int) (string, int) {
	if limit <= 0 || content == "" {
		return "", 0
	}
	runes := []rune(content)
	used := 0
	end := 0
	for end < len(runes) {
		runeWidth := runeDisplayWidth(runes[end])
		if end > 0 && used+runeWidth > limit {
			break
		}
		used += runeWidth
		end++
		if used >= limit {
			break
		}
	}
	if end == 0 {
		return "", 0
	}
	return string(runes[:end]), used
}

func (s *State) insertRunes(runes []rune) {
	if len(runes) == 0 {
		return
	}
	insert := sanitizeRunes(runes)
	if len(insert) == 0 {
		return
	}
	current := []rune(s.value)
	index := clamp(s.cursor, 0, len(current))
	next := make([]rune, 0, len(current)+len(insert))
	next = append(next, current[:index]...)
	next = append(next, insert...)
	next = append(next, current[index:]...)
	s.value = string(next)
	s.cursor = index + len(insert)
}

func (s *State) deleteRuneBeforeCursor() {
	current := []rune(s.value)
	index := clamp(s.cursor, 0, len(current))
	if index <= 0 || len(current) == 0 {
		return
	}
	current = append(current[:index-1], current[index:]...)
	s.value = string(current)
	s.cursor = index - 1
}

func (s *State) deleteRuneAtCursor() {
	current := []rune(s.value)
	index := clamp(s.cursor, 0, len(current))
	if index >= len(current) || len(current) == 0 {
		return
	}
	current = append(current[:index], current[index+1:]...)
	s.value = string(current)
	s.cursor = clamp(index, 0, len(current))
}

func (s Style) style() lipgloss.Style {
	style := lipgloss.NewStyle()
	if strings.TrimSpace(s.Foreground) != "" {
		style = style.Foreground(lipgloss.Color(s.Foreground))
	}
	if strings.TrimSpace(s.Background) != "" {
		style = style.Background(lipgloss.Color(s.Background))
	}
	if s.Bold {
		style = style.Bold(true)
	}
	if s.Underline {
		style = style.Underline(true)
	}
	return style
}

func viewportStart(runes []rune, cursor int, width int) int {
	cursor = clamp(cursor, 0, len(runes))
	if width <= 0 || runesDisplayWidth(runes) <= width {
		return 0
	}
	start := 0
	targetWidth := maxInt(0, width-1)
	for start < cursor && runesDisplayWidth(runes[start:cursor]) > targetWidth {
		start++
	}
	return start
}

func viewportEnd(runes []rune, start int, width int) int {
	if start < 0 {
		start = 0
	}
	if width <= 0 {
		return len(runes)
	}
	used := 0
	end := start
	for end < len(runes) {
		runeWidth := runeDisplayWidth(runes[end])
		if end > start && used+runeWidth > width {
			break
		}
		used += runeWidth
		end++
		if used >= width {
			break
		}
	}
	return end
}

func sanitizeText(value string) string {
	return string(sanitizeRunes([]rune(value)))
}

func sanitizeRunes(runes []rune) []rune {
	if len(runes) == 0 {
		return nil
	}
	out := make([]rune, 0, len(runes))
	for _, r := range runes {
		switch r {
		case '\n', '\r', '\t':
			out = append(out, ' ')
		default:
			out = append(out, r)
		}
	}
	return out
}

func runeDisplayWidth(r rune) int {
	width := xansi.StringWidth(string(r))
	if width <= 0 {
		return 1
	}
	return width
}

func runesDisplayWidth(runes []rune) int {
	if len(runes) == 0 {
		return 0
	}
	return xansi.StringWidth(string(runes))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func abs(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
