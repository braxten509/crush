// VIEW's registrations (desktop-design-5-6 2.10; loaded by lib/plugins.js):
// the dialog `filepicker`, opened by Ctrl+F (PAL's global key) and the
// palette's "Open File Picker".
import ImagePicker from "../../components/pictures/ImagePicker.svelte";
import { registerDialog } from "../dialogs.js";

registerDialog("filepicker", ImagePicker);
