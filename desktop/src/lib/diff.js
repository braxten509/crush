// Line diffs exactly as the terminal makes them (desktop-design CHAT-107 to
// CHAT-112, D23: our own line diff, no new library). A port of go-udiff v0.4.1,
// which the terminal's diff view and its review drawer use:
//   lcs/old.go, lcs/common.go     the two-sided LCS search, limited to 50 steps
//                                 each way, and the repair of its partial answer
//   ndiff.go Lines                line edits
//   diff.go lineEdits             edits widened to whole lines
//   unified.go toUnified, String  hunks with 3 lines of context, and their text
// Go's sort.Slice (sort/zsortfunc.go, pdqsort) is ported too: lcs.fix sorts
// diagonals by length, and ties must land where Go puts them, or a large
// diff picks other hunks than the terminal.
// Then the rows the diff view draws (internal/ui/diffview): unified and side by
// side (split.go hunkToSplit), the height cap with its "…" row and the x and y
// offsets (render.go), tabs and control characters (diffview.go replaceTabs,
// escape.go). internal/diff/diff.go GenerateDiff is here for the copy text.

// -- Go's sort.Slice --

const bitsLen = (n) => (n <= 0 ? 0 : 32 - Math.clz32(n));

// Sorts arr in place the way Go's sort.Slice(arr, less) does, ties included.
export function goSortSlice(arr, less) {
  const data = {
    less: (i, j) => less(arr[i], arr[j]),
    swap: (i, j) => {
      const t = arr[i];
      arr[i] = arr[j];
      arr[j] = t;
    },
  };
  pdqsort(data, 0, arr.length, bitsLen(arr.length));
  return arr;
}

function insertionSort(data, a, b) {
  for (let i = a + 1; i < b; i++) {
    for (let j = i; j > a && data.less(j, j - 1); j--) data.swap(j, j - 1);
  }
}

function siftDown(data, lo, hi, first) {
  let root = lo;
  for (;;) {
    let child = 2 * root + 1;
    if (child >= hi) break;
    if (child + 1 < hi && data.less(first + child, first + child + 1)) child++;
    if (!data.less(first + root, first + child)) return;
    data.swap(first + root, first + child);
    root = child;
  }
}

function heapSort(data, a, b) {
  const first = a;
  const hi = b - a;
  for (let i = Math.floor((hi - 1) / 2); i >= 0; i--) siftDown(data, i, hi, first);
  for (let i = hi - 1; i >= 0; i--) {
    data.swap(first, first + i);
    siftDown(data, 0, i, first);
  }
}

const UNKNOWN_HINT = 0;
const INCREASING_HINT = 1;
const DECREASING_HINT = 2;

function pdqsort(data, a, b, limit) {
  const maxInsertion = 12;
  let wasBalanced = true;
  let wasPartitioned = true;
  for (;;) {
    const length = b - a;
    if (length <= maxInsertion) {
      insertionSort(data, a, b);
      return;
    }
    if (limit === 0) {
      heapSort(data, a, b);
      return;
    }
    if (!wasBalanced) {
      breakPatterns(data, a, b);
      limit--;
    }
    let [pivot, hint] = choosePivot(data, a, b);
    if (hint === DECREASING_HINT) {
      reverseRange(data, a, b);
      pivot = b - 1 - (pivot - a);
      hint = INCREASING_HINT;
    }
    if (wasBalanced && wasPartitioned && hint === INCREASING_HINT) {
      if (partialInsertionSort(data, a, b)) return;
    }
    if (a > 0 && !data.less(a - 1, pivot)) {
      a = partitionEqual(data, a, b, pivot);
      continue;
    }
    const [mid, alreadyPartitioned] = partition(data, a, b, pivot);
    wasPartitioned = alreadyPartitioned;
    const leftLen = mid - a;
    const rightLen = b - mid;
    const balanceThreshold = Math.floor(length / 8);
    if (leftLen < rightLen) {
      wasBalanced = leftLen >= balanceThreshold;
      pdqsort(data, a, mid, limit);
      a = mid + 1;
    } else {
      wasBalanced = rightLen >= balanceThreshold;
      pdqsort(data, mid + 1, b, limit);
      b = mid;
    }
  }
}

function partition(data, a, b, pivot) {
  data.swap(a, pivot);
  let i = a + 1;
  let j = b - 1;
  while (i <= j && data.less(i, a)) i++;
  while (i <= j && !data.less(j, a)) j--;
  if (i > j) {
    data.swap(j, a);
    return [j, true];
  }
  data.swap(i, j);
  i++;
  j--;
  for (;;) {
    while (i <= j && data.less(i, a)) i++;
    while (i <= j && !data.less(j, a)) j--;
    if (i > j) break;
    data.swap(i, j);
    i++;
    j--;
  }
  data.swap(j, a);
  return [j, false];
}

