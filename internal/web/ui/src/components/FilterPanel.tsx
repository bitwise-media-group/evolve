// The left filter column: one always-visible multi-select group per facet, with
// per-option counts that reflect the other facets' current selection.

import { FACETS, facetOptions, type FacetKey, type Selection } from "../filters";
import type { Row } from "../types";

interface Props {
  rows: Row[];
  selection: Selection;
  onToggle: (key: FacetKey, value: string) => void;
  onClear: (key: FacetKey) => void;
}

export function FilterPanel({ rows, selection, onToggle, onClear }: Props) {
  return (
    <aside className="filters">
      {FACETS.map(({ key, label }) => {
        const options = facetOptions(rows, selection, key);
        const active = selection[key].length;
        return (
          <section className="facet" key={key}>
            <header className="facet-head">
              <span className="facet-title">{label}</span>
              {active > 0 && (
                <button className="facet-clear" onClick={() => onClear(key)} type="button">
                  clear
                </button>
              )}
            </header>
            <ul className="facet-options">
              {options.length === 0 && <li className="facet-empty">no values</li>}
              {options.map((opt) => (
                <li key={opt.value}>
                  <label className={`facet-option${opt.selected ? " is-selected" : ""}`}>
                    <input
                      type="checkbox"
                      checked={opt.selected}
                      onChange={() => onToggle(key, opt.value)}
                    />
                    <span className={`facet-value v-${key}-${opt.value}`}>{opt.value}</span>
                    <span className="facet-count">{opt.count}</span>
                  </label>
                </li>
              ))}
            </ul>
          </section>
        );
      })}
    </aside>
  );
}
