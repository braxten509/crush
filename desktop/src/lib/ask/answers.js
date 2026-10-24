// Sending a question form's answers (desktop-design-5-6 2.6 "Answering").
// `resolved: false` means another app answered first: the form closes
// silently either way.
import { api } from "../engine/api.js";
import { prompts } from "../stores/prompts.svelte.js";
import { closeSlot, notify, requestEnd, ui } from "../stores/ui.svelte.js";
import { sessions } from "../stores/sessions.svelte.js";
import { answerBody } from "./form.js";

// Only this request's form closes: a newer one that arrived meanwhile stays.
function done(request) {
  if (prompts.question?.id === request.id) prompts.question = null;
  if (ui.slot?.kind === "question" && ui.slot.props?.request?.id === request.id) closeSlot("question");
}

export async function submitAnswers(request, answers) {
  try {
    const result = await api.answerQuestions(answerBody(request, answers));
    if (result && result.resolved === false) {
      // The form outlived the question (its run ended): the answer could
      // not reach the chat. Say so instead of closing as if it had.
      notify("error", "That question is no longer open; ask me again.");
      done(request);
      return false;
    }
    if (request.session_id === sessions.currentId && ui.follow) requestEnd();
    done(request);
    return true;
  } catch (error) {
    notify("error", error?.message ?? String(error));
    return false;
  }
}

export async function cancelQuestions(request) {
  try {
    await api.cancelQuestions();
  } catch (error) {
    notify("error", error?.message ?? String(error));
  }
  done(request);
}
