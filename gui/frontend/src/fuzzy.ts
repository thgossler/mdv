// Smart fuzzy-phrase matching for the navigator's filename/title filter. This
// mirrors the Go content-search engine (internal/core/search.go) so name
// filtering behaves consistently with document content search: the query's
// words must appear in order and close together within the target string,
// tolerating a few intervening words, minor spelling differences (Levenshtein
// edit distance) and a query word contained in a longer one ("approval" finds
// "approvals").

// maxPhraseGap is how many non-matching word tokens may sit between two
// consecutive query words and still count as part of the same phrase, so
// "client approvals" matches "Client-side Approvals".
const maxPhraseGap = 2;

const wordRe = /[\p{L}\p{N}]/u;

// tokenize lowercases s and splits it into its word tokens (maximal runs of
// letters and digits), so "Client-side" yields ["client", "side"].
export function tokenize(s: string): string[] {
  const out: string[] = [];
  let cur = "";
  for (const ch of s.toLowerCase()) {
    if (wordRe.test(ch)) {
      cur += ch;
    } else if (cur) {
      out.push(cur);
      cur = "";
    }
  }
  if (cur) out.push(cur);
  return out;
}

// maxEditDist returns the Levenshtein budget for a query word of n characters.
// Short words must match as a substring to avoid spurious fuzzy hits.
function maxEditDist(n: number): number {
  if (n >= 8) return 2;
  if (n >= 5) return 1;
  return 0;
}

// levenshtein computes the edit distance between two strings.
function levenshtein(a: string, b: string): number {
  const la = a.length;
  const lb = b.length;
  if (la === 0) return lb;
  if (lb === 0) return la;
  let prev = new Array<number>(lb + 1);
  let curr = new Array<number>(lb + 1);
  for (let j = 0; j <= lb; j++) prev[j] = j;
  for (let i = 1; i <= la; i++) {
    curr[0] = i;
    for (let j = 1; j <= lb; j++) {
      const cost = a[i - 1] === b[j - 1] ? 0 : 1;
      curr[j] = Math.min(prev[j] + 1, curr[j - 1] + 1, prev[j - 1] + cost);
    }
    [prev, curr] = [curr, prev];
  }
  return prev[lb];
}

// wordMatch reports whether query word q matches token t: as a substring
// (covering exact, prefix and infix matches) or within a small edit distance.
function wordMatch(q: string, t: string): boolean {
  if (!q) return false;
  if (t.includes(q)) return true;
  const d = maxEditDist(q.length);
  if (d === 0) return false;
  if (Math.abs(q.length - t.length) > d) return false;
  return levenshtein(q, t) <= d;
}

// fuzzyPhraseMatch reports whether query matches haystack as a smart fuzzy
// phrase. A blank query matches anything.
export function fuzzyPhraseMatch(haystack: string, query: string): boolean {
  const words = tokenize(query);
  if (words.length === 0) return true;
  const tokens = tokenize(haystack);
  if (tokens.length === 0) return false;

  for (let s = 0; s < tokens.length; s++) {
    if (!wordMatch(words[0], tokens[s])) continue;
    // words[0] anchored at s; align the rest allowing small gaps.
    let ti = s + 1;
    let matched = true;
    for (let qi = 1; qi < words.length; qi++) {
      let found = false;
      for (let gap = 0; ti < tokens.length && gap <= maxPhraseGap; gap++) {
        if (wordMatch(words[qi], tokens[ti])) {
          ti++;
          found = true;
          break;
        }
        ti++;
      }
      if (!found) {
        matched = false;
        break;
      }
    }
    if (matched) return true;
  }
  return false;
}
