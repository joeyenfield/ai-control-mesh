import { useEffect, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, api, type SecurityScan } from "../api";
import { ConfirmDialog } from "./ConfirmDialog";

const SCAN_LABEL: Record<SecurityScan, string> = {
  off: "Off",
  basic: "Basic: credentials only",
  standard: "Standard: credentials and personal data",
  thorough: "Thorough: looser, and the model's replies too",
};

// What each level of the security scan reads for (review.SCAN_LEVELS on the server).
const SCAN_ABOUT: Record<SecurityScan, string> = {
  off: "Reviews don't look for credentials or personal data, and show no security score. What an AI review is sent is still redacted.",
  basic: "Reviews look for credentials and files of secrets in what the model was sent, up to 8 MB of text a session. Personal data isn't checked.",
  standard: "Reviews look for credentials, personal data and files of secrets in what the model was sent, up to 16 MB of text a session.",
  thorough:
    "Also counts shorter, plainer passwords and reads the model's own replies, up to 64 MB of text a session. Finds more, with more false alarms, and the Review tab takes longer.",
};

/**
 * Server settings, Review prompts: the AI reviews a session's Review tab offers, edited as one YAML
 * document. The server checks it (the names a prompt may list for its data and metrics are the server's)
 * and says what's wrong; nothing is parsed here.
 */
export function ReviewPromptsSettings() {
  const table = useQuery({ queryKey: ["review-prompts"], queryFn: api.reviewPrompts, retry: false });
  const yaml = useQuery({ queryKey: ["review-prompts", "yaml"], queryFn: api.reviewPromptsYaml, retry: false });
  const client = useQueryClient();
  const [text, setText] = useState("");
  const [message, setMessage] = useState<{ text: string; tone: "good" | "bad" } | null>(null);
  const [asking, setAsking] = useState(false);
  useEffect(() => {
    if (yaml.data !== undefined) setText(yaml.data);
  }, [yaml.data]);
  const saved = (said: string) => {
    setMessage({ text: said, tone: "good" });
    client.invalidateQueries({ queryKey: ["review-prompts"] });
    client.invalidateQueries({ queryKey: ["session-review"] });
  };
  const save = useMutation({
    mutationFn: () => api.saveReviewPrompts(text),
    onSuccess: () => saved("Saved; the next review uses them."),
    onError: (error) => setMessage({ text: error.message, tone: "bad" }),
  });
  const scan = useMutation({
    mutationFn: (level: SecurityScan) => api.saveSecurityScan(level),
    onSuccess: (table) => saved(`Security scan: ${SCAN_LABEL[table.security_scan].toLowerCase()}. The next review uses it.`),
    onError: (error) => setMessage({ text: error.message, tone: "bad" }),
  });
  const reset = useMutation({
    mutationFn: () => api.saveReviewPrompts(null),
    onSuccess: () => saved("Back to the prompts the server ships with."),
    onError: (error) => setMessage({ text: error.message, tone: "bad" }),
  });
  // Site admins only on the server: anyone else sees nothing here rather than an error.
  if (table.error instanceof ApiError && table.error.status === 403) return null;
  return (
    <section className="section" aria-labelledby="review-prompts-heading">
      <div className="section-head">
        <h2 id="review-prompts-heading">Review prompts</h2>
        {table.data && <span className="pill">{table.data.custom ? "This server's own" : "Shipped"}</span>}
      </div>
      <p className="subtle">
        The AI reviews offered on a session's Review tab. Each prompt has an <code>id</code>, a <code>title</code> (its button), a line{" "}
        <code>about</code> it, its <code>focus</code> (what the model is asked to look at and say), the <code>data</code> it is sent and
        the <code>metrics</code> (checks) whose findings go with it. The rules every review shares, and the form of the answer, are
        added by the server. The first prompt is the one chosen to begin with.
      </p>
      {table.isError && <p className="bad">{table.error.message}</p>}
      {table.data && (
        <div className="panel review-scan">
          <label className="row wrap">
            <span>Security scan</span>
            <select
              value={table.data.security_scan}
              disabled={scan.isPending}
              onChange={(event) => {
                setMessage(null);
                scan.mutate(event.target.value as SecurityScan);
              }}
            >
              {table.data.security_scans.map((level) => (
                <option key={level} value={level}>
                  {SCAN_LABEL[level]}
                </option>
              ))}
            </select>
          </label>
          <p className="subtle">{SCAN_ABOUT[table.data.security_scan]}</p>
        </div>
      )}
      <form
        className="stored-editor panel"
        onSubmit={(event) => {
          event.preventDefault();
          setMessage(null);
          save.mutate();
        }}
      >
        <textarea
          aria-label="Review prompts (YAML)"
          rows={Math.min(40, Math.max(12, text.split("\n").length + 1))}
          value={text}
          onChange={(event) => setText(event.target.value)}
          spellCheck={false}
        />
        <div className="row wrap">
          <button className="primary" type="submit" disabled={save.isPending || yaml.data === undefined}>
            {save.isPending ? "Saving…" : "Save prompts"}
          </button>
          {table.data?.custom && (
            <button type="button" disabled={reset.isPending} onClick={() => setAsking(true)}>
              Use the shipped prompts
            </button>
          )}
          {message && (
            <span className={message.tone} role="status">
              {message.text}
            </span>
          )}
        </div>
      </form>
      {table.data && (
        <div className="review-names">
          <details>
            <summary>Names for data: what a prompt can be sent</summary>
            <dl>
              {Object.entries(table.data.data).map(([name, label]) => (
                <div key={name}>
                  <dt>
                    <code>{name}</code>
                  </dt>
                  <dd>{label}</dd>
                </div>
              ))}
            </dl>
          </details>
          <details>
            <summary>Names for metrics: the checks</summary>
            <dl>
              {Object.entries(table.data.metrics).map(([name, label]) => (
                <div key={name}>
                  <dt>
                    <code>{name}</code>
                  </dt>
                  <dd>{label}</dd>
                </div>
              ))}
            </dl>
          </details>
        </div>
      )}
      <ConfirmDialog
        open={asking}
        title="Use the shipped prompts?"
        confirmLabel="Use the shipped prompts"
        onCancel={() => setAsking(false)}
        onConfirm={() => {
          setAsking(false);
          setMessage(null);
          reset.mutate();
        }}
      >
        <p>This server's own review prompts are removed. Reviews already made keep their answers.</p>
      </ConfirmDialog>
    </section>
  );
}
