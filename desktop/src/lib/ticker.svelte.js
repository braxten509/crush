// The clock every working and helpers mark steps by (desktop-design-5-6 2.20,
// look 5.2; scaffold S5, complete). Both values come from the wall clock, so
// every mark in every window shows the same frame: frame = floor(now_ms / 100)
// mod 8, second = floor(now_ms / 1000) mod 4. It ticks only while something
// reads it, and stands still (at 0) under prefers-reduced-motion.
import { createSubscriber } from "svelte/reactivity";

const FRAME_MS = 100;

export function frameAt(ms) {
  return Math.floor(ms / FRAME_MS) % 8;
}

export function secondAt(ms) {
  return Math.floor(ms / 1000) % 4;
}

function reducedMotion() {
  try {
    return globalThis.matchMedia?.("(prefers-reduced-motion: reduce)").matches === true;
  } catch {
    return false;
  }
}

// Wakes its readers at each 100 ms boundary of the wall clock while any read;
// a change of the reduced-motion setting wakes them too.
const subscribe = createSubscriber((update) => {
  let timer = null;
  const next = () => {
    if (reducedMotion()) return;
    timer = setTimeout(() => {
      update();
      next();
    }, FRAME_MS - (Date.now() % FRAME_MS));
  };
  const media = (() => {
    try {
      return globalThis.matchMedia?.("(prefers-reduced-motion: reduce)") ?? null;
    } catch {
      return null;
    }
  })();
  const changed = () => {
    clearTimeout(timer);
    update();
    next();
  };
  media?.addEventListener?.("change", changed);
  next();
  return () => {
    clearTimeout(timer);
    media?.removeEventListener?.("change", changed);
  };
});

export const ticker = {
  get frame() {
    subscribe();
    return reducedMotion() ? 0 : frameAt(Date.now());
  },
  get second() {
    subscribe();
    return reducedMotion() ? 0 : secondAt(Date.now());
  },
};
