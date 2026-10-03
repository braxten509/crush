package cliagent

// Content protocol tests ignore timing-dependent heartbeats. The reasoning
// lifecycle test separately checks that those activity events are emitted.
func withoutActivity(events []Event) []Event {
	var content []Event
	for _, event := range events {
		if event.Type != EventActivity {
			content = append(content, event)
		}
	}
	return content
}
