// Display formatting. Absent figures render as an em dash, matching the report.

export const DASH = "—";

export function num(n?: number | null): string {
  return n == null ? DASH : n.toLocaleString();
}

export function dur(s?: number | null): string {
  return s == null ? DASH : `${s.toFixed(1)}s`;
}

export function cost(c?: number | null): string {
  if (c == null) return DASH;
  return `$${c.toFixed(c !== 0 && Math.abs(c) < 0.01 ? 4 : 2)}`;
}

export function pct(r?: number | null): string {
  return r == null ? DASH : `${Math.round(r * 100)}%`;
}

export function hitsRuns(h?: number | null, r?: number | null): string {
  return h == null || r == null ? DASH : `${h}/${r}`;
}
