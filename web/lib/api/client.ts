/**
 * Typed fetch client for the Parallel API. Types come from schema.d.ts, which
 * is generated from backend/api/openapi.yaml (`npm run gen:api`).
 *
 * The client returns server values unchanged. It never computes metrics.
 */
import type { components } from "./schema";

type Schemas = components["schemas"];

export type World = Schemas["World"];
export type Branch = Schemas["Branch"];
export type BranchList = Schemas["BranchList"];
export type TickFrame = Schemas["TickFrame"];
export type TickFramesResponse = Schemas["TickFramesResponse"];
export type Metrics = Schemas["Metrics"];
export type MetricsResponse = Schemas["MetricsResponse"];
export type Observation = Schemas["Observation"];
export type BranchState = Schemas["BranchState"];
export type Command = Schemas["Command"];
export type CommandKind = Schemas["CommandKind"];
export type ValidationResult = Schemas["ValidationResult"];
export type AcceptedCommand = Schemas["AcceptedCommand"];
export type SubmitCommandsResult = Schemas["SubmitCommandsResult"];
export type AdvanceResult = Schemas["AdvanceResult"];
export type LedgerEntry = Schemas["LedgerEntry"];
export type LedgerLine = Schemas["LedgerLine"];
export type LedgerResponse = Schemas["LedgerResponse"];
export type Account = Schemas["Account"];
export type Assumption = Schemas["Assumption"];
export type ScenarioSummary = Schemas["ScenarioSummary"];
export type DemoResponse = Schemas["DemoResponse"];
export type DemoPlan = Schemas["DemoPlan"];
export type Health = Schemas["Health"];
export type ManagerKind = Schemas["ManagerKind"];
export type Problem = Schemas["Problem"];
export type ProblemIssue = Schemas["ProblemIssue"];
export type ProblemType = Schemas["ProblemType"];
export type CreateWorldRequest = Schemas["CreateWorldRequest"];
export type CreateWorldResponse = Schemas["CreateWorldResponse"];
export type CreateDemoRequest = Schemas["CreateDemoRequest"];
export type ForkRequest = Schemas["ForkRequest"];
export type AdvanceRequest = Schemas["AdvanceRequest"];
export type SubmitCommandsRequest = Schemas["SubmitCommandsRequest"];
export type EndWindowRequest = Schemas["EndWindowRequest"];

const PROBLEM_PREFIX = "urn:parallel:problem:";

/** Problem type without the `urn:parallel:problem:` prefix, for example `stale-head`. */
export type ProblemSlug = ProblemType extends `${typeof PROBLEM_PREFIX}${infer S}` ? S : never;

export const DEFAULT_API_BASE_URL = "http://localhost:8080/api/v1";

export function apiBaseUrl(): string {
  const configured = process.env.NEXT_PUBLIC_API_BASE_URL;
  const base = configured !== undefined && configured !== "" ? configured : DEFAULT_API_BASE_URL;
  return base.replace(/\/+$/, "");
}

/** Error raised for every non-2xx response, parsed from problem+json when present. */
export class ApiError extends Error {
  readonly status: number;
  readonly type: ProblemSlug | "unknown";
  readonly title: string;
  readonly detail: string | undefined;
  readonly issues: ProblemIssue[];
  readonly expectedHeadTick: number | undefined;
  readonly actualHeadTick: number | undefined;

  constructor(init: {
    status: number;
    type: ProblemSlug | "unknown";
    title: string;
    detail?: string | undefined;
    issues?: ProblemIssue[] | undefined;
    expectedHeadTick?: number | undefined;
    actualHeadTick?: number | undefined;
  }) {
    super(init.detail !== undefined && init.detail !== "" ? `${init.title}: ${init.detail}` : init.title);
    this.name = "ApiError";
    this.status = init.status;
    this.type = init.type;
    this.title = init.title;
    this.detail = init.detail;
    this.issues = init.issues ?? [];
    this.expectedHeadTick = init.expectedHeadTick;
    this.actualHeadTick = init.actualHeadTick;
  }
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function toSlug(type: unknown): ProblemSlug | "unknown" {
  if (typeof type === "string" && type.startsWith(PROBLEM_PREFIX)) {
    return type.slice(PROBLEM_PREFIX.length) as ProblemSlug;
  }
  return "unknown";
}

function parseIssues(v: unknown): ProblemIssue[] {
  if (!Array.isArray(v)) return [];
  const out: ProblemIssue[] = [];
  for (const item of v) {
    if (isRecord(item) && typeof item.field === "string" && typeof item.message === "string") {
      out.push({ field: item.field, message: item.message });
    }
  }
  return out;
}

async function toApiError(res: Response): Promise<ApiError> {
  let body: unknown;
  try {
    body = await res.json();
  } catch {
    body = undefined;
  }
  if (isRecord(body)) {
    return new ApiError({
      status: typeof body.status === "number" ? body.status : res.status,
      type: toSlug(body.type),
      title: typeof body.title === "string" ? body.title : res.statusText || "Request failed",
      detail: typeof body.detail === "string" ? body.detail : undefined,
      issues: parseIssues(body.issues),
      expectedHeadTick: typeof body.expectedHeadTick === "number" ? body.expectedHeadTick : undefined,
      actualHeadTick: typeof body.actualHeadTick === "number" ? body.actualHeadTick : undefined,
    });
  }
  return new ApiError({ status: res.status, type: "unknown", title: res.statusText || "Request failed" });
}

type QueryValue = string | number | undefined;

function buildUrl(path: string, query?: Record<string, QueryValue>): string {
  const params = new URLSearchParams();
  if (query) {
    for (const [k, v] of Object.entries(query)) {
      if (v !== undefined) params.set(k, String(v));
    }
  }
  const qs = params.toString();
  return `${apiBaseUrl()}${path}${qs === "" ? "" : `?${qs}`}`;
}

interface RequestOptions {
  method: "GET" | "POST";
  path: string;
  query?: Record<string, QueryValue>;
  body?: unknown;
  headers?: Record<string, string>;
  signal?: AbortSignal | undefined;
}

async function request(opts: RequestOptions): Promise<Response> {
  const headers: Record<string, string> = { Accept: "application/json", ...opts.headers };
  const init: RequestInit = { method: opts.method, headers };
  if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(opts.body);
  }
  if (opts.signal) init.signal = opts.signal;
  const res = await fetch(buildUrl(opts.path, opts.query), init);
  if (!res.ok) throw await toApiError(res);
  return res;
}

