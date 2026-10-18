import { mount } from "svelte";
import "./app.css";
import App from "./App.svelte";
import { startProbe } from "./lib/probe.js";
import { startZoom } from "./lib/zoom.js";

startZoom();
const app = mount(App, { target: document.getElementById("app") });
startProbe();

export default app;
