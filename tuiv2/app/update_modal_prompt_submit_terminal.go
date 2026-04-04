package app

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lozzow/termx/protocol"
	"github.com/lozzow/termx/tuiv2/input"
	"github.com/lozzow/termx/tuiv2/modal"
)

func (m *Model) submitCreateTerminalFormPrompt(prompt *modal.PromptState, paneID string) tea.Cmd {
	if m == nil || prompt == nil || m.modalHost == nil {
		return nil
	}
	name := promptFieldValue(prompt, "name")
	if name == "" {
		return func() tea.Msg { return inputError("name is required") }
	}
	command, err := promptCommandFromField(prompt)
	if err != nil {
		return func() tea.Msg { return err }
	}
	if len(command) == 0 {
		command = []string{"/bin/sh"}
	}
	workdir := promptWorkdirFromField(prompt)
	tags, err := promptTagsFromField(prompt)
	if err != nil {
		return func() tea.Msg { return err }
	}
	return m.submitCreateTerminal(prompt, paneID, protocol.CreateParams{
		Command: command,
		Name:    name,
		Tags:    tags,
		Dir:     workdir,
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
}

func (m *Model) submitCreateTerminalNamePrompt(prompt *modal.PromptState) tea.Cmd {
	if m == nil || prompt == nil {
		return nil
	}
	name := strings.TrimSpace(prompt.Value)
	if name == "" {
		name = strings.TrimSpace(prompt.Original)
	}
	prompt.Kind = "create-terminal-tags"
	prompt.Title = "Create Terminal"
	prompt.Hint = "[Enter] create  [Esc] cancel"
	prompt.AllowEmpty = true
	prompt.Name = name
	prompt.Value = ""
	prompt.Cursor = 0
	m.render.Invalidate()
	return nil
}

func (m *Model) submitEditTerminalNamePrompt(prompt *modal.PromptState) tea.Cmd {
	if m == nil || prompt == nil {
		return nil
	}
	name := strings.TrimSpace(prompt.Value)
	if name == "" {
		name = strings.TrimSpace(prompt.Original)
	}
	prompt.Kind = "edit-terminal-tags"
	prompt.Title = "Edit Terminal"
	prompt.Hint = "[Enter] save  [Esc] cancel"
	prompt.AllowEmpty = true
	prompt.Name = name
	prompt.Value = formatPromptTags(prompt.Tags)
	prompt.Cursor = len([]rune(prompt.Value))
	m.render.Invalidate()
	return nil
}

func (m *Model) submitEditTerminalTagsPrompt(prompt *modal.PromptState) tea.Cmd {
	if m == nil || prompt == nil || m.modalHost == nil {
		return nil
	}
	terminalID := prompt.TerminalID
	name := strings.TrimSpace(prompt.Name)
	if name == "" {
		name = strings.TrimSpace(prompt.DefaultName)
	}
	tags, err := parsePromptTags(prompt.Value)
	if err != nil {
		return func() tea.Msg { return err }
	}
	requestID := ""
	if m.modalHost.Session != nil {
		requestID = m.modalHost.Session.RequestID
	}
	m.modalHost.Close(input.ModePrompt, requestID)
	m.restorePromptReturnMode(prompt)
	m.render.Invalidate()
	return func() tea.Msg {
		client := m.runtime.Client()
		if client == nil {
			return context.Canceled
		}
		if err := client.SetMetadata(context.Background(), terminalID, name, tags); err != nil {
			return err
		}
		if m.runtime != nil && m.runtime.Registry() != nil {
			m.runtime.Registry().SetMetadata(terminalID, name, tags)
		}
		if err := saveState(m.statePath, m.workbench, m.runtime); err != nil {
			return err
		}
		m.render.Invalidate()
		return nil
	}
}

func (m *Model) submitCreateTerminalTagsPrompt(prompt *modal.PromptState, paneID string) tea.Cmd {
	if m == nil || prompt == nil {
		return nil
	}
	name := strings.TrimSpace(prompt.Name)
	if name == "" {
		name = strings.TrimSpace(prompt.DefaultName)
	}
	tags, err := parsePromptTags(prompt.Value)
	if err != nil {
		return func() tea.Msg { return err }
	}
	pane := paneID
	if pane == "" {
		pane = prompt.PaneID
	}
	command := append([]string(nil), prompt.Command...)
	if len(command) == 0 {
		command = []string{"/bin/sh"}
	}
	return m.submitCreateTerminal(prompt, paneID, protocol.CreateParams{
		Command: command,
		Name:    name,
		Tags:    tags,
		Size:    protocol.Size{Cols: 80, Rows: 24},
	})
}

func (m *Model) submitCreateTerminal(prompt *modal.PromptState, paneID string, params protocol.CreateParams) tea.Cmd {
	if m == nil || prompt == nil || m.modalHost == nil {
		return nil
	}
	pane := paneID
	if pane == "" {
		pane = prompt.PaneID
	}
	requestID := ""
	if m.modalHost.Session != nil {
		requestID = m.modalHost.Session.RequestID
	}
	m.modalHost.Close(input.ModePrompt, requestID)
	m.restorePromptReturnMode(prompt)
	m.render.Invalidate()
	return func() tea.Msg {
		client := m.runtime.Client()
		if client == nil {
			return context.Canceled
		}
		created, err := client.Create(context.Background(), params)
		if err != nil {
			return err
		}
		if m.runtime != nil && m.runtime.Registry() != nil {
			terminal := m.runtime.Registry().GetOrCreate(created.TerminalID)
			if terminal != nil {
				terminal.Name = params.Name
				terminal.Tags = cloneStringMap(params.Tags)
				terminal.Command = append([]string(nil), params.Command...)
				terminal.State = created.State
			}
		}
		switch prompt.CreateTarget {
		case modal.CreateTargetSplit:
			if cmd := m.splitPaneAndAttachTerminalCmd(pane, created.TerminalID); cmd != nil {
				return cmd()
			}
			return nil
		case modal.CreateTargetNewTab:
			if cmd := m.createTabAndAttachTerminalCmd(created.TerminalID); cmd != nil {
				return cmd()
			}
			return nil
		case modal.CreateTargetFloating:
			if cmd := m.createFloatingPaneAndAttachTerminalCmd(created.TerminalID); cmd != nil {
				return cmd()
			}
			return nil
		default:
			msgs, err := m.orchestrator.AttachAndLoadSnapshot(context.Background(), pane, created.TerminalID, "collaborator", 0, 200)
			if err != nil {
				return err
			}
			cmds := make([]tea.Cmd, 0, len(msgs))
			for _, msg := range msgs {
				value := msg
				cmds = append(cmds, func() tea.Msg { return value })
			}
			return tea.Batch(cmds...)()
		}
	}
}
