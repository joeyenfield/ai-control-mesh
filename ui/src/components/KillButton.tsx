import { useState } from "react";
import type { TerminalSummary } from "../api";
import { ConfirmDialog } from "./ConfirmDialog";
import { Icon } from "./Icon";

/** Whether the agent has exited already, so there's nothing to kill. */
export const ended = (terminal: TerminalSummary | undefined) => Boolean(terminal?.ended_at ?? terminal?.session_ended_at);

/**
 * Kills an agent, after asking: the launcher kills its process and everything it started, at once,
 * as closing its window would. Shown only to people who may control the terminal while it runs.
 */
export function KillButton({ name, kill, available }: { name: string; kill: () => boolean; available: boolean }) {
  const [asking, setAsking] = useState(false);
  if (!available) return null;
  return (
    <>
      <button type="button" className="icon-button kill-button" title="Kill: end the agent and everything it started"
        aria-label={`Kill ${name}`} onClick={() => setAsking(true)}>
        <Icon name="power" size={16} />
      </button>
      <ConfirmDialog
        open={asking}
        title={`Kill ${name}?`}
        confirmLabel="Kill"
        onCancel={() => setAsking(false)}
        onConfirm={() => {
          if (kill()) setAsking(false);
        }}
      >
        <p>
          This ends the agent at once, with whatever it started, and loses work it hasn't saved. Its session and recording
          stay.
        </p>
      </ConfirmDialog>
    </>
  );
}
