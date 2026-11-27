"use client";

/**
 * "What changed between these two futures". Every statement is rendered from
 * the server's DiffResult (POST /diffs); nothing is computed here except the
 * display of B minus A, which the server also reports as `delta`. The panel
 * never claims a cause: it lists observed differences and shows the server's
 * statement verbatim.
 */
import { useEffect, useState } from "react";
import {
  ApiError,
  createDiff,
  type Branch,
  type DiffResult,
} from "@/lib/api/client";
import {
  branchDisplayName,
  type BranchLabelContext,
} from "@/features/branches/labels";
import {
  DISCLAIMER_FULL,
  formatCount,
  formatMoney,
  formatSimTime,
} from "@/lib/glossary";
import {
  GROUP_TITLES,
  componentLabel,
  describeDiffInput,
  inputReference,
  isInternalComponent,
  metricRows,
  signedCount,
  type MetricGroup,
} from "./diffText";

export interface DiffPanelProps {
  branchA: Branch;
  branchB: Branch;
  /** Every branch of the world, for display names. */
  branches: readonly Branch[];
  startClock: string;
  /** Product names by SKU index. */
  productNames: readonly string[];
  engineering: boolean;
  llmModel?: string | undefined;
  /** Moves the shared displayed hour. It reads state and never advances a branch. */
  onSelectTick: (tick: number) => void;
  /** Reports the loaded result (null while loading or after a failure) so the time strip can mark the divergence. */
  onResult?: (result: DiffResult | null) => void;
  onClose?: () => void;
}

const GROUP_ORDER: MetricGroup[] = ["financial", "service", "backlog"];

function failureText(e: unknown): string {
  if (e instanceof ApiError) {
    if (e.type === "incomparable-branches")
      return "These two futures can't be compared: they were set up differently.";
    if (e.type === "not-yet-simulated")
      return "One of these futures has not been simulated that far yet.";
  }
  return "The differences could not be loaded. Please try again.";
}

