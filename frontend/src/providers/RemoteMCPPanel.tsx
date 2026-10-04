import { useEffect, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";
import type * as S from "../../../contracts/generated/service";

const toolLabels: Record<string, string> = {
  readPage: "Read selected pages",
  "search.query": "Search selected pages",
  queryRecords: "Read database records",
  resolveCitation: "Resolve source references",
  allocateIds: "Prepare new content identifiers",
  proposeChanges: "Propose edits for your approval",
  proposeActions: "Propose page, task and database changes for your approval",
};
type Preview = {
  pages: { pageId: string; title: string; revision: string }[];
  notice: string;
};
export function RemoteMCPPanel({
  workspace,
}: {
  workspace: WorkspaceSnapshot;
}) {
  const [status, setStatus] = useState<S.RemoteStatus | null>(null);
  const [certificate, setCertificate] = useState<S.RemoteCertificate | null>(
    null,
  );
  const [origin, setOrigin] = useState("");
  const [bind, setBind] = useState("127.0.0.1:8443");
  const [origins, setOrigins] = useState("");
  const [pages, setPages] = useState<string[]>([]);
  const [tools, setTools] = useState(["readPage", "search.query"]);
  const [preview, setPreview] = useState<Preview | null>(null);
  const [confirmed, setConfirmed] = useState<Record<string, boolean>>({});
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  async function load() {
    setStatus(
      await dispatch<S.RemoteStatus>(
        "providers.remote.status",
        envelope(workspace),
      ),
    );
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
  const title = (id: string) =>
    workspace.pages.find((p) => p.id === id)?.title ?? id;
  return (
    <section aria-label="Remote MCP access">
      <h3>Remote MCP access</h3>
      <p>
        Connect a hosted client through your own HTTPS address. Remote access
        starts disabled each time Trove opens. Keep Trove open for the
        connection to work.
      </p>
      {status?.enabled ? (
        <>
          <p role="status">
            HTTPS enabled at {status.origin}. Certificate expires{" "}
            {new Date(status.certificateExpiresAt).toLocaleString()}.
          </p>
          <button
            disabled={busy}
            onClick={() =>
              act(async () => {
                await dispatch("providers.remote.disable", envelope(workspace));
                setCertificate(null);
                setPreview(null);
              })
            }
          >
            Disable remote access and disconnect all clients
          </button>
          <fieldset disabled={busy}>
            <legend>Select context for a new remote connection</legend>
            {workspace.pages
              .filter((p) => !p.deletedAt)
              .map((p) => (
                <label key={p.id}>
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
                    }}
                  />
                  {p.title || "Untitled"}
                </label>
              ))}
          </fieldset>
          <fieldset disabled={busy}>
            <legend>Allowed tools</legend>
            {Object.entries(toolLabels).map(([name, label]) => (
              <label key={name}>
                <input
                  type="checkbox"
                  checked={tools.includes(name)}
                  onChange={(e) => {
                    setTools(
                      e.target.checked
                        ? [...tools, name]
                        : tools.filter((t) => t !== name),
                    );
                    setPreview(null);
                  }}
                />
                {label}
              </label>
            ))}
          </fieldset>
          <button
            disabled={busy || !pages.length || !tools.length}
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
            Review selected remote context
          </button>
          {preview && (
            <div aria-label="Remote context preview">
              <p>{preview.notice}</p>
              <ul>
                {preview.pages.map((p) => (
                  <li key={p.pageId}>
                    {p.title} (revision {p.revision})
                  </li>
                ))}
              </ul>
              <p>
                Tools: {tools.map((t) => toolLabels[t]).join(", ")}. Every
                proposed write still needs your approval in Trove.
              </p>
              <button
                disabled={busy}
                onClick={() =>
                  act(async () => {
                    await dispatch("providers.remote.createConnection", {
                      ...envelope(workspace),
                      pageIds: pages,
                      tools,
                    });
                    setPreview(null);
                  })
                }
              >
                Create scoped remote URL
              </button>
            </div>
          )}
        </>
      ) : (
        <>
          <p role="status">Remote access is disabled.</p>
          <label>
            Public HTTPS origin
            <input
              placeholder="https://trove.example.com"
              value={origin}
              onChange={(e) => setOrigin(e.target.value)}
              disabled={busy}
            />
          </label>
          <label>
            Listener IP and port
            <input
              value={bind}
              onChange={(e) => setBind(e.target.value)}
              disabled={busy}
            />
          </label>
          <label>
            Allowed browser origins (one HTTPS origin per line)
            <textarea
              value={origins}
              onChange={(e) => setOrigins(e.target.value)}
              disabled={busy}
            />
          </label>
          <p>
            The public hostname must reach this listener and match your valid
            certificate. Hosted clients generally call it from their servers;
            add browser origins only when needed.
          </p>
          <button
            disabled={busy}
            onClick={() =>
              act(async () =>
                setCertificate(
                  await dispatch<S.RemoteCertificate>(
                    "providers.remote.selectCertificate",
                    envelope(workspace),
                  ),
                ),
              )
            }
          >
            Choose certificate and private key…
          </button>
          {certificate && (
            <p>
              Certificate for {certificate.dnsNames.join(", ")} · expires{" "}
              {new Date(certificate.expiresAt).toLocaleString()}. File paths and
              private key stay in the native host.
            </p>
          )}
          <button
            disabled={busy || !certificate || !origin || !bind}
            onClick={() =>
              act(async () => {
                await dispatch("providers.remote.enable", {
                  ...envelope(workspace),
                  certificateHandle: certificate!.handle,
                  publicOrigin: origin.trim(),
                  bindAddress: bind.trim(),
                  allowedOrigins: origins
                    .split(/\n/)
                    .map((s) => s.trim())
                    .filter(Boolean),
                });
                setCertificate(null);
              })
            }
          >
            Enable HTTPS listener
          </button>
        </>
      )}
      {status?.connections.map((c) => (
        <article key={c.id} aria-label="Remote connection">
          <p>
            {c.state === "awaiting_client"
              ? "Waiting for a client authorization request"
              : c.state === "authorized"
                ? "Authorized remote client"
                : "Disconnected"}
          </p>
          <p>
            Pages: {c.pageIds.map(title).join(", ")}. Tools:{" "}
            {c.tools.map((t) => toolLabels[t] ?? t).join(", ")}.
          </p>
          {c.state !== "disconnected" && (
            <>
              <label>
                Connection URL
                <input
                  readOnly
                  value={c.url}
                  onFocus={(e) => e.currentTarget.select()}
                />
              </label>
              <p>
                Enter this URL in your hosted client. Its authorization request
                will appear here for review.
              </p>
            </>
          )}
          {c.clientId && <p>Bound client: {c.clientId}</p>}
          <button
            disabled={busy || c.state === "disconnected"}
            onClick={() =>
              act(async () => {
                await dispatch("providers.remote.disconnect", {
                  ...envelope(workspace),
                  connectionId: c.id,
                });
              })
            }
          >
            Disconnect remote client
          </button>
        </article>
      ))}
      {status?.authorizations
        .filter((a) => a.state === "pending")
        .map((a) => {
          const c = status.connections.find((c) => c.id === a.connectionId);
          return (
            <article key={a.id} aria-label="Remote authorization review">
              <h4>Client requests access</h4>
              <p>Client label (supplied by client): {a.clientName}</p>
              <p>
                {a.metadataUrl
                  ? `Verified metadata address: ${a.metadataUrl}`
                  : `Dynamically registered public client: ${a.clientId}`}
              </p>
              <p>Exact return address: {a.redirectUri}</p>
              <p>
                Pages: {c?.pageIds.map(title).join(", ")}. Tools:{" "}
                {c?.tools.map((t) => toolLabels[t] ?? t).join(", ")}.
              </p>
              <p>
                Comparison code: <strong>{a.comparisonCode}</strong>. Request
                expires {new Date(a.expiresAt).toLocaleTimeString()}.
              </p>
              <label>
                <input
                  type="checkbox"
                  checked={confirmed[a.id] ?? false}
                  onChange={(e) =>
                    setConfirmed({ ...confirmed, [a.id]: e.target.checked })
                  }
                />
                I initiated this connection and the browser shows this
                comparison code.
              </label>
              <button
                disabled={busy || !confirmed[a.id]}
                onClick={() =>
                  act(async () => {
                    await dispatch("providers.remote.approveAuthorization", {
                      ...envelope(workspace),
                      authorizationId: a.id,
                      reviewHash: a.reviewHash,
                    });
                  })
                }
              >
                Approve this client and context
              </button>
              <button
                disabled={busy}
                onClick={() =>
                  act(async () => {
                    await dispatch("providers.remote.denyAuthorization", {
                      ...envelope(workspace),
                      authorizationId: a.id,
                      reviewHash: a.reviewHash,
                    });
                  })
                }
              >
                Deny remote authorization
              </button>
            </article>
          );
        })}
      {status?.message && <p role="status">{status.message}</p>}
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
