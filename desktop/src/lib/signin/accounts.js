// Settings → Accounts' sign-in and sign-out (desktop-design-5-6 2.4, 7.4).
// Placeholder from scaffold S5 with the pinned exports; AUTH owns it (7.3).

// → Promise<"done" | "cancelled">
export async function signIn(_platform) {
  return "cancelled";
}

// → Promise<"done" | "cancelled">
export async function signOut(_platform) {
  return "cancelled";
}
