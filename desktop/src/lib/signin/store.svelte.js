// AUTH's window state (desktop-design-5-6 2.4): the AWS SSO sheet's command,
// link and result, as agent_event delivers them for this window's chat.
export const awsSso = $state({
  command: "",
  url: "",
  state: "waiting", // "waiting" | "success" | "error"
  error: "",
});

export function resetAwsSso(command = "", url = "") {
  awsSso.command = command;
  awsSso.url = url;
  awsSso.state = "waiting";
  awsSso.error = "";
}
