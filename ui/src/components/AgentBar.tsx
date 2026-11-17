/**
 * The one-line header shared by Running agents and the Live agent view: the title, the running
 * agents' counts and estimated cost, a dropdown to jump to any agent's terminal, and the page's own
 * controls. Both pages use it so moving between them keeps the same header.
 */
import { useCallback, useEffect, useRef, useState, type ReactNode, type RefObject } from "react";
import { Link } from "react-router-dom";
import { useQueries } from "@tanstack/react-query";
import { api, type TerminalSummary } from "../api";
import { formatNumber, formatUsd } from "../format";
import { terminalState } from "../pages/Terminals";
import { Icon } from "./Icon";
import { PartTag } from "./PartTag";
import { SessionCostPill } from "./SessionCost";

const touch = typeof matchMedia === "function" && matchMedia("(pointer: coarse)").matches;

/** Closes a dropdown on Escape or a press outside it, as the account menu does. */
export function useDismiss(open: boolean, box: RefObject<HTMLElement | null>, close: () => void) {
  useEffect(() => {
    if (!open) return;
    const handle = (event: MouseEvent | KeyboardEvent) => {
      if (event instanceof KeyboardEvent ? event.key === "Escape" : !box.current?.contains(event.target as Node)) close();
    };
    document.addEventListener("mousedown", handle);
    document.addEventListener("keydown", handle);
    return () => {
      document.removeEventListener("mousedown", handle);
      document.removeEventListener("keydown", handle);
    };
  }, [open, box, close]);
}

const isRunning = (terminal: TerminalSummary) => !(terminal.ended_at ?? terminal.session_ended_at);

export function AgentBar({ title, terminals, slim = false, fromGrid = false, children, end }: {
  title: string;
  /** Every terminal the page knows; the bar counts and lists the running ones. */
  terminals: TerminalSummary[];
  /** Collapsed: only the title and `end` stay. */
  slim?: boolean;
  /** Opened terminals' back button returns to the Live agent view rather than Running agents. */
  fromGrid?: boolean;
  /** The page's own controls, after the agent switcher. */
  children?: ReactNode;
  /** Kept at the far end even when collapsed (the collapse button). */
  end?: ReactNode;
}) {
  const running = terminals.filter(isRunning);
  return (
    <header className={`grid-bar agent-bar${slim ? " slim" : ""}`}>
      <PartTag />
      <h1>{title}</h1>
      {!slim && <AgentTotals running={running} />}
      <div className="grid-actions">
        {!slim && <AgentSwitch running={running} fromGrid={fromGrid} />}
        {children}
        {end}
      </div>
    </header>
  );
}

/** How many agents run, need you and work, their requests, and what they have cost so far. */
function AgentTotals({ running }: { running: TerminalSummary[] }) {
  // The same query keys as each agent's cost pill, so the bar and the rows share one poll per session.
  const costs = useQueries({
    queries: running.map((terminal) => ({
      queryKey: ["session-cost", terminal.session_id, "watch"],
      queryFn: () => api.sessionCost(terminal.session_id),
      retry: false,
      refetchInterval: 15_000,
    })),
  });
  if (!running.length) return null;
  const now = Date.now();
  const tones = running.map((terminal) => terminalState(terminal, now).tone);
  const asking = tones.filter((tone) => tone === "ask").length;
  const working = tones.filter((tone) => tone === "live").length;
  const requests = running.reduce((total, item) => total + item.request_count, 0);
  const inFlight = running.reduce((total, item) => total + item.in_flight, 0);
  const priced = costs.map((cost) => cost.data).filter((cost) => cost && cost.priced > 0);
  const usd = priced.reduce((total, cost) => total + (cost?.usd ?? 0), 0);
  const subscription = priced.reduce((total, cost) => total + (cost?.subscription_usd ?? 0), 0);
  return (
    <ul className="agent-totals" aria-label="Running agents in total">
      <li><b>{formatNumber(running.length)}</b> running</li>
      {asking > 0 && <li className="needs"><b>{formatNumber(asking)}</b> {asking === 1 ? "needs" : "need"} you</li>}
      <li><b>{formatNumber(working)}</b> working</li>
      <li title={inFlight ? `${inFlight} in flight` : undefined}><b>{formatNumber(requests)}</b> {requests === 1 ? "request" : "requests"}</li>
      {priced.length > 0 && (
        <li className="cost-pill" title={usd > 0
          ? `Estimated cost of the running agents' sessions${subscription > 0 ? `, plus ${formatUsd(subscription)} of subscription calls at API prices` : ""}`
          : "What the running agents' sessions would cost at API prices: a subscription doesn't bill them"}>
          <b>{usd > 0 || subscription === 0 ? formatUsd(usd) : `≈ ${formatUsd(subscription)}`}</b>
        </li>
      )}
    </ul>
  );
}

/** A dropdown of the running agents, each opening its terminal: a quick switch between sessions. */
function AgentSwitch({ running, fromGrid }: { running: TerminalSummary[]; fromGrid: boolean }) {
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const box = useRef<HTMLDivElement>(null);
  const close = useCallback(() => setOpen(false), []);
  useDismiss(open, box, close);
  if (!running.length) return null;
  const text = search.trim().toLowerCase();
  const matching = running.filter((terminal) =>
    [terminal.agent_name, terminal.project, terminal.machine_name, terminal.session_id].some((value) => value?.toLowerCase().includes(text)),
  );
  return (
    <div className="grid-menu" ref={box}>
      <button type="button" className={`small grid-menu-button${open ? " open" : ""}`} aria-haspopup="dialog" aria-expanded={open}
        aria-controls="agent-switch" title="Open an agent's terminal" onClick={() => setOpen(!open)}>
        <Icon name="terminal" size={14} /> Open
        <Icon name="chevron" size={12} className="grid-menu-caret" />
      </button>
      {open && <div className="grid-popover grid-picker panel" id="agent-switch" role="dialog" aria-label="Open an agent">
        <input type="search" aria-label="Search agents" placeholder="Search by agent, project or machine" autoFocus={!touch}
          value={search} onChange={(event) => setSearch(event.target.value)} />
        <div className="grid-agent-options" role="list">
          {matching.map((terminal) => {
            const state = terminalState(terminal);
            return (
              <Link role="listitem" className="grid-agent-option" key={terminal.session_id} to={`/terminals/${encodeURIComponent(terminal.session_id)}`}
                state={fromGrid ? { fromGrid: true } : undefined} title={`${terminal.agent_name} · ${state.label}`} onClick={close}>
                <span className={`state-dot ${state.tone}`} aria-hidden="true" />
                <span className="grid-agent-name">
                  <strong>{terminal.agent_name}</strong>
                  <span className="subtle">{terminal.project || terminal.session_id} · {terminal.machine_name || "Unknown machine"}</span>
                </span>
                <SessionCostPill sessionId={terminal.session_id} />
                <span className="grid-agent-state subtle">{state.label}</span>
              </Link>
            );
          })}
          {!matching.length && <p className="subtle grid-picker-note">No agents match your search.</p>}
        </div>
      </div>}
    </div>
  );
}