function partitionEqual(data, a, b, pivot) {
  data.swap(a, pivot);
  let i = a + 1;
  let j = b - 1;
  for (;;) {
    while (i <= j && !data.less(a, i)) i++;
    while (i <= j && data.less(a, j)) j--;
    if (i > j) break;
    data.swap(i, j);
    i++;
    j--;
  }
  return i;
}

function partialInsertionSort(data, a, b) {
  const maxSteps = 5;
  const shortestShifting = 50;
  let i = a + 1;
  for (let step = 0; step < maxSteps; step++) {
    while (i < b && !data.less(i, i - 1)) i++;
    if (i === b) return true;
    if (b - a < shortestShifting) return false;
    data.swap(i, i - 1);
    if (i - a >= 2) {
      for (let j = i - 1; j >= 1; j--) {
        if (!data.less(j, j - 1)) break;
        data.swap(j, j - 1);
      }
    }
    if (b - i >= 2) {
      for (let j = i + 1; j < b; j++) {
        if (!data.less(j, j - 1)) break;
        data.swap(j, j - 1);
      }
    }
  }
  return false;
}

const MASK64 = (1n << 64n) - 1n;

function breakPatterns(data, a, b) {
  const length = b - a;
  if (length < 8) return;
  let random = BigInt(length);
  const next = () => {
    random = (random ^ (random << 13n)) & MASK64;
    random = random ^ (random >> 7n);
    random = (random ^ (random << 17n)) & MASK64;
    return random;
  };
  const modulus = 1 << bitsLen(length);
  const base = a + Math.floor(length / 4) * 2;
  for (let idx = base - 1; idx <= base + 1; idx++) {
    let other = Number(next() & BigInt(modulus - 1));
    if (other >= length) other -= length;
    data.swap(idx, a + other);
  }
}

function choosePivot(data, a, b) {
  const shortestNinther = 50;
  const maxSwaps = 4 * 3;
  const l = b - a;
  const swaps = { n: 0 };
  const quarter = Math.floor(l / 4);
  let i = a + quarter;
  let j = a + quarter * 2;
  let k = a + quarter * 3;
  if (l >= 8) {
    if (l >= shortestNinther) {
      i = median(data, i - 1, i, i + 1, swaps);
      j = median(data, j - 1, j, j + 1, swaps);
      k = median(data, k - 1, k, k + 1, swaps);
    }
    j = median(data, i, j, k, swaps);
  }
  if (swaps.n === 0) return [j, INCREASING_HINT];
  if (swaps.n === maxSwaps) return [j, DECREASING_HINT];
  return [j, UNKNOWN_HINT];
}

function order2(data, a, b, swaps) {
  if (data.less(b, a)) {
    swaps.n++;
    return [b, a];
  }
  return [a, b];
}

function median(data, a, b, c, swaps) {
  [a, b] = order2(data, a, b, swaps);
  [b, c] = order2(data, b, c, swaps);
  [a, b] = order2(data, a, b, swaps);
  return b;
}

function reverseRange(data, a, b) {
  for (let i = a, j = b - 1; i < j; i++, j--) data.swap(i, j);
}

// -- The LCS search (lcs/old.go, lcs/common.go, lcs/labels.go) --

const MAX_DIFFS = 100; // lcs.maxDiffs; diff() searches maxDiffs/2 each way

// For each D, vec[D] has length D+1; the label for (D, k) is vec[D][(D+k)/2].
class Label {
  vec = [];
  set(D, k, x) {
    while (this.vec.length <= D) this.vec.push(null);
    if (!this.vec[D]) this.vec[D] = new Array(D + 1).fill(0);
    this.vec[D][(D + k) >> 1] = x;
  }
  get(d, k) {
    return this.vec[d][(d + k) >> 1];
  }
}

const okDK = (d, k) => d >= 0 && -d <= k && k <= d;

function commonPrefixLen(a, ai, aj, b, bi, bj) {
  const n = Math.min(aj - ai, bj - bi);
  let i = 0;
  while (i < n && a[ai + i] === b[bi + i]) i++;
  return i;
}

function commonSuffixLen(a, ai, aj, b, bi, bj) {
  const n = Math.min(aj - ai, bj - bi);
  let i = 0;
  while (i < n && a[aj - 1 - i] === b[bj - 1 - i]) i++;
  return i;
}

// Diagonals {x, y, len}: a[x+i] === b[y+i] for 0 <= i < len.
function lcsSort(l) {
  return goSortSlice(l, (p, q) => (p.x !== q.x ? p.x < q.x : p.len > q.len));
}

const EMPTY = 0;
const LEFTDOWN = 1;
const RIGHTUP = 2;
const BAD = 3;

