package main

import (
	"fmt"
	"os"
	"path/filepath"

	"relay/internal/live"
)

func main() {
	files, _ := filepath.Glob(os.Args[1] + "/*.txt")
	for _, f := range files {
		b, _ := os.ReadFile(f)
		s := live.Parse(string(b), "claude")
		text, tool := "<nil>", "<nil>"
		if s.Text != nil {
			text = *s.Text
			if len(text) > 90 {
				text = "…" + text[len(text)-90:]
			}
		}
		if s.Tool != nil {
			tool = s.Tool.Name + " " + fmt.Sprint(s.Tool.Input)
			if len(tool) > 60 {
				tool = tool[:60]
			}
		}
		fmt.Printf("%s\n   text=%q\n   tool=%q\n", filepath.Base(f), text, tool)
	}
}
