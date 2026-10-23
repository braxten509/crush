import { beforeEach, expect, it, vi } from "vitest";

const h = vi.hoisted(() => ({
  answer: vi.fn(), end: vi.fn(), close: vi.fn(), notify: vi.fn(),
  sessions: { currentId: "chat" }, prompts: { question: null }, ui: { slot: null, follow: true },
}));
vi.mock("../engine/api.js", () => ({ api: { answerQuestions: h.answer } }));
vi.mock("../stores/prompts.svelte.js", () => ({ prompts: h.prompts }));
vi.mock("../stores/sessions.svelte.js", () => ({ sessions: h.sessions }));
vi.mock("../stores/ui.svelte.js", () => ({ ui: h.ui, closeSlot: h.close, notify: h.notify, requestEnd: h.end }));
import { submitAnswers } from "./answers.js";

const request = { id: "question", session_id: "chat", questions: [{ id: "answer", type: "yes_no" }] };
beforeEach(() => {
  vi.clearAllMocks();
  h.sessions.currentId = "chat";
  h.ui.follow = true;
  h.prompts.question = request;
  h.ui.slot = { kind: "question", props: { request } };
  h.answer.mockResolvedValue({ resolved: true });
});

it("answering brings the current chat back to following after a small upward scroll", async () => {
  expect(await submitAnswers(request, [{ question_id: "answer", yes: true }])).toBe(true);
  expect(h.end).toHaveBeenCalledOnce();
  expect(h.close).toHaveBeenCalledWith("question");
});

it("a failed answer or a reply after switching chats does not move the shown chat", async () => {
  h.answer.mockRejectedValueOnce(new Error("offline"));
  expect(await submitAnswers(request, [])).toBe(false);
  expect(h.end).not.toHaveBeenCalled();
  h.answer.mockImplementationOnce(async () => {
    h.sessions.currentId = "another-chat";
    return { resolved: true };
  });
  expect(await submitAnswers(request, [])).toBe(true);
  expect(h.end).not.toHaveBeenCalled();
});

it("answering while reading well above the bottom preserves the reading position", async () => {
  h.ui.follow = false;
  expect(await submitAnswers(request, [])).toBe(true);
  expect(h.end).not.toHaveBeenCalled();
});
