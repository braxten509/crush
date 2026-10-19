// A yes/no form in the typing box's place with the slot kind "update", never
// sent to the engine (desktop-design-5-6 2.19, 7.4): SYS's CLI update offer.
// onAnswer(true | false) once answered (Esc counts as No), or
// onAnswer(null) when the slot was refused or another form took it.
import QuestionForm from "../../components/ask/QuestionForm.svelte";
import { closeSlot, openSlot, ui } from "../stores/ui.svelte.js";

let serial = 0;

// → {opened, close()}
export function askYesNo({ label, question, description = "", onAnswer = () => {} }) {
  const id = `local-${++serial}`;
  let done = false;
  const finish = (answer) => {
    if (done) return;
    done = true;
    if (ui.slot?.kind === "update" && ui.slot.props?.request?.id === id) closeSlot("update");
    onAnswer(answer);
  };
  const request = { id, session_id: "", tool_call_id: "", questions: [{ id: "answer", type: "yes_no", label, question, description, choices: [] }] };
  const local = {
    onSubmit: (answers) => finish(answers[0]?.yes === true),
    onCancel: () => finish(false),
    onGone: () => finish(null),
  };
  const opened = openSlot({ kind: "update", component: QuestionForm, props: { request, local } });
  if (!opened) {
    done = true;
    onAnswer(null);
  }
  return {
    opened,
    close() {
      done = true;
      if (ui.slot?.kind === "update" && ui.slot.props?.request?.id === id) closeSlot("update");
    },
  };
}
