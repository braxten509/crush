// The desktop's theme before the page draws (desktop-design-5-6 3.3; scaffold
// S5, complete). A plain script that index.html loads in its <head>: it reads
// the theme cache THM keeps in local storage under "crush-desktop.theme",
// {name, revision, dark, tokens: {"--paper": "#f9f8ef", …}}, and sets each
// cached token and the color scheme on the root element, so the first frame
// already shows the saved theme. With no cache, or any error, app.css's
// defaults (the Scholar's edition) draw the first frame.
(function () {
  try {
    var raw = window.localStorage.getItem("crush-desktop.theme");
    if (!raw) return;
    var cache = JSON.parse(raw);
    var tokens = cache && cache.tokens;
    if (!tokens || typeof tokens !== "object") return;
    var root = document.documentElement;
    for (var name in tokens) {
      if (Object.prototype.hasOwnProperty.call(tokens, name) && name.indexOf("--") === 0 && typeof tokens[name] === "string") {
        root.style.setProperty(name, tokens[name]);
      }
    }
    root.style.colorScheme = cache.dark === true ? "dark" : "light";
  } catch (error) {
    // Any error leaves app.css's defaults.
  }
})();
