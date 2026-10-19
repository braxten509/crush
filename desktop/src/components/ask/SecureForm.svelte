<script>
  // A secure entry form (desktop-design-5-6 2.7; DLG-189 to DLG-201): the
  // question form with the secure questions as masked fields. Values are
  // sent once, on submit, through apiSecret (the Rust side zeroes its copy)
  // and cleared here right after; a 422 secure_field_error keeps the form
  // open on that field, and the window reads the new field statuses. Esc
  // cancels: nothing is written. secure_form_closed closes it (lib/ask).
  //   form (proto.SecureForm)
  import { untrack } from "svelte";
  import QuestionForm from "./QuestionForm.svelte";
  import { answerBody, typeOf } from "../../lib/ask/form.js";
  import { dropSecureForm, readSecureForms } from "../../lib/ask/store.svelte.js";
  import { api } from "../../lib/engine/api.js";
  import { apiSecret } from "../../lib/engine/commands.js";
  import { notify } from "../../lib/stores/ui.svelte.js";

  let { form } = $props();

  const formId = untrack(() => form.form_id);

  async function onSubmit(answers, values) {
    // Only the ordinary questions answer here; the secure ones go in values.
    const body = answerBody(form.request, answers);
    const ordinary = new Set((form.request.questions ?? []).filter((item) => typeOf(item) !== "secure_entry").map((item) => item.id));
    const submit = { answers: [{ ...body, responses: body.responses.filter((resp) => ordinary.has(resp.request_id)) }], values };
    try {
      await apiSecret(`secure-forms/${encodeURIComponent(formId)}/submit`, submit);
      dropSecureForm(formId);
      return null;
    } catch (error) {
      if (error?.code === "secure_field_error" && error.field) {
        readSecureForms();
        return { questionId: error.field, text: error.message ?? "" };
      }
      notify("error", error?.message ?? String(error));
      return { questionId: null };
    } finally {
      for (const id of Object.keys(values)) values[id] = "";
    }
  }

  async function onCancel() {
    try {
      await api.cancelSecureForm(formId);
    } catch (error) {
      notify("error", error?.message ?? String(error));
    }
    dropSecureForm(formId);
  }
</script>

<QuestionForm request={form.request} secure={{ fields: form.fields, onSubmit, onCancel }} />
