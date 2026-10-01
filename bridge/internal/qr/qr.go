// Package qr renders the pairing QR code for a terminal.
package qr

import (
	"strings"

	"rsc.io/qr"
)

// Modules is the module matrix (true = dark), without quiet zone, at error correction M.
func Modules(text string) ([][]bool, bool) {
	c, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, false
	}
	rows := make([][]bool, c.Size)
	for y := range rows {
		rows[y] = make([]bool, c.Size)
		for x := range rows[y] {
			rows[y][x] = c.Black(x, y)
		}
	}
	return rows, true
}

// Terminal is black-on-white half-block rendering with a `quiet`-module quiet zone, readable
// on dark terminals.
func Terminal(text string, quiet int) (string, bool) {
	m, ok := Modules(text)
	if !ok || len(m) == 0 {
		return "", false
	}
	size := len(m[0]) + quiet*2
	blank := func() []bool { return make([]bool, size) }
	var grid [][]bool
	for range quiet {
		grid = append(grid, blank())
	}
	for _, row := range m {
		r := blank()
		copy(r[quiet:], row)
		grid = append(grid, r)
	}
	for range quiet {
		grid = append(grid, blank())
	}
	if len(grid)%2 == 1 {
		grid = append(grid, blank())
	}
	var b strings.Builder
	for y := 0; y < len(grid); y += 2 {
		b.WriteString("\x1b[30;107m")
		for x := range size {
			switch top, bottom := grid[y][x], grid[y+1][x]; {
			case top && bottom:
				b.WriteString("█")
			case top:
				b.WriteString("▀")
			case bottom:
				b.WriteString("▄")
			default:
				b.WriteString(" ")
			}
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String(), true
}
