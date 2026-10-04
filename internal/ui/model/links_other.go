//go:build !linux

package model

import "github.com/pkg/browser"

func openDesktopLink(target string) error {
	return browser.OpenURL(target)
}
