import { useEffect, useRef, useState } from "react";
import {
  batch,
  dispatch,
  envelope,
  fileGuard,
  pageGuards,
  requestID,
  type S,
  type WorkspaceSnapshot,
} from "../api";
import { ImportExportPanel } from "../imports/ImportExportPanel";
import "./connectors.css";

type Account = S.ConnectorAccount;
type Selection = S.ConnectorSelection;
type Source = S.ConnectorSnapshot;
type Proposal = S.ConnectorProposal;
type History = { refreshes: S.ConnectorRefresh[]; proposals: Proposal[] };
const names: Record<string, string> = {
  google_calendar: "Google Calendar",
  google_drive: "Google Drive",
  gmail: "Gmail",
  github: "GitHub",
  notion: "Notion",
  mcp: "MCP server",
};
const emptyHistory: History = { refreshes: [], proposals: [] };
function message(error: unknown) {
  return error instanceof Error ? error.message : String(error);
}

export function ConnectorsPanel(props: {
  workspace: WorkspaceSnapshot;
  onChanged: () => Promise<void> | void;
  onOpenPage: (id: string) => void;
}) {
  return <Connections key={props.workspace.workspaceId} {...props} />;
}
function Connections({
  workspace,
  onChanged,
  onOpenPage,
}: {
  workspace: WorkspaceSnapshot;
  onChanged: () => Promise<void> | void;
  onOpenPage: (id: string) => void;
}) {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [selected, setSelected] = useState("");
  const [kind, setKind] =
    useState<S.ConnectorRegistration["kind"]>("google_calendar");
  const [clientId, setClientId] = useState("");
  const [endpoint, setEndpoint] = useState("");
  const [dynamic, setDynamic] = useState(false);
  const [attempt, setAttempt] = useState<S.ConnectorAuthAttempt>();
  const [history, setHistory] = useState<History>(emptyHistory);
  const [options, setOptions] = useState<Selection[]>([]);
  const [choices, setChoices] = useState<Selection[]>([]);
  const [cursor, setCursor] = useState("");
  const [query, setQuery] = useState("");
  const [sources, setSources] = useState<Source[]>([]);
  const [preview, setPreview] = useState<Source>();
  const [planId, setPlanId] = useState("");
  const [proposal, setProposal] = useState<Proposal>();
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const [busy, setBusy] = useState(false);
  const [sourceOperation, setSourceOperation] = useState("");
  const [manual, setManual] = useState("");
  const [destination, setDestination] = useState("");
  const [acquired, setAcquired] = useState<S.PrepareFileResult>();
  const alive = useRef(true);
  const epoch = useRef(0);
  const account = accounts.find((a) => a.id === selected);
  const request = <T,>(method: string, data: object = {}) =>
    dispatch<T>(method, { ...envelope(workspace), ...data });
  async function sourceRequest<T>(method: string, data: object) {
    const operationId = requestID();
    setSourceOperation(operationId);
    try {
      return await dispatch<T>(method, {
        ...envelope(workspace),
        ...data,
        requestId: operationId,
      });
    } finally {
      if (alive.current)
        setSourceOperation((id) => (id === operationId ? "" : id));
    }
  }
  const current = (stamp: number) => alive.current && epoch.current === stamp;
  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
      epoch.current++;
    };
  }, []);
  useEffect(() => {
    const stamp = epoch.current;
    void request<Account[]>("connectors.list")
      .then((value) => {
        if (current(stamp)) setAccounts(value);
      })
      .catch((e) => {
        if (current(stamp)) setError(message(e));
      });
  }, []);
  async function act(fn: (stamp: number) => Promise<void>) {
    const stamp = epoch.current;
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await fn(stamp);
    } catch (e) {
      if (current(stamp)) setError(message(e));
    } finally {
      if (current(stamp)) setBusy(false);
    }
  }
  async function reload(stamp: number) {
    const value = await request<Account[]>("connectors.list");
    if (current(stamp)) setAccounts(value);
  }
  function choose(a?: Account) {
    if ((a?.id ?? "") === selected) return;
    epoch.current++;
    setBusy(false);
    setSelected(a?.id ?? "");
    setChoices(a?.selections ?? []);
    setOptions([]);
    setCursor("");
    setHistory(emptyHistory);
    setPreview(undefined);
    setProposal(undefined);
    setAcquired(undefined);
    setPlanId("");
    setError("");
    setNotice("");
    setManual("");
    setSources([]);
  }
  useEffect(() => {
    if (!account) return;
    const stamp = epoch.current;
    let pending = false;
    const poll = async () => {
      if (pending) return;
      pending = true;
      try {
        const h = await request<History>("connectors.history", {
          accountId: account.id,
        });
        if (current(stamp)) {
          setHistory(h);
          setProposal((p) =>
            p ? (h.proposals.find((v) => v.id === p.id) ?? p) : p,
          );
        }
      } catch (e) {
        if (current(stamp)) setError(message(e));
      } finally {
        pending = false;
      }
    };
    void poll();
    const timer = setInterval(() => void poll(), 3000);
    return () => clearInterval(timer);
  }, [account?.id]);
  useEffect(() => {
    if (
      !attempt ||
      !["waiting", "authorized", "verifying"].includes(attempt.state)
    )
      return;
    const stamp = epoch.current;
    let pending = false;
    const poll = async () => {
      if (pending) return;
      pending = true;
      try {
        const next = await request<S.ConnectorAuthAttempt>(
          "connectors.native.authPoll",
          { attemptId: attempt.id },
        );
        if (current(stamp)) {
          setAttempt(next);
          if (next.state === "complete") await reload(stamp);
        }
      } catch (e) {
        if (current(stamp)) setError(message(e));
      } finally {
        pending = false;
      }
    };
    const timer = setInterval(() => void poll(), 1500);
    void poll();
    return () => clearInterval(timer);
  }, [attempt?.id, attempt?.state]);
  const begin = (existing?: Account) =>
    act(async (stamp) => {
      const registration = {
        kind: existing?.kind ?? kind,
        clientId: dynamic ? "" : clientId,
        endpoint:
          existing?.endpoint ||
          endpoint ||
          ((existing?.kind ?? kind) === "notion"
            ? "https://mcp.notion.com/mcp"
            : undefined),
        dynamicClientRegistration: dynamic,
      };
      const value = await request<S.ConnectorAuthAttempt>(
        "connectors.native.authBegin",
        {
          registration,
          ...(existing
            ? { accountId: existing.id, expectedRevision: existing.revision }
            : {}),
        },
      );
      if (current(stamp)) setAttempt(value);
    });
  const search = () =>
    act(async (stamp) => {
      const value = await request<Source[]>("connectors.search", { query });
      if (current(stamp)) setSources(value);
    });
  const discover = (more = false) =>
    act(async (stamp) => {
      if (!account) return;
      const result = await request<{
        selections: Selection[];
        nextCursor: string;
      }>("connectors.native.discover", {
        accountId: account.id,
        cursor: more ? cursor : "",
      });
      if (current(stamp)) {
        setOptions((old) => [
          ...new Map(
            [...(more ? old : []), ...result.selections].map((s) => [s.id, s]),
          ).values(),
        ]);
        setCursor(result.nextCursor);
      }
    });
  const refresh = (resumeId?: string) =>
    act(async (stamp) => {
      if (!account) return;
      const result = await request<S.ConnectorRefresh>(
        "connectors.native.refresh",
        { accountId: account.id, ...(resumeId ? { resumeId } : {}) },
      );
      if (current(stamp)) {
        setHistory((h) => ({
          ...h,
          refreshes: [result, ...h.refreshes.filter((r) => r.id !== result.id)],
        }));
        setNotice(
          "Refresh started. Progress and cancellation are available below.",
        );
      }
    });
  const showSource = (source: Source) =>
    act(async (stamp) => {
      const result = await sourceRequest<Source>("connectors.native.preview", {
        snapshotId: source.id,
      });
      if (current(stamp)) {
        setPreview(result);
        setAcquired(undefined);
        setPlanId("");
      }
    });
  const activeAttempt =
    attempt && ["waiting", "authorized", "verifying"].includes(attempt.state);
  return (
    <div className="connections-panel">
      <span className="eyebrow">SELECTED EXTERNAL SOURCES</span>
      <h1>Connections</h1>
      <p>
        Choose accounts and sources, refresh their local evidence, then review
        anything you import or send.
      </p>
      {busy && <p role="status">Working on the selected connection…</p>}
      {sourceOperation && (
        <button
          onClick={() =>
            void request("connectors.native.cancel", {
              operationId: sourceOperation,
            })
              .then(() =>
                setNotice(
                  "Cancellation requested. A completed result may still be retained.",
                ),
              )
              .catch((e) => setError(message(e)))
          }
        >
          Cancel source operation
        </button>
      )}
      {error && <p role="alert">{error}</p>}
      {notice && <p role="status">{notice}</p>}
      <div className="connection-columns">
        <aside aria-label="Connected accounts">
          <button disabled={!!activeAttempt} onClick={() => choose()}>
            New connection
          </button>
          {accounts.map((a) => (
            <button
              key={a.id}
              aria-pressed={a.id === selected}
              disabled={!!activeAttempt}
              onClick={() => choose(a)}
            >
              {names[a.kind]} · {a.identity.label}
              <small>{a.state}</small>
            </button>
          ))}
        </aside>
        <div>
          {!account ? (
            <section>
              <h2>Connect an account</h2>
              <label>
                Service
                <select
                  value={kind}
                  disabled={!!activeAttempt}
                  onChange={(e) => setKind(e.target.value as typeof kind)}
                >
                  {Object.entries(names).map(([id, name]) => (
                    <option key={id} value={id}>
                      {name}
                    </option>
                  ))}
                </select>
              </label>
              <label>
                Application client ID
                <input
                  value={clientId}
                  onChange={(e) => setClientId(e.target.value)}
                  autoComplete="off"
                />
              </label>
              <p>
                Use your provider's registered desktop application ID.
                Authorization opens in your browser; credentials stay in
                protected storage on this Mac.
              </p>
              {(kind === "mcp" || kind === "notion") && (
                <>
                  <label>
                    Trusted server HTTPS address
                    <input
                      value={endpoint}
                      placeholder={
                        kind === "notion"
                          ? "https://mcp.notion.com/mcp"
                          : "https://example.com/mcp"
                      }
                      onChange={(e) => setEndpoint(e.target.value)}
                    />
                  </label>
                  <label>
                    <input
                      type="checkbox"
                      checked={dynamic}
                      onChange={(e) => setDynamic(e.target.checked)}
                    />{" "}
                    Allow this server to register a public application for this
                    connection
                  </label>
                </>
              )}
              {kind === "google_drive" && (
                <p>
                  Select files in Google's authorization picker. Folder
                  selection grants the selected folder itself; it does not
                  select every child.
                </p>
              )}
              {kind === "gmail" && (
                <p>
                  Gmail-derived content stays disabled for external AI pending
                  separate qualification.
                </p>
              )}
              <button
                disabled={busy || !!activeAttempt || (!clientId && !dynamic)}
                onClick={() => void begin()}
              >
                Authorize connection
              </button>
            </section>
          ) : (
            <section>
              <h2>
                {names[account.kind]} · {account.identity.label}
              </h2>
              <p>
                {account.state} · Identity: {account.identity.subject}
              </p>
              {account.kind === "mcp" && (
                <p>
                  This identity identifies an authorization grant, not a
                  verified person. New authorization creates a new connection.
                </p>
              )}
              {account.state !== "active" && (
                <>
                  <button
                    disabled={busy}
                    onClick={() =>
                      void act(async (stamp) => {
                        await request("connectors.native.cleanup", {
                          accountId: account.id,
                        });
                        if (current(stamp))
                          setNotice("Protected credential cleanup completed.");
                      })
                    }
                  >
                    Retry credential cleanup
                  </button>
                  <p>
                    Restored connections remain inactive. Reauthorize explicitly
                    to refresh evidence; pending external operations are not
                    resumed automatically.
                  </p>
                  <label>
                    Application client ID
                    <input
                      value={clientId}
                      onChange={(e) => setClientId(e.target.value)}
                    />
                  </label>
                  {account.kind !== "mcp" && (
                    <button
                      disabled={busy || !!activeAttempt || !clientId}
                      onClick={() => void begin(account)}
                    >
                      Reauthorize account
                    </button>
                  )}
                </>
              )}
              {account.state === "active" && (
                <>
                  <div className="connection-actions">
                    <button disabled={busy} onClick={() => void discover()}>
                      Choose sources
                    </button>
                    <button
                      disabled={busy || !account.selections.length}
                      onClick={() => void refresh()}
                    >
                      Refresh selected sources
                    </button>
                    <button
                      disabled={busy}
                      onClick={() =>
                        void act(async (stamp) => {
                          const result = await request<{ detail: string }>(
                            "connectors.native.disconnect",
                            {
                              accountId: account.id,
                              expectedRevision: account.revision,
                            },
                          );
                          if (current(stamp)) setNotice(result.detail);
                          await reload(stamp);
                        })
                      }
                    >
                      Disconnect
                    </button>
                  </div>
                  <button
                    disabled={busy}
                    onClick={() =>
                      void act(async (stamp) => {
                        await request("connectors.native.cleanup", {
                          accountId: account.id,
                        });
                        if (current(stamp))
                          setNotice("Protected credential cleanup completed.");
                      })
                    }
                  >
                    Retry credential cleanup
                  </button>
                  <h3>Selected sources</h3>
                  {!choices.length && <p>No sources selected yet.</p>}
                  {[
                    ...new Map(
                      [...options, ...choices].map((s) => [s.id, s]),
                    ).values(),
                  ].map((s) => (
                    <div key={s.id}>
                      <label>
                        <input
                          type="checkbox"
                          checked={choices.some((c) => c.id === s.id)}
                          onChange={(e) =>
                            setChoices((old) =>
                              e.target.checked
                                ? [...old, s]
                                : old.filter((c) => c.id !== s.id),
                            )
                          }
                        />
                        {s.label} <small>{s.kind}</small>
                      </label>
                      {s.kind === "repository" &&
                        choices.some((c) => c.id === s.id) && (
                          <label>
                            Documentation paths (one per line; blank selects
                            README)
                            <textarea
                              value={
                                choices.find((c) => c.id === s.id)?.query ?? ""
                              }
                              onChange={(e) =>
                                setChoices((old) =>
                                  old.map((c) =>
                                    c.id === s.id
                                      ? { ...c, query: e.target.value }
                                      : c,
                                  ),
                                )
                              }
                            />
                          </label>
                        )}
                    </div>
                  ))}
                  {cursor && (
                    <button disabled={busy} onClick={() => void discover(true)}>
                      More sources
                    </button>
                  )}
                  {account.kind === "notion" && (
                    <label>
                      Additional selected page ID
                      <input
                        value={manual}
                        placeholder="Notion page UUID"
                        onChange={(e) => setManual(e.target.value)}
                      />
                      <button
                        disabled={!manual.trim()}
                        onClick={() => {
                          setChoices((old) => [
                            ...old,
                            {
                              id: manual.trim(),
                              kind: "page",
                              label: manual.trim(),
                            },
                          ]);
                          setManual("");
                        }}
                      >
                        Select page
                      </button>
                      <small>
                        Child and linked pages require their own selection.
                      </small>
                    </label>
                  )}
                  {account.kind === "gmail" && (
                    <label>
                      Additional Gmail query
                      <input
                        value={manual}
                        placeholder="label:work newer_than:30d"
                        onChange={(e) => setManual(e.target.value)}
                      />
                      <button
                        disabled={!manual.trim()}
                        onClick={() => {
                          setChoices((old) => [
                            ...old,
                            {
                              id: `query:${crypto.randomUUID()}`,
                              kind: "query",
                              label: manual,
                              query: manual,
                            },
                          ]);
                          setManual("");
                        }}
                      >
                        Add query
                      </button>
                    </label>
                  )}
                  <button
                    disabled={busy}
                    onClick={() =>
                      void act(async (stamp) => {
                        const updated = await request<Account>(
                          "connectors.selections",
                          {
                            accountId: account.id,
                            expectedRevision: account.revision,
                            selections: choices,
                          },
                        );
                        if (current(stamp)) {
                          setAccounts((old) =>
                            old.map((a) => (a.id === updated.id ? updated : a)),
                          );
                          setChoices(updated.selections);
                          setNotice(
                            "Selection saved. Refresh to retrieve current evidence.",
                          );
                        }
                      })
                    }
                  >
                    Save source selection
                  </button>
                  <WriteComposer
                    key={account.id}
                    workspace={workspace}
                    account={account}
                    disabled={busy}
                    onPrepare={(action) =>
                      act(async (stamp) => {
                        const p = await request<Proposal>(
                          "connectors.native.prepare",
                          { accountId: account.id, action },
                        );
                        if (current(stamp)) setProposal(p);
                      })
                    }
                  />
                </>
              )}
            </section>
          )}
          {attempt && (
            <section aria-label="Authorization status">
              <h3>Authorization · {attempt.state}</h3>
              <p>{attempt.message}</p>
              {attempt.userCode && (
                <p>
                  Enter code <strong>{attempt.userCode}</strong> in the
                  authorization browser.
                </p>
              )}
              {attempt.verificationUrl && (
                <a
                  href={attempt.verificationUrl}
                  target="_blank"
                  rel="noreferrer"
                >
                  Open provider authorization
                </a>
              )}
              {activeAttempt && (
                <button
                  onClick={() =>
                    void act(async (stamp) => {
                      await request("connectors.native.authCancel", {
                        attemptId: attempt.id,
                      });
                      if (current(stamp))
                        setAttempt({ ...attempt, state: "cancelled" });
                    })
                  }
                >
                  Cancel authorization
                </button>
              )}
            </section>
          )}
        </div>
      </div>
      {proposal && (
        <section
          className="connection-review"
          aria-label="External write review"
        >
          <h2>Review external change</h2>
          <p>
            {
              names[
                accounts.find((a) => a.id === proposal.accountId)?.kind ?? ""
              ]
            }{" "}
            · {proposal.identity.label} · {proposal.state}
          </p>
          <p>
            Selected source: {proposal.action.selectionId}
            {proposal.action.remoteId && ` · Item: ${proposal.action.remoteId}`}
          </p>
          <ul>
            {proposal.action.effects.map((effect, i) => (
              <li key={i}>{effect}</li>
            ))}
          </ul>
          <pre>{JSON.stringify(proposal.action.arguments, null, 2)}</pre>
          {proposal.receipt && (
            <p role="status">
              {proposal.receipt.outcome}: {proposal.receipt.detail}
            </p>
          )}
          {proposal.state === "review" && (
            <>
              <button
                disabled={busy}
                onClick={() =>
                  void act(async (stamp) => {
                    await request("connectors.native.execute", {
                      proposalId: proposal.id,
                      hash: proposal.hash,
                    });
                    if (current(stamp))
                      setProposal({ ...proposal, state: "sending" });
                  })
                }
              >
                Approve and send this exact change
              </button>
              <button
                disabled={busy}
                onClick={() =>
                  void act(async (stamp) => {
                    await request("connectors.discard", {
                      proposalId: proposal.id,
                    });
                    if (current(stamp))
                      setProposal({ ...proposal, state: "discarded" });
                  })
                }
              >
                Discard proposal
              </button>
            </>
          )}
          {proposal.state === "unknown" && (
            <>
              <p>
                The provider may have applied this change. Reconciliation checks
                existing results without sending it again.
              </p>
              <button
                disabled={busy}
                onClick={() =>
                  void act(async (stamp) => {
                    const receipt = await request<S.ConnectorReceipt>(
                      "connectors.native.reconcile",
                      { proposalId: proposal.id },
                    );
                    if (current(stamp))
                      setProposal({
                        ...proposal,
                        state: receipt.outcome,
                        receipt,
                      });
                  })
                }
              >
                Reconcile outcome
              </button>
            </>
          )}
          {proposal.state === "sending" && (
            <button
              onClick={() =>
                void act(async () => {
                  await request("connectors.native.cancel", {
                    operationId: proposal.id,
                  });
                })
              }
            >
              Request cancellation
            </button>
          )}
        </section>
      )}
      <section>
        <h2>Search external evidence</h2>
        <form
          onSubmit={(e) => {
            e.preventDefault();
            void search();
          }}
        >
          <label>
            Search selected sources
            <input value={query} onChange={(e) => setQuery(e.target.value)} />
          </label>
          <button disabled={busy}>Search</button>
        </form>
        <p>
          Search uses the last completed refresh. Opening a preview rechecks
          current access and content.
        </p>
        {sources.map((s) => (
          <button
            className="connection-source"
            key={s.id}
            disabled={busy}
            onClick={() => void showSource(s)}
          >
            <strong>{s.title || s.remoteId}</strong>
            <small>
              {names[accounts.find((a) => a.id === s.accountId)?.kind ?? ""]} ·
              Retrieved {new Date(s.retrievedAt).toLocaleString()}
            </small>
          </button>
        ))}
      </section>
      {preview && (
        <section aria-label="Authenticated external preview">
          <h2>{preview.title}</h2>
          <p>
            Retrieved {new Date(preview.retrievedAt).toLocaleString()} ·
            External AI disabled
          </p>
          <pre className="connection-text">{preview.text}</pre>
          {/^https:\/\//.test(preview.url) && (
            <a href={preview.url} target="_blank" rel="noreferrer">
              Open original source
            </a>
          )}
          <button
            disabled={busy}
            onClick={() =>
              void act(async (stamp) => {
                const plan = await sourceRequest<{ planId: string }>(
                  "connectors.native.import",
                  {
                    snapshotId: preview.id,
                    expectedRevision: preview.remoteRevision,
                  },
                );
                if (current(stamp)) setPlanId(plan.planId);
              })
            }
          >
            Review import into Trove
          </button>
          {accounts.find((a) => a.id === preview.accountId)?.kind ===
            "google_drive" && (
            <button
              disabled={busy}
              onClick={() =>
                void act(async (stamp) => {
                  const file = await sourceRequest<S.PrepareFileResult>(
                    "connectors.native.acquire",
                    {
                      snapshotId: preview.id,
                      expectedRevision: preview.remoteRevision,
                      remoteId: preview.remoteId,
                      format: "",
                    },
                  );
                  if (current(stamp)) setAcquired(file);
                })
              }
            >
              Acquire this file
            </button>
          )}
          {acquired && (
            <div>
              <p>
                Acquired {acquired.mediaType} · {acquired.sizeBytes} bytes.
                Choose a destination before attaching.
              </p>
              <label>
                Destination page
                <select
                  value={destination}
                  onChange={(e) => setDestination(e.target.value)}
                >
                  <option value="">Choose page</option>
                  {workspace.pages
                    .filter((p) => !p.deletedAt)
                    .map((p) => (
                      <option key={p.id} value={p.id}>
                        {p.title}
                      </option>
                    ))}
                </select>
              </label>
              <button
                disabled={busy || !destination}
                onClick={() =>
                  void act(async (stamp) => {
                    const page = workspace.pages.find(
                      (p) => p.id === destination,
                    );
                    if (!page) throw new Error("Reload the destination page");
                    await batch(
                      workspace,
                      [
                        {
                          kind: "file.attach",
                          pageId: page.id,
                          fileId: acquired.fileId,
                          ingestHandle: acquired.ingestHandle,
                        },
                      ],
                      [
                        ...pageGuards(workspace, page),
                        fileGuard(workspace, acquired.fileId),
                      ],
                    );
                    if (current(stamp)) {
                      setAcquired(undefined);
                      setNotice("File attached.");
                      await onChanged();
                    }
                  })
                }
              >
                Attach acquired file
              </button>
            </div>
          )}
        </section>
      )}
      {planId && (
        <ImportExportPanel
          workspace={workspace}
          initialPlanId={planId}
          onChanged={onChanged}
          onOpenPage={onOpenPage}
        />
      )}
      {account && (
        <section>
          <h2>Connection history</h2>
          {history.refreshes.map((r) => (
            <div key={r.id}>
              <p>
                Refresh · {r.state} · {r.pages} pages / {r.items} items{" "}
                {r.error && `· ${r.error}`}
                {r.retryAfter &&
                  ` · Retry after ${new Date(r.retryAfter).toLocaleString()}`}
              </p>
              {r.state === "running" && (
                <button
                  onClick={() =>
                    void act(async () => {
                      await request("connectors.native.cancel", {
                        operationId: r.id,
                      });
                    })
                  }
                >
                  Cancel refresh
                </button>
              )}
              {["paused", "interrupted"].includes(r.state) && (
                <button disabled={busy} onClick={() => void refresh(r.id)}>
                  Resume refresh
                </button>
              )}
            </div>
          ))}
          {history.proposals.map((p) => (
            <button key={p.id} onClick={() => setProposal(p)}>
              {p.action.kind} · {p.state} ·{" "}
              {new Date(p.createdAt).toLocaleString()}
            </button>
          ))}
        </section>
      )}
    </div>
  );
}

