package backend

import (
	"net/url"
	"regexp"
	"strings"
)

// urlInText finds web addresses in an error text.
var urlInText = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s"'<>]+`)

// redactSecrets removes what could carry a secret from an error text sent
// to an app: each address's user, password, query and fragment (a Google
// key check puts the key in the query, and url.Error quotes the address),
// and every given secret wherever it still appears.
func redactSecrets(text string, secrets ...string) string {
	text = urlInText.ReplaceAllStringFunc(text, func(raw string) string {
		u, err := url.Parse(raw)
		if err != nil {
			return "[address]"
		}
		u.User, u.RawQuery, u.ForceQuery, u.Fragment, u.RawFragment = nil, "", false, "", ""
		return u.String()
	})
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		for _, form := range []string{secret, url.QueryEscape(secret), url.PathEscape(secret)} {
			text = strings.ReplaceAll(text, form, "[redacted]")
		}
	}
	return text
}
