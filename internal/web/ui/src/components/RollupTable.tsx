// The rollup view: one row per (plugin, skill, model, tier) with pass/fail/error
// tallies and aggregate cost/time, each expandable to its member cases.

import { useState } from "preact/hooks";
import { rollup, type RollupGroup } from "../rollup";
import type { Row } from "../types";
import { cost, dur, pct } from "../format";
import { StatusPill } from "./StatusPill";

const COLS = 10;

export function RollupTable({ rows }: { rows: Row[] }) {
  const groups = rollup(rows);
  const [open, setOpen] = useState<Set<string>>(new Set());

  function toggle(id: string) {
    setOpen((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  return (
    <table className="grid rollup">
      <thead>
        <tr>
          <th>Plugin</th>
          <th>Skill</th>
          <th>Model</th>
          <th>Type</th>
          <th className="num">Pass</th>
          <th className="num">Fail</th>
          <th className="num">Err</th>
          <th className="num">Rate</th>
          <th className="num">Avg time</th>
          <th className="num">Cost</th>
        </tr>
      </thead>
      <tbody>
        {groups.map((g) => (
          <GroupRows key={g.id} g={g} open={open.has(g.id)} onToggle={() => toggle(g.id)} />
        ))}
        {groups.length === 0 && (
          <tr>
            <td className="empty" colSpan={COLS}>
              No results match the current filters.
            </td>
          </tr>
        )}
      </tbody>
    </table>
  );
}

function GroupRows({ g, open, onToggle }: { g: RollupGroup; open: boolean; onToggle: () => void }) {
  return (
    <>
      <tr className={`group${open ? " open" : ""}`} onClick={onToggle}>
        <td>
          <span className="disclosure">{open ? "▾" : "▸"}</span>
          {g.plugin}
        </td>
        <td>{g.skill}</td>
        <td>
          <span className={`tag prov-${g.provider}`}>{g.provider}</span>
          <span className="model-id">{g.display}</span>
        </td>
        <td>
          <span className={`tag type-${g.type}`}>{g.type}</span>
        </td>
        <td className="num pass">{g.passed}</td>
        <td className="num fail">{g.failed || ""}</td>
        <td className="num err">{g.errored || ""}</td>
        <td className="num">{pct(g.passRate)}</td>
        <td className="num">{dur(g.avgDurationSeconds)}</td>
        <td className="num">{cost(g.totalCostUSD)}</td>
      </tr>
      {open &&
        g.rows.map((r, i) => (
          <tr className="child" key={`${r.id}-${i}`}>
            <td colSpan={3} className="child-case" title={r.id}>
              {r.name || r.id}
            </td>
            <td>
              <StatusPill status={r.status} />
            </td>
            <td className="num" colSpan={4}>
              {r.type === "trigger" && r.hits != null ? `${r.hits}/${r.runs} hits` : ""}
            </td>
            <td className="num">{dur(r.durationSeconds)}</td>
            <td className="num">{cost(r.costUSD ?? null)}</td>
          </tr>
        ))}
    </>
  );
}
