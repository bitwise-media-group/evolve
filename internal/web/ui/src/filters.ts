// Pure faceting, filtering, and sorting over the row list. Kept free of React so
// it is easy to reason about (and unit-test): the App component holds the
// Selection/Sort state and calls these.

import type { Row } from "./types";

export const FACETS = [
  { key: "provider", label: "Provider" },
  { key: "model", label: "Model" },
  { key: "plugin", label: "Plugin" },
  { key: "skill", label: "Skill" },
  { key: "type", label: "Type" },
  { key: "status", label: "Status" },
] as const;

export type FacetKey = (typeof FACETS)[number]["key"];

// Selection holds the chosen values per facet. An empty list means "no filter"
// on that facet (every value passes); within a facet the chosen values are ORed,
// and facets are ANDed together.
export type Selection = Record<FacetKey, string[]>;

export function emptySelection(): Selection {
  return { provider: [], model: [], plugin: [], skill: [], type: [], status: [] };
}

// facetValue reads the row's value for a facet (model uses the bare id).
export function facetValue(row: Row, key: FacetKey): string {
  return String(row[key as keyof Row] ?? "");
}

// passesExcept reports whether a row satisfies every facet filter except `skip`
// (pass null to test all). Used both for the row list and for faceted counts.
function passesExcept(row: Row, sel: Selection, skip: FacetKey | null): boolean {
  for (const { key } of FACETS) {
    if (key === skip) continue;
    const chosen = sel[key];
    if (chosen.length > 0 && !chosen.includes(facetValue(row, key))) return false;
  }
  return true;
}

export function matches(row: Row, sel: Selection): boolean {
  return passesExcept(row, sel, null);
}

export function filterRows(rows: Row[], sel: Selection): Row[] {
  return rows.filter((r) => matches(r, sel));
}

export interface FacetOption {
  value: string;
  count: number; // rows that would match if this value were selected
  selected: boolean;
}

// Fixed display orders for the small closed-vocabulary facets.
const TYPE_ORDER = ["trigger", "eval"];
const STATUS_ORDER = ["pass", "fail", "error"];

// facetOptions builds, for one facet, the option list with counts that respect
// every *other* facet's current selection — so a count shows how many rows
// selecting that value would yield, the standard faceted-search behaviour.
export function facetOptions(rows: Row[], sel: Selection, key: FacetKey): FacetOption[] {
  const counts = new Map<string, number>();
  for (const row of rows) {
    const v = facetValue(row, key);
    if (v === "") continue;
    if (!passesExcept(row, sel, key)) continue;
    counts.set(v, (counts.get(v) ?? 0) + 1);
  }
  // Include selected values even if they currently count zero, so a user can
  // always see and clear an active selection.
  for (const v of sel[key]) if (!counts.has(v)) counts.set(v, 0);

  const order = key === "type" ? TYPE_ORDER : key === "status" ? STATUS_ORDER : null;
  const values = [...counts.keys()].sort((a, b) => {
    if (order) {
      const ia = order.indexOf(a);
      const ib = order.indexOf(b);
      if (ia !== ib) return ia - ib;
    }
    return a.localeCompare(b);
  });
  return values.map((value) => ({
    value,
    count: counts.get(value) ?? 0,
    selected: sel[key].includes(value),
  }));
}

export function toggleFacet(sel: Selection, key: FacetKey, value: string): Selection {
  const chosen = sel[key];
  const next = chosen.includes(value)
    ? chosen.filter((v) => v !== value)
    : [...chosen, value];
  return { ...sel, [key]: next };
}

export function clearFacet(sel: Selection, key: FacetKey): Selection {
  return { ...sel, [key]: [] };
}

// ---- sorting ----

export type SortKey =
  | "plugin"
  | "skill"
  | "model"
  | "type"
  | "case"
  | "status"
  | "hits"
  | "runs"
  | "durationSeconds"
  | "costUSD"
  | "inputTokens"
  | "outputTokens";

export type SortDir = "asc" | "desc";

export interface Sort {
  key: SortKey;
  dir: SortDir;
}

const STATUS_RANK: Record<string, number> = { pass: 0, fail: 1, error: 2 };

// sortValue returns a comparable value for a column, or null when absent (nulls
// always sort last regardless of direction).
function sortValue(row: Row, key: SortKey): string | number | null {
  switch (key) {
    case "model":
      return row.display || row.model;
    case "case":
      return (row.name || row.id).toLowerCase();
    case "status":
      return STATUS_RANK[row.status] ?? 99;
    case "hits":
      return row.hits ?? null;
    case "runs":
      return row.runs ?? null;
    case "durationSeconds":
      return row.durationSeconds ?? null;
    case "costUSD":
      return row.costUSD ?? null;
    case "inputTokens":
      return row.inputTokens ?? null;
    case "outputTokens":
      return row.outputTokens ?? null;
    default:
      return String(row[key as keyof Row] ?? "").toLowerCase();
  }
}

export function sortRows(rows: Row[], sort: Sort): Row[] {
  const factor = sort.dir === "asc" ? 1 : -1;
  return [...rows].sort((a, b) => {
    const av = sortValue(a, sort.key);
    const bv = sortValue(b, sort.key);
    if (av === null && bv === null) return 0;
    if (av === null) return 1; // nulls last
    if (bv === null) return -1;
    if (typeof av === "number" && typeof bv === "number") return (av - bv) * factor;
    return String(av).localeCompare(String(bv)) * factor;
  });
}
