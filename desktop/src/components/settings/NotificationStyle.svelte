<script>
  // Notification Style (desktop-design-5-6 2.12; DLG-274 to DLG-280):
  // internal/ui/dialog/notifications.go. Five rows with "current" on the
  // saved one; ↑/↓ wrap, Enter saves options.notifications.
  import Field from "../kit/Field.svelte";
  import FilterList from "../kit/FilterList.svelte";
  import Sheet from "../kit/Sheet.svelte";
  import { closeDialog } from "../../lib/dialogs.js";
  import { NOTIFICATION_STYLES, notificationStyle, saveNotificationStyle } from "../../lib/settings/store.svelte.js";

  const ID = "notifications";

  let query = $state("");
  let selected = $state(Math.max(0, NOTIFICATION_STYLES.findIndex((s) => s.id === notificationStyle())));

  const help = [
    { key: "↑/↓", text: "choose" },
    { key: "enter", text: "confirm" },
    { key: "esc", text: "cancel" },
  ];

  async function choose(style) {
    if (style && (await saveNotificationStyle(style.id))) closeDialog(ID);
  }
</script>

<Sheet id={ID} title="Notification style" width="small" {help}>
  <Field bind:value={query} icon="search" placeholder="Type to filter" label="Filter styles" />
  <FilterList
    items={NOTIFICATION_STYLES}
    filterText={(style) => style.label}
    info={(style) => (style.id === notificationStyle() ? "current" : "")}
    detail={(style) => style.description}
    {query}
    bind:selected
    onChoose={(style) => choose(style)}
    label="Notification styles"
  />
</Sheet>
