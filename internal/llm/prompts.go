package llm

import (
	"embed"
	"strings"
)

//go:embed prompts/*.md
var builtin embed.FS

// Render fills prompt placeholders. Prompts ship with the code (each package embeds its own prompts/*.md),
// so they're versioned and reviewed like code; personal details come in only through placeholders.
func Render(prompt, owner string) string {
	return strings.TrimSpace(strings.ReplaceAll(prompt, "{{owner}}", owner))
}

// SystemPrompt is the chat persona.
func SystemPrompt(owner string) string {
	raw, err := builtin.ReadFile("prompts/system.md")
	if err != nil {
		panic(err) // embedded at build time; missing means a broken build
	}
	return Render(string(raw), owner)
}
