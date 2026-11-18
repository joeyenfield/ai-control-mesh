import { useEffect, useRef, useState } from "react";
import { Icon } from "./Icon";
import { Link, useLocation, useNavigate } from "react-router-dom";
import type { TerminalSummary } from "../api";
import { remember, remembered, useToast } from "../hooks";

/** Whether this browser shows a system notification when an agent starts waiting, kept per browser. */
const NOTIFY_KEY = "ai-proxy-notify";

const terminalPath = (id: string) => `/terminals/${encodeURIComponent(id)}`;
const nameOf = (terminal: TerminalSummary) => `${terminal.agent_name}${terminal.project ? ` · ${terminal.project}` : ""}`;

/** Agents that wait for their user now, asking ones first. */
export function waitingAgents(terminals: TerminalSummary[] | undefined): TerminalSummary[] {
  return (terminals ?? [])
    .filter((item) => item.attention && !item.ended_at && !item.session_ended_at)
    .sort((a, b) => Number(b.attention?.kind === "question") - Number(a.attention?.kind === "question"));
}

/**
 * The Android app's notifications. Its WebView has no Notification API, so the app gives the page this
 * object instead (android/…/AgentNotifications.kt), and fires APP_PERMISSION_EVENT when its permission
 * may have changed.
 */
type AppNotifications = {
  permission(): string;
  requestPermission(): void;
  show(title: string, body: string, tag: string, path: string): boolean;
};
const APP_PERMISSION_EVENT = "agentproxy-notifications";

function appNotifications(): AppNotifications | undefined {
  return typeof window === "undefined" ? undefined : (window as { AgentProxyNotifications?: AppNotifications }).AgentProxyNotifications;
}

/** Whether this is the Android app, whose console keeps polling in the background so it can notify. */
export const inAndroidApp = () => appNotifications() !== undefined;

export function notificationsSupported() {
  return typeof window !== "undefined" && ("Notification" in window || inAndroidApp());
}

function permission(): string {
  const app = appNotifications();
  if (app) return app.permission();
  return "Notification" in window ? Notification.permission : "denied";
}

function requestPermission(): Promise<string> {
  const app = appNotifications();
  if (!app) return Notification.requestPermission();
  return new Promise((resolve) => {
    const answered = () => {
      window.removeEventListener(APP_PERMISSION_EVENT, answered);
      resolve(permission());
    };
    window.addEventListener(APP_PERMISSION_EVENT, answered);
    app.requestPermission();
  });
}

function showNotification(title: string, body: string, tag: string, path: string, open: () => void) {
  const app = appNotifications();
  if (app) {
    app.show(title, body, tag, path);
    return;
  }
  const shown = new Notification(title, { body, tag });
  shown.onclick = () => {
    window.focus();
    open();
    shown.close();
  };
}

const notifyStored = () => remembered(NOTIFY_KEY) === "1";
const notifyOn = () => notificationsSupported() && permission() === "granted" && notifyStored();

export function useNotifyChoice(): [boolean, (on: boolean) => Promise<void>] {
  const [on, setOn] = useState(notifyOn);
  const choose = async (value: boolean) => {
    if (value && notificationsSupported() && permission() !== "granted") {
      if ((await requestPermission()) !== "granted") value = false;
    }
    remember(NOTIFY_KEY, value ? "1" : "0");
    setOn(value);
    window.dispatchEvent(new Event(NOTIFY_KEY));
  };
  useEffect(() => {
    const changed = () => setOn(notifyOn());
    window.addEventListener(NOTIFY_KEY, changed);
    // The app's permission can change in Android's settings, away from the page.
    window.addEventListener(APP_PERMISSION_EVENT, changed);
    return () => {
      window.removeEventListener(NOTIFY_KEY, changed);
      window.removeEventListener(APP_PERMISSION_EVENT, changed);
    };
  }, []);
  return [on, choose];
}

