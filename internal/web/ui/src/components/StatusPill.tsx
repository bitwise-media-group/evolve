import type { Status } from "../types";

export function StatusPill({ status }: { status: Status }) {
  return <span className={`pill status-${status}`}>{status}</span>;
}