async function requestJson<T>(opts: RequestOptions): Promise<T> {
  const res = await request(opts);
  return (await res.json()) as T;
}

/** Per-call options. */
export interface CallOptions {
  signal?: AbortSignal;
}

const id = (v: string): string => encodeURIComponent(v);

// operationId: getHealth
export function getHealth(opts: CallOptions = {}): Promise<Health> {
  return requestJson<Health>({ method: "GET", path: "/health", signal: opts.signal });
}

/** Alias of getHealth. */
export const health = getHealth;

export function createWorld(body: CreateWorldRequest, opts: CallOptions = {}): Promise<CreateWorldResponse> {
  return requestJson<CreateWorldResponse>({ method: "POST", path: "/worlds", body, signal: opts.signal });
}

export function getWorld(worldId: string, opts: CallOptions = {}): Promise<World> {
  return requestJson<World>({ method: "GET", path: `/worlds/${id(worldId)}`, signal: opts.signal });
}

export function listBranches(worldId: string, opts: CallOptions = {}): Promise<BranchList> {
  return requestJson<BranchList>({ method: "GET", path: `/worlds/${id(worldId)}/branches`, signal: opts.signal });
}

export function createDemo(body?: CreateDemoRequest, opts: CallOptions = {}): Promise<DemoResponse> {
  return requestJson<DemoResponse>({ method: "POST", path: "/demo", body, signal: opts.signal });
}

export function getBranch(branchId: string, opts: CallOptions = {}): Promise<Branch> {
  return requestJson<Branch>({ method: "GET", path: `/branches/${id(branchId)}`, signal: opts.signal });
}

export function forkBranch(branchId: string, body: ForkRequest, opts: CallOptions = {}): Promise<Branch> {
  return requestJson<Branch>({ method: "POST", path: `/branches/${id(branchId)}/fork`, body, signal: opts.signal });
}

export function advanceBranch(
  branchId: string,
  body?: AdvanceRequest,
  opts: CallOptions = {},
): Promise<AdvanceResult> {
  return requestJson<AdvanceResult>({
    method: "POST",
    path: `/branches/${id(branchId)}/advance`,
    body,
    signal: opts.signal,
  });
}

/** Sends the optional Idempotency-Key header when `idempotencyKey` is given. */
export function submitCommands(
  branchId: string,
  body: SubmitCommandsRequest,
  idempotencyKey?: string,
  opts: CallOptions = {},
): Promise<SubmitCommandsResult> {
  return requestJson<SubmitCommandsResult>({
    method: "POST",
    path: `/branches/${id(branchId)}/commands`,
    body,
    headers: idempotencyKey !== undefined ? { "Idempotency-Key": idempotencyKey } : {},
    signal: opts.signal,
  });
}

export async function endDecisionWindow(
  branchId: string,
  body: EndWindowRequest,
  opts: CallOptions = {},
): Promise<void> {
  await request({
    method: "POST",
    path: `/branches/${id(branchId)}/decision-window/end`,
    body,
    signal: opts.signal,
  });
}

export function listTickFrames(
  branchId: string,
  range: { from: number; to: number },
  opts: CallOptions = {},
): Promise<TickFramesResponse> {
  return requestJson<TickFramesResponse>({
    method: "GET",
    path: `/branches/${id(branchId)}/tick-frames`,
    query: { from: range.from, to: range.to },
    signal: opts.signal,
  });
}

export function getBranchState(branchId: string, tick: number, opts: CallOptions = {}): Promise<BranchState> {
  return requestJson<BranchState>({
    method: "GET",
    path: `/branches/${id(branchId)}/state`,
    query: { tick },
    signal: opts.signal,
  });
}

export function getBranchMetrics(branchId: string, tick: number, opts: CallOptions = {}): Promise<MetricsResponse> {
  return requestJson<MetricsResponse>({
    method: "GET",
    path: `/branches/${id(branchId)}/metrics`,
    query: { tick },
    signal: opts.signal,
  });
}

export function listLedgerEntries(
  branchId: string,
  query: { from: number; to: number; account?: Account; limit?: number; offset?: number },
  opts: CallOptions = {},
): Promise<LedgerResponse> {
  return requestJson<LedgerResponse>({
    method: "GET",
    path: `/branches/${id(branchId)}/ledger`,
    query: {
      from: query.from,
      to: query.to,
      account: query.account,
      limit: query.limit,
      offset: query.offset,
    },
    signal: opts.signal,
  });
}
