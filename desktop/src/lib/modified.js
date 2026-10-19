// FILES (SIDE-008): the shown chat's modified files, counted from its file
// history as the terminal counts them (loadSessionFiles and fileList,
// internal/ui/model/session.go:151-299).
//
//   modifiedFiles(history) → [{path, additions, deletions, updatedAt}]
//       one per path: the diff from its first version to its latest, newest
//       change first; history is ws.files[sid] (File[], by version)
//   fileRows(files, {cwd, max}) → {rows: [{path, additions, deletions}], more}
//       files with changes only, each path relative to cwd and trimmed to
//       its last two folders (fsext.DirTrim); more is "…and N more" or ""
//   lineCounts(before, after) → {additions, deletions}
//       internal/diff/diff.go:10-29: the "+" and "-" lines of go-udiff
//       v0.4.1's unified diff. Its line diff (lcs/old.go) is ported line by
//       line below, with Go's own sort, because past 50 changes the search
//       gives up and keeps a best effort: the counts then depend on exactly
//       how it gives up. Like the terminal, a changed line whose own text
//       starts "++" (added) or "--" (removed) is not counted: its diff line
//       reads like a file header.
//   dirTrim(path, lim), relPath(base, target), cleanPath(path)

// --- Go's sort.Slice (sort/zsortfunc.go: pdqsort_func, Go 1.27) ---------
// The diff sorts diagonals by length, and equal lengths keep the order this
// unstable sort leaves them in.

function insertionSort(data, less, a, b) {
  for (let i = a + 1; i < b; i++) {
    for (let j = i; j > a && less(data[j], data[j - 1]); j--) swap(data, j, j - 1);
  }
}

function swap(data, i, j) {
  const t = data[i];
  data[i] = data[j];
  data[j] = t;
}

function siftDown(data, less, lo, hi, first) {
  let root = lo;
  for (;;) {
    let child = 2 * root + 1;
    if (child >= hi) break;
    if (child + 1 < hi && less(data[first + child], data[first + child + 1])) child++;
    if (!less(data[first + root], data[first + child])) return;
    swap(data, first + root, first + child);
    root = child;
  }
}

function heapSort(data, less, a, b) {
  const first = a;
  const hi = b - a;
  for (let i = Math.trunc((hi - 1) / 2); i >= 0; i--) siftDown(data, less, i, hi, first);
  for (let i = hi - 1; i >= 0; i--) {
    swap(data, first, first + i);
    siftDown(data, less, 0, i, first);
  }
}

const INCREASING = 1;
const DECREASING = 2;

function pdqsort(data, less, a, b, limit) {
  const maxInsertion = 12;
  let wasBalanced = true;
  let wasPartitioned = true;
  for (;;) {
    const length = b - a;
    if (length <= maxInsertion) {
      insertionSort(data, less, a, b);
      return;
    }
    if (limit === 0) {
      heapSort(data, less, a, b);
      return;
    }
    if (!wasBalanced) {
      breakPatterns(data, a, b);
      limit--;
    }
    let [pivot, hint] = choosePivot(data, less, a, b);
    if (hint === DECREASING) {
      reverseRange(data, a, b);
      pivot = b - 1 - (pivot - a);
      hint = INCREASING;
    }
    if (wasBalanced && wasPartitioned && hint === INCREASING && partialInsertionSort(data, less, a, b)) return;
    if (a > 0 && !less(data[a - 1], data[pivot])) {
      a = partitionEqual(data, less, a, b, pivot);
      continue;
    }
    const [mid, alreadyPartitioned] = partition(data, less, a, b, pivot);
    wasPartitioned = alreadyPartitioned;
    const leftLen = mid - a;
    const rightLen = b - mid;
    const balanceThreshold = Math.trunc(length / 8);
    if (leftLen < rightLen) {
      wasBalanced = leftLen >= balanceThreshold;
      pdqsort(data, less, a, mid, limit);
      a = mid + 1;
    } else {
      wasBalanced = rightLen >= balanceThreshold;
      pdqsort(data, less, mid + 1, b, limit);
      b = mid;
    }
  }
}

