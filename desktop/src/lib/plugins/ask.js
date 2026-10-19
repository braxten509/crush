// ASK's registrations (desktop-design-5-6 2.6, 2.7; loaded by
// lib/plugins.js): the watcher that puts the shown chat's question batch or
// secure form in the typing box's place, and the secure form events. The
// question batch itself is P3.3's prompts store (prompts.question).
import { dropSecureForm, holdSecureForm, readSecureForms, watchAsk } from "../ask/store.svelte.js";
import { onEvent, onResync } from "../engine/events.js";

watchAsk();

onEvent("secure_form", (_kind, form) => holdSecureForm(form));
onEvent("secure_form_closed", (_kind, closed) => {
  if (closed?.form_id) dropSecureForm(closed.form_id);
});
onResync(() => readSecureForms());
