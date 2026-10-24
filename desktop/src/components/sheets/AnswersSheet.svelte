<script>
  // Your answers to a question form (owner, 2026-10-20: clicking the "Your
  // answers to the questions" line opens a pop-up; picked "just your picks"):
  // each question, then only the answers chosen (✓, in ink at weight 600)
  // and any notes in italics. lib/answers.js reads them; NoticeItem opens it.
  //   answers: [{description, question, picks: [text], skipped, notes: [text]}], state
  import Sheet from "../kit/Sheet.svelte";
  import Icon from "../Icon.svelte";

  let { answers = [], state = "answered" } = $props();

  const help = [{ key: "esc", text: "close" }];
</script>

<Sheet id="answers" title={state === "cancelled" ? "Your answers (cancelled)" : "Your answers"} width="standard" {help}>
  <div class="page" data-probe="answers-sheet">
    {#each answers as answer, i (i)}
      <section class="answer">
        {#if answer.description}<p class="description">{answer.description}</p>{/if}
        {#if answer.question}<p class="question">{answer.question}</p>{/if}
        {#each answer.picks as pick, p (p)}
          <p class="pick"><span class="mark"><Icon name="check" size={12} /></span><b>{pick}</b></p>
        {/each}
        {#if answer.skipped}<p class="pick skipped">Skipped</p>{/if}
        {#each answer.notes as note, n (n)}
          <p class="note">Note: {note}</p>
        {/each}
      </section>
    {:else}
      <p class="note">No answers were sent.</p>
    {/each}
  </div>
</Sheet>

<style>
  p {
    margin: 0;
  }
  .page {
    overflow-y: auto;
    min-height: 0;
  }
  .answer + .answer {
    margin-top: calc(16 * var(--px));
  }
  .question {
    font: var(--t-item);
    color: var(--ink);
  }
  .description {
    margin-bottom: calc(4 * var(--px));
    font: var(--t-body-small);
    color: var(--slate);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .pick,
  .note {
    margin: calc(4 * var(--px)) 0 0 calc(18 * var(--px));
    font: var(--t-body-small);
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .pick {
    display: flex;
    align-items: baseline;
    gap: calc(8 * var(--px));
    color: var(--slate);
  }
  .mark {
    flex: none;
    color: var(--slate);
  }
  b {
    font-weight: 600;
    color: var(--ink);
  }
  .skipped {
    color: var(--slate);
  }
  .note {
    font-style: italic;
    color: var(--slate);
  }
</style>