// A greedy repair of overlapping diagonals, only when the search stopped early.
function lcsFix(l) {
  if (l.length === 0) return [];
  goSortSlice(l, (p, q) => p.len > q.len);
  const tmp = [l[0]];
  for (let i = 1; i < l.length; i++) {
    let dir = EMPTY;
    let nxt = { ...l[i] };
    for (const exist of tmp) {
      [dir, nxt] = overlap(exist, nxt);
      if (dir === EMPTY || dir === BAD) break;
    }
    if (nxt.len > 0 && dir !== BAD) tmp.push(nxt);
  }
  return lcsSort(tmp);
}

function overlap(exist, p) {
  const prop = { ...p };
  if (prop.x <= exist.x && exist.x < prop.x + prop.len) {
    prop.len -= prop.x + prop.len - exist.x;
    if (prop.len <= 0) return [EMPTY, prop];
  }
  if (exist.x <= prop.x && prop.x < exist.x + exist.len) {
    const delta = exist.x + exist.len - prop.x;
    prop.len -= delta;
    if (prop.len <= 0) return [EMPTY, prop];
    prop.x += delta;
    prop.y += delta;
  }
  if (prop.y <= exist.y && exist.y < prop.y + prop.len) {
    prop.len -= prop.y + prop.len - exist.y;
    if (prop.len <= 0) return [EMPTY, prop];
  }
  if (exist.y <= prop.y && prop.y < exist.y + exist.len) {
    const delta = exist.y + exist.len - prop.y;
    prop.len -= delta;
    if (prop.len <= 0) return [EMPTY, prop];
    prop.x += delta;
    prop.y += delta;
  }
  if (prop.x + prop.len <= exist.x && prop.y + prop.len <= exist.y) return [LEFTDOWN, prop];
  if (exist.x + exist.len <= prop.x && exist.y + exist.len <= prop.y) return [RIGHTUP, prop];
  return [BAD, prop];
}

function lcsPrepend(l, x, y) {
  if (l.length > 0) {
    const d = l[0];
    if (d.x === x + 1 && d.y === y + 1) {
      d.x = x;
      d.y = y;
      d.len++;
      return l;
    }
  }
  return [{ x, y, len: 1 }, ...l];
}

function lcsAppend(l, x, y) {
  if (l.length > 0) {
    const last = l[l.length - 1];
    if (last.x + last.len === x && last.y + last.len === y) {
      last.len++;
      return l;
    }
  }
  l.push({ x, y, len: 1 });
  return l;
}

function toDiffs(l, alen, blen) {
  const diffs = [];
  let pa = 0;
  let pb = 0;
  for (const d of l) {
    if (pa < d.x || pb < d.y) diffs.push({ start: pa, end: d.x, replStart: pb, replEnd: d.y });
    pa = d.x + d.len;
    pb = d.y + d.len;
  }
  if (pa < alen || pb < blen) diffs.push({ start: pa, end: alen, replStart: pb, replEnd: blen });
  return diffs;
}

class EditGraph {
  constructor(a, b, limit) {
    this.a = a;
    this.b = b;
    this.vf = new Label();
    this.vb = new Label();
    this.limit = limit;
    this.lx = 0;
    this.ly = 0;
    this.ux = a.length;
    this.uy = b.length;
    this.delta = a.length - b.length;
  }

  // Forward: start at (x, y), go up the diagonal as far as possible.
  lookForward(k, relx) {
    const rely = relx - k;
    let x = relx + this.lx;
    const y = rely + this.ly;
    if (x < this.ux && y < this.uy) x += commonPrefixLen(this.a, x, this.ux, this.b, y, this.uy);
    return x;
  }
  setForward(d, k, relx) {
    this.vf.set(d, k, this.lookForward(k, relx) - this.lx);
  }
  getForward(d, k) {
    return this.vf.get(d, k);
  }
  fdone(D, k) {
    const x = this.vf.get(D, k);
    const y = x - k;
    return x === this.ux && y === this.uy ? this.forwardlcs(D, k) : null;
  }
  forwardlcs(D, k) {
    let ans = [];
    for (let x = this.getForward(D, k); x !== 0 || x - k !== 0; ) {
      if (okDK(D - 1, k - 1) && x - 1 === this.getForward(D - 1, k - 1)) {
        D--;
        k--;
        x--;
        continue;
      } else if (okDK(D - 1, k + 1) && x === this.getForward(D - 1, k + 1)) {
        D--;
        k++;
        continue;
      }
      const y = x - k;
      ans = lcsPrepend(ans, x + this.lx - 1, y + this.ly - 1);
      x--;
    }
    return ans;
  }

