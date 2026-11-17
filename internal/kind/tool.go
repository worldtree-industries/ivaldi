package kind

// Tool returns the tool Kind: a binary for local use, installed with
// go install. It has no follow-up questions and no settings.
func Tool() Kind {
	return Kind{Name: "tool"}
}
