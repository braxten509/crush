// The words of the approved picture, used until the engine feeds real chats in
// (phase 3). Kept verbatim: the owner approved the screen with these words.

export const sample = {
  project: "crush-desktop",
  chats: {
    groups: [
      {
        label: "Today",
        chats: [
          {
            id: "login",
            title: "Login keeps redirecting",
            agent: "Claude",
            time: "10:43",
            selected: true,
            expanded: true,
            branches: [
              { id: "login-cookie", title: "Cookie lifetime", agent: "Claude", time: "10:55" },
              { id: "login-samesite", title: "Is SameSite=Lax safe?", agent: "Claude", time: "11:12" },
            ],
          },
          { id: "card-wars", title: "Card Wars stutter at 60 fps", agent: "Codex", state: { kind: "working", text: "Working…" } },
          { id: "hermodr", title: "Hermóðr sends mail twice", agent: "Grok", state: { kind: "waiting", text: "Waiting for permission" } },
        ],
      },
    ],
  },
  conversation: {
    title: "Login keeps redirecting",
    path: ["crush-desktop", "auth", "session.go"],
    agent: "Claude",
    model: "Opus 5.5 · max",
    timeline: [
      { kind: "message", id: "m1", author: "you", time: "10:42", parts: [{ text: "Why does the login page keep sending me back after I sign in?" }] },
      {
        kind: "message",
        id: "m2",
        author: "claude",
        time: "10:43",
        key: "B",
        parts: [
          { text: "The session cookie uses " },
          { text: "SameSite=Strict", href: "#samesite" },
          { text: ", so the browser drops it on the redirect back from sign-in." },
        ],
      },
      { kind: "tools", id: "t1", key: "C", count: 4, summary: "read session.go, ran tests" },
      { kind: "message", id: "m3", author: "claude", time: "10:44", parts: [{ text: "Switching to Lax fixes the loop. All 12 tests pass." }] },
    ],
  },
  usage: [
    { id: "context", label: "Context", percent: 62 },
    { id: "five-hour", label: "5-hour", percent: 86, warn: true },
    { id: "week", label: "Week", percent: 31 },
  ],
  visuals: {
    flow: {
      title: "Sign-in flow",
      steps: ["Sign in", "Check session cookie", "Home"],
    },
    table: {
      title: "Cookie settings",
      columns: ["Property", "Current", "Fixed"],
      rows: [
        ["SameSite", "Strict", "Lax"],
        ["Lifetime", "24 h", "24 h"],
      ],
    },
  },
};