  // Backward: start at (x, y), go down the diagonal as far as possible.
  lookBackward(k, relx) {
    const rely = relx - (k + this.delta);
    let x = relx + this.lx;
    const y = rely + this.ly;
    if (x > 0 && y > 0) x -= commonSuffixLen(this.a, 0, x, this.b, 0, y);
    return x;
  }
  setBackward(d, k, relx) {
    this.vb.set(d, k, this.lookBackward(k, relx) - this.lx);
  }
  getBackward(d, k) {
    return this.vb.get(d, k);
  }
  backwardlcs(D, k) {
    let ans = [];
    for (let x = this.getBackward(D, k); x !== this.ux || x - (k + this.delta) !== this.uy; ) {
      if (okDK(D - 1, k - 1) && x === this.getBackward(D - 1, k - 1)) {
        D--;
        k--;
        continue;
      } else if (okDK(D - 1, k + 1) && x + 1 === this.getBackward(D - 1, k + 1)) {
        D--;
        k++;
        x++;
        continue;
      }
      const y = x - (k + this.delta);
      ans = lcsAppend(ans, x + this.lx, y + this.ly);
      x++;
    }
    return ans;
  }
}

function forward(e) {
  e.setForward(0, 0, e.lx);
  let ans = e.fdone(0, 0);
  if (ans) return ans;
  for (let D = 0; D < e.limit; D++) {
    e.setForward(D + 1, -(D + 1), e.getForward(D, -D));
    if ((ans = e.fdone(D + 1, -(D + 1)))) return ans;
    e.setForward(D + 1, D + 1, e.getForward(D, D) + 1);
    if ((ans = e.fdone(D + 1, D + 1))) return ans;
    for (let k = -D + 1; k <= D - 1; k += 2) {
      const lookv = e.lookForward(k, e.getForward(D, k - 1) + 1);
      const lookh = e.lookForward(k, e.getForward(D, k + 1));
      e.setForward(D + 1, k, lookv > lookh ? lookv : lookh);
      if ((ans = e.fdone(D + 1, k))) return ans;
    }
  }
  let kmax = -e.limit - 1;
  let diagmax = -1;
  for (let k = -e.limit; k <= e.limit; k += 2) {
    const x = e.getForward(e.limit, k);
    const y = x - k;
    if (x + y > diagmax && x <= e.ux && y <= e.uy) {
      diagmax = x + y;
      kmax = k;
    }
  }
  return e.forwardlcs(e.limit, kmax);
}

function twosided(e) {
  e.setForward(0, 0, e.lx);
  e.setBackward(0, 0, e.ux);
  for (let D = 0; D < e.limit; D++) {
    let got = twoDone(e, D, D);
    if (got !== null) return twolcs(e, D, D, got);
    e.setForward(D + 1, -(D + 1), e.getForward(D, -D));
    e.setForward(D + 1, D + 1, e.getForward(D, D) + 1);
    for (let k = -D + 1; k <= D - 1; k += 2) {
      const lookv = e.lookForward(k, e.getForward(D, k - 1) + 1);
      const lookh = e.lookForward(k, e.getForward(D, k + 1));
      e.setForward(D + 1, k, lookv > lookh ? lookv : lookh);
    }
    got = twoDone(e, D + 1, D);
    if (got !== null) return twolcs(e, D + 1, D, got);
    e.setBackward(D + 1, -(D + 1), e.getBackward(D, -D) - 1);
    e.setBackward(D + 1, D + 1, e.getBackward(D, D));
    for (let k = -D + 1; k <= D - 1; k += 2) {
      const lookv = e.lookBackward(k, e.getBackward(D, k - 1));
      const lookh = e.lookBackward(k, e.getBackward(D, k + 1) - 1);
      e.setBackward(D + 1, k, lookv < lookh ? lookv : lookh);
    }
  }
  // Too many differences: join a forward and a backward partial answer.
  let kmax = -e.limit - 1;
  let diagmax = -1;
  for (let k = -e.limit; k <= e.limit; k += 2) {
    const x = e.getForward(e.limit, k);
    const y = x - k;
    if (x + y > diagmax && x <= e.ux && y <= e.uy) {
      diagmax = x + y;
      kmax = k;
    }
  }
  if (kmax < -e.limit) throw new Error(`no forward paths when limit=${e.limit}`);
  let lcs = e.forwardlcs(e.limit, kmax);
  let diagmin = 1 << 25;
  for (let k = -e.limit; k <= e.limit; k += 2) {
    const x = e.getBackward(e.limit, k);
    const y = x - (k + e.delta);
    if (x + y < diagmin && x >= 0 && y >= 0) {
      diagmin = x + y;
      kmax = k;
    }
  }
  if (kmax < -e.limit) throw new Error(`no backward paths when limit=${e.limit}`);
  lcs = lcs.concat(e.backwardlcs(e.limit, kmax));
  return lcsFix(lcs);
}

// Does Myers' lemma apply? The k where the paths meet, or null.
function twoDone(e, df, db) {
  if ((df + db + e.delta) % 2 !== 0) return null;
  const kmin = Math.max(-df, -db + e.delta);
  const kmax = Math.min(df, db + e.delta);
  for (let k = kmin; k <= kmax; k += 2) {
    const x = e.vf.get(df, k);
    const u = e.vb.get(db, k - e.delta);
    if (u <= x) {
      for (let l = k; l <= kmax; l += 2) {
        const x2 = e.vf.get(df, l);
        const y2 = x2 - l;
        const u2 = e.vb.get(db, l - e.delta);
        const v2 = u2 - l;
        if (x2 === u2 || u2 === 0 || v2 === 0 || y2 === e.uy || x2 === e.ux) return l;
      }
      return k;
    }
  }
  return null;
}

