package providers

import "context"

// Tests never read OpenRouter's real host list.
func init() {
	openRouterHostOrder = func(context.Context, string, string) []string { return nil }
}
