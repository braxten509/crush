// Whether the gray loading layer shows right now (LoadingLayer.svelte), so the
// engine gate hides its own starting line under it instead of showing it twice.
export const loadingLayer = $state({ shown: false, label: "" });