function twolcs(e, df, db, kf) {
  const x = e.vf.get(df, kf);
  const y = x - kf;
  const kb = kf - e.delta;
  const u = e.vb.get(db, kb);
  const v = u - kf;
  if (x === u) return lcsSort(e.forwardlcs(df, kf).concat(e.backwardlcs(db, kb)));
  if (u > 0 && okDK(df - 1, u - 1 - v) && e.vf.get(df - 1, u - 1 - v) === u - 1) {
    return lcsSort(e.forwardlcs(df - 1, u - 1 - v).concat(e.backwardlcs(db, kb)));
  }
  if (v > 0 && okDK(df - 1, u - (v - 1)) && e.vf.get(df - 1, u - (v - 1)) === u) {
    return lcsSort(e.forwardlcs(df - 1, u - (v - 1)).concat(e.backwardlcs(db, kb)));
  }
  if (u === 0 || v === 0 || x === e.ux || y === e.uy) {
    if (u === 0 || v === 0) return e.backwardlcs(db, kb);
    return e.forwardlcs(df, kf);
  }
  if (x + 1 <= e.ux && okDK(db - 1, x + 1 - y - e.delta) && e.vb.get(db - 1, x + 1 - y - e.delta) === x + 1) {
    return lcsSort(e.backwardlcs(db - 1, kb + 1).concat(e.forwardlcs(df, kf)));
  }
  if (y + 1 <= e.uy && okDK(db - 1, x - (y + 1) - e.delta) && e.vb.get(db - 1, x - (y + 1) - e.delta) === x) {
    return lcsSort(e.backwardlcs(db - 1, kb - 1).concat(e.forwardlcs(df, kf)));
  }
  let lcs = e.backwardlcs(db, kb);
  const oldx = e.ux;
  const oldy = e.uy;
  e.ux = u;
  e.uy = v;
  lcs = lcs.concat(forward(e));
  e.ux = oldx;
  e.uy = oldy;
  return lcsSort(lcs);
}

// The differences between two sequences of lines: [{start, end, replStart, replEnd}].
export function diffSequences(a, b) {
  const e = new EditGraph(a, b, MAX_DIFFS / 2);
  return toDiffs(twosided(e), a.length, b.length);
}

// -- Edits and hunks (ndiff.go, diff.go, unified.go) --

// The lines of text with their newlines, and the offsets of the lines' starts
// (plus the end when the last line has no newline).
export function splitLines(text) {
  const lines = [];
  const offsets = [0];
  let start = 0;
  for (let i = text.indexOf("\n"); i >= 0; i = text.indexOf("\n", start)) {
    lines.push(text.slice(start, i + 1));
    start = i + 1;
    offsets.push(start);
  }
  if (start < text.length) {
    lines.push(text.slice(start));
    offsets.push(text.length);
  }
  return [lines, offsets];
}

// udiff.Lines: edits {start, end, text} at line boundaries; offsets index before.
export function lineEdits(before, after) {
  const [beforeLines, offsets] = splitLines(before);
  const [afterLines] = splitLines(after);
  return diffSequences(beforeLines, afterLines).map((d) => ({
    start: offsets[d.start],
    end: offsets[d.end],
    text: afterLines.slice(d.replStart, d.replEnd).join(""),
  }));
}

function sortEdits(edits) {
  const sorted = edits.every((e, i) => i === 0 || edits[i - 1].start < e.start || (edits[i - 1].start === e.start && edits[i - 1].end <= e.end));
  if (sorted) return edits;
  return edits
    .map((e, i) => [e, i])
    .sort((p, q) => p[0].start - q[0].start || p[0].end - q[0].end || p[1] - q[1])
    .map(([e]) => e);
}

function expandEdit(edit, src) {
  const out = { ...edit };
  const start = out.start;
  const lastNewline = start === 0 ? -1 : src.lastIndexOf("\n", start - 1);
  const delta = start - 1 - lastNewline;
  if (delta > 0) {
    out.start -= delta;
    out.text = src.slice(start - delta, start) + out.text;
  }
  const end = out.end;
  if ((end > 0 && src[end - 1] !== "\n") || (out.text !== "" && out.text[out.text.length - 1] !== "\n")) {
    const nl = src.indexOf("\n", end);
    out.end = nl < 0 ? src.length : nl + 1;
  }
  out.text += src.slice(end, out.end);
  return out;
}

