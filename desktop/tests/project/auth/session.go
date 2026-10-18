// Package auth keeps a signed-in user's session in a cookie.
package auth

import (
	"net/http"
	"time"
)

// CookieName is the session cookie's name.
const CookieName = "session"

// Lifetime is how long a session lasts.
const Lifetime = 24 * time.Hour

// NewCookie makes the session cookie for a signed-in user.
func NewCookie(token string, now time.Time) *http.Cookie {
	return &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  now.Add(Lifetime),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	}
}

// FromRequest returns the session token, or "" when there is none.
func FromRequest(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}
