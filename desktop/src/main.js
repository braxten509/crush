import { mount } from "svelte";
import "./app.css";
import App from "./App.svelte";
import { startEvents } from "./lib/engine/events.js";
import { startKeys } from "./lib/keys.js";
import "./lib/plugins.js";
import { startProbe } from "./lib/probe.js";
import { startZoom } from "./lib/zoom.js";

startZoom();
startKeys();
const app = mount(App, { target: document.getElementById("app") });
startProbe();
startEvents();

export default app;