function WriteComposer({
  workspace,
  account,
  disabled,
  onPrepare,
}: {
  workspace: WorkspaceSnapshot;
  account: Account;
  disabled: boolean;
  onPrepare: (action: S.ConnectorAction) => Promise<void>;
}) {
  const operations: Record<string, [string, string][]> = {
    github: [
      ["issue.create", "Create issue"],
      ["issue.update", "Update issue"],
      ["comment.create", "Add comment"],
      ["comment.update", "Update comment"],
      ["pull_request.create", "Create pull request"],
    ],
    google_calendar: [
      ["calendar.create", "Create event"],
      ["calendar.update", "Update event"],
      ["calendar.cancel", "Cancel event"],
    ],
    google_drive: [
      ["drive.upload", "Upload new file"],
      ["drive.replace", "Replace file"],
    ],
    gmail: [
      ["gmail.send", "Compose email"],
      ["gmail.reply", "Reply to thread"],
    ],
    mcp: [["mcp.tool.call", "Run selected tool"]],
    notion: [
      ["notion.page.create", "Create child page"],
      ["notion.page.update", "Replace page content"],
      ["notion.page.properties", "Update page properties"],
    ],
  };
  const available = operations[account.kind] ?? [];
  const [operation, setOperation] = useState(available[0]?.[0] ?? "");
  const [selection, setSelection] = useState("");
  const [remote, setRemote] = useState("");
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [head, setHead] = useState("");
  const [base, setBase] = useState("");
  const [recipients, setRecipients] = useState("");
  const [start, setStart] = useState("");
  const [end, setEnd] = useState("");
  const [timezone, setTimezone] = useState(
    Intl.DateTimeFormat().resolvedOptions().timeZone,
  );
  const [allDay, setAllDay] = useState(true);
  const [notify, setNotify] = useState("none");
  const [repeat, setRepeat] = useState("");
  const [file, setFile] = useState<{
    name: string;
    mediaType: string;
    dataBase64: string;
  }>();
  const [localError, setLocalError] = useState("");
  const [draftRevision, setDraftRevision] = useState("");
  const [draftLoaded, setDraftLoaded] = useState(false);
  const [draftNotice, setDraftNotice] = useState("");
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  async function loadDraft() {
    try {
      const d = await dispatch<{
        revision: string;
        fields: Record<string, unknown>;
      }>("connectors.draft", { ...envelope(workspace), accountId: account.id });
      if (!mounted.current) return;
      const f = d.fields;
      const text = (key: string, fallback = "") =>
        typeof f[key] === "string" ? (f[key] as string) : fallback;
      setOperation(text("operation", available[0]?.[0] ?? ""));
      setSelection(text("selection"));
      setRemote(text("remote"));
      setTitle(text("title"));
      setBody(text("body"));
      setHead(text("head"));
      setBase(text("base"));
      setRecipients(text("recipients"));
      setStart(text("start"));
      setEnd(text("end"));
      setTimezone(
        text("timezone", Intl.DateTimeFormat().resolvedOptions().timeZone),
      );
      setAllDay(f.allDay !== false);
      setNotify(text("notify", "none"));
      setRepeat(text("repeat"));
      const attached = f.file as typeof file;
      setFile(
        attached &&
          typeof attached.name === "string" &&
          typeof attached.mediaType === "string" &&
          typeof attached.dataBase64 === "string"
          ? attached
          : undefined,
      );
      setDraftRevision(d.revision);
      setDraftLoaded(true);
      setDraftNotice(
        d.revision ? "Saved composition loaded." : "New local composition.",
      );
    } catch (e) {
      if (mounted.current) setLocalError(message(e));
    }
  }
  useEffect(() => {
    void loadDraft();
  }, []);
  async function saveDraft() {
    try {
      const d = await dispatch<{ revision: string }>("connectors.saveDraft", {
        ...envelope(workspace),
        accountId: account.id,
        expectedRevision: draftRevision,
        fields: {
          operation,
          selection,
          remote,
          title,
          body,
          head,
          base,
          recipients,
          start,
          end,
          timezone,
          allDay,
          notify,
          repeat,
          ...(file ? { file } : {}),
        },
      });
      if (mounted.current) {
        setDraftRevision(d.revision);
        setDraftNotice(
          "Composition saved in this workspace. Nothing was sent.",
        );
      }
    } catch (e) {
      if (mounted.current) setLocalError(message(e));
    }
  }
  if (!available.length) return null;
  async function prepare() {
    try {
      setLocalError("");
      let args: Record<string, unknown> = {};
      if (account.kind === "github") {
        args = { body };
        if (
          operation.startsWith("issue.") ||
          operation === "pull_request.create"
        )
          args.title = title;
        if (operation === "pull_request.create")
          Object.assign(args, { head, base });
      } else if (account.kind === "gmail") {
        args = {
          to: recipients
            .split(",")
            .map((s) => s.trim())
            .filter(Boolean),
          subject: title,
          text: body,
          ...(file ? { attachments: [file] } : {}),
        };
      } else if (account.kind === "google_calendar") {
        args = { sendUpdates: notify };
        if (operation !== "calendar.cancel")
          args.event = {
            summary: title,
            description: body,
            ...(repeat ? { recurrence: [`RRULE:FREQ=${repeat}`] } : {}),
            start: allDay
              ? { date: start }
              : { dateTime: start, timeZone: timezone },
            end: allDay ? { date: end } : { dateTime: end, timeZone: timezone },
            ...(recipients.trim()
              ? {
                  attendees: recipients
                    .split(",")
                    .map((email) => ({ email: email.trim() })),
                }
              : {}),
          };
      } else if (account.kind === "google_drive") {
        if (!file) throw new Error("Choose a file first");
        args = { ...file, name: title || file.name };
      } else if (account.kind === "notion") {
        if (operation === "notion.page.create")
          args = {
            parent: { page_id: selection },
            pages: [{ properties: { title }, content: body }],
            allow_async: false,
          };
        else if (operation === "notion.page.properties")
          args = {
            page_id: selection,
            command: "update_properties",
            properties: JSON.parse(body || "{}"),
            allow_async: false,
          };
        else
          args = {
            page_id: selection,
            command: "replace_content",
            new_str: body,
            allow_async: false,
          };
      } else {
        args = JSON.parse(body || "{}");
      }
      await onPrepare({
        version: 1,
        kind:
          operation === "notion.page.properties"
            ? "notion.page.update"
            : operation,
        selectionId: selection,
        ...(account.kind === "notion" && operation !== "notion.page.create"
          ? { remoteId: selection }
          : remote
            ? { remoteId: remote }
            : {}),
        arguments: args as S.ConnectorAction["arguments"],
        effects: [],
      });
    } catch (e) {
      setLocalError(message(e));
    }
  }
  return (
    <details className="connection-compose">
      <summary>Prepare an external change</summary>
      <p>
        Nothing is sent until you approve the resulting account, arguments and
        effects.
      </p>
      <button
        disabled={disabled || !draftLoaded}
        onClick={() => void saveDraft()}
      >
        Save local composition
      </button>
      <button disabled={disabled} onClick={() => void loadDraft()}>
        Reload saved composition
      </button>
      {draftNotice && <p role="status">{draftNotice}</p>}
      <fieldset disabled={!draftLoaded}>
        <label>
          Action
          <select
            value={operation}
            onChange={(e) => setOperation(e.target.value)}
          >
            {available.map(([id, name]) => (
              <option key={id} value={id}>
                {name}
              </option>
            ))}
          </select>
        </label>
        <label>
          Selected destination
          <select
            value={selection}
            onChange={(e) => setSelection(e.target.value)}
          >
            <option value="">Choose source</option>
            {account.selections.map((s) => (
              <option key={s.id} value={s.id}>
                {s.label}
              </option>
            ))}
          </select>
        </label>
        {![
          "issue.create",
          "pull_request.create",
          "gmail.send",
          "calendar.create",
          "drive.upload",
          "mcp.tool.call",
          "notion.page.create",
          "notion.page.update",
          "notion.page.properties",
        ].includes(operation) && (
          <label>
            Existing item ID
            <input
              value={remote}
              onChange={(e) => setRemote(e.target.value)}
              placeholder={
                account.kind === "github"
                  ? "issue:123 or comment:456"
                  : "Item ID from the source preview"
              }
            />
          </label>
        )}
        {account.kind !== "mcp" && operation !== "calendar.cancel" && (
          <label>
            {account.kind === "gmail"
              ? "Subject"
              : account.kind === "google_drive"
                ? "File name"
                : "Title"}
            <input value={title} onChange={(e) => setTitle(e.target.value)} />
          </label>
        )}
        {!["google_drive"].includes(account.kind) &&
          operation !== "calendar.cancel" && (
            <label>
              {account.kind === "mcp"
                ? "Exact tool arguments (JSON)"
                : operation === "notion.page.properties"
                  ? "Properties to update (JSON)"
                  : "Message or description"}
              <textarea
                value={body}
                onChange={(e) => setBody(e.target.value)}
              />
            </label>
          )}
        {operation === "pull_request.create" && (
          <>
            <label>
              Existing head branch
              <input value={head} onChange={(e) => setHead(e.target.value)} />
            </label>
            <label>
              Existing base branch
              <input value={base} onChange={(e) => setBase(e.target.value)} />
            </label>
          </>
        )}
        {(account.kind === "gmail" || account.kind === "google_calendar") && (
          <label>
            {account.kind === "gmail"
              ? "To (comma separated)"
              : "Attendees (comma separated)"}
            <input
              value={recipients}
              onChange={(e) => setRecipients(e.target.value)}
            />
          </label>
        )}
        {account.kind === "google_calendar" && (
          <>
            <label>
              <input
                type="checkbox"
                checked={allDay}
                onChange={(e) => setAllDay(e.target.checked)}
              />
              All-day event
            </label>
            <label>
              Start
              <input
                type={allDay ? "date" : "text"}
                placeholder="2026-10-16T09:00:00+05:30"
                value={start}
                onChange={(e) => setStart(e.target.value)}
              />
            </label>
            <label>
              End (exclusive for all-day events)
              <input
                type={allDay ? "date" : "text"}
                placeholder="2026-10-16T10:00:00+05:30"
                value={end}
                onChange={(e) => setEnd(e.target.value)}
              />
            </label>
            {!allDay && (
              <label>
                Timezone
                <input
                  value={timezone}
                  onChange={(e) => setTimezone(e.target.value)}
                />
              </label>
            )}
            <label>
              Repeats
              <select
                value={repeat}
                onChange={(e) => setRepeat(e.target.value)}
              >
                <option value="">Does not repeat</option>
                <option value="DAILY">Daily</option>
                <option value="WEEKLY">Weekly</option>
              </select>
            </label>
            <label>
              Send attendee updates
              <select
                value={notify}
                onChange={(e) => setNotify(e.target.value)}
              >
                <option value="none">None</option>
                <option value="all">All attendees</option>
                <option value="externalOnly">External attendees</option>
              </select>
            </label>
          </>
        )}
        {(account.kind === "google_drive" || account.kind === "gmail") && (
          <label>
            {account.kind === "gmail"
              ? "Attachment (optional, up to 4 MiB)"
              : "File (up to 4 MiB)"}
            <input
              type="file"
              onChange={(e) => {
                const chosen = e.target.files?.[0];
                setFile(undefined);
                if (!chosen) return;
                if (chosen.size > 4 * 1024 * 1024) {
                  setLocalError("Choose a file no larger than 4 MiB");
                  return;
                }
                const reader = new FileReader();
                reader.onload = () =>
                  setFile({
                    name: chosen.name,
                    mediaType: chosen.type || "application/octet-stream",
                    dataBase64: String(reader.result).split(",")[1],
                  });
                reader.onerror = () => setLocalError("File could not be read");
                reader.readAsDataURL(chosen);
              }}
            />
          </label>
        )}
        {localError && <p role="alert">{localError}</p>}
        <button
          disabled={disabled || !selection}
          onClick={() => void prepare()}
        >
          Prepare review
        </button>
      </fieldset>
    </details>
  );
}
