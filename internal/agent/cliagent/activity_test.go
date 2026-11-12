package cliagent

// Content protocol tests ignore timing-dependent heartbeats.
func withoutActivity(events []Event) []Event {
	var content []Event
	for _, event := range events {
		if event.Type != EventActivity {
			content = append(content, event)
		}
	}
	return content
}