// diff.go lineEdits: every edit widened to replace whole lines.
function wholeLineEdits(src, edits) {
  edits = sortEdits(edits);
  let lastEnd = 0;
  for (const e of edits) {
    if (!(e.start >= 0 && e.start <= e.end && e.end <= src.length) || e.start < lastEnd) throw new Error("diff has inconsistent edits");
    lastEnd = e.end;
  }
  const aligned = edits.every(
    (e) =>
      !(
        e.start >= src.length ||
        (e.start > 0 && src[e.start - 1] !== "\n") ||
        (e.end > 0 && src[e.end - 1] !== "\n") ||
        (e.text !== "" && e.text[e.text.length - 1] !== "\n")
      ),
  );
  if (aligned || edits.length === 0) return edits;
  const expanded = [];
  let prev = { ...edits[0] };
  for (const edit of edits.slice(1)) {
    const between = src.slice(prev.end, edit.start);
    if (!between.includes("\n")) {
      prev.text += between + edit.text;
      prev.end = edit.end;
    } else {
      expanded.push(expandEdit(prev, src));
      prev = { ...edit };
    }
  }
  expanded.push(expandEdit(prev, src));
  return expanded;
}

export const DELETE = "delete";
export const INSERT = "insert";
export const EQUAL = "equal";

const countNewlines = (text, upto) => {
  let n = 0;
  for (let i = text.indexOf("\n"); i >= 0 && i < upto; i = text.indexOf("\n", i + 1)) n++;
  return n;
};

function addEqualLines(h, lines, start, end) {
  let delta = 0;
  for (let i = start; i < end; i++) {
    if (i < 0) continue;
    if (i >= lines.length) return delta;
    h.lines.push({ kind: EQUAL, content: lines[i] });
    delta++;
  }
  return delta;
}

// udiff.ToUnifiedDiff: {from, to, hunks: [{fromLine, toLine, lines: [{kind, content}]}]}.
// The hunks' numbers are Go's, including its one quirk: a hunk that starts
// after a hunk which joined two edits counts its new-side start without the
// joined lines (toLine is not moved on a join, unified.go:135-137).
export function toUnified(fromName, toName, content, edits, contextLines = 3) {
  const gap = contextLines * 2;
  const u = { from: fromName, to: toName, hunks: [] };
  if (edits.length === 0) return u;
  edits = wholeLineEdits(content, edits);
  const [lines] = splitLines(content);
  let h = null;
  let last = 0;
  let toLine = 0;
  for (const edit of edits) {
    const start = countNewlines(content, edit.start);
    let end = countNewlines(content, edit.end);
    if (edit.end === content.length && content.length > 0 && content[content.length - 1] !== "\n") end++;
    if (h !== null && start === last) {
      // direct extension
    } else if (h !== null && start <= last + gap) {
      addEqualLines(h, lines, last, start);
    } else {
      if (h !== null) {
        addEqualLines(h, lines, last, last + contextLines);
        u.hunks.push(h);
      }
      toLine += start - last;
      h = { fromLine: start + 1, toLine: toLine + 1, lines: [] };
      const delta = addEqualLines(h, lines, start - contextLines, start);
      h.fromLine -= delta;
      h.toLine -= delta;
    }
    last = start;
    for (let i = start; i < end; i++) {
      h.lines.push({ kind: DELETE, content: lines[i] });
      last++;
    }
    if (edit.text !== "") {
      for (const line of splitLines(edit.text)[0]) {
        h.lines.push({ kind: INSERT, content: line });
        toLine++;
      }
    }
  }
  if (h !== null) {
    addEqualLines(h, lines, last, last + contextLines);
    u.hunks.push(h);
  }
  return u;
}

// unified.String(): the text of a unified diff, "" when there are no hunks.
export function unifiedText(u) {
  if (u.hunks.length === 0) return "";
  let out = `--- ${u.from}\n+++ ${u.to}\n`;
  for (const hunk of u.hunks) {
    let fromCount = 0;
    let toCount = 0;
    for (const l of hunk.lines) {
      if (l.kind === DELETE) fromCount++;
      else if (l.kind === INSERT) toCount++;
      else {
        fromCount++;
        toCount++;
      }
    }
    out += "@@";
    if (fromCount > 1) out += ` -${hunk.fromLine},${fromCount}`;
    else if (hunk.fromLine === 1 && fromCount === 0) out += " -0,0";
    else out += ` -${hunk.fromLine}`;
    if (toCount > 1) out += ` +${hunk.toLine},${toCount}`;
    else if (hunk.toLine === 1 && toCount === 0) out += " +0,0";
    else out += ` +${hunk.toLine}`;
    out += " @@\n";
    for (const l of hunk.lines) {
      out += (l.kind === DELETE ? "-" : l.kind === INSERT ? "+" : " ") + l.content;
      if (!l.content.endsWith("\n")) out += "\n\\ No newline at end of file\n";
    }
  }
  return out;
}

// udiff.Unified: the unified diff text of old and new, "" when they are equal.
export function unifiedDiff(oldLabel, newLabel, before, after) {
  return unifiedText(toUnified(oldLabel, newLabel, before, lineEdits(before, after), 3));
}

