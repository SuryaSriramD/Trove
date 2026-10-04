import { useEffect, useState } from "react";
import {
  batch,
  dispatch,
  envelope,
  ids,
  type WorkspaceSnapshot,
  type S,
} from "../api";
import type { Location, Memory } from "../../../contracts/generated/domain";
import "./ai.css";
import { Suggestions } from "./Suggestions";
import {
  validateMermaidDraft,
  validateMermaidCommands,
} from "./validateMermaid";

type Citation = {
  number: number;
  title: string;
  location: Location;
  state: string;
};
type Source = Citation & { text: string; memoryId?: string };
type Run = {
  id: string;
  conversationId: string;
  state: string;
  request: {
    mode: string;
    prompt: string;
    pageId?: string;
    useMemory: boolean;
  };
  text: string;
  citations: Citation[];
  sources: Source[];
  warning?: string;
  error?: string;
  createdAt: string;
};
type Proposal = {
  before?: string;
  after: string;
  title: string;
  id: string;
  kind: string;
  body: string;
  commands: unknown[];
  pageId: string;
};
type Pack = {
  id: string;
  name: string;
  kind: string;
  qualificationOnly?: boolean;
};
export type AIPanelProps = {
  workspace: WorkspaceSnapshot;
  pageId?: string;
  onNavigate: (location: Location) => void;
  onCommitted: () => void | Promise<void>;
};
const plainText = (value: unknown): string => {
  if (Array.isArray(value)) return value.map(plainText).join("");
  if (value && typeof value === "object") {
    const v = value as Record<string, unknown>;
    return typeof v.text === "string" ? v.text : plainText(v.richText);
  }
  return "";
};

