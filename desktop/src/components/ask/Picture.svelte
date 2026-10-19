<script>
  // A choice's picture beside the answers (desktop-design-5-6 2.6 "Text,
  // links, pictures"; DLG-187): 416 picture px tall, keeping its aspect, read
  // through file_read as a data: URL (20 MB at most). A click opens it in the
  // image viewer. Texts from questionform/preview.go:260-279.
  //   path (absolute, on this computer)
  import { file_read } from "../../lib/engine/commands.js";
  import { projectDir } from "../../lib/stores/composer.svelte.js";
  import { openViewer } from "../../lib/stores/ui.svelte.js";

  const MAX_BYTES = 20 * 1024 * 1024;
  const cache = new Map(); // path → data URL | null (can't be opened), for the form's life

  let { path = "" } = $props();

  let src = $state(null);
  let failed = $state(false);

  $effect(() => {
    const wanted = path;
    src = null;
    failed = false;
    if (!wanted) return;
    if (cache.has(wanted)) {
      src = cache.get(wanted);
      failed = src === null;
      return;
    }
    let live = true;
    file_read(wanted, projectDir(), MAX_BYTES)
      .then((file) => {
        const url = file?.base64 && /^image\//.test(file.mime ?? "") ? `data:${file.mime};base64,${file.base64}` : null;
        cache.set(wanted, url);
        if (!live) return;
        src = url;
        failed = url === null;
      })
      .catch(() => {
        cache.set(wanted, null);
        if (live) failed = true;
      });
    return () => (live = false);
  });
</script>

<div class="picture" data-probe="question-picture">
  {#if !path}
    <p>No picture for this choice.</p>
  {:else if failed}
    <p>This picture can't be opened.</p>
  {:else if !src}
    <p>Loading the picture…</p>
  {:else}
    <button type="button" tabindex="-1" onclick={() => openViewer({ path, name: path.split("/").pop() })}>
      <img {src} alt={path.split("/").pop()} />
    </button>
  {/if}
</div>

<style>
  .picture {
    display: grid;
    place-items: center;
    flex: none;
    height: calc(416 * var(--px));
    max-width: 100%;
    overflow: hidden;
  }
  p {
    margin: 0;
    font: var(--t-small);
    color: var(--slate);
  }
  button {
    height: 100%;
    padding: 0;
    border: 0;
    background: none;
    cursor: zoom-in;
  }
  img {
    display: block;
    height: 100%;
    max-width: 100%;
    object-fit: contain;
  }
</style>
