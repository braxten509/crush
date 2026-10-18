// The chat's figures for the VISUALS section (desktop-design 1.5.3, 4.3).
// Scaffold S3: the pinned exports with empty bodies; piece P3.8 writes them.

export const visuals = $state({ bySession: {}, marked: "" }); // sid → { order: [figureId], byId: { figureId: Figure } }; marked: the figure with the 1.2 s bar

// Replaces that message's figures (Figure[] in source order).
export function setFigures(sessionId, messageId, figures) {}

// Figure[] in chat order: message order, then index.
export function figuresFor(sessionId) {
  return [];
}

// A chip's click: scroll VISUALS to the figure, mark it 1.2 s (4.3).
export function revealFigure(figureId) {}
