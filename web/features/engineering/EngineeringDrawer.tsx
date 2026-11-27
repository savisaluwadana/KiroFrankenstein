"use client";

/**
 * Engineering drawer, shown only in Engineering Mode. It re-homes the technical
 * panels: tick number, state hash, ids, raw metrics, the ledger with cause
 * references, the event log, the plan hint with the server's reasons (these
 * leak crisis timing, so they never appear in normal mode) and the branch tree.
 * Nothing here changes a request or a number.
 */
import { useState } from "react";
import type { AcceptedCommand, Branch, DemoPlan, MetricsResponse, Observation, TickFrame, World } from "@/lib/api/client";
import { BranchTree } from "@/features/branches/BranchTree";
import { LedgerPanel } from "@/features/studio/LedgerPanel";
import { productName } from "@/lib/glossary";

type Tab = "overview" | "ledger" | "events" | "plan" | "branches";

export interface EngineeringDrawerProps {
  world: World;
  branch: Branch;
  branches: readonly Branch[];
  tick: number;
  frame: TickFrame | null;
  /** Frames of the active branch by tick. */
  frames: Record<number, TickFrame> | undefined;
  metrics: MetricsResponse | null;
  observation: Observation | null;
  plan: DemoPlan | null;
  accepted: readonly AcceptedCommand[];
  onSelectBranch: (id: string) => void;
  onGoToTick: (tick: number) => void;
}

const tabBtn = (active: boolean) =>
  `min-h-11 rounded-[4px] border px-3 text-sm ${active ? "border-amber text-amber" : "border-hairline text-ivory-dim"}`;

function Json({ value, label }: { value: unknown; label: string }) {
  return (
    <pre
      tabIndex={0}
      aria-label={label}
      className="max-h-80 overflow-auto rounded-[4px] border border-hairline bg-ink p-2 font-mono text-xs text-ivory"
    >
      {JSON.stringify(value, null, 2)}
    </pre>
  );
}

export function PlanHint({ plan, onGoToFork }: { plan: DemoPlan; onGoToFork: () => void }) {
  return (
    <section
      aria-label="Demo plan hint"
      className="space-y-2 rounded-[4px] border border-hairline bg-ink p-3 text-xs"
      data-testid="plan-hint"
    >
      <h3 className="font-medium text-ivory">Demo plan hint</h3>
      <p className="text-ivory-dim">
        A suggestion computed by the server from the no-command run. It is not a guaranteed good decision.
      </p>
      <p className="font-mono tabular-nums text-ivory">Crisis at tick {plan.crisisTick}; fork at tick {plan.forkTick}</p>
      <ul className="font-mono tabular-nums text-ivory">
        {plan.commands.map((c, i) => (
          <li key={i}>
            Order {c.qty} units of {c.skuId} from the {c.role} supplier
          </li>
        ))}
      </ul>
      <button
        type="button"
        onClick={onGoToFork}
        className="min-h-11 rounded-[4px] border border-hairline px-3 text-ivory hover:border-amber"
      >
        Show tick {plan.forkTick}
      </button>
      <details>
        <summary className="cursor-pointer text-ivory-dim">How the server derived it</summary>
        <ul className="list-disc pl-4 text-ivory-dim">
          {plan.reasons.map((r, i) => (
            <li key={i}>{r}</li>
          ))}
        </ul>
      </details>
    </section>
  );
}

