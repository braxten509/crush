<script>
  // A + mark where rules meet or end. It is drawn around its own 1x1 pixel cell,
  // which the parent places on the line; the arms reach `arm` picture pixels out.
  let { arm = 4, class: extra = "" } = $props();
</script>

<span class="plus {extra}" style:--arm={arm} aria-hidden="true"></span>

<style>
  .plus {
    position: absolute;
    width: 1px;
    height: 1px;
    pointer-events: none;
  }
  .plus::before,
  .plus::after {
    content: "";
    position: absolute;
    background: var(--mark);
  }
  .plus::before {
    left: calc(-1 * var(--arm) * var(--px));
    top: 0;
    width: calc(2 * var(--arm) * var(--px) + 1px);
    height: 1px;
  }
  .plus::after {
    top: calc(-1 * var(--arm) * var(--px));
    left: 0;
    width: 1px;
    height: calc(2 * var(--arm) * var(--px) + 1px);
  }
</style>
