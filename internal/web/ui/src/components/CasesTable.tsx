// The per-case table: one row per trigger query / eval case, with sortable
// columns (click a header to toggle asc/desc).

import type { Row } from "../types";
import type { Sort, SortKey } from "../filters";
import { cost, dur, DASH, hitsRuns, num } from "../format";
import { StatusPill } from "./StatusPill";

interface Props {
  rows: Row[];
  sort: Sort;
  onSort: (key: SortKey) => void;
}

interface Col {
  key: SortKey;
  label: string;
  numeric?: boolean;
}

const COLS: Col[] = [
  { key: "plugin", label: "Plugin" },
  { key: "skill", label: "Skill" },
  { key: "model", label: "Model" },
  { key: "type", label: "Type" },
  { key: "case", label: "Case" },
  { key: "status", label: "Status" },
  { key: "hits", label: "Hits/Runs", numeric: true },
  { key: "durationSeconds", label: "Time", numeric: true },
  { key: "costUSD", label: "Cost", numeric: true },
  { key: "inputTokens", label: "In", numeric: true },
  { key: "outputTokens", label: "Out", numeric: true },
];

export function CasesTable({ rows, sort, onSort }: Props) {
  return (
    <table className="grid">
      <thead>
        <tr>
          {COLS.map((c) => (
            <th
              key={c.key}
              className={`${c.numeric ? "num " : ""}sortable${sort.key === c.key ? " active" : ""}`}
              onClick={() => onSort(c.key)}
            >
              {c.label}
              <span className="arrow">
                {sort.key === c.key ? (sort.dir === "asc" ? "▲" : "▼") : ""}
              </span>
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((r, i) => (
          <tr key={`${r.modelKey}-${r.type}-${r.id}-${i}`}>
            <td>{r.plugin}</td>
            <td>{r.skill}</td>
            <td>
              <span className={`tag prov-${r.provider}`}>{r.provider}</span>
              <span className="model-id">{r.display || r.model}</span>
            </td>
            <td>
              <span className={`tag type-${r.type}`}>{r.type}</span>
            </td>
            <td className="case-cell" title={r.id}>
              {r.name || r.id}
            </td>
            <td>
              <StatusPill status={r.status} />
            </td>
            <td className="num">{r.type === "trigger" ? hitsRuns(r.hits, r.runs) : DASH}</td>
            <td className="num">{dur(r.durationSeconds)}</td>
            <td className="num">{cost(r.costUSD)}</td>
            <td className="num">{num(r.inputTokens)}</td>
            <td className="num">{num(r.outputTokens)}</td>
          </tr>
        ))}
        {rows.length === 0 && (
          <tr>
            <td className="empty" colSpan={COLS.length}>
              No cases match the current filters.
            </td>
          </tr>
        )}
      </tbody>
    </table>
  );
}