export function EngineeringDrawer(props: EngineeringDrawerProps) {
  const { world, branch, tick, frame, metrics, observation, plan } = props;
  const [tab, setTab] = useState<Tab>("overview");
  const startClock = world.scenario.startClock;
  const tabs: [Tab, string][] = [
    ["overview", "Overview"],
    ["ledger", "Ledger"],
    ["events", "Events"],
    ...(plan !== null ? ([["plan", "Plan"]] as [Tab, string][]) : []),
    ["branches", "Branches"],
  ];

  const markerRows: string[] = [];
  const disruptionRows: string[] = [];
  for (const f of Object.values(props.frames ?? {}).sort((a, b) => a.tick - b.tick)) {
    if (f.tick > tick) continue;
    for (const m of f.markers) {
      markerRows.push(`tick ${f.tick}: ${m.kind}${m.windowReason ? ` (${m.windowReason})` : ""}${m.disruptionKind ? ` ${m.disruptionKind}` : ""}`);
    }
  }
  for (const d of frame?.disruptions ?? []) {
    disruptionRows.push(
      `${d.kind} origin=${d.origin} target=${d.target} magnitudeBps=${d.magnitudeBps} onset=${d.onsetTick} end=${d.endTick}`,
    );
  }

  return (
    <section aria-label="Engineering drawer" className="space-y-3 rounded-[8px] border border-hairline bg-surface p-3 font-mono text-xs text-ivory">
      <div role="tablist" aria-label="Engineering views" className="flex flex-wrap gap-1 font-sans">
        {tabs.map(([id, label]) => (
          <button key={id} type="button" role="tab" aria-selected={tab === id} onClick={() => setTab(id)} className={tabBtn(tab === id)}>
            {label}
          </button>
        ))}
      </div>

      {tab === "overview" ? (
        <div className="space-y-2">
          {(world.warnings ?? []).map((w) => (
            <p key={w.code} role="alert" data-testid={`eng-warning-${w.code}`} className="text-clay">
              {w.message}
            </p>
          ))}
          <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 tabular-nums">
            <dt className="text-ivory-dim">tick</dt>
            <dd data-testid="eng-tick">{tick}</dd>
            <dt className="text-ivory-dim">state hash</dt>
            <dd className="break-all" data-testid="eng-hash">{frame?.stateHash ?? "not loaded"}</dd>
            <dt className="text-ivory-dim">world id</dt>
            <dd className="break-all" data-testid="eng-world-id">{world.id}</dd>
            <dt className="text-ivory-dim">branch id</dt>
            <dd className="break-all" data-testid="eng-branch-id">{branch.id}</dd>
            <dt className="text-ivory-dim">engine version</dt>
            <dd>{world.engineVersion}</dd>
            <dt className="text-ivory-dim">head tick</dt>
            <dd>{branch.headTick}</dd>
          </dl>
          <Json value={metrics?.metrics ?? null} label="Raw metrics" />
          {observation !== null ? <Json value={observation} label="Raw observation" /> : null}
        </div>
      ) : null}

      {tab === "ledger" ? (
        <LedgerPanel branchId={branch.id} tick={tick} title="Ledger" startClock={startClock} engineering />
      ) : null}

      {tab === "events" ? (
        <div className="space-y-2" data-testid="eng-events">
          <h3 className="font-sans text-sm font-medium">Markers up to tick {tick}</h3>
          <ul>{markerRows.length === 0 ? <li className="text-ivory-dim">none</li> : markerRows.map((r, i) => <li key={i}>{r}</li>)}</ul>
          <h3 className="font-sans text-sm font-medium">Disruptions at tick {tick}</h3>
          <ul>{disruptionRows.length === 0 ? <li className="text-ivory-dim">none</li> : disruptionRows.map((r, i) => <li key={i}>{r}</li>)}</ul>
          <h3 className="font-sans text-sm font-medium">Fulfilment report</h3>
          <Json value={frame?.report ?? null} label="Fulfilment report" />
          <h3 className="font-sans text-sm font-medium">Moves locked in this session</h3>
          <ul>
            {props.accepted.length === 0 ? (
              <li className="text-ivory-dim">none</li>
            ) : (
              props.accepted.map((a) => (
                <li key={a.seq}>
                  seq {a.seq} effective tick {a.effectiveTick} {a.command.kind}
                  {a.command.sku !== undefined ? ` ${productName(world.scenario.skus[a.command.sku]?.id ?? "product")}` : ""} ({a.provenance})
                </li>
              ))
            )}
          </ul>
        </div>
      ) : null}

      {tab === "plan" && plan !== null ? <PlanHint plan={plan} onGoToFork={() => props.onGoToTick(plan.forkTick)} /> : null}

      {tab === "branches" ? (
        <div className="h-72 rounded-[4px] border border-hairline bg-ink">
          <BranchTree branches={props.branches} activeBranchId={branch.id} onSelect={props.onSelectBranch} />
        </div>
      ) : null}
    </section>
  );
}