function partition(data, less, a, b, pivot) {
  swap(data, a, pivot);
  let i = a + 1;
  let j = b - 1;
  while (i <= j && less(data[i], data[a])) i++;
  while (i <= j && !less(data[j], data[a])) j--;
  if (i > j) {
    swap(data, j, a);
    return [j, true];
  }
  swap(data, i, j);
  i++;
  j--;
  for (;;) {
    while (i <= j && less(data[i], data[a])) i++;
    while (i <= j && !less(data[j], data[a])) j--;
    if (i > j) break;
    swap(data, i, j);
    i++;
    j--;
  }
  swap(data, j, a);
  return [j, false];
}

function partitionEqual(data, less, a, b, pivot) {
  swap(data, a, pivot);
  let i = a + 1;
  let j = b - 1;
  for (;;) {
    while (i <= j && !less(data[a], data[i])) i++;
    while (i <= j && less(data[a], data[j])) j--;
    if (i > j) break;
    swap(data, i, j);
    i++;
    j--;
  }
  return i;
}

function partialInsertionSort(data, less, a, b) {
  const maxSteps = 5;
  const shortestShifting = 50;
  let i = a + 1;
  for (let step = 0; step < maxSteps; step++) {
    while (i < b && !less(data[i], data[i - 1])) i++;
    if (i === b) return true;
    if (b - a < shortestShifting) return false;
    swap(data, i, i - 1);
    if (i - a >= 2) {
      for (let j = i - 1; j >= 1; j--) {
        if (!less(data[j], data[j - 1])) break;
        swap(data, j, j - 1);
      }
    }
    if (b - i >= 2) {
      for (let j = i + 1; j < b; j++) {
        if (!less(data[j], data[j - 1])) break;
        swap(data, j, j - 1);
      }
    }
  }
  return false;
}

// bits.Len: the number of bits needed for n (0 for 0).
function bitsLen(n) {
  return n <= 0 ? 0 : Math.floor(Math.log2(n)) + 1;
}

const MASK64 = (1n << 64n) - 1n;

function breakPatterns(data, a, b) {
  const length = b - a;
  if (length < 8) return;
  let random = BigInt(length); // xorshift (sort/sort.go:68-75), uint64
  const next = () => {
    random ^= (random << 13n) & MASK64;
    random ^= random >> 7n;
    random ^= (random << 17n) & MASK64;
    return random;
  };
  const modulus = 2 ** bitsLen(length);
  const middle = a + Math.trunc(length / 4) * 2;
  for (let idx = middle - 1; idx <= middle + 1; idx++) {
    let other = Number(next() & BigInt(modulus - 1));
    if (other >= length) other -= length;
    swap(data, idx, a + other);
  }
}

function choosePivot(data, less, a, b) {
  const shortestNinther = 50;
  const maxSwaps = 4 * 3;
  const l = b - a;
  const swaps = { n: 0 };
  let i = a + Math.trunc(l / 4) * 1;
  let j = a + Math.trunc(l / 4) * 2;
  let k = a + Math.trunc(l / 4) * 3;
  if (l >= 8) {
    if (l >= shortestNinther) {
      i = median(data, less, i - 1, i, i + 1, swaps);
      j = median(data, less, j - 1, j, j + 1, swaps);
      k = median(data, less, k - 1, k, k + 1, swaps);
    }
    j = median(data, less, i, j, k, swaps);
  }
  if (swaps.n === 0) return [j, INCREASING];
  if (swaps.n === maxSwaps) return [j, DECREASING];
  return [j, 0];
}

function order2(data, less, a, b, swaps) {
  if (less(data[b], data[a])) {
    swaps.n++;
    return [b, a];
  }
  return [a, b];
}

function median(data, less, a, b, c, swaps) {
  [a, b] = order2(data, less, a, b, swaps);
  [b, c] = order2(data, less, b, c, swaps);
  [a, b] = order2(data, less, a, b, swaps);
  return b;
}

function reverseRange(data, a, b) {
  for (let i = a, j = b - 1; i < j; i++, j--) swap(data, i, j);
}

// sort.Slice(x, less): less takes the two elements.
export function goSortSlice(data, less) {
  pdqsort(data, less, 0, data.length, bitsLen(data.length));
  return data;
}

