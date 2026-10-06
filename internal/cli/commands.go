package cli

// AllCommands is the CLI's whole surface. One table drives argv parsing, help
// text, and `reflow describe`, so what an agent reads is what the binary accepts.
func AllCommands() []Command {
	out := append([]Command{}, describeCommand())
	out = append(out, paperCommands()...)
	out = append(out, tagCommands()...)
	out = append(out, entryCommands()...)
	out = append(out, subscriptionCommands()...)
	out = append(out, literatureCommands()...)
	out = append(out, aiCommands()...)
	out = append(out, statusCommands()...)
	return out
}