export function AIPanel({
  workspace,
  pageId,
  onNavigate,
  onCommitted,
}: AIPanelProps) {
  const [scope, setScope] = useState(pageId ? "page" : "workspace"),
    [project, setProject] = useState("");
  const [mode, setMode] = useState("answer"),
    [prompt, setPrompt] = useState(""),
    [selection, setSelection] = useState(""),
    [block, setBlock] = useState("");
  const [models, setModels] = useState<Pack[]>([]),
    [model, setModel] = useState(""),
    [useMemory, setUseMemory] = useState(true);
  const [runs, setRuns] = useState<Run[]>([]),
    [current, setCurrent] = useState<Run | null>(null),
    [conversation, setConversation] = useState("");
  const [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [draft, setDraft] = useState(""),
    [title, setTitle] = useState("");
  const [action, setAction] = useState("append"),
    [proposal, setProposal] = useState<Proposal | null>(null);
  const page = workspace.pages.find((p) => p.id === pageId),
    blocks = workspace.blocks.filter((b) => b.pageId === pageId);
  const running = current?.state === "running";
  useEffect(() => {
    dispatch<{ packs: { pack: Pack; state: string }[] }>("models.list")
      .then((r) => {
        const ready = r.packs
          .filter(
            (p) =>
              p.pack.kind === "chat" &&
              p.state === "ready" &&
              !p.pack.qualificationOnly,
          )
          .map((p) => p.pack);
        setModels(ready);
        setModel(
          ready.find((p) => p.id.endsWith("000000000105"))?.id ??
            ready[0]?.id ??
            "",
        );
      })
      .catch(() => {});
  }, []);
  async function loadRuns() {
    const r = await dispatch<Run[]>("ai.runs", envelope(workspace));
    setRuns(r);
    return r;
  }
  useEffect(() => {
    let active = true;
    dispatch<Run[]>("ai.runs", envelope(workspace))
      .then((r) => {
        if (active) {
          setRuns(r);
          setCurrent(r[0] ?? null);
        }
      })
      .catch((e) => setError(String(e)));
    return () => {
      active = false;
    };
  }, [workspace.workspaceId]);
  useEffect(() => {
    if (!current || current.state === "running") return;
    let live = true;
    dispatch<Run>("ai.run", { ...envelope(workspace), runId: current.id })
      .then((r) => {
        if (live) setCurrent(r);
      })
      .catch((e) => setError(String(e)));
    return () => {
      live = false;
    };
  }, [workspace.snapshotCursor, current?.id]);
  useEffect(() => {
    setBlock("");
    setSelection("");
    setProposal(null);
  }, [pageId]);
  useEffect(
    () =>
      window.runtime?.EventsOn("ai:progress", (...args: unknown[]) => {
        const e = args[0] as {
          workspaceId: string;
          runId: string;
          token?: string;
          run?: Run;
          state?: string;
          error?: string;
        };
        if (e.workspaceId !== workspace.workspaceId) return;
        setCurrent((c) =>
          c?.id === e.runId
            ? (e.run ?? {
                ...c,
                text: c.text + (e.token ?? ""),
                state: e.state ?? c.state,
                error: e.error,
              })
            : c,
        );
        if (e.run) {
          setDraft(e.run.text);
          setRuns((r) => [e.run!, ...r.filter((v) => v.id !== e.runId)]);
        }
      }),
    [workspace.workspaceId],
  );
  useEffect(() => {
    if (!running || !current) return;
    const timer = setInterval(() => {
      dispatch<Run>("ai.run", { ...envelope(workspace), runId: current.id })
        .then((r) => {
          if (r.state !== "running") {
            setCurrent(r);
            setDraft(r.text);
            void loadRuns();
          }
        })
        .catch((e) => setError(String(e)));
    }, 1500);
    return () => clearInterval(timer);
  }, [current?.id, running, workspace.workspaceId]);
  async function start() {
    setBusy(true);
    setError("");
    setProposal(null);
    try {
      const r = await dispatch<Run>("ai.start", {
        ...envelope(workspace),
        mode,
        prompt,
        modelPackId: model,
        useMemory,
        ...(conversation ? { conversationId: conversation } : {}),
        ...(scope === "page" && pageId
          ? { pageId, ...(block ? { blockId: block } : {}), selection }
          : {}),
        ...(scope === "project" ? { projectId: project } : {}),
      });
      setCurrent(r);
      setConversation(r.conversationId);
      setDraft("");
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  async function openCitation(c: Citation) {
    try {
      const result = await dispatch<{ state: string }>("resolveCitation", {
        ...envelope(workspace),
        location: c.location,
      });
      if (result.state !== "current")
        throw new Error(
          `Source is ${result.state}; ask again to use the current revision.`,
        );
      onNavigate(c.location);
    } catch (e) {
      setError(String(e));
    }
  }
  async function review() {
    if (!current) return;
    setBusy(true);
    setProposal(null);
    try {
      if (action === "mermaid")
        await validateMermaidDraft(draft || current.text);
      const p = await dispatch<Proposal>("ai.prepareAction", {
        ...envelope(workspace),
        runId: current.id,
        kind:
          current.request.mode === "workspace_agent"
            ? "workspace_patch"
            : action,
        title,
        body: draft,
      });
      await validateMermaidCommands(p.commands);
      setProposal(p);
      setError("");
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  async function apply() {
    if (!proposal) return;
    setBusy(true);
    try {
      const result = await dispatch<{ pageId: string }>("ai.applyAction", {
        ...envelope(workspace),
        proposalId: proposal.id,
      });
      setProposal(null);
      await onCommitted();
      if (!result.pageId) return;
      const fresh = await dispatch<{ page: { revision: string } }>("readPage", {
        ...envelope(workspace),
        pageId: result.pageId,
      });
      onNavigate({
        pageId: result.pageId,
        sourceRevision: fresh.page.revision,
      });
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="ai-panel" aria-label="Local AI and approved memory">
      <h2>Local AI</h2>
      {pageId && (
        <Suggestions
          workspace={workspace}
          pageId={pageId}
          onNavigate={onNavigate}
        />
      )}
      <p>
        Ask about your sources, write a draft, and review changes before
        applying them.
      </p>
      <div className="ai-controls">
        <select
          aria-label="AI source scope"
          value={scope}
          onChange={(e) => setScope(e.target.value)}
        >
          <option value="workspace">Workspace</option>
          <option value="page" disabled={!page}>
            Current page{page ? ` · ${page.title}` : ""}
          </option>
          <option value="project">Project</option>
        </select>
        {scope === "project" && (
          <select
            aria-label="AI project"
            value={project}
            onChange={(e) => setProject(e.target.value)}
          >
            <option value="">Choose project</option>
            {workspace.containers
              .filter((c) => c.kind === "project")
              .map((c) => (
                <option key={c.id} value={c.id}>
                  {workspace.pages.find((p) => p.id === c.owningPageId)
                    ?.title ?? c.id}
                </option>
              ))}
          </select>
        )}
        <select
          aria-label="Writing operation"
          value={mode}
          onChange={(e) => setMode(e.target.value)}
        >
          {[
            "answer",
            "rewrite",
            "summarize",
            "explain",
            "shorten",
            "expand",
            "outline",
            "generate",
            "database",
            "schema",
            "mermaid",
            "code_explain",
            "code_refactor",
            "code_comment",
            "organize",
            "workspace_agent",
          ].map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </select>
        <select
          aria-label="Local instruction model"
          value={model}
          onChange={(e) => setModel(e.target.value)}
        >
          <option value="">Choose installed model</option>
          {models.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </div>
      {models.length === 0 && (
        <p role="status">
          Install an instruction model in Local models to generate an answer.
          Search and editing remain available.
        </p>
      )}
      {scope === "page" && (
        <details>
          <summary>Use a selected block or text</summary>
          <select
            aria-label="Source block"
            value={block}
            onChange={(e) => {
              setBlock(e.target.value);
              setSelection("");
            }}
          >
            <option value="">Whole page</option>
            {blocks.map((b) => (
              <option key={b.id} value={b.id}>
                {plainText(b.data).slice(0, 90) || b.type}
              </option>
            ))}
          </select>
          <textarea
            aria-label="Selected source text"
            maxLength={16384}
            placeholder="Optional: paste an exact selection from the source block"
            value={selection}
            onChange={(e) => setSelection(e.target.value)}
          />
        </details>
      )}
      <textarea
        aria-label="AI prompt"
        placeholder="Ask a question or describe the draft you want…"
        value={prompt}
        onChange={(e) => setPrompt(e.target.value)}
        maxLength={8192}
      />
      <div className="ai-controls">
        <label>
          <input
            type="checkbox"
            checked={useMemory}
            onChange={(e) => setUseMemory(e.target.checked)}
          />{" "}
          Use approved memory
        </label>
        <button
          onClick={start}
          disabled={
            busy ||
            running ||
            !model ||
            (mode === "answer" && !prompt.trim()) ||
            (scope === "project" && !project)
          }
        >
          Generate locally
        </button>
        {running && (
          <button
            onClick={() =>
              dispatch("ai.cancel", {
                ...envelope(workspace),
                runId: current.id,
              }).catch((e) => setError(String(e)))
            }
          >
            Cancel generation
          </button>
        )}
        <button
          disabled={running}
          onClick={() => {
            setConversation("");
            setCurrent(null);
            setProposal(null);
          }}
        >
          New conversation
        </button>
      </div>
      {error && (
        <p role="alert" className="ai-error">
          {error}
        </p>
      )}
      {current && (
        <article className="ai-answer">
          <p className="ai-meta">
            {current.request.mode} · {current.state} · memory{" "}
            {current.request.useMemory ? "on" : "off"}
          </p>
          {current.state === "running" && (
            <p role="status">
              Generating a draft… source links are validated when generation
              finishes.
            </p>
          )}
          {current.request.mode === "workspace_agent" ? (
            <p>
              {current.state === "completed"
                ? "Your workspace plan is ready to review. No changes have been applied."
                : "Preparing a workspace plan…"}
            </p>
          ) : (
            <div className="ai-prose">
              {current.text.split(/(\[\d+\])/g).map((part, i) => {
                const c = current.citations.find(
                  (c) => `[${c.number}]` === part,
                );
                return c ? (
                  <button
                    className="ai-citation"
                    key={i}
                    disabled={c.state !== "current"}
                    onClick={() => openCitation(c)}
                    title={c.title}
                  >
                    {part}
                    {c.state !== "current" ? " stale" : ""}
                  </button>
                ) : (
                  <span key={i}>{part}</span>
                );
              })}
            </div>
          )}
          {current.warning && <p className="ai-note">{current.warning}</p>}
          {current.error && <p role="alert">{current.error}</p>}
          <details>
            <summary>Source context · {current.sources.length}</summary>
            {current.sources.map((s) => (
              <div className="ai-source" key={s.number}>
                <button
                  disabled={!!s.state && s.state !== "current"}
                  onClick={() => openCitation(s)}
                >
                  [{s.number}] {s.title}
                  {s.state && s.state !== "current" ? " · source changed" : ""}
                </button>
                <p>{s.text}</p>
              </div>
            ))}
          </details>
          {current.state === "completed" && (
            <details className="ai-review">
              <summary>Review a page or task change</summary>
              <p>
                {current.request.mode === "workspace_agent"
                  ? "Review the destinations and each proposed change. If the plan needs corrections, change your request and generate a fresh plan."
                  : "Edit the proposed text, then inspect the exact changes before approval. A text replacement removes formatting in replaced text."}
              </p>
              {current.request.mode !== "workspace_agent" && (
                <>
                  <textarea
                    aria-label="Reviewed draft text"
                    value={draft || current.text}
                    onChange={(e) => {
                      setDraft(e.target.value);
                      setProposal(null);
                    }}
                  />
                  <select
                    aria-label="Reviewed action"
                    value={action}
                    onChange={(e) => {
                      setAction(e.target.value);
                      setProposal(null);
                    }}
                  >
                    <option value="append">Append blocks to source page</option>
                    <option value="replace_block">
                      Replace selected source block/text
                    </option>
                    <option value="replace_page">
                      Replace plain text page
                    </option>
                    <option value="create_page">Create page</option>
                    <option value="create_task">Create task</option>
                    <option value="create_database">
                      Create database from JSON draft
                    </option>
                    <option value="mermaid">Insert or update Mermaid</option>
                    <option value="replace_code">
                      Replace selected code (never execute)
                    </option>
                    <option value="workspace_patch">
                      Review named workspace plan
                    </option>
                  </select>
                  {action.startsWith("create_") && (
                    <input
                      aria-label="New page or task title"
                      placeholder="Title"
                      value={title}
                      onChange={(e) => {
                        setTitle(e.target.value);
                        setProposal(null);
                      }}
                    />
                  )}
                </>
              )}
              <button disabled={busy} onClick={review}>
                Preview exact changes
              </button>
              {proposal && (
                <div className="ai-proposal">
                  <strong>
                    {proposal.commands.length} reviewed changes ·{" "}
                    {proposal.kind}
                  </strong>
                  {proposal.title && <h4>{proposal.title}</h4>}
                  {proposal.before && (
                    <>
                      <h4>Before</h4>
                      <pre>{proposal.before}</pre>
                    </>
                  )}
                  <h4>
                    {proposal.kind === "append" ? "Blocks to append" : "After"}
                  </h4>
                  <pre>{proposal.after}</pre>
                  <button disabled={busy} onClick={apply}>
                    Approve and apply these changes
                  </button>
                  <button onClick={() => setProposal(null)}>
                    Discard preview
                  </button>
                </div>
              )}
            </details>
          )}
        </article>
      )}
      <details>
        <summary>Saved conversations · {runs.length}</summary>
        {runs.map((r) => (
          <button
            className="ai-history"
            key={r.id}
            onClick={() => {
              setCurrent(r);
              setDraft(r.text);
              setConversation(r.conversationId);
              setProposal(null);
            }}
          >
            {r.request.prompt || r.request.mode} · {r.state}
          </button>
        ))}
      </details>
      <MemoryPanel
        workspace={workspace}
        pageId={pageId}
        onCommitted={onCommitted}
      />
    </section>
  );
}

function MemoryPanel({
  workspace,
  pageId,
  onCommitted,
}: Omit<AIPanelProps, "onNavigate">) {
  const [entries, setEntries] = useState<Memory[]>([]),
    [error, setError] = useState("");
  const [kind, setKind] = useState("preference"),
    [content, setContent] = useState(""),
    [scope, setScope] = useState("workspace"),
    [project, setProject] = useState("");
  const [edit, setEdit] = useState<Memory | null>(null),
    [supersedes, setSupersedes] = useState(""),
    [busy, setBusy] = useState(false);
  const source = workspace.pages.find((p) => p.id === pageId);
  async function load() {
    const result = await dispatch<{ entries: Memory[] }>("memory.list", {
      ...envelope(workspace),
      includeInactive: true,
    });
    setEntries(result.entries);
  }
  useEffect(() => {
    void load().catch((e) => setError(String(e)));
  }, [workspace.workspaceId, workspace.snapshotCursor]);
  const guard = (m: Memory): S.ExpectedRevision[] => [
    { target: { entityType: "memory", entityId: m.id }, revision: m.revision },
    {
      target: { entityType: "page", entityId: m.pageId },
      revision: workspace.pages.find((p) => p.id === m.pageId)?.revision ?? "0",
    },
  ];
  async function mutate(
    m: Memory,
    op: "approve" | "forget" | "disable" | "restore",
  ) {
    setBusy(true);
    try {
      const commands: S.Command[] =
        op === "forget"
          ? [{ kind: "memory.forget", memoryId: m.id }]
          : op === "disable"
            ? [{ kind: "memory.setStatus", memoryId: m.id, status: "disabled" }]
            : [{ kind: "memory.approve", memoryId: m.id }];
      const guards = guard(m);
      if (m.supersedesId) {
        const prev = entries.find((v) => v.id === m.supersedesId);
        if (prev) guards.push(...guard(prev));
      }
      await batch(workspace, commands, guards);
      await onCommitted();
      await load();
      setError("");
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  async function save() {
    setBusy(true);
    try {
      const sources: Location[] = source
        ? [{ pageId: source.id, sourceRevision: source.revision }]
        : [];
      if ((kind === "decision" || kind === "project_context") && !source)
        throw new Error(
          "Open the source page before remembering context or a decision.",
        );
      if (edit) {
        const command: S.Command = {
          kind: "memory.update",
          memoryId: edit.id,
          ...(edit.kind !== "project_context" ? { content } : {}),
          ...(source ? { sources } : {}),
        };
        await batch(workspace, [command], guard(edit));
      } else {
        const [memoryId, memoryPage] = await ids(workspace, 2);
        const memoryScope =
          scope === "project"
            ? {
                kind: "project" as const,
                workspaceId: workspace.workspaceId,
                projectId: project,
              }
            : scope === "pages" && source
              ? {
                  kind: "pages" as const,
                  workspaceId: workspace.workspaceId,
                  pageIds: [source.id],
                }
              : {
                  kind: "workspace" as const,
                  workspaceId: workspace.workspaceId,
                };
        const proposal = {
          kind: "memory.propose",
          memoryId,
          pageId: memoryPage,
          scope: memoryScope,
          memoryKind: kind,
          sources,
          ...(kind === "project_context"
            ? { pageRef: source!.id }
            : { content }),
          ...(supersedes ? { supersedesId: supersedes } : {}),
        } as S.Command;
        await batch(
          workspace,
          [
            {
              kind: "page.create",
              pageId: memoryPage,
              title: `Memory · ${kind.replaceAll("_", " ")}`,
            },
            proposal,
          ],
          [
            {
              target: { entityType: "page", entityId: memoryPage },
              revision: "0",
            },
            {
              target: { entityType: "memory", entityId: memoryId },
              revision: "0",
            },
          ],
        );
      }
      setEdit(null);
      setContent("");
      setSupersedes("");
      await onCommitted();
      await load();
      setError("");
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section className="ai-memory" aria-label="Approved memory">
      <h3>Approved memory</h3>
      <p>
        Preferences, project context, and evidence-linked decisions. Project
        context references the live page. Current task and database values stay
        in their canonical records.
      </p>
      {error && (
        <p role="alert" className="ai-error">
          {error}
        </p>
      )}
      <div className="ai-controls">
        <select
          aria-label="Memory kind"
          value={kind}
          disabled={!!edit}
          onChange={(e) => setKind(e.target.value)}
        >
          <option value="preference">Preference</option>
          <option value="project_context">Project context</option>
          <option value="decision">Decision with evidence</option>
        </select>
        <select
          aria-label="Memory scope"
          value={scope}
          disabled={!!edit}
          onChange={(e) => setScope(e.target.value)}
        >
          <option value="workspace">Workspace</option>
          <option value="pages" disabled={!source}>
            Current page
          </option>
          <option value="project">Project</option>
        </select>
        {scope === "project" && (
          <select
            aria-label="Memory project"
            value={project}
            onChange={(e) => setProject(e.target.value)}
          >
            <option value="">Choose project</option>
            {workspace.containers
              .filter((c) => c.kind === "project")
              .map((c) => (
                <option key={c.id} value={c.id}>
                  {workspace.pages.find((p) => p.id === c.owningPageId)?.title}
                </option>
              ))}
          </select>
        )}
      </div>
      {kind !== "project_context" && (
        <textarea
          aria-label="Memory content"
          placeholder="An enduring preference or evidence-linked decision…"
          value={content}
          maxLength={16000}
          onChange={(e) => setContent(e.target.value)}
        />
      )}
      <p>
        Evidence:{" "}
        {source
          ? `${source.title} · revision ${source.revision}`
          : "No page selected"}
        {supersedes ? " · supersedes the selected memory after approval" : ""}
      </p>
      <button
        disabled={
          busy ||
          (kind !== "project_context" && !content.trim()) ||
          (scope === "project" && !project)
        }
        onClick={save}
      >
        {edit ? "Save edit for review" : "Remember for review"}
      </button>
      {edit && (
        <button
          onClick={() => {
            setEdit(null);
            setContent("");
          }}
        >
          Cancel edit
        </button>
      )}
      {entries.map((m) => (
        <article className="ai-memory-card" key={m.id}>
          <strong>
            {m.kind.replaceAll("_", " ")} · {m.status}
          </strong>
          <p>
            {m.content ??
              workspace.pages.find((p) => p.id === m.pageRef)?.title ??
              "Referenced page unavailable"}
          </p>
          <small>
            {m.sources.length} source references · {m.scope.kind}
          </small>
          <div className="ai-controls">
            {m.status === "proposed" && (
              <button disabled={busy} onClick={() => mutate(m, "approve")}>
                Approve memory
              </button>
            )}
            {["disabled", "forgotten"].includes(m.status) && (
              <button disabled={busy} onClick={() => mutate(m, "restore")}>
                Restore and approve
              </button>
            )}
            {m.status === "active" && (
              <button disabled={busy} onClick={() => mutate(m, "disable")}>
                Disable
              </button>
            )}
            {m.status !== "forgotten" && (
              <button disabled={busy} onClick={() => mutate(m, "forget")}>
                Forget
              </button>
            )}
            <button
              disabled={busy}
              onClick={() => {
                setEdit(m);
                setKind(m.kind);
                setContent(m.content ?? "");
                setScope(m.scope.kind);
                if (m.scope.kind === "project") setProject(m.scope.projectId);
              }}
            >
              Edit for review
            </button>
            <button
              disabled={busy}
              onClick={() => {
                setEdit(null);
                setSupersedes(m.id);
                setKind(m.kind);
                setContent(m.content ?? "");
              }}
            >
              Supersede
            </button>
          </div>
          {m.status === "stale" && (
            <p>
              Evidence changed. Open the current source, edit the memory, and
              approve it again.
            </p>
          )}
        </article>
      ))}
    </section>
  );
}