// internal/diff GenerateDiff: the diff with a/ and b/ names and its counts.
export function generateDiff(before, after, fileName) {
  const name = fileName.startsWith("/") ? fileName.slice(1) : fileName;
  const diff = unifiedDiff(`a/${name}`, `b/${name}`, before, after);
  let additions = 0;
  let removals = 0;
  for (const line of diff.split("\n")) {
    if (line.startsWith("+") && !line.startsWith("+++")) additions++;
    else if (line.startsWith("-") && !line.startsWith("---")) removals++;
  }
  return { diff, additions, removals };
}

// -- Side by side (diffview/split.go) --

// A deleted line is paired with the next inserted line before any equal one.
export function hunkToSplit(h) {
  const lines = h.lines.slice();
  const out = { fromLine: h.fromLine, toLine: h.toLine, lines: [] };
  while (lines.length > 0) {
    const ul = lines.shift();
    const sl = { before: null, after: null };
    if (ul.kind === EQUAL) {
      sl.before = ul;
      sl.after = ul;
    } else if (ul.kind === INSERT) {
      sl.after = ul;
    } else {
      sl.before = ul;
      for (let i = 0; i < lines.length; i++) {
        if (lines[i].kind === INSERT) {
          sl.after = lines.splice(i, 1)[0];
          break;
        }
        if (lines[i].kind === EQUAL) break;
      }
    }
    out.lines.push(sl);
  }
  return out;
}

// -- The diff view's rows (diffview.go, render.go) --

export const TAB_WIDTH = 4; // common/diff.go: the terminal's diffs use TabWidth(4)

// escape.go escapeControl: control characters as their Unicode pictures.
export function escapeControl(text) {
  let out = "";
  for (const ch of text) {
    const code = ch.codePointAt(0);
    if (code <= 0x1f) out += String.fromCodePoint(0x2400 + code);
    else if (code === 0x7f) out += "␡";
    else out += ch;
  }
  return out;
}

// The cells a character takes in a monospace grid: 2 for wide East Asian
// characters, 0 for combining marks and joiners, else 1.
const WIDE =
  /[ᄀ-ᅟ⺀-〾ぁ-㏿㐀-䶿一-鿿ꀀ-꓏가-힣豈-﫿︰-﹏＀-｠￠-￦\u{1f300}-\u{1f64f}\u{1f900}-\u{1f9ff}\u{20000}-\u{3fffd}]/u;
const ZERO = /[\p{M}​-‏⁠﻿]/u;
export function charCells(ch) {
  if (ZERO.test(ch)) return 0;
  return WIDE.test(ch) ? 2 : 1;
}
export function cells(text) {
  let n = 0;
  for (const ch of text) n += charCells(ch);
  return n;
}

// The text without its first n cells (ansi.GraphemeWidth.Cut(text, n, len)).
export function cutLeft(text, n) {
  if (n <= 0) return text;
  let skipped = 0;
  let i = 0;
  const chars = [...text];
  while (i < chars.length && skipped < n) {
    skipped += charCells(chars[i]);
    i++;
  }
  while (i < chars.length && charCells(chars[i]) === 0) i++;
  return chars.slice(i).join("");
}

// One file made ready for the view: line endings and tabs normalized, its
// hunks found (3 lines of context, as the terminal's view).
export function prepareFile(file, { contextLines = 3, tabWidth = TAB_WIDTH } = {}) {
  const spaces = " ".repeat(tabWidth);
  const norm = (text) => String(text ?? "").replaceAll("\r\n", "\n").replaceAll("\t", spaces);
  const before = norm(file.before);
  const after = norm(file.after);
  const u = toUnified(file.path, file.path, before, lineEdits(before, after), contextLines);
  return { path: file.path, before, after, hunks: u.hunks };
}

function shownCounts(h) {
  let before = 0;
  let after = 0;
  for (const l of h.lines) {
    if (l.kind === EQUAL) {
      before++;
      after++;
    } else if (l.kind === INSERT) after++;
    else before++;
  }
  return [before, after];
}

// render.go hunkLineFor, without the view's padding: "@@ -a,b +c,d @@".
export function hunkHeading(h) {
  const [before, after] = shownCounts(h);
  return `@@ -${h.fromLine},${before} +${h.toLine},${after} @@`;
}

// The digits the number columns need (diffview.go detectNumDigits).
export function numberDigits(files) {
  let before = 0;
  let after = 0;
  for (const f of files) {
    for (const h of f.hunks) {
      before = Math.max(before, String(h.fromLine + h.lines.length).length);
      after = Math.max(after, String(h.toLine + h.lines.length).length);
    }
  }
  return [before, after];
}

