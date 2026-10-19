<script>
  // A list row's state mark (SIDE-009 to SIDE-011): the terminal's ● in each
  // state's colour (styles.Resource.*Icon, internal/ui/styles/quickstyle.go:583-589)
  // as the desktop's drawn icons, coloured as 4.11 says: errors --error-ink,
  // warnings amber, nothing green.
  import Icon from "../Icon.svelte";

  let { status } = $props();

  const ICON = { online: "dot", busy: "half-circle", offline: "offline", error: "cross", auth: "warning", disabled: "circle" };
</script>

<span class="status {status}" aria-hidden="true"><Icon name={ICON[status] ?? "offline"} size={12} /></span>

<style>
  .status {
    flex: none;
    display: grid;
    place-items: center;
    width: calc(12 * var(--px));
    height: calc(12 * var(--px));
    color: var(--slate);
  }
  .online {
    color: var(--ink);
  }
  .error {
    color: var(--error-ink);
  }
  .disabled {
    color: var(--rule-control);
  }
</style>
