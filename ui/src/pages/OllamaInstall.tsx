import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, type FleetMachine, type OllamaServerState } from "../api";
import { useToast } from "../hooks";

/** Machine-reported install choices; selecting a GPU is explicit and never inferred by the console. */
export function OllamaInstall({ machine, status, dirty }: { machine: FleetMachine; status: OllamaServerState; dirty: boolean }) {
  const [choosing, setChoosing] = useState(!status.found);
  const [picked, setPicked] = useState("cpu");
  const client = useQueryClient();
  const notify = useToast();
  const options = useQuery({
    queryKey: ["ollama-install", machine.id], queryFn: () => api.ollamaInstallOptions(machine.id),
    enabled: Boolean(status.install_available && machine.you.servers && choosing), staleTime: 60_000,
  });
  const install = useMutation({
    mutationFn: () => api.machineServer(machine.id, "ollama", "install", undefined, picked),
    onSuccess: () => { setChoosing(false); client.invalidateQueries({ queryKey: ["ollama-settings", machine.id] }); },
    onError: (error: Error) => notify(error.message),
  });
  if (!status.install_available) return null;
  const job = status.install;
  const busy = install.isPending || job?.state === "installing";
  return (
    <section className="panel install-chooser" aria-label="Install Ollama">
      <div className="panel-head">
        <h2>{status.found ? "Ollama setup" : "Install Ollama"}</h2>
        <p className="subtle">Install from Termux's packages on {machine.name}, and choose how Ollama uses its hardware. Downloaded models stay in place.</p>
      </div>
      {job && <div role="status" aria-live="polite">
        <p className={job.state === "failed" ? "bad" : "subtle"}>
          {job.state === "installing" ? `Installing… ${job.step ?? ""}` : job.state === "failed" ? job.error : job.step}
        </p>
      </div>}
      {!machine.you.servers ? <p className="subtle">You need permission to manage this machine's model servers.</p> : <>
        {!choosing && <button type="button" disabled={busy} onClick={() => setChoosing(true)}>Choose CPU / GPU setup</button>}
        {choosing && <>
          {options.isPending && <p>Asking {machine.name} for install options…</p>}
          {options.isError && <p className="bad">{options.error.message}</p>}
          {options.data?.error && <p className="bad">{options.data.error}</p>}
          <fieldset className="choice-list" disabled={busy}>
            <legend>Hardware</legend>
            {options.data?.variants.map((variant) => <label key={variant.id} className="check">
              <input type="radio" name={`ollama-variant-${machine.id}`} checked={picked === variant.id} onChange={() => setPicked(variant.id)} />
              <span><strong>{variant.label}</strong>{variant.recommended && <span className="pill good">Suggested</span>}<small>{variant.help}</small></span>
            </label>)}
          </fieldset>
          {picked !== "cpu" && <div className="callout"><p className="warn">
            GPU setup uses an F16 cache, disables Flash Attention and starts with an 8K context. Vulkan is experimental: detecting a GPU does not verify correct replies. Test a model after starting Ollama; choose CPU if replies are corrupted.
          </p></div>}
          {status.running && <p className="warn">Stop Ollama above before changing its installation or GPU setup.</p>}
          {dirty && <p className="warn">Save or revert your settings edits before installing.</p>}
          <div className="row">
            <button type="button" className="primary" disabled={busy || status.running || dirty || !options.data?.variants.some((v) => v.id === picked)} onClick={() => install.mutate()}>
              {busy ? "Installing…" : "Install and configure"}
            </button>
            {status.found && <button type="button" disabled={busy} onClick={() => setChoosing(false)}>Cancel</button>}
          </div>
          <p className="subtle">This installs packages and saves the selected hardware settings. Start Ollama above when installation finishes.</p>
        </>}
      </>}
    </section>
  );
}
