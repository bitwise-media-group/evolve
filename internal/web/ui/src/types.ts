// Mirrors internal/web/data.go. Pointer fields in Go are optional here; a missing
// metric renders as "—" rather than a false zero.

export type CaseType = "trigger" | "eval";
export type Status = "pass" | "fail" | "error";

export interface Row {
  plugin: string;
  skill: string;
  provider: string;
  model: string;
  modelKey: string;
  display?: string;
  harness?: string;
  type: CaseType;
  id: string;
  name?: string;
  status: Status;
  executed: boolean;
  ranAt?: string;
  shouldTrigger?: boolean;
  hits?: number;
  runs?: number;
  durationSeconds?: number;
  costUSD?: number;
  inputTokens?: number;
  outputTokens?: number;
}

export interface Dataset {
  generatedAt: string;
  toolVersion: string;
  repo: string;
  repoPath: string;
  rows: Row[];
}
