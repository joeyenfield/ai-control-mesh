import { fetchMachineLog, type FleetMachine } from "../api";
import { ALL_LINES, LogPanel, PROBLEMS, type LogTone } from "../components/LogPanel";

/** agent-machine's log: refused or failed lines stand out, and the reconnect chatter can be dimmed. */
function machineTone(line: string): LogTone {
  if (/\b(error|failed|refused|couldn't|can't|denied|panic)\b/i.test(line)) return "bad";
  if (/\b(warn(ing)?|timeout|retrying|stop it there|answered 5\d\d)\b/i.test(line)) return "warn";
  if (/\bfleet: (connected to|websocket: close|read tcp)\b/.test(line)) return "muted";
  return "";
}

/** Whether you may read the machine's own log: who manages it, or may update it. */
export const readsLog = (machine: FleetMachine) => Boolean(machine.you.manage || machine.you.update);

/** The Log tab: the end of agent-machine's own log on the machine, read through its fleet connection. */
export function MachineLog({ machine }: { machine: FleetMachine }) {
  if (!machine.connected) {
    return <p className="empty">{machine.name} isn't connected, so its log can't be read. It's in agent-machine.log in its settings folder there.</p>;
  }
  return (
    <LogPanel
      queryKey={["machine-log", machine.id]}
      load={() => fetchMachineLog(machine.id)}
      tone={machineTone}
      views={[ALL_LINES, PROBLEMS]}
      empty="Nothing logged yet."
      footer={`The end of agent-machine's own log on ${machine.name}: what it connected to, what it started for the server, and how its updates went.`}
    />
  );
}