export function DiffPanel(props: DiffPanelProps) {
  const {
    branchA,
    branchB,
    startClock,
    productNames,
    engineering,
    onSelectTick,
    onResult,
  } = props;
  const pairKey = `${branchA.id}|${branchB.id}|${branchA.headTick}|${branchB.headTick}`;
  const [loaded, setLoaded] = useState<{
    key: string;
    result: DiffResult | null;
    error: string | null;
  } | null>(null);
  const current = loaded !== null && loaded.key === pairKey ? loaded : null;
  const result = current?.result ?? null;
  const error = current?.error ?? null;
  const [technical, setTechnical] = useState(false);
  const ctx: BranchLabelContext = {
    branches: props.branches,
    startClock,
    llmModel: props.llmModel,
  };
  const nameA = branchDisplayName(branchA, ctx);
  const nameB = branchDisplayName(branchB, ctx);

  useEffect(() => {
    let cancelled = false;
    onResult?.(null);
    createDiff({ branchA: branchA.id, branchB: branchB.id })
      .then((r) => {
        if (cancelled) return;
        setLoaded({ key: pairKey, result: r, error: null });
        onResult?.(r);
      })
      .catch((e: unknown) => {
        if (!cancelled)
          setLoaded({ key: pairKey, result: null, error: failureText(e) });
      });
    return () => {
      cancelled = true;
    };
    // `onResult` is a notification callback, not an input of the request.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [branchA.id, branchB.id, branchA.headTick, branchB.headTick]);

  const at = (tick: number): string => formatSimTime(tick, startClock);
  const heading = (
    <h3 className="font-display text-base">
      What changed between these two futures
    </h3>
  );

  let body: React.ReactNode;
  if (error !== null) {
    body = (
      <p role="alert" className="text-sm text-clay">
        {error}
      </p>
    );
  } else if (result === null) {
    body = <p className="text-sm text-ivory-dim">Loading the differences…</p>;
  } else {
    const d = result.divergence;
    const plainComponents =
      d === null
        ? []
        : d.components.filter(
            (c) => engineering || !isInternalComponent(c.name),
          );
    const rows = metricRows(result.metrics);
    const stockRows = result.skuStock.filter(
      (k) => k.delta !== 0 || k.valueCentsDelta !== 0,
    );
    body = (
      <div className="space-y-4">
        <p className="text-xs text-ivory-dim" data-testid="diff-compared-at">
          Compared at {at(result.comparisonTick)}. Differences are shown as{" "}
          {nameB} minus {nameA}.
        </p>

        <section
          aria-label="When the futures first differed"
          className="space-y-1"
          data-testid="diff-divergence"
        >
          {d === null ? (
            <p>
              These two futures look the same up to {at(result.comparisonTick)}.
            </p>
          ) : (
            <>
              <p>
                The two futures first looked different at{" "}
                <button
                  type="button"
                  data-testid="diff-divergence-link"
                  onClick={() => onSelectTick(d.tick)}
                  className="min-h-11 underline decoration-amber underline-offset-2 hover:text-amber"
                >
                  {at(d.tick)}
                </button>
                .
              </p>
              {plainComponents.length > 0 ? (
                <ul className="list-disc pl-5" aria-label="What first differed">
                  {plainComponents.map((c) => (
                    <li key={c.name}>{componentLabel(c, productNames)}</li>
                  ))}
                </ul>
              ) : (
                <p className="text-ivory-dim">
                  Only internal bookkeeping differed at that hour.
                </p>
              )}
            </>
          )}
        </section>

        <section
          aria-label="Different decisions"
          className="space-y-1"
          data-testid="diff-inputs"
        >
          <h4 className="font-medium">Different decisions</h4>
          {result.differingInputs.length === 0 ? (
            <p className="text-ivory-dim">
              Neither future has a decision or surprise that the other lacks.
            </p>
          ) : (
            <ul className="space-y-2">
              {result.differingInputs.map((i) => (
                <li key={`${i.branchId}-${i.seq}`} data-testid="diff-input">
                  <span className="text-xs text-ivory-dim">
                    Only in {i.side === "A" ? nameA : nameB}:{" "}
                  </span>
                  <span>{describeDiffInput(i, productNames, startClock)}</span>
                  <div
                    className="text-xs text-ivory-dim"
                    data-testid="diff-input-ref"
                  >
                    {inputReference(i)}
                  </div>
                  {engineering && technical ? (
                    <div
                      className="font-mono text-xs"
                      data-testid="diff-input-tech"
                    >
                      branch {i.branchId} · seq {i.seq} · provenance{" "}
                      {i.provenance}
                    </div>
                  ) : null}
                </li>
              ))}
            </ul>
          )}
        </section>

        <section
          aria-label="Figures"
          className="space-y-3"
          data-testid="diff-metrics"
        >
          {GROUP_ORDER.map((g) => {
            const list = rows.filter((r) => r.group === g);
            if (list.length === 0) return null;
            return (
              <div key={g} className="space-y-1">
                <h4 className="font-medium">{GROUP_TITLES[g]}</h4>
                <table className="w-full text-left tabular-nums">
                  <thead>
                    <tr className="text-xs text-ivory-dim">
                      <th scope="col" className="py-1 pr-2 font-normal">
                        &nbsp;
                      </th>
                      <th scope="col" className="py-1 pr-2 font-normal">
                        {nameA}
                      </th>
                      <th scope="col" className="py-1 pr-2 font-normal">
                        {nameB}
                      </th>
                      <th scope="col" className="py-1 font-normal">
                        B minus A
                      </th>
                    </tr>
                  </thead>
                  {list.map((r) => (
                    <tbody
                      key={r.key}
                      data-testid={`diff-metric-${r.key}`}
                      className="border-t border-hairline"
                    >
                      <tr className="align-top">
                        <th scope="row" className="py-1 pr-2 font-normal">
                          {r.label}
                        </th>
                        <td className="py-1 pr-2">{r.a}</td>
                        <td className="py-1 pr-2">{r.b}</td>
                        <td className="py-1 font-semibold">{r.delta}</td>
                      </tr>
                      {r.explanation !== null ? (
                        <tr>
                          <td
                            colSpan={4}
                            className="pb-1 text-xs text-ivory-dim"
                          >
                            {r.explanation}
                          </td>
                        </tr>
                      ) : null}
                    </tbody>
                  ))}
                </table>
              </div>
            );
          })}
          {stockRows.length > 0 ? (
            <div className="space-y-1" data-testid="diff-stock">
              <h4 className="font-medium">Stock on hand</h4>
              <table className="w-full text-left tabular-nums">
                <thead>
                  <tr className="text-xs text-ivory-dim">
                    <th scope="col" className="py-1 pr-2 font-normal">
                      Product
                    </th>
                    <th scope="col" className="py-1 pr-2 font-normal">
                      {nameA}
                    </th>
                    <th scope="col" className="py-1 pr-2 font-normal">
                      {nameB}
                    </th>
                    <th scope="col" className="py-1 font-normal">
                      B minus A
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {stockRows.map((k) => (
                    <tr
                      key={k.sku}
                      data-testid={`diff-stock-${k.sku}`}
                      className="border-t border-hairline"
                    >
                      <th scope="row" className="py-1 pr-2 font-normal">
                        {productNames[k.sku] ?? "A product"}
                      </th>
                      <td className="py-1 pr-2">{formatCount(k.a)} units</td>
                      <td className="py-1 pr-2">{formatCount(k.b)} units</td>
                      <td className="py-1 font-semibold">
                        {signedCount(k.delta)} units (
                        {formatMoney(k.valueCentsDelta, { signed: true })})
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </section>

        {result.warnings !== undefined && result.warnings.length > 0 ? (
          <ul
            className="space-y-1 text-xs text-amber"
            data-testid="diff-warnings"
          >
            {result.warnings.map((w) => (
              <li key={w.code}>{w.message}</li>
            ))}
          </ul>
        ) : null}

        <p className="text-xs text-ivory-dim" data-testid="diff-statement">
          {result.statement}
        </p>
        <p className="text-xs text-ivory-dim" data-testid="diff-disclaimer">
          {result.disclaimer || DISCLAIMER_FULL}
        </p>

        {engineering ? (
          <div className="space-y-1">
            <button
              type="button"
              aria-expanded={technical}
              onClick={() => setTechnical((t) => !t)}
              className="min-h-11 rounded-[4px] border border-hairline px-3 text-xs"
            >
              {technical
                ? "Hide technical references"
                : "Show technical references"}
            </button>
            {technical ? (
              <div
                className="space-y-1 rounded-[4px] border border-hairline bg-ink p-2 font-mono text-xs"
                data-testid="diff-tech"
              >
                <p>branch A {result.branchA}</p>
                <p>branch B {result.branchB}</p>
                <p>comparison tick {result.comparisonTick}</p>
                {d !== null ? (
                  <>
                    <p>first divergence tick {d.tick}</p>
                    <p>state hash A {d.stateHashA}</p>
                    <p>state hash B {d.stateHashB}</p>
                    <p>
                      differing components{" "}
                      {d.components.map((c) => c.name).join(", ")}
                    </p>
                  </>
                ) : null}
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    );
  }

  return (
    <section
      aria-label="What changed between these two futures"
      data-testid="diff-panel"
      className="space-y-2 rounded-[8px] border border-hairline bg-raised p-3 text-sm"
    >
      <div className="flex items-center justify-between gap-2">
        {heading}
        {props.onClose !== undefined ? (
          <button
            type="button"
            onClick={props.onClose}
            className="min-h-11 rounded-[4px] border border-hairline px-3 text-xs"
          >
            Close
          </button>
        ) : null}
      </div>
      {body}
      {result === null ? (
        <p className="text-xs text-ivory-dim" data-testid="diff-disclaimer">
          {DISCLAIMER_FULL}
        </p>
      ) : null}
    </section>
  );
}
