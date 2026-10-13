package config

import (
	"log/slog"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// migrateDisableNotifications migrates the deprecated disable_notifications
// and notification_style fields to the unified notifications field. It checks
// both the user config (~/.config) and data config (~/.local) files. If
// disable_notifications is true, it sets notifications to "disabled" in the
// data file. If notification_style is set, it moves the value to notifications.
// Regardless of value, it removes the deprecated fields from any file that
// contains them.
func migrateDisableNotifications() {
	globalConfig := GlobalConfig()
	dataConfig := GlobalConfigData()

	var wasDisabled bool
	var styleValue string
	filesToClean := []string{}

	for _, path := range []string{globalConfig, dataConfig} {
		data, err := readFile(path)
		if err != nil {
			continue
		}
		needsClean := false
		if gjson.Get(string(data), "options.disable_notifications").Exists() {
			needsClean = true
			if gjson.Get(string(data), "options.disable_notifications").Bool() {
				wasDisabled = true
			}
		}
		if v := gjson.Get(string(data), "options.notification_style"); v.Exists() {
			needsClean = true
			if styleValue == "" {
				styleValue = v.String()
			}
		}
		if needsClean {
			filesToClean = append(filesToClean, path)
		}
	}

	if len(filesToClean) == 0 {
		return
	}

	// Determine the value to persist: notification_style takes precedence,
	// then disable_notifications: true maps to "disabled".
	migratedValue := styleValue
	if migratedValue == "" && wasDisabled {
		migratedValue = "disabled"
	}

	if migratedValue != "" {
		data, err := readFile(dataConfig)
		if err == nil {
			if !gjson.Get(string(data), "options.notifications").Exists() {
				updated, err := sjson.Set(string(data), "options.notifications", migratedValue)
				if err == nil {
					if err := atomicWriteFile(dataConfig, []byte(updated), 0o600); err != nil {
						slog.Warn("Failed to migrate to notifications field", "error", err)
					} else {
						slog.Info("Migrated notification settings to notifications field", "value", migratedValue)
					}
				}
			}
		}
	}

	// Remove deprecated fields from all files that contain them.
	for _, path := range filesToClean {
		data, err := readFile(path)
		if err != nil {
			continue
		}
		updated := string(data)
		updated, _ = sjson.Delete(updated, "options.disable_notifications")
		updated, _ = sjson.Delete(updated, "options.notification_style")
		if updated == string(data) {
			continue
		}
		if err := atomicWriteFile(path, []byte(updated), 0o600); err != nil {
			slog.Warn("Failed to write migrated config", "path", path, "error", err)
		}
	}
}
