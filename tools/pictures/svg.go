package main

import (
	"fmt"
	"html"
	"strings"
)

// terminalPicture is a terminal session drawn as an SVG card.
type terminalPicture struct {
	title, command string
	lines          []string
	keep           func(i int) bool // lines left out are counted on one muted line
	mark           func(line string) bool
	end            string // a last line, such as ^C
	caption        []string
}

const (
	pad        = 28
	lineHeight = 21
	charWidth  = 8.2 // 13.5 px monospace advance, rounded up
)

// terminal draws the picture with the palette of the architecture picture (docs/img/architecture.svg), light and dark.
func terminal(p terminalPicture) []byte {
	type row struct{ text, class string }
	rows := []row{{"examples $ " + p.command, "cmd"}}
	for i := 0; i < len(p.lines); {
		if p.keep(i) {
			class := "out"
			if p.mark(p.lines[i]) {
				class = "mark"
			}
			rows = append(rows, row{p.lines[i], class})
			i++
			continue
		}
		j := i
		for j < len(p.lines) && !p.keep(j) {
			j++
		}
		rows = append(rows, row{fmt.Sprintf("… %d lines left out …", j-i), "skip"})
		i = j
	}
	if p.end != "" {
		rows = append(rows, row{p.end, "cmd"})
	}
	longest := 0
	for _, r := range rows {
		longest = max(longest, len([]rune(r.text)))
	}
	width := max(1000, int(float64(longest)*charWidth)+2*(pad+20))
	termTop := pad + 40
	termHeight := 2*18 + len(rows)*lineHeight
	height := termTop + termHeight + 24 + len(p.caption)*19 + pad
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" role="img" aria-labelledby="t d">
`, width, height, width, height)
	fmt.Fprintf(&b, "  <title id=\"t\">%s</title>\n  <desc id=\"d\">%s</desc>\n", esc(p.title), esc(strings.Join(p.caption, " ")))
	b.WriteString(`  <style>
    .bg { fill: #f6f4f0; }
    .term { fill: #16191d; }
    .h { font: 700 17px system-ui, -apple-system, 'Segoe UI', sans-serif; fill: #16181b; }
    .k { font: 600 11px ui-monospace, 'SF Mono', Menlo, monospace; letter-spacing: .1em; fill: #595e65; }
    .cap { font: 400 13px system-ui, -apple-system, 'Segoe UI', sans-serif; fill: #33373c; }
    .cmd, .out, .mark, .skip { font: 500 13.5px ui-monospace, 'SF Mono', Menlo, monospace; white-space: pre; }
    .cmd { fill: #9da3aa; } .out { fill: #e9e6e1; } .mark { fill: #ff7a52; font-weight: 700; } .skip { fill: #7d838a; font-style: italic; }
    .dot { fill: #e4502a; }
    @media (prefers-color-scheme: dark) {
      .bg { fill: #0d0e10; } .term { fill: #1b1e22; } .h { fill: #ece9e4; } .k { fill: #a2a7ad; } .cap { fill: #cbc7c0; }
    }
  </style>
`)
	fmt.Fprintf(&b, "  <rect class=\"bg\" width=\"%d\" height=\"%d\" rx=\"18\"/>\n", width, height)
	fmt.Fprintf(&b, "  <text class=\"k\" x=\"%d\" y=\"%d\">PDIGGER FACTORY SDK · GO</text>\n", pad, pad+8)
	fmt.Fprintf(&b, "  <text class=\"h\" x=\"%d\" y=\"%d\">%s</text>\n", pad, pad+30, esc(p.title))
	fmt.Fprintf(&b, "  <rect class=\"term\" x=\"%d\" y=\"%d\" width=\"%d\" height=\"%d\" rx=\"12\"/>\n", pad, termTop+8,
		width-2*pad, termHeight)
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&b, "  <circle class=\"dot\" cx=\"%d\" cy=\"%d\" r=\"4\" opacity=\"%.2f\"/>\n", width-pad-20-i*14, termTop+22,
			1-0.3*float64(i))
	}
	y := termTop + 8 + 18 + 15
	for _, r := range rows {
		fmt.Fprintf(&b, "  <text class=\"%s\" x=\"%d\" y=\"%d\">%s</text>\n", r.class, pad+20, y, esc(r.text))
		y += lineHeight
	}
	y = termTop + 8 + termHeight + 26
	for _, c := range p.caption {
		fmt.Fprintf(&b, "  <text class=\"cap\" x=\"%d\" y=\"%d\">%s</text>\n", pad, y, esc(c))
		y += 19
	}
	b.WriteString("</svg>\n")
	return []byte(b.String())
}

func esc(s string) string { return html.EscapeString(s) }
