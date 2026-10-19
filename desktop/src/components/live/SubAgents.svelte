<script>
  // The Sub-agents sheet (desktop-design-5-6 2.15; DLG-224, DLG-226, DLG-228):
  // the Background sheet with the helpers' rows, as the terminal opens one
  // dialog type under the id "sub-agents" (internal/ui/dialog/background.go:77-82).
  import Background from "./Background.svelte";
  import { SHEET } from "../../lib/live/helpers.svelte.js";
</script>

<Background kind={SHEET.helpers} />