// The text a code cell shows: the line without its newline, control
// characters drawn, the first xOffset cells cut, and whether the left "…"
// mark shows (render.go lineContent). The terminal cuts the highlighted line,
// whose color codes survive the cut, so every line that had any text gets the
// mark while scrolled, even one scrolled out of sight; an empty line does not.
function cellText(content, xOffset) {
  const line = escapeControl(content.endsWith("\n") ? content.slice(0, -1) : content);
  return { text: cutLeft(line, xOffset), lead: xOffset > 0 && line !== "" };
}

// The rows of the diff view (render.go renderUnified / renderSplit), for one or
// more prepared files. A file name row comes before each file's first hunk
// when names is true (one per file in a multi-file diff, CHAT-111). Rows:
//   {type: "file", path}
//   {type: "hunk", text}
//   unified: {type: "line", kind, before, after, text, lead}   (before/after: line numbers or null)
//   split:   {type: "pair", left: cell|null, right: cell|null}  cell: {kind, number, text, lead}
//   {type: "more", left: kind|null, right: kind|null}          the "…" row at the height cap
// height (0: no cap) keeps that many rows; when more would follow, the last
// one becomes the "…" row. yOffset drops that many rows first (clamped unless
// infinite). An empty diff has no rows.
export function diffRows(files, { layout = "unified", height = 0, xOffset = 0, yOffset = 0, infinite = false, names = files.length > 1 } = {}) {
  const split = layout === "split";
  const hunkCount = files.reduce((n, f) => n + f.hunks.length, 0);
  let total = 0;
  for (const f of files) {
    if (f.hunks.length === 0) continue;
    if (names) total++;
    for (const h of f.hunks) total += 1 + (split ? hunkToSplit(h).lines.length : h.lines.length);
  }
  if (!infinite) {
    yOffset = height > 0 ? Math.min(yOffset, Math.max(0, total - height)) : Math.min(yOffset, Math.max(0, total - 1));
    yOffset = Math.max(0, yOffset);
  }
  const rows = [];
  let printed = -yOffset;
  const add = (row) => {
    if (printed >= 0) rows.push(row);
    printed++;
  };
  const cutsOff = (lastHunk, lastLine) => height > 0 && printed + 1 === height && (!lastHunk || !lastLine);
  let hunkIndex = 0;
  outer: for (const f of files) {
    for (let i = 0; i < f.hunks.length; i++) {
      const h = f.hunks[i];
      hunkIndex++;
      if (i === 0 && names) add({ type: "file", path: f.path });
      add({ type: "hunk", text: hunkHeading(h) });
      const lastHunk = hunkIndex === hunkCount;
      if (split) {
        const sh = hunkToSplit(h);
        let before = sh.fromLine;
        let after = sh.toLine;
        for (let j = 0; j < sh.lines.length; j++) {
          const l = sh.lines[j];
          if (cutsOff(lastHunk, j + 1 === sh.lines.length)) {
            if (printed >= 0) rows.push({ type: "more", left: l.before?.kind ?? null, right: l.after?.kind ?? null });
            break outer;
          }
          const cell = (line, number) => line && { kind: line.kind, number, ...cellText(line.content, xOffset) };
          add({
            type: "pair",
            left: cell(l.before, before),
            right: cell(l.after, after),
          });
          if (l.before && l.before.kind !== INSERT) before++;
          if (l.after && l.after.kind !== DELETE) after++;
        }
      } else {
        let before = h.fromLine;
        let after = h.toLine;
        for (let j = 0; j < h.lines.length; j++) {
          const l = h.lines[j];
          if (cutsOff(lastHunk, j + 1 === h.lines.length)) {
            if (printed >= 0) rows.push({ type: "more", left: l.kind, right: l.kind });
            break outer;
          }
          add({
            type: "line",
            kind: l.kind,
            before: l.kind === INSERT ? null : before,
            after: l.kind === DELETE ? null : after,
            ...cellText(l.content, xOffset),
          });
          if (l.kind !== INSERT) before++;
          if (l.kind !== DELETE) after++;
        }
      }
    }
  }
  return { rows: height > 0 ? rows.slice(0, height) : rows, total };
}

// DLG-310: the diff tints follow the paper: a light paper takes the light set,
// a dark one the dark set; a color it cannot read counts as dark, Crush's
// default (dialog/changes.go isDark).
export function tintFor(paper) {
  const rgb = parseColor(paper);
  if (!rgb) return "dark";
  const [r, g, b] = rgb;
  return 0.2126 * r + 0.7152 * g + 0.0722 * b < 0.5 * 255 ? "dark" : "light";
}

function parseColor(value) {
  const text = String(value ?? "").trim().toLowerCase();
  let m = /^#([0-9a-f]{3}|[0-9a-f]{6})$/.exec(text);
  if (m) {
    const hex = m[1].length === 3 ? [...m[1]].map((c) => c + c).join("") : m[1];
    return [0, 2, 4].map((i) => parseInt(hex.slice(i, i + 2), 16));
  }
  m = /^rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)/.exec(text);
  if (m) return [m[1], m[2], m[3]].map(Number);
  return null;
}
