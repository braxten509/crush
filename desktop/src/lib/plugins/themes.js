// THM's registrations (desktop-design-5-6 2.14, 3.3; loaded by
// lib/plugins.js): public/theme-boot.js already put the cached theme on the
// page before it drew; this confirms it once the engine answers. Dialogs
// `theme` and `theme_editor` (docked, no veil, so the preview shows on the
// window behind them) and `theme_new`.
import NewTheme from "../../components/themes/NewTheme.svelte";
import ThemeEditor from "../../components/themes/ThemeEditor.svelte";
import Themes from "../../components/themes/Themes.svelte";
import { registerDialog } from "../dialogs.js";
import { startThemes } from "../themes/current.svelte.js";

registerDialog("theme", Themes, { veil: false });
registerDialog("theme_editor", ThemeEditor, { veil: false });
registerDialog("theme_new", NewTheme);

startThemes();
