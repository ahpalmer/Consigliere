// Package commands defines skills' slash commands, which are handled deterministically without the LLM.
package commands

import "context"

type Info struct{ Name, Description string }

type Handler interface {
	Commands() []Info
	// Handle runs command (lower-cased, including the slash, e.g. "/scan") with args (everything after it, trimmed).
	Handle(ctx context.Context, command, args string, chatID int64) (string, error)
}
