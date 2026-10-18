// Loads every registration file in lib/plugins/ (desktop-design 1.10). Each
// one registers its actions, menu items, sheets, tray sections, decoration
// sources and key handlers when imported, so later phases add surfaces
// without editing main.js or any phase 3 file.
import.meta.glob("./plugins/*.js", { eager: true });
