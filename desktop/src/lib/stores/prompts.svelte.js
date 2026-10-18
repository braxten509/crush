// Permission requests, question batches and waiting marks (desktop-design
// 1.5.3). Scaffold S3: the pinned exports; piece P3.3 fills them, P3.7 draws
// permission prompts and phase 5 draws question forms.

export const prompts = $state({ permission: null, question: null, pendingByRoot: {} });
