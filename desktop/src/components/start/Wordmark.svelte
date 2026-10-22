<script>
  // The CRUSH AI wordmark as built in the list (ChatList.svelte, drawn type,
  // PLAN.md:130), twice its size for the landing page (START-004): iron black
  // on the paper with no animation (START-005), "HYPERCRUSH" when Hyper
  // provides the large model (internal/ui/model/header.go:45-57), and
  // "Charm™ · v<version>" under it.
  let { hyper = false, version = "" } = $props();
</script>

<div class="mark" role="img" aria-label="{hyper ? 'Hypercrush' : 'Crush'} AI, Charm{version ? `, version ${version}` : ''}">
  <p class="word" aria-hidden="true"><span class="crush">{hyper ? "Hypercrush" : "Crush"}</span> <span class="ai">AI</span></p>
  <p class="charm" aria-hidden="true">Charm™{#if version}{" · "}v{version}{/if}</p>
</div>

<style>
  .mark {
    flex: none;
  }
  .word {
    font: var(--t-wordmark-large);
    color: var(--ink);
    white-space: nowrap;
  }
  .crush {
    text-transform: uppercase;
  }
  .ai {
    margin-left: calc(28 * var(--px));
    font-weight: 400;
    color: var(--slate);
  }
  .charm {
    margin-top: calc(8 * var(--px));
    font: var(--t-meta);
    color: var(--slate);
    white-space: nowrap;
  }
</style>
