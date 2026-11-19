import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { api, type FleetMachine } from "../api";
import { ConfirmDialog } from "./ConfirmDialog";
import { Icon } from "./Icon";
import { machinePath } from "../pages/Machine";

/** How long a machine has to come back on the server's build before it counts as failed. */
const RECONNECT_MS = 4 * 60_000;

type Outcome =
  | { state: "asking" }
  | { state: "updating"; since: number; to: string | null }
  | { state: "done" }
  | { state: "failed"; reason: string };

/** Why the console can't update this machine itself, or null when it can. */
export function cantUpdate(machine: FleetMachine): string | null {
  if (!machine.connected) return "It's offline.";
  if (!machine.updates) return "Updates from the console are off there (agent-machine updates on).";
  if (!machine.you.update) return "You can't update it.";
  return null;
}

/**
 * Updates every machine on another build than the server's, at once, then watches them come back:
 * one that refuses, can't be updated from here, or doesn't reconnect on the new build counts as failed.
 */
export function BulkUpdate({ machines }: { machines: FleetMachine[] }) {
  const client = useQueryClient();
  const [asking, setAsking] = useState(false);
  const [agent, setAgent] = useState(false);
  const [outcomes, setOutcomes] = useState<Record<string, Outcome>>({});
  const [now, setNow] = useState(() => Date.now());
  const outdated = machines.filter((machine) => machine.update?.available);
  const byId = new Map(machines.map((machine) => [machine.id, machine]));
  const ran = Object.keys(outcomes).length > 0;

  // The machine list refreshes every few seconds: a machine back on the server's build is done.
  useEffect(() => {
    setOutcomes((previous) => {
      let changed = false;
      const next = { ...previous };
      for (const [id, outcome] of Object.entries(previous)) {
        if (outcome.state !== "updating") continue;
        const machine = byId.get(id);
        if (machine?.connected && !machine.update?.available) {
          next[id] = { state: "done" };
          changed = true;
        } else if (now - outcome.since > RECONNECT_MS) {
          next[id] = { state: "failed", reason: machine?.connected ? `It's still on ${machine.version || "an older build"}.` : "It hasn't reconnected." };
          changed = true;
        }
      }
      return changed ? next : previous;
    });
  }, [machines, now]);
  const watching = Object.values(outcomes).some((outcome) => outcome.state === "updating" || outcome.state === "asking");
  useEffect(() => {
    if (!watching) return;
    const timer = setInterval(() => setNow(Date.now()), 5000);
    return () => clearInterval(timer);
  }, [watching]);

  const run = async () => {
    setAsking(false);
    const started: Record<string, Outcome> = {};
    for (const machine of outdated) {
      const reason = cantUpdate(machine);
      started[machine.id] = reason ? { state: "failed", reason } : { state: "asking" };
    }
    setOutcomes(started);
    await Promise.all(
      outdated
        .filter((machine) => !cantUpdate(machine))
        .map(async (machine) => {
          let outcome: Outcome;
          try {
            const answer = await api.updateMachine(machine.id, agent);
            outcome = answer.updating
              ? { state: "updating", since: Date.now(), to: answer.to }
              : answer.message
                ? { state: "failed", reason: answer.message }
                : { state: "done" };
          } catch (error) {
            outcome = { state: "failed", reason: error instanceof Error ? error.message : String(error) };
          }
          setOutcomes((previous) => ({ ...previous, [machine.id]: outcome }));
        }),
    );
    client.invalidateQueries({ queryKey: ["machines"] });
  };

  if (!outdated.length && !ran) return null;
  const entries = Object.entries(outcomes);
  const failed = entries.filter(([, outcome]) => outcome.state === "failed") as [string, Extract<Outcome, { state: "failed" }>][];
  const pending = entries.filter(([, outcome]) => outcome.state === "updating" || outcome.state === "asking").length;
  const done = entries.filter(([, outcome]) => outcome.state === "done").length;
  const updatable = outdated.filter((machine) => !cantUpdate(machine)).length;

  return (
    <section className="bulk-update" aria-labelledby="bulk-update">
      <div className="callout row">
        <p id="bulk-update">
          {outdated.length
            ? `${outdated.length} ${outdated.length === 1 ? "machine is" : "machines are"} on another build than this server's.`
            : "Every machine is on this server's build."}
        </p>
        {outdated.length > 0 && (
          <button type="button" className="primary" disabled={watching} onClick={() => setAsking(true)}>
            <Icon name="download" size={14} />
            {watching ? "Updating…" : `Update all (${outdated.length})`}
          </button>
        )}
      </div>

      {ran && (
        <p className="subtle" role="status">
          {pending > 0 && `${pending} updating, waiting for ${pending === 1 ? "it" : "them"} to reconnect on the new build. `}
          {done > 0 && `${done} updated. `}
          {failed.length > 0 && `${failed.length} failed.`}
        </p>
      )}
      {failed.length > 0 && (
        <div className="callout warning" role="alert">
          <p>
            <strong>{failed.length === 1 ? "One machine wasn't updated" : `${failed.length} machines weren't updated`}.</strong>{" "}
            Open one for the command that updates it there.
          </p>
          <ul>
            {failed.map(([id, outcome]) => (
              <li key={id}>
                <Link to={machinePath(id)}>{byId.get(id)?.name ?? id}</Link>: {outcome.reason}
                {byId.get(id)?.connected && (byId.get(id)?.you.manage || byId.get(id)?.you.update) && (
                  <>
                    {" "}
                    <Link to={machinePath(id, "log")}>See its log</Link>
                  </>
                )}
              </li>
            ))}
          </ul>
        </div>
      )}

      <ConfirmDialog
        open={asking}
        title={`Update ${outdated.length === 1 ? "1 machine" : `${outdated.length} machines`}?`}
        confirmLabel="Update all"
        onCancel={() => setAsking(false)}
        onConfirm={() => void run()}
      >
        <p>
          Each installs this server's build of agent-machine, checked against its SHA-256, and restarts.{" "}
          {updatable < outdated.length &&
            `${outdated.length - updatable} can't be updated from here (offline, or with updates from the console off) and will be listed as failed. `}
          One running in a terminal there has to be restarted by hand.
        </p>
        <label className="check">
          <input type="checkbox" checked={agent} onChange={(event) => setAgent(event.target.checked)} />
          Update agent there too (it stops agent-proxy, so agents running there lose their connection until the next one starts)
        </label>
      </ConfirmDialog>
    </section>
  );
}