/**
 * Tells the user when an agent starts waiting for them: a toast and, if they turned it on, a
 * system notification; the page title counts waiting agents. Agents asking something (a
 * permission prompt) also get a banner above every page until they're answered or dismissed.
 */
export function AttentionAlerts({ terminals }: { terminals: TerminalSummary[] | undefined }) {
  const notify = useToast();
  const navigate = useNavigate();
  const { pathname } = useLocation();
  const [notifying] = useNotifyChoice();
  const [dismissed, setDismissed] = useState<Set<string>>(() => new Set());
  // What each agent was doing at the last poll; undefined before the first, so agents already waiting when the page opens don't all alert.
  const seen = useRef<Map<string, string> | undefined>(undefined);
  const waiting = waitingAgents(terminals);
  const key = (terminal: TerminalSummary) => `${terminal.session_id}@${terminal.attention?.since ?? ""}`;

  useEffect(() => {
    if (!terminals) return;
    const previous = seen.current;
    const now = new Map(waiting.map((item) => [item.session_id, item.attention!.kind]));
    seen.current = now;
    if (!previous) return;
    for (const terminal of waiting) {
      const before = previous.get(terminal.session_id);
      const kind = terminal.attention!.kind;
      // A new wait, or a quiet agent that has since put a question up.
      if (before === kind || (before === "question" && kind === "idle")) continue;
      if (pathname === terminalPath(terminal.session_id) && document.visibilityState === "visible") continue;
      const body = kind === "question" ? terminal.attention!.question ?? "It's asking you something." : "It's done for now and waiting for you.";
      notify(`${nameOf(terminal)}: ${kind === "question" ? "needs you" : "waiting"}`);
      if (notifying) {
        const path = terminalPath(terminal.session_id);
        showNotification(`${nameOf(terminal)} ${kind === "question" ? "needs you" : "is waiting"}`, body, terminal.session_id, path, () => navigate(path));
      }
    }
  }, [terminals]);

  const count = waiting.length;
  useEffect(() => {
    const base = document.title.replace(/^\(\d+\) /, "");
    document.title = count ? `(${count}) ${base}` : base;
  }, [count]);

  const asking = waiting.filter((item) => item.attention?.kind === "question" && pathname !== terminalPath(item.session_id) && !dismissed.has(key(item)));
  if (!asking.length) return null;
  return (
    <div className="attention" role="status" aria-live="polite">
      {asking.slice(0, 3).map((terminal) => (
        <p key={terminal.session_id} className="attention-item">
          <span className="attention-icon" aria-hidden="true">
            <Icon name="chat" size={16} />
          </span>
          <span className="attention-text">
            <span className="attention-who">
              <strong>{nameOf(terminal)}</strong>
              {terminal.machine_name && <span className="subtle"> on {terminal.machine_name}</span>}
            </span>
            <span className="attention-question" title={terminal.attention?.question ?? undefined}>{terminal.attention?.question ?? "It's asking you something."}</span>
          </span>
          <Link to={terminalPath(terminal.session_id)} className="button primary small">
            Answer
          </Link>
          <button type="button" className="small" onClick={() => setDismissed(new Set(dismissed).add(key(terminal)))}>
            Later
          </button>
        </p>
      ))}
      {asking.length > 3 && (
        <p className="subtle">
          And {asking.length - 3} more on <Link to="/terminals">Running agents</Link>.
        </p>
      )}
    </div>
  );
}

/** Turns system notifications on and off for this browser, where it has them. */
export function NotifyToggle() {
  const [on, choose] = useNotifyChoice();
  if (!notificationsSupported()) return null;
  const app = inAndroidApp();
  // The app opens Android's notification settings instead, so its switch stays usable.
  const blocked = !app && permission() === "denied";
  return (
    <label className="check" title={blocked ? "This browser blocks notifications from this site; allow them in its site settings." : undefined}>
      <input type="checkbox" checked={on} disabled={blocked} onChange={(event) => void choose(event.target.checked)} />
      Notify me when an agent is waiting{blocked && <span className="subtle"> (blocked in this browser)</span>}
    </label>
  );
}
