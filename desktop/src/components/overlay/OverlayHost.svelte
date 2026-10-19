<script>
  // Everything drawn over the window (desktop-design 2.1, 1.10): the help
  // sheet and the details overlay, the stack of sheets on the veil (front
  // last; every sheet but the front one is inert, so neither keys nor clicks
  // reach it), the picture viewer, and the right-click menu on top. Each
  // surface draws and places itself; the veil covers the window behind the
  // sheets with --scrim and keeps the page underneath from being clicked. A
  // front sheet registered with {veil: false} (the docked Themes sheet and
  // editor, and the Changes sheet, which draws its own lighter scrim) is
  // marked bare: no scrim behind it, while the page behind stays inert
  // (desktop-design-5-6 2.13, 5.1).
  import { dialogs, frontDialog } from "../../lib/dialogs.js";
  import { ui } from "../../lib/stores/ui.svelte.js";
  import HelpSheet from "../HelpSheet.svelte";
  import DetailsOverlay from "../panel/DetailsOverlay.svelte";
  import ImageViewer from "../panel/ImageViewer.svelte";
  import ContextMenu from "../timeline/ContextMenu.svelte";

  const front = $derived(frontDialog());
  const bare = $derived(dialogs.find((entry) => entry.id === front)?.veil === false);
</script>

<HelpSheet />
<DetailsOverlay />

{#if dialogs.length > 0}
  <div class="veil" class:bare data-veil={bare ? "none" : "veil"}>
    {#each dialogs as entry (entry.id)}
      <div class="sheet" data-dialog={entry.id} inert={entry.id === front ? undefined : true}>
        <entry.component {...entry.props} />
      </div>
    {/each}
  </div>
{/if}

<ImageViewer />

{#if ui.menu}
  <ContextMenu menu={ui.menu} />
{/if}

<style>
  /* Above the page's own layers (the conversation's rules sit at z-index 1). */
  .veil {
    position: fixed;
    inset: 0;
    z-index: 10;
    background: var(--scrim);
  }
  .veil.bare {
    background: none;
  }
  /* Each sheet gets the whole window to place itself in; the front one is last, so on top. */
  .sheet {
    position: absolute;
    inset: 0;
  }
</style>
