import { useEffect, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";
import "./providers.css";
type ContextPreview = {
  pages: { pageId: string; title: string; revision: string; blocks?: number }[];
  notice: string;
};
const toolLabels: Record<string, string> = {
  readPage: "Read selected pages",
  "search.query": "Search selected pages",
  queryRecords: "Read database records",
  resolveCitation: "Resolve source references",
  allocateIds: "Prepare identifiers for new content",
  proposeChanges: "Propose edits to selected content",
  proposeActions: "Propose named page, task and database changes for review",
};
type Connection = {
  id: string;
  provider: string;
  state: string;
  authMode: string;
  billingMode: string;
  pageIds: string[];
  tools: string[];
  token?: string;
};
type Proposal = {
  id: string;
  connectionId: string;
  state: string;
  request: unknown;
  changes?: {
    label: string;
    destination: string;
    before?: string;
    after: string;
  }[];
  result?: unknown;
};
type Capability = {
  provider: string;
  authOwner: string;
  route: string;
  enabled: boolean;
  reason: string;
};
export function ProviderPanel({
  workspace,
  onCommitted,
}: {
  workspace: WorkspaceSnapshot;
  onCommitted: () => void | Promise<void>;
}) {
  const [pages, setPages] = useState<string[]>([]),
    [tools, setTools] = useState(["readPage", "search.query"]),
    [connections, setConnections] = useState<Connection[]>([]),
    [caps, setCaps] = useState<Capability[]>([]),
    [proposals, setProposals] = useState<Proposal[]>([]),
    [preview, setPreview] = useState<ContextPreview | null>(null),
    [pair, setPair] = useState<{
      connection: Connection;
      url: string;
      relayPath: string;
    } | null>(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false),
    [provider, setProvider] = useState("mcp"),
    [handoff, setHandoff] = useState(""),
    [objective, setObjective] = useState("");
  async function load() {
    const [s, p] = await Promise.all([
      dispatch<{ connections: Connection[]; capabilities: Capability[] }>(
        "providers.status",
        envelope(workspace),
      ),
      dispatch<Proposal[]>("providers.proposals", envelope(workspace)),
    ]);
    setConnections(s.connections);
    setCaps(s.capabilities);
    setProposals(p);
  }
  useEffect(() => {
    void load().catch((e) => setError(String(e)));
    const timer = setInterval(
      () => void load().catch((e) => setError(String(e))),
      3000,
    );
    return () => clearInterval(timer);
  }, [workspace.workspaceId]);
  async function act(fn: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await fn();
      await load();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <section
      className="ai-panel provider-panel"
      aria-label="Provider connections"
    >
      <h2>Providers and workspace access</h2>
      <p>
        Local AI remains the default. Connect a client to selected pages, then
        review every proposed change here. Connections expire when Trove closes.
      </p>
      <label>
        Provider{" "}
        <select
          aria-label="Provider"
          value={provider}
          onChange={(e) => {
            setProvider(e.target.value);
            setPreview(null);
          }}
        >
          <option value="mcp">Paired local MCP client</option>
          <option value="claude_code">Claude Code</option>
          <option value="codex">Codex</option>
        </select>
      </label>
      {provider !== "mcp" ? (
        caps
          .filter((c) => c.provider === provider)
          .map((c) => (
            <article key={c.provider}>
              <h3>{c.provider === "codex" ? "Codex" : "Claude Code"}</h3>
              <p>
                Authentication owner: {c.authOwner}. {c.route}.
              </p>
              <p role="status">
                Unavailable in this build. {c.reason}. No account credentials
                are requested and no billing mode changes.
              </p>
            </article>
          ))
      ) : (
        <>
          <fieldset>
            <legend>Pages this client may read and propose changes to</legend>
            {workspace.pages
              .filter((p) => !p.deletedAt)
              .map((p) => (
                <label key={p.id} style={{ display: "block" }}>
                  <input
                    type="checkbox"
                    checked={pages.includes(p.id)}
                    onChange={(e) => {
                      setPages(
                        e.target.checked
                          ? [...pages, p.id]
                          : pages.filter((id) => id !== p.id),
                      );
                      setPreview(null);
                      setPair(null);
                    }}
                  />
                  {p.title || "Untitled"}
                </label>
              ))}
          </fieldset>
          <fieldset>
            <legend>Allowed tools</legend>
            {[
              "readPage",
              "search.query",
              "queryRecords",
              "resolveCitation",
              "allocateIds",
              "proposeChanges",
              "proposeActions",
            ].map((t) => (
              <label key={t} style={{ display: "block" }}>
                <input
                  type="checkbox"
                  checked={tools.includes(t)}
                  onChange={(e) => {
                    setTools(
                      e.target.checked
                        ? [...tools, t]
                        : tools.filter((v) => v !== t),
                    );
                    setPreview(null);
                    setPair(null);
                  }}
                />
                {toolLabels[t]}
              </label>
            ))}
          </fieldset>
          {tools.includes("proposeActions") && (
            <p>
              New pages and tasks require approval in a selected destination.
              Created pages remain private until you explicitly share them.
            </p>
          )}
          <button
            disabled={busy || !pages.length || !tools.length}
            onClick={() =>
              act(async () =>
                setPreview(
                  await dispatch<ContextPreview>("providers.preview", {
                    ...envelope(workspace),
                    pageIds: pages,
                  }),
                ),
              )
            }
          >
            Preview shared context
          </button>
          {preview != null && (
            <div className="provider-context">
              <h3>Context to share</h3>
              <ul>
                {preview.pages.map((p) => (
                  <li key={p.pageId}>
                    <strong>{p.title || "Untitled"}</strong>
                    <span>
                      Revision {p.revision}
                      {p.blocks != null ? ` · ${p.blocks} blocks` : ""}
                    </span>
                  </li>
                ))}
              </ul>
              <p>{preview.notice}</p>
              <button
                disabled={busy}
                onClick={() =>
                  act(async () => {
                    setPair(
                      await dispatch("providers.connect", {
                        ...envelope(workspace),
                        provider: "mcp",
                        pageIds: pages,
                        tools,
                      }),
                    );
                    setPreview(null);
                  })
                }
              >
                Approve context and pair client
              </button>
            </div>
          )}
          {pair && (
            <details open>
              <summary>
                Client configuration · keep the pairing token private
              </summary>
              <p>
                Use this configuration in your MCP client. Disconnect below
                revokes this token.
              </p>
              <pre>
                {JSON.stringify(
                  {
                    mcpServers: {
                      trove: {
                        command: pair.relayPath,
                        env: {
                          TROVE_MCP_URL: pair.url,
                          TROVE_MCP_CONNECTION: pair.connection.id,
                          TROVE_MCP_TOKEN: pair.connection.token,
                        },
                      },
                    },
                  },
                  null,
                  2,
                )}
              </pre>
              <button onClick={() => setPair(null)}>Hide token</button>
            </details>
          )}
        </>
      )}
      {error && <p role="alert">{error}</p>}
      <h3>Connections</h3>
      {connections.length === 0 && <p>No clients connected.</p>}
      {connections.map((c) => (
        <article key={c.id}>
          <strong>
            {c.provider} · {c.state}
          </strong>
          <p>
            {c.pageIds.length} pages ·{" "}
            {c.tools.map((t) => toolLabels[t] || t).join(", ")}
          </p>
          <button
            disabled={c.state !== "connected"}
            onClick={async () => {
              try {
                await dispatch("providers.disconnect", {
                  ...envelope(workspace),
                  connectionId: c.id,
                });
                if (pair?.connection.id === c.id) setPair(null);
                await load();
              } catch (e) {
                setError(String(e));
              }
            }}
          >
            Disconnect and revoke local access
          </button>
          <input
            aria-label="Handoff objective"
            placeholder="Objective for a reviewed handoff"
            value={objective}
            onChange={(e) => setObjective(e.target.value)}
          />
          <button
            disabled={busy || !objective}
            onClick={() =>
              act(async () =>
                setHandoff(
                  JSON.stringify(
                    await dispatch("providers.handoff", {
                      ...envelope(workspace),
                      connectionId: c.id,
                      objective,
                      result: "",
                      pending:
                        "Review the current sources and choose next steps.",
                    }),
                    null,
                    2,
                  ),
                ),
              )
            }
          >
            Preview handoff
          </button>
        </article>
      ))}
      {handoff && <pre>{handoff}</pre>}
      <h3>Proposed changes</h3>
      {proposals.length === 0 && <p>No proposals awaiting review.</p>}
      {proposals.map((p) => (
        <article key={p.id}>
          <strong>{p.state}</strong>
          {p.changes?.map((change, index) => (
            <section key={index}>
              <h4>{change.label}</h4>
              <p>Destination: {change.destination}</p>
              {change.before && <p>Before: {change.before}</p>}
              <pre>{change.after}</pre>
            </section>
          ))}
          <details>
            <summary>Exact commands and revision checks</summary>
            <pre>{JSON.stringify(p.request, null, 2)}</pre>
          </details>
          {p.state === "pending" && (
            <>
              <button
                disabled={busy}
                onClick={() =>
                  act(async () => {
                    await dispatch("providers.approve", {
                      ...envelope(workspace),
                      proposalId: p.id,
                    });
                    await onCommitted();
                  })
                }
              >
                Approve these exact changes
              </button>
              <button
                disabled={busy}
                onClick={() =>
                  act(async () => {
                    await dispatch("providers.reject", {
                      ...envelope(workspace),
                      proposalId: p.id,
                    });
                  })
                }
              >
                Deny
              </button>
            </>
          )}
          {p.result != null && <pre>{JSON.stringify(p.result, null, 2)}</pre>}
        </article>
      ))}
    </section>
  );
}
