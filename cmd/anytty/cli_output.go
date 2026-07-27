package main

import (
	"fmt"
	"io"
	"strings"

	xansi "github.com/charmbracelet/x/ansi"
)

const cliColumnGap = 2

type cliField struct {
	Label string
	Value string
}

// writeCLITable renders human output with terminal-cell-aware columns.
func writeCLITable(writer io.Writer, header []string, rows [][]string) error {
	columnCount := len(header)
	for _, row := range rows {
		if len(row) > columnCount {
			columnCount = len(row)
		}
	}
	if columnCount == 0 {
		return nil
	}

	lines := make([][]string, 0, len(rows)+1)
	if len(header) > 0 {
		lines = append(lines, normalizeCLIRow(header, columnCount))
	}
	for _, row := range rows {
		lines = append(lines, normalizeCLIRow(row, columnCount))
	}

	widths := make([]int, columnCount)
	for _, line := range lines {
		for column, cell := range line {
			if width := xansi.StringWidth(cell); width > widths[column] {
				widths[column] = width
			}
		}
	}
	for _, line := range lines {
		var output strings.Builder
		for column, cell := range line {
			output.WriteString(cell)
			if column < columnCount-1 {
				padding := widths[column] - xansi.StringWidth(cell) + cliColumnGap
				output.WriteString(strings.Repeat(" ", padding))
			}
		}
		output.WriteByte('\n')
		if _, err := io.WriteString(writer, output.String()); err != nil {
			return err
		}
	}
	return nil
}

// writeCLIFields aligns labels for compact human-readable detail output.
func writeCLIFields(writer io.Writer, fields ...cliField) error {
	width := 0
	for index := range fields {
		fields[index].Label = cleanCLICell(fields[index].Label)
		fields[index].Value = cleanCLICell(fields[index].Value)
		if labelWidth := xansi.StringWidth(fields[index].Label); labelWidth > width {
			width = labelWidth
		}
	}
	for _, field := range fields {
		padding := width - xansi.StringWidth(field.Label) + cliColumnGap
		if _, err := fmt.Fprintf(writer, "%s%s%s\n", field.Label, strings.Repeat(" ", padding), field.Value); err != nil {
			return err
		}
	}
	return nil
}

func normalizeCLIRow(row []string, columnCount int) []string {
	normalized := make([]string, columnCount)
	for column := range normalized {
		if column < len(row) {
			normalized[column] = cleanCLICell(row[column])
		} else {
			normalized[column] = "-"
		}
	}
	return normalized
}

func cleanCLICell(value string) string {
	value = strings.TrimSpace(value)
	value = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ", "\t", " ").Replace(value)
	if value == "" {
		return "-"
	}
	return value
}
