import type { SkillExecution } from "../ai/SkillsPanel";
import { useEffect, useRef, useState } from "react";
import { dispatch, envelope, type S, type WorkspaceSnapshot } from "../api";
import { ProviderAccounts } from "./ProviderAccounts";
import { ClaudeAccounts } from "./ClaudeAccounts";

export type SelectedConnectedSource = {
  reference: S.ConnectedSourceReference;
  title: string;
};
type SharingPreview = {
  id: string;
  hash: string;
  target: { provider: string; account: string };
  sources: {
    connected: {
      snapshotId: string;
      provider: string;
      remoteRevision: string;
      checkedAt: string;
      partial: boolean;
      url: string;
    };
    title: string;
    text: string;
  }[];
  expiresAt: string;
  notice: string;
};
type Runtime = {
  provider: string;
  version: string;
  sha256: string;
  available: boolean;
  reason: string;
};
type Run = {
  skill?: SkillExecution["reference"];
  connectedSources?: {
    snapshotId: string;
    remoteRevision: string;
    contentHash: string;
  }[];
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

export function ManagedProviderPanel(
  props: Parameters<typeof ManagedProviderContent>[0],
) {
  return (
    <ManagedProviderContent
      key={`${props.workspace.workspaceId}:${props.workspace.scopeGrantId}:${props.provider ?? "codex"}:${props.skill?.reference.invocationId ?? ""}:${JSON.stringify(props.connectedSources ?? [])}`}
      {...props}
    />
  );
}
function ManagedProviderContent({
  connectedSources = [],
  skill,
  workspace,
  provider = "codex",
}: {
  connectedSources?: SelectedConnectedSource[];
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
    skill?.reference.tools ??
      (connectedSources.length
        ? ["readConnectedSource"]
        : ["readPage", "search.query"]),
  );
  const [sharing, setSharing] = useState<SharingPreview | null>(null);
  const [preparation, setPreparation] = useState<{
    id: string;
    label: string;
  } | null>(null);
  const cancelledPreparation = useRef("");
  const [cancelBusy, setCancelBusy] = useState(false);
  const [approvedSharing, setApprovedSharing] = useState(false);
  const [clock, setClock] = useState(Date.now());
  const active = useRef(true);
  const readSequence = useRef(0);
  const changing = useRef(false);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [prompt, setPrompt] = useState(skill?.prompt ?? "");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function load() {
    const sequence = ++readSequence.current;
    try {
      const result = await dispatch<Status>(
        "providers.runs",
        envelope(workspace),
      );
      if (active.current && sequence === readSequence.current)
        setStatus(result);
    } catch (e) {
      if (active.current && sequence === readSequence.current)
        setError(String(e));
    }
  }
  useEffect(() => {
    active.current = true;
    void load();
    const timer = setInterval(() => {
      setClock(Date.now());
      if (!changing.current) void load();
    }, 1000);
    return () => {
      active.current = false;
      readSequence.current++;
      clearInterval(timer);
    };
  }, []);
  async function act(action: () => Promise<void>) {
    if (changing.current || !active.current) return;
    changing.current = true;
    readSequence.current++;
    setBusy(true);
    setError("");
    try {
      await action();
      if (active.current) await load();
    } catch (e) {
      if (active.current) setError(String(e));
    } finally {
      if (active.current) {
        changing.current = false;
        setBusy(false);
      }
    }
  }
  function invalidatePreview() {
    setPreview(null);
    setSharing(null);
    setApprovedSharing(false);
  }
  async function reviewContext() {
    invalidatePreview();
    const request = envelope(workspace);
    setPreparation({
      id: request.requestId,
      label: "Checking selected sources and recipient…",
    });
    cancelledPreparation.current = "";
    try {
      const canonical = await dispatch<Preview>("providers.preview", {
        ...request,
        pageIds: pages,
      });
      if (!active.current || cancelledPreparation.current === request.requestId)
        return;
      const external = connectedSources.length
        ? await dispatch<SharingPreview>("providers.connectedPreview", {
            ...request,
            provider,
            registrationId: registration,
            billingMode: billing,
            sources: connectedSources.map((s) => s.reference),
          })
        : null;
      if (!active.current || cancelledPreparation.current === request.requestId)
        return;
      setPreview(canonical);
      setSharing(external);
    } finally {
      if (active.current) setPreparation(null);
    }
  }
  async function cancelPreparation() {
    if (!preparation) return;
    cancelledPreparation.current = preparation.id;
    invalidatePreview();
    setCancelBusy(true);
    try {
      await dispatch("providers.cancel", {
        ...envelope(workspace),
        runId: preparation.id,
      });
      if (active.current)
        setError(
          "Preparation cancelled. Review context again before starting.",
        );
    } catch (e) {
      if (active.current) setError(String(e));
    } finally {
      if (active.current) setCancelBusy(false);
    }
  }
  function selectAccount(id: string) {
    setRegistration(id);
    setModels([]);
    setModel("");
    invalidatePreview();
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
    (!connectedSources.length ||
      (!!sharing &&
        approvedSharing &&
        clock < Date.parse(sharing.expiresAt) &&
        tools.includes("readConnectedSource"))) &&
    prompt.trim().length > 0;
  async function start(resumeRunId?: string) {
    if (!ready || !preview) return;
    invalidatePreview();
    const request = envelope(workspace);
    setPreparation({
      id: request.requestId,
      label: "Preparing provider access…",
    });
    cancelledPreparation.current = "";
    try {
      await dispatch("providers.start", {
        ...request,
        provider,
        ...(skill ? { skill: skill.reference } : {}),
        registrationId: registration,
        billingMode: billing,
        model,
        prompt,
        pageIds: pages,
        tools,
        previewHash: preview.hash,
        ...(sharing
          ? { connectedSharing: { id: sharing.id, hash: sharing.hash } }
          : {}),
        ...(resumeRunId ? { resumeRunId } : {}),
      });
    } finally {
      if (active.current) setPreparation(null);
    }
  }
  return (
    <section aria-label={`Managed ${name}`}>
      <h3>Run {name} with selected context</h3>
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
                  if (!active.current) return;
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
                    invalidatePreview();
                  }}
                />
                {p.title || "Untitled"}
              </label>
            ))}
        </fieldset>
        <fieldset>
          <legend>Tools available to {name}</legend>
          {Object.entries({
            ...allowedTools,
            ...(connectedSources.length
              ? { readConnectedSource: "Read reviewed external sources" }
              : {}),
          }).map(([id, label]) => (
            <label key={id} style={{ display: "block" }}>
              <input
                type="checkbox"
                disabled={!!skill || id === "readConnectedSource"}
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
          disabled={
            (!pages.length && !connectedSources.length) ||
            !tools.length ||
            (connectedSources.length > 0 && (!registration || !billing))
          }
          onClick={() => act(reviewContext)}
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
            <p>
              Allowed tools:{" "}
              {tools
                .map((t) =>
                  t === "readConnectedSource"
                    ? "Read reviewed external sources"
                    : allowedTools[t],
                )
                .join(", ")}
            </p>
          </div>
        )}
        {sharing && (
          <section aria-label="External sharing review">
            <h4>External content to share with {name}</h4>
            <p>
              Recipient account: <strong>{sharing.target.account}</strong>
            </p>
            <p>{sharing.notice}</p>
            {sharing.sources.map((source) => (
              <article key={source.connected.snapshotId}>
                <h5>{source.title}</h5>
                <p>
                  {source.connected.provider} · revision{" "}
                  {source.connected.remoteRevision} · access checked{" "}
                  {new Date(source.connected.checkedAt).toLocaleString()}
                  {source.connected.partial ? " · partial source" : ""}
                </p>
                {/^https:\/\//i.test(source.connected.url) && (
                  <a
                    href={source.connected.url}
                    target="_blank"
                    rel="noreferrer"
                  >
                    Open source
                  </a>
                )}
                <pre style={{ whiteSpace: "pre-wrap" }}>{source.text}</pre>
              </article>
            ))}
            <p>
              Review expires {new Date(sharing.expiresAt).toLocaleString()}.
            </p>
            {clock >= Date.parse(sharing.expiresAt) && (
              <p role="alert">
                This sharing review expired. Review context again.
              </p>
            )}
            <label>
              <input
                type="checkbox"
                checked={approvedSharing}
                onChange={(e) => setApprovedSharing(e.target.checked)}
              />
              I approve sharing these excerpts with this recipient when I start
              the run.
            </label>
          </section>
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
      {preparation && (
        <div role="status">
          <p>{preparation.label}</p>
          <button
            disabled={cancelBusy}
            onClick={() => void cancelPreparation()}
          >
            Cancel provider preparation
          </button>
        </div>
      )}
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
            (!skill || r.skill?.skillId === skill.reference.skillId) &&
            (!connectedSources.length ||
              JSON.stringify(
                (r.connectedSources ?? []).map((s) => ({
                  snapshotId: s.snapshotId,
                  remoteRevision: s.remoteRevision,
                  contentHash: s.contentHash,
                })),
              ) === JSON.stringify(connectedSources.map((s) => s.reference))),
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