// --- go-udiff v0.4.1 lcs (lcs/old.go, common.go, labels.go) -------------
// A diag {x, y, len} is a run where a[x+i] === b[y+i]; an lcs is a list of
// diags. Sequences are arrays compared with ===.

function sortLcs(lcs) {
  // Lowest x first; for equal x, the longer first.
  return goSortSlice(lcs, (p, q) => (p.x !== q.x ? p.x < q.x : p.len > q.len));
}

export function validLcs(lcs) {
  for (let i = 1; i < lcs.length; i++) {
    if (lcs[i - 1].x + lcs[i - 1].len > lcs[i].x) return false;
    if (lcs[i - 1].y + lcs[i - 1].len > lcs[i].y) return false;
  }
  return true;
}

const EMPTY = 0;
const LEFTDOWN = 1;
const RIGHTUP = 2;
const BAD = 3;

// Trims the proposed diag so it does not overlap the one already kept.
function overlap(exist, prop) {
  prop = { ...prop };
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

// Repairs an lcs whose parts overlap (only after the search gave up): the
// longest diags first, each trimmed against those kept.
export function fixLcs(l) {
  if (l.length === 0) return [];
  goSortSlice(l, (p, q) => p.len > q.len);
  const tmp = [{ ...l[0] }];
  for (let i = 1; i < l.length; i++) {
    let dir = EMPTY;
    let nxt = { ...l[i] };
    for (const kept of tmp.slice()) {
      [dir, nxt] = overlap(kept, nxt);
      if (dir === EMPTY || dir === BAD) break;
    }
    if (nxt.len > 0 && dir !== BAD) tmp.push(nxt);
  }
  return sortLcs(tmp);
}

function prepend(lcs, x, y) {
  if (lcs.length > 0) {
    const d = lcs[0];
    if (d.x === x + 1 && d.y === y + 1) {
      d.x = x;
      d.y = y;
      d.len++;
      return lcs;
    }
  }
  return [{ x, y, len: 1 }, ...lcs];
}

function append(lcs, x, y) {
  if (lcs.length > 0) {
    const last = lcs[lcs.length - 1];
    if (last.x + last.len === x && last.y + last.len === y) {
      last.len++;
      return lcs;
    }
  }
  lcs.push({ x, y, len: 1 });
  return lcs;
}

function ok(d, k) {
  return d >= 0 && -d <= k && k <= d;
}

// For each D, vec[D] holds D+1 labels; (D, k) is at (D+k)/2 (Go's integer division).
class Labels {
  constructor() {
    this.vec = [];
  }
  set(d, k, x) {
    if (!this.vec[d]) this.vec[d] = new Array(d + 1).fill(0);
    this.vec[d][(d + k) >> 1] = x;
  }
  get(d, k) {
    return this.vec[d][(d + k) >> 1];
  }
}

class EditGraph {
  constructor(a, b, limit) {
    this.a = a;
    this.b = b;
    this.vf = new Labels();
    this.vb = new Labels();
    this.limit = limit;
    this.lx = 0;
    this.ly = 0;
    this.ux = a.length;
    this.uy = b.length;
    this.delta = a.length - b.length;
  }

  commonPrefixLen(ai, aj, bi, bj) {
    const n = Math.min(aj - ai, bj - bi);
    let i = 0;
    while (i < n && this.a[ai + i] === this.b[bi + i]) i++;
    return i;
  }

  commonSuffixLen(ai, aj, bi, bj) {
    const n = Math.min(aj - ai, bj - bi);
    let i = 0;
    while (i < n && this.a[aj - 1 - i] === this.b[bj - 1 - i]) i++;
    return i;
  }

  // forward

  fdone(d, k) {
    const x = this.vf.get(d, k);
    const y = x - k;
    return x === this.ux && y === this.uy ? this.forwardlcs(d, k) : null;
  }

  forwardlcs(d, k) {
    let ans = [];
    let x = this.getForward(d, k);
    while (x !== 0 || x - k !== 0) {
      if (ok(d - 1, k - 1) && x - 1 === this.getForward(d - 1, k - 1)) {
        [d, k, x] = [d - 1, k - 1, x - 1];
        continue;
      } else if (ok(d - 1, k + 1) && x === this.getForward(d - 1, k + 1)) {
        [d, k] = [d - 1, k + 1];
        continue;
      }
      const y = x - k;
      ans = prepend(ans, x + this.lx - 1, y + this.ly - 1);
      x--;
    }
    return ans;
  }

  lookForward(k, relx) {
    const rely = relx - k;
    let x = relx + this.lx;
    const y = rely + this.ly;
    if (x < this.ux && y < this.uy) x += this.commonPrefixLen(x, this.ux, y, this.uy);
    return x;
  }

  setForward(d, k, relx) {
    const x = this.lookForward(k, relx);
    this.vf.set(d, k, x - this.lx);
  }

  getForward(d, k) {
    return this.vf.get(d, k);
  }

  // backward

  bdone(d, k) {
    const x = this.vb.get(d, k);
    const y = x - (k + this.delta);
    return x === 0 && y === 0 ? this.backwardlcs(d, k) : null;
  }

  backwardlcs(d, k) {
    let ans = [];
    let x = this.getBackward(d, k);
    while (x !== this.ux || x - (k + this.delta) !== this.uy) {
      if (ok(d - 1, k - 1) && x === this.getBackward(d - 1, k - 1)) {
        [d, k] = [d - 1, k - 1];
        continue;
      } else if (ok(d - 1, k + 1) && x + 1 === this.getBackward(d - 1, k + 1)) {
        [d, k, x] = [d - 1, k + 1, x + 1];
        continue;
      }
      const y = x - (k + this.delta);
      ans = append(ans, x + this.lx, y + this.ly);
      x++;
    }
    return ans;
  }

  lookBackward(k, relx) {
    const rely = relx - (k + this.delta);
    let x = relx + this.lx;
    const y = rely + this.ly;
    if (x > 0 && y > 0) x -= this.commonSuffixLen(0, x, 0, y);
    return x;
  }

  setBackward(d, k, relx) {
    const x = this.lookBackward(k, relx);
    this.vb.set(d, k, x - this.lx);
  }

  getBackward(d, k) {
    return this.vb.get(d, k);
  }

  // two-sided

  twoDone(df, db) {
    if ((df + db + this.delta) % 2 !== 0) return null;
    const kmin = Math.max(-df, -db + this.delta);
    const kmax = Math.min(df, db + this.delta);
    for (let k = kmin; k <= kmax; k += 2) {
      const x = this.vf.get(df, k);
      const u = this.vb.get(db, k - this.delta);
      if (u <= x) {
        for (let l = k; l <= kmax; l += 2) {
          const lx = this.vf.get(df, l);
          const ly = lx - l;
          const lu = this.vb.get(db, l - this.delta);
          const lv = lu - l;
          if (lx === lu || lu === 0 || lv === 0 || ly === this.uy || lx === this.ux) return l;
        }
        return k;
      }
    }
    return null;
  }

  twolcs(df, db, kf) {
    const x = this.vf.get(df, kf);
    const y = x - kf;
    const kb = kf - this.delta;
    const u = this.vb.get(db, kb);
    const v = u - kf;
    if (x === u) return sortLcs([...this.forwardlcs(df, kf), ...this.backwardlcs(db, kb)]);
    if (u > 0 && ok(df - 1, u - 1 - v) && this.vf.get(df - 1, u - 1 - v) === u - 1) {
      return sortLcs([...this.forwardlcs(df - 1, u - 1 - v), ...this.backwardlcs(db, kb)]);
    }
    if (v > 0 && ok(df - 1, u - (v - 1)) && this.vf.get(df - 1, u - (v - 1)) === u) {
      return sortLcs([...this.forwardlcs(df - 1, u - (v - 1)), ...this.backwardlcs(db, kb)]);
    }
    if (u === 0 || v === 0 || x === this.ux || y === this.uy) {
      if (u === 0 || v === 0) return this.backwardlcs(db, kb);
      return this.forwardlcs(df, kf);
    }
    if (x + 1 <= this.ux && ok(db - 1, x + 1 - y - this.delta) && this.vb.get(db - 1, x + 1 - y - this.delta) === x + 1) {
      return sortLcs([...this.backwardlcs(db - 1, kb + 1), ...this.forwardlcs(df, kf)]);
    }
    if (y + 1 <= this.uy && ok(db - 1, x - (y + 1) - this.delta) && this.vb.get(db - 1, x - (y + 1) - this.delta) === x) {
      return sortLcs([...this.backwardlcs(db - 1, kb - 1), ...this.forwardlcs(df, kf)]);
    }
    // Another forward path, to (u, v).
    const lcs = this.backwardlcs(db, kb);
    const [oldx, oldy] = [this.ux, this.uy];
    this.ux = u;
    this.uy = v;
    const more = forward(this);
    this.ux = oldx;
    this.uy = oldy;
    return sortLcs([...lcs, ...more]);
  }
}

function forward(e) {
  e.setForward(0, 0, e.lx);
  let done = e.fdone(0, 0);
  if (done) return done;
  for (let d = 0; d < e.limit; d++) {
    e.setForward(d + 1, -(d + 1), e.getForward(d, -d));
    if ((done = e.fdone(d + 1, -(d + 1)))) return done;
    e.setForward(d + 1, d + 1, e.getForward(d, d) + 1);
    if ((done = e.fdone(d + 1, d + 1))) return done;
    for (let k = -d + 1; k <= d - 1; k += 2) {
      const lookv = e.lookForward(k, e.getForward(d, k - 1) + 1);
      const lookh = e.lookForward(k, e.getForward(d, k + 1));
      e.setForward(d + 1, k, lookv > lookh ? lookv : lookh);
      if ((done = e.fdone(d + 1, k))) return done;
    }
  }
  // Too many changes: the farthest forward path found.
  let kmax = -e.limit - 1;
  let diagmax = -1;
  for (let k = -e.limit; k <= e.limit; k += 2) {
    const x = e.getForward(e.limit, k);
    const y = x - k;
    if (x + y > diagmax && x <= e.ux && y <= e.uy) [diagmax, kmax] = [x + y, k];
  }
  return e.forwardlcs(e.limit, kmax);
}

function backward(e) {
  e.setBackward(0, 0, e.ux);
  let done = e.bdone(0, 0);
  if (done) return done;
  for (let d = 0; d < e.limit; d++) {
    e.setBackward(d + 1, -(d + 1), e.getBackward(d, -d) - 1);
    if ((done = e.bdone(d + 1, -(d + 1)))) return done;
    e.setBackward(d + 1, d + 1, e.getBackward(d, d));
    if ((done = e.bdone(d + 1, d + 1))) return done;
    for (let k = -d + 1; k <= d - 1; k += 2) {
      const lookv = e.lookBackward(k, e.getBackward(d, k - 1));
      const lookh = e.lookBackward(k, e.getBackward(d, k + 1) - 1);
      e.setBackward(d + 1, k, lookv < lookh ? lookv : lookh);
      if ((done = e.bdone(d + 1, k))) return done;
    }
  }
  let kmax = -e.limit - 1;
  let diagmin = 1 << 25;
  for (let k = -e.limit; k <= e.limit; k += 2) {
    const x = e.getBackward(e.limit, k);
    const y = x - (k + e.delta);
    if (x + y < diagmin && x >= 0 && y >= 0) [diagmin, kmax] = [x + y, k];
  }
  if (kmax < -e.limit) throw new Error(`no paths when limit=${e.limit}`);
  return e.backwardlcs(e.limit, kmax);
}

function twosided(e) {
  e.setForward(0, 0, e.lx);
  e.setBackward(0, 0, e.ux);
  for (let d = 0; d < e.limit; d++) {
    let got = e.twoDone(d, d);
    if (got !== null) return e.twolcs(d, d, got);
    e.setForward(d + 1, -(d + 1), e.getForward(d, -d));
    e.setForward(d + 1, d + 1, e.getForward(d, d) + 1);
    for (let k = -d + 1; k <= d - 1; k += 2) {
      const lookv = e.lookForward(k, e.getForward(d, k - 1) + 1);
      const lookh = e.lookForward(k, e.getForward(d, k + 1));
      e.setForward(d + 1, k, lookv > lookh ? lookv : lookh);
    }
    got = e.twoDone(d + 1, d);
    if (got !== null) return e.twolcs(d + 1, d, got);
    e.setBackward(d + 1, -(d + 1), e.getBackward(d, -d) - 1);
    e.setBackward(d + 1, d + 1, e.getBackward(d, d));
    for (let k = -d + 1; k <= d - 1; k += 2) {
      const lookv = e.lookBackward(k, e.getBackward(d, k - 1));
      const lookh = e.lookBackward(k, e.getBackward(d, k + 1) - 1);
      e.setBackward(d + 1, k, lookv < lookh ? lookv : lookh);
    }
  }
  // Too many changes: a forward and a backward partial lcs, repaired.
  let kmax = -e.limit - 1;
  let diagmax = -1;
  for (let k = -e.limit; k <= e.limit; k += 2) {
    const x = e.getForward(e.limit, k);
    const y = x - k;
    if (x + y > diagmax && x <= e.ux && y <= e.uy) [diagmax, kmax] = [x + y, k];
  }
  if (kmax < -e.limit) throw new Error(`no forward paths when limit=${e.limit}`);
  let lcs = e.forwardlcs(e.limit, kmax);
  let diagmin = 1 << 25;
  for (let k = -e.limit; k <= e.limit; k += 2) {
    const x = e.getBackward(e.limit, k);
    const y = x - (k + e.delta);
    if (x + y < diagmin && x >= 0 && y >= 0) [diagmin, kmax] = [x + y, k];
  }
  if (kmax < -e.limit) throw new Error(`no backward paths when limit=${e.limit}`);
  lcs = [...lcs, ...e.backwardlcs(e.limit, kmax)];
  return fixLcs(lcs);
}

export const ALGORITHMS = { forward, backward, twosided };

// The diffs between a and b ({start, end, replStart, replEnd}) and the lcs.
// limit bounds the search (≤ 0: none); the diff itself uses 50.
export function computeDiff(a, b, algorithm = twosided, limit = 50) {
  const e = new EditGraph(a, b, limit <= 0 ? 1 << 25 : limit);
  const lcs = algorithm(e);
  const diffs = [];
  let pa = 0;
  let pb = 0;
  for (const d of lcs) {
    if (pa < d.x || pb < d.y) diffs.push({ start: pa, end: d.x, replStart: pb, replEnd: d.y });
    pa = d.x + d.len;
    pb = d.y + d.len;
  }
  if (pa < a.length || pb < b.length) diffs.push({ start: pa, end: a.length, replStart: pb, replEnd: b.length });
  return { diffs, lcs };
}

// go-udiff's splitLines: each line keeps its "\n"; no empty last line.
export function splitLines(text) {
  const lines = [];
  let start = 0;
  for (let i = 0; i < text.length; i++) {
    if (text.charCodeAt(i) === 10) {
      lines.push(text.slice(start, i + 1));
      start = i + 1;
    }
  }
  if (start < text.length) lines.push(text.slice(start));
  return lines;
}

export function lineCounts(before, after) {
  const a = String(before ?? "");
  const b = String(after ?? "");
  if (a === b) return { additions: 0, deletions: 0 };
  const left = splitLines(a);
  const right = splitLines(b);
  let additions = 0;
  let deletions = 0;
  for (const d of computeDiff(left, right).diffs) {
    for (let i = d.start; i < d.end; i++) if (!left[i].startsWith("--")) deletions++;
    for (let j = d.replStart; j < d.replEnd; j++) if (!right[j].startsWith("++")) additions++;
  }
  return { additions, deletions };
}

// --- the file list ----------------------------------------------------------

// The counts of one path's first and latest version; the same pair is never
// counted twice.
const counted = new Map(); // "<first id>\0<latest id>" → {additions, deletions}
const COUNTED_LIMIT = 500;

function countsFor(first, last) {
  const key = `${first.id}\u0000${last.id}\u0000${first.content?.length ?? 0}\u0000${last.content?.length ?? 0}`;
  let found = counted.get(key);
  if (!found) {
    found = lineCounts(first.content ?? "", last.content ?? "");
    counted.set(key, found);
    if (counted.size > COUNTED_LIMIT) counted.delete(counted.keys().next().value);
  }
  return found;
}

// One entry per path: its first version (lowest number, the earliest of
// equals) against its latest (highest, the earliest of equals), newest change
// first. The terminal walks a Go map, so paths changed at the same moment
// come in no set order; here they keep the history's order.
export function modifiedFiles(history) {
  const byPath = new Map();
  for (const file of Array.isArray(history) ? history : []) {
    if (!file || typeof file.path !== "string") continue;
    if (!byPath.has(file.path)) byPath.set(file.path, []);
    byPath.get(file.path).push(file);
  }
  const files = [];
  for (const [path, versions] of byPath) {
    let first = versions[0];
    let last = versions[0];
    for (const v of versions) {
      if ((v.version ?? 0) < (first.version ?? 0)) first = v;
      if ((v.version ?? 0) > (last.version ?? 0)) last = v;
    }
    const { additions, deletions } = countsFor(first, last);
    files.push({ path: first.path ?? path, additions, deletions, updatedAt: Number(last.updated_at) || 0 });
  }
  return files
    .map((file, order) => ({ file, order }))
    .sort((p, q) => q.file.updatedAt - p.file.updatedAt || p.order - q.order)
    .map(({ file }) => file);
}

// Go's path.Clean for "/" paths.
export function cleanPath(path) {
  const text = String(path ?? "");
  if (text === "") return ".";
  const rooted = text.startsWith("/");
  const out = [];
  for (const part of text.split("/")) {
    if (part === "" || part === ".") continue;
    if (part === "..") {
      if (out.length > 0 && out[out.length - 1] !== "..") out.pop();
      else if (!rooted) out.push("..");
      continue;
    }
    out.push(part);
  }
  const joined = out.join("/");
  if (rooted) return "/" + joined;
  return joined === "" ? "." : joined;
}

// Go's filepath.Rel: target relative to base, or null when it cannot be
// (one absolute and the other not, or base climbs out of what is known).
export function relPath(base, target) {
  const b = cleanPath(base);
  const t = cleanPath(target);
  if (b === t) return ".";
  const baseAbs = b.startsWith("/");
  if (baseAbs !== t.startsWith("/")) return null;
  const parts = (p) => (p === "." || p === "/" ? [] : p.replace(/^\//u, "").split("/"));
  const bp = parts(b);
  const tp = parts(t);
  let i = 0;
  while (i < bp.length && i < tp.length && bp[i] === tp[i]) i++;
  if (bp.slice(i).includes("..")) return null;
  const up = bp.slice(i).map(() => "..");
  return [...up, ...tp.slice(i)].join("/") || ".";
}

const graphemes = typeof Intl !== "undefined" && Intl.Segmenter ? new Intl.Segmenter("en", { granularity: "grapheme" }) : null;

function firstGrapheme(text) {
  if (!text) return "";
  if (graphemes) {
    for (const { segment } of graphemes.segment(text)) return segment;
  }
  return String.fromCodePoint(text.codePointAt(0));
}

// fsext.DirTrim (internal/fsext/fileutil.go:187-213): the file and the
// first letter of each of its last lim-1 folders, "..." for the rest, under
// "~". A path with lim folders or fewer stays as it is.
export function dirTrim(path, lim) {
  const dirs = String(path).split("/");
  if (lim > dirs.length - 1 || lim <= 0) return path;
  let out = "";
  for (let i = dirs.length - 1; i > 0; i--) {
    out = "/" + out;
    if (i === dirs.length - 1) out = dirs[i];
    else if (i >= dirs.length - lim) out = firstGrapheme(dirs[i]) + out;
    else {
      out = "..." + out;
      break;
    }
  }
  return cleanPath("~/" + out);
}

// The rows FILES draws: files with changes, cut at max with "…and N more"
// (fileList, session.go:248-299; N counts every file past max).
export function fileRows(files, { cwd = "", max = Infinity } = {}) {
  const changed = (Array.isArray(files) ? files : []).filter((f) => f.additions !== 0 || f.deletions !== 0);
  const shown = max > 0 ? changed.slice(0, max) : [];
  const rows = shown.map((f) => {
    const relative = cwd ? relPath(cwd, f.path) : null;
    return { path: dirTrim(relative ?? f.path, 2), fullPath: f.path, additions: f.additions, deletions: f.deletions };
  });
  const more = changed.length > max && max > 0 ? `…and ${changed.length - max} more` : "";
  return { rows, more };
}
