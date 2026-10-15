import type { SkillExecution } from "../ai/SkillsPanel";
import { useEffect, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";
import { ProviderAccounts } from "./ProviderAccounts";
import { ClaudeAccounts } from "./ClaudeAccounts";

type Runtime = {
  provider: string;
  version: string;
  sha256: string;
  available: boolean;
  reason: string;
};
type Run = {
  skill?: SkillExecution["reference"];
  id: string;
  provider: string;
  account: string;
  accountLabel: string;
  billingMode: string;
  model: string;
  state: string;
  prompt: string;
  text: string;
  message: string;
  createdAt: string;
  checkpoint?: unknown;
  pageIds: string[];
  tools: string[];
};
type Preview = {
  hash: string;
  pages: { pageId: string; title: string; revision: string }[];
  notice: string;
};
type Status = { runs: Run[]; activeRunId: string; runtimes: Runtime[] };
const allowedTools: Record<string, string> = {
  readPage: "Read selected pages",
  "search.query": "Search selected pages",
  queryRecords: "Read selected database records",
  resolveCitation: "Resolve source references",
  allocateIds: "Prepare new content identifiers",
  proposeChanges: "Propose edits for approval",
  proposeActions: "Propose page and task creation for approval",
};

export function ManagedProviderPanel({
  skill,
  workspace,
  provider = "codex",
}: {
  skill?: SkillExecution;
  workspace: WorkspaceSnapshot;
  provider?: "codex" | "claude_code";
}) {
  const [status, setStatus] = useState<Status>({
    runs: [],
    activeRunId: "",
    runtimes: [],
  });
  const name = provider === "codex" ? "Codex" : "Claude Code";
  const [billing, setBilling] = useState(
    provider === "codex" ? "chatgpt_plan" : "",
  );
  const [registration, setRegistration] = useState("");
  const [models, setModels] = useState<{ id: string; name: string }[]>([]);
  const [model, setModel] = useState("");
  const [pages, setPages] = useState<string[]>(
    skill?.reference.sourcePageIds ?? [],
  );
  const [tools, setTools] = useState<string[]>(
    skill?.reference.tools ?? ["readPage", "search.query"],
  );
  const [preview, setPreview] = useState<Preview | null>(null);
  const [prompt, setPrompt] = useState(skill?.prompt ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function load() {
    setStatus(await dispatch<Status>("providers.runs", envelope(workspace)));
  }
  useEffect(() => {
    let stopped = false;
    async function poll() {
      try {
        const result = await dispatch<Status>(
          "providers.runs",
          envelope(workspace),
        );
        if (!stopped) setStatus(result);
      } catch (e) {
        if (!stopped) setError(String(e));
      }
    }
    void poll();
    const timer = setInterval(() => void poll(), 1000);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, [workspace.workspaceId]);
  async function act(action: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await action();
      await load();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  function selectAccount(id: string) {
    setRegistration(id);
    setModels([]);
    setModel("");
    setPreview(null);
  }
  const runtime = status.runtimes.find((r) => r.provider === provider);
  const running = !!status.activeRunId;
  const ready =
    !busy &&
    !running &&
    !!runtime?.available &&
    !!registration &&
    !!model &&
    !!billing &&
    (provider === "codex" ||
      /^claude-[A-Za-z0-9][A-Za-z0-9._-]{1,120}$/.test(model)) &&
    !!preview &&
    prompt.trim().length > 0;
  async function start(resumeRunId?: string) {
    if (!preview) return;
    await dispatch("providers.start", {
      ...envelope(workspace),
      provider,
      ...(skill ? { skill: skill.reference } : {}),
      registrationId: registration,
      billingMode: billing,
      model,
      prompt,
      pageIds: pages,
      tools,
      previewHash: preview.hash,
      ...(resumeRunId ? { resumeRunId } : {}),
    });
    setPreview(null);
  }
  return (
    <section aria-label={`Managed ${name}`}>
      <h3>Run {name} with selected workspace access</h3>
      <p>
        {provider === "codex"
          ? "Uses the installed Codex runtime and your explicitly selected ChatGPT account. Requests use that account’s ChatGPT plan."
          : "Uses the unchanged installed Claude Code runtime and the vendor account signed in to your selected private profile. The profile fixes the billing route."}{" "}
        Every proposed workspace change still requires your approval below.
      </p>
      <fieldset disabled={busy || running}>
        <legend>Account, model and shared context</legend>
        <button
          onClick={() =>
            act(async () => {
              await dispatch("providers.discover", {
                ...envelope(workspace),
                provider,
              });
            })
          }
        >
          Check installed {name}
        </button>
        {runtime && (
          <p role="status">
            {runtime.available
              ? `Found ${name} ${runtime.version}.`
              : runtime.reason}
          </p>
        )}
        {provider === "codex" ? (
          <>
            {" "}
            <ProviderAccounts
              workspace={workspace}
              selectedRegistrationId={registration}
              onSelect={selectAccount}
            />
            <button
              disabled={!registration}
              onClick={() =>
                act(async () => {
                  setModels([]);
                  setModel("");
                  const catalog = await dispatch<{
                    registrationId: string;
                    models: { id: string; name: string }[];
                  }>("providers.models", {
                    ...envelope(workspace),
                    registrationId: registration,
                  });
                  if (catalog.registrationId !== registration)
                    throw new Error(
                      "Account changed; load its model list again",
                    );
                  setModels(catalog.models);
                  if (!catalog.models.length)
                    throw new Error(
                      "This account currently lists no available models",
                    );
                })
              }
            >
              Load models for selected account
            </button>
            <label>
              Model{" "}
              <select
                aria-label="Account model"
                value={model}
                onChange={(e) => setModel(e.target.value)}
              >
                <option value="">Choose a model</option>
                {models.map((m) => (
                  <option key={m.id} value={m.id}>
                    {m.name} ({m.id})
                  </option>
                ))}
              </select>
            </label>
          </>
        ) : (
          <>
            <ClaudeAccounts
              workspace={workspace}
              runtimeAvailable={!!runtime?.available}
              selectedProfileId={registration}
              onSelect={(id, mode) => {
                selectAccount(id);
                setBilling(mode);
              }}
            />
            <label>
              Full Claude model identifier
              <input
                aria-label="Full Claude model identifier"
                maxLength={128}
                value={model}
                onChange={(e) => setModel(e.target.value)}
              />
            </label>
            <p>
              Choose the full model identifier supported by your Claude account.
              The selected billing route will not change automatically.
            </p>
          </>
        )}
        <fieldset>
          <legend>Pages shared with {name}</legend>
          {workspace.pages
            .filter((p) => !p.deletedAt)
            .map((p) => (
              <label key={p.id} style={{ display: "block" }}>
                <input
                  type="checkbox"
                  disabled={!!skill}
                  checked={pages.includes(p.id)}
                  onChange={(e) => {
                    setPages(
                      e.target.checked
                        ? [...pages, p.id]
                        : pages.filter((id) => id !== p.id),
                    );
                    setPreview(null);
                  }}
                />
                {p.title || "Untitled"}
              </label>
            ))}
        </fieldset>
        <fieldset>
          <legend>Tools available to {name}</legend>
          {Object.entries(allowedTools).map(([id, label]) => (
            <label key={id} style={{ display: "block" }}>
              <input
                type="checkbox"
                disabled={!!skill}
                checked={tools.includes(id)}
                onChange={(e) => {
                  setTools(
                    e.target.checked
                      ? [...tools, id]
                      : tools.filter((t) => t !== id),
                  );
                  setPreview(null);
                }}
              />
              {label}
            </label>
          ))}
        </fieldset>
        <button
          disabled={!pages.length || !tools.length}
          onClick={() =>
            act(async () =>
              setPreview(
                await dispatch<Preview>("providers.preview", {
                  ...envelope(workspace),
                  pageIds: pages,
                }),
              ),
            )
          }
        >
          Review context for this run
        </button>
        {preview && (
          <div className="provider-context">
            <h4>Context to share</h4>
            <ul>
              {preview.pages.map((p) => (
                <li key={p.pageId}>
                  {p.title || "Untitled"} · revision {p.revision}
                </li>
              ))}
            </ul>
            <p>{preview.notice}</p>
            <p>Allowed tools: {tools.map((t) => allowedTools[t]).join(", ")}</p>
          </div>
        )}
        <label>
          Request to {name}
          <textarea
            aria-label={`Request to ${name}`}
            readOnly={!!skill}
            maxLength={65536}
            value={prompt}
            onChange={(e) => setPrompt(e.target.value)}
          />
        </label>
        <button disabled={!ready} onClick={() => act(() => start())}>
          Start with selected account and context
        </button>
      </fieldset>
      {running && (
        <button
          disabled={busy}
          onClick={() =>
            act(async () => {
              await dispatch("providers.cancel", {
                ...envelope(workspace),
                runId: status.activeRunId,
              });
            })
          }
        >
          Cancel provider run
        </button>
      )}
      {error && <p role="alert">{error}</p>}
      <h4>Provider runs</h4>
      {status.runs
        .filter(
          (r) =>
            r.provider === provider &&
            (!skill || r.skill?.skillId === skill.reference.skillId),
        )
        .map((run) => (
          <article key={run.id}>
            <h4>
              {run.state} · {run.model}
            </h4>
            <p>
              {run.accountLabel} ·{" "}
              {run.billingMode === "chatgpt_plan"
                ? "ChatGPT plan"
                : run.billingMode === "claude_subscription"
                  ? "Claude subscription"
                  : "Anthropic API"}{" "}
              · {new Date(run.createdAt).toLocaleString()}
            </p>
            <p>{run.prompt}</p>
            {run.text && (
              <pre style={{ whiteSpace: "pre-wrap" }}>{run.text}</pre>
            )}
            {run.message && <p role="status">{run.message}</p>}
            {skill && run.skill && (
              <button
                disabled={running}
                onClick={() => skill.onRetry?.(run.skill!)}
              >
                Review retry with original skill scope
              </button>
            )}
            {!!run.checkpoint && (
              <button
                disabled={
                  !ready ||
                  run.account !== registration ||
                  run.model !== model ||
                  run.billingMode !== billing
                }
                onClick={() => act(() => start(run.id))}
              >
                Continue this session with reviewed context
              </button>
            )}
          </article>
        ))}
    </section>
  );
}
