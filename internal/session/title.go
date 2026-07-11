package session

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	tagRe       = regexp.MustCompile(`<[^>]+>`)
	commandName = regexp.MustCompile(`(?s)<command-name>\s*(.*?)\s*</command-name>`)
	commandArgs = regexp.MustCompile(`(?s)<command-args>\s*(.*?)\s*</command-args>`)
	stdoutBlock = regexp.MustCompile(`(?s)<local-command-stdout>\s*(.*?)\s*</local-command-stdout>`)
	wsRe        = regexp.MustCompile(`\s+`)
	goalSetPfx  = regexp.MustCompile(`^Goal set:\s*`)
)

// cleanText turns raw prompt content into a human sentence: it unwraps slash
// command envelopes and strips XML-ish tags and reminder noise.
func cleanText(raw string) string {
	if name := commandName.FindStringSubmatch(raw); name != nil {
		cmd := strings.TrimSpace(name[1])
		if args := commandArgs.FindStringSubmatch(raw); args != nil && strings.TrimSpace(args[1]) != "" {
			return collapse(cmd + " " + tagRe.ReplaceAllString(args[1], " "))
		}
		return collapse(cmd)
	}
	if out := stdoutBlock.FindStringSubmatch(raw); out != nil {
		// e.g. the "/goal" echo: "Goal set: <the actual goal>"
		return collapse(goalSetPfx.ReplaceAllString(tagRe.ReplaceAllString(out[1], " "), ""))
	}
	// Drop injected reminder/stdout envelopes entirely, keep real prose.
	cleaned := tagRe.ReplaceAllString(raw, " ")
	return collapse(cleaned)
}

func collapse(s string) string {
	return strings.TrimSpace(wsRe.ReplaceAllString(s, " "))
}

// Title returns a single-line title truncated to width runes (… if cut).
func (t Turn) Title(width int) string {
	s := t.PromptText()
	if s == "" {
		s = "(empty prompt)"
	}
	r := []rune(s)
	if width > 0 && len(r) > width {
		return string(r[:width-1]) + "…"
	}
	return s
}

// Detail summarizes what the turn contains, for the collapsed row's right side.
func (t Turn) Detail() string {
	var tools, results, texts, attachments int
	toolNames := map[string]bool{}
	for _, e := range t.Entries {
		switch e.Type() {
		case "assistant":
			for _, blk := range blocks(e.Obj) {
				switch str(blk["type"]) {
				case "tool_use":
					tools++
					toolNames[str(blk["name"])] = true
				case "text":
					if strings.TrimSpace(str(blk["text"])) != "" {
						texts++
					}
				}
			}
		case "user":
			for _, blk := range blocks(e.Obj) {
				if str(blk["type"]) == "tool_result" {
					results++
				}
			}
		case "attachment":
			attachments++
		}
	}
	var parts []string
	if tools > 0 {
		parts = append(parts, fmt.Sprintf("%d tool", tools))
	}
	if results > 0 {
		parts = append(parts, fmt.Sprintf("%d result", results))
	}
	if attachments > 0 {
		parts = append(parts, fmt.Sprintf("%d attach", attachments))
	}
	return strings.Join(parts, " · ")
}

// PreviewLines renders the full interaction for the expanded view.
func (t Turn) PreviewLines() []string {
	var out []string
	for _, e := range t.Entries {
		switch e.Type() {
		case "user":
			if e.IsPrompt() {
				out = append(out, "▸ you")
				out = append(out, splitNonEmpty(cleanText(textOf(e.Obj)))...)
			} else {
				out = append(out, "  · tool result")
			}
		case "assistant":
			for _, blk := range blocks(e.Obj) {
				switch str(blk["type"]) {
				case "text":
					if strings.TrimSpace(str(blk["text"])) != "" {
						out = append(out, "▸ claude")
						out = append(out, splitNonEmpty(str(blk["text"]))...)
					}
				case "thinking":
					out = append(out, "  · thinking")
				case "tool_use":
					out = append(out, "  · tool: "+str(blk["name"]))
				}
			}
		case "attachment":
			out = append(out, "  · attachment")
		}
	}
	return out
}

func blocks(obj map[string]any) []map[string]any {
	c, ok := messageContent(obj).([]any)
	if !ok {
		return nil
	}
	var out []map[string]any
	for _, b := range c {
		if m, ok := b.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func splitNonEmpty(s string) []string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l)
	}
	return out
}
