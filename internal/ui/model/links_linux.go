package model

import (
	"fmt"
	"os/exec"
	"strings"
)

// openDesktopLink keeps the launched app away from Crush's terminal. Nil
// streams connect to /dev/null, including for children left running by the
// desktop opener. Do not use the browser package's process-wide output globals:
// changing those around a call races with concurrent login/browser requests.
func openDesktopLink(target string) error {
	providers := []string{"xdg-open", "x-www-browser", "www-browser"}
	for _, provider := range providers {
		path, err := exec.LookPath(provider)
		if err != nil {
			continue
		}
		if err := exec.Command(path, target).Run(); err != nil {
			return fmt.Errorf("could not open link: %w", err)
		}
		return nil
	}
	return &exec.Error{Name: strings.Join(providers, ","), Err: exec.ErrNotFound}
}
