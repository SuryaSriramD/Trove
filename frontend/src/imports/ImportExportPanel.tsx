import { useEffect, useRef, useState } from "react";
import { dispatch, envelope, fileURL, type WorkspaceSnapshot } from "../api";
import "./imports.css";
import { NotionCapturePanel } from "./NotionCapturePanel";

type Column = {
  name: string;
  type: string;
  propertyId: string;
  samples: string[];
  warning?: string;
};
type Item = {
  csvKeyColumn?: string;
  merge?: {
    baselineStatus: "retained" | "unavailable";
    localDeleted: boolean;
    fields: Array<{
      path: string;
      outcome: string;
      base: Presence;
      local: Presence;
      incoming: Presence;
    }>;
    resolutions: Array<{ path: string; choice: string }>;
  };
  sourceLocator: string;
  kind: string;
  outcome: string;
  reason: string;
  retainedFileId?: string;
  pageId?: string;
  title?: string;
  conflict?: string;
  choice?: string;
  committed: boolean;
  columns?: Column[];
};
type Presence = { present: boolean; value?: unknown };
const describeField = (value: Presence) =>
  value.present ? JSON.stringify(value.value, null, 2) : "(absent)";
function describeImportPath(
  path: string,
  item: Item,
  items: Item[],
  workspace: WorkspaceSnapshot,
) {
  const parts = path.split("/");
  if (parts[1] === "exists")
    return "Source page existence (explicit deletion moves only this page to Trash; content and attachments remain recoverable)";
  if (parts[1] === "collectionTitle") return "Collection title";
  if (parts[1] === "records") {
    const record = workspace.records.find((r) => r.id === parts[2]);
    const name =
      workspace.pages.find((p) => p.id === record?.pageId)?.title ?? parts[2];
    return `Record: ${name} (accepting source deletion moves it to Trash; content and attachments remain recoverable)`;
  }
  if (parts[1] === "attachments")
    return "Attachment relationship (removal requires review)";
  if (parts[1] === "siblingOrder")
    return "Sibling block order (choose the complete list)";
  if (parts[1] !== "schema" && parts[1] !== "values") return path;
  const property = parts[1] === "schema" ? parts[2] : parts[3];
  const schema = item.merge?.fields.find(
    (field) => field.path === `/schema/${property}`,
  );
  const definition = (
    schema?.local.present ? schema.local.value : schema?.incoming.value
  ) as { name?: string } | undefined;
  const name = definition?.name ?? property;
  if (parts[1] === "schema") return `Property: ${name}`;
  const record = item.merge?.fields.find(
    (field) => field.path === `/records/${parts[2]}`,
  );
  const page = record?.local.present
    ? record.local.value
    : record?.incoming.value;
  const title =
    items.find((candidate) => candidate.pageId === page)?.title ?? parts[2];
  return `${title} · ${name}`;
}
function EditedImportField({
  value,
  disabled,
  onResolve,
}: {
  value: Presence;
  disabled: boolean;
  onResolve: (value: Presence) => void;
}) {
  const [draft, setDraft] = useState(
    JSON.stringify(value.value ?? null, null, 2),
  );
  const [present, setPresent] = useState(value.present);
  const [error, setError] = useState("");
  return (
    <details>
      <summary>Use an edited value</summary>
      <label>
        <input
          type="checkbox"
          checked={present}
          disabled={disabled}
          onChange={(event) => setPresent(event.target.checked)}
        />
        Field is present
      </label>
      {present && (
        <textarea
          aria-label="Merged import field JSON"
          value={draft}
          disabled={disabled}
          onChange={(event) => setDraft(event.target.value)}
        />
      )}
      <button
        disabled={disabled}
        onClick={() => {
          try {
            const next: Presence = present
              ? { present: true, value: JSON.parse(draft) }
              : { present: false };
            setError("");
            onResolve(next);
          } catch {
            setError(
              "Enter valid JSON, including null for a present empty value.",
            );
          }
        }}
      >
        Save edited decision
      </button>
      {error && <p role="alert">{error}</p>}
    </details>
  );
}
type Plan = {
  notionGraph?: boolean;
  typedDatabaseMerge?: boolean;
  jobId?: string;
  planId: string;
  planHash: string;
  sourceNamespace: string;
  sourceName: string;
  rootPageId: string;
  sourceFileId: string;
  state: string;
  cursor: number;
  initialized: boolean;
  items: Item[];
  sourceCompleteness: string;
  sourceOmissions: string[];
  unknownCompleteness: string[];
};
type Recent = {
  planId: string;
  sourceName: string;
  state: string;
  cursor: number;
  total: number;
  createdAt: string;
};
const notionGraphKinds = new Set([
  "notion_container",
  "notion_template",
  "portable_collection",
  "portable_record",
]);
const labels: Record<string, string> = {
  preserved: "Preserved",
  adapted: "Adapted",
  partial: "Partial",
  unsupported_but_retained: "Unsupported but retained",
  failed: "Failed",
};

export function ImportExportPanel({
  workspace,
  onChanged,
  onOpenPage,
  initialPlanId,
}: {
  initialPlanId?: string;
  workspace: WorkspaceSnapshot;
  onChanged: () => Promise<void> | void;
  onOpenPage?: (id: string) => void;
}) {
  const [plan, setPlan] = useState<Plan>();
  const [recent, setRecent] = useState<Recent[]>([]);
  const [namespace, setNamespace] = useState("my-notion-workspace");
  const [busy, setBusy] = useState(false);
  const [running, setRunning] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const stop = useRef(false);
  const previewRequest = useRef("");
  const [inventoryProgress, setInventoryProgress] = useState<{
    stage: string;
    completed: number;
    total: number;
  } | null>(null);
  const request = <T,>(method: string, data: object = {}) =>
    dispatch<T>(method, { ...envelope(workspace), ...data });
  const refresh = () => request<Recent[]>("imports.list").then(setRecent);
  useEffect(() => {
    if (!initialPlanId) return;
    let active = true;
    void request<Plan>("imports.plan", { planId: initialPlanId })
      .then((value) => {
        if (active) setPlan(value);
      })
      .catch((e) => {
        if (active) setError(String(e));
      });
    return () => {
      active = false;
    };
  }, [initialPlanId, workspace.workspaceId]);
  useEffect(() => {
    void refresh().catch((e) => setError(String(e)));
    return () => {
      stop.current = true;
    };
  }, [workspace.workspaceId]);
  useEffect(
    () =>
      window.runtime?.EventsOn(
        "imports:previewProgress",
        (...args: unknown[]) => {
          const progress = args[0] as any;
          if (
            progress?.workspaceId === workspace.workspaceId &&
            progress.requestId === previewRequest.current
          )
            setInventoryProgress(progress);
        },
      ),
    [workspace.workspaceId],
  );
  const act = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await fn();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy(false);
    }
  };
  const preview = (folder = false) =>
    act(async () => {
      const bridge = window.go!.main.App as unknown as {
        PickImportFolder: () => Promise<string>;
      };
      const sourceHandle = folder
        ? await bridge.PickImportFolder()
        : await window.go!.main.App.PickFile();
      if (!sourceHandle) return;
      const base = envelope(workspace);
      previewRequest.current = base.requestId;
      setInventoryProgress({ stage: "reading", completed: 0, total: 0 });
      try {
        setPlan(
          await dispatch<Plan>("imports.preview", {
            ...base,
            sourceHandle,
            sourceNamespace: namespace,
          }),
        );
        await refresh();
      } finally {
        setInventoryProgress(null);
        previewRequest.current = "";
      }
    });
  const configure = (data: object) =>
    act(async () => {
      if (!plan) return;
      setPlan(
        await request<Plan>("imports.configure", {
          planId: plan.planId,
          planHash: plan.planHash,
          ...data,
        }),
      );
    });
  const run = async () => {
    if (!plan) return;
    stop.current = false;
    setRunning(true);
    setError("");
    let current = plan;
    try {
      while (!stop.current && current.state !== "complete") {
        current = await request<Plan>("imports.commit", {
          planId: current.planId,
          planHash: current.planHash,
        });
        setPlan(current);
      }
      await onChanged();
      await refresh();
      if (current.state === "complete")
        setNotice("Import processing finished. Review all outcomes below.");
      else
        setNotice(
          "Paused after a durable commit. Resume here, including after restarting Trove.",
        );
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setRunning(false);
    }
  };
  const exportWorkspace = () =>
    act(async () => {
      const bridge = window.go!.main.App as unknown as {
        ExportPortable?: () => Promise<string>;
      };
      if (!bridge.ExportPortable)
        throw new Error("Portable export is available in the desktop app.");
      const raw = await bridge.ExportPortable();
      if (raw) {
        const result = JSON.parse(raw) as {
          path: string;
          pages: number;
          collections: number;
          assets: number;
        };
        setNotice(
          `Exported ${result.pages} pages, ${result.collections} databases and ${result.assets} assets to ${result.path}`,
        );
      }
    });
  const exportAdditional = (kind: "HTML" | "PDF") =>
    act(async () => {
      const bridge = window.go!.main.App as unknown as {
        ExportHTML: () => Promise<string>;
        ExportPDF: () => Promise<string>;
      };
      const raw = await (kind === "HTML"
        ? bridge.ExportHTML()
        : bridge.ExportPDF());
      if (raw) {
        const result = JSON.parse(raw);
        setNotice(
          `Exported ${result.pages} pages to ${result.path}. ${(result.sourceOmissions ?? []).join(" ")}`,
        );
      }
    });
  const unresolved =
    plan?.items.filter((i) => i.conflict && !i.choice).length ?? 0;
  return (
    <section
      className="import-export-panel"
      aria-label="Import and portable export"
    >
      <div className="import-heading">
        <div>
          <h2>Import &amp; export</h2>
          <p>
            Bring ChatGPT or Claude conversation JSON, HTML or Markdown ZIPs,
            CSV, DOCX, text, or a selected folder into Trove. Original source
            files remain available.
          </p>
        </div>
        <button disabled={busy || running} onClick={exportWorkspace}>
          Export Markdown &amp; CSV
        </button>
        <button
          disabled={busy || running}
          onClick={() => exportAdditional("HTML")}
        >
          Export HTML
        </button>
        <button
          disabled={busy || running}
          onClick={() => exportAdditional("PDF")}
        >
          Export readable PDF
        </button>
      </div>
      <div className="import-controls">
        <label>
          Source namespace
          <input
            value={namespace}
            onChange={(e) => setNamespace(e.target.value)}
            disabled={busy || running}
            placeholder="Stable name for this source workspace"
          />
        </label>
        <button
          disabled={!namespace.trim() || busy || running}
          onClick={() => preview()}
        >
          {busy ? "Working…" : "Choose import file"}
        </button>
        <button
          disabled={!namespace.trim() || busy || running}
          onClick={() => preview(true)}
        >
          Import selected folder
        </button>
      </div>
      <p className="import-help">
        Reuse the same namespace to identify re-imports. Export creates a ZIP
        with readable pages, CSV databases, hierarchy, attachments, and a
        provenance manifest. Conversation JSON preserves message roles, IDs,
        timestamps, all supplied branches and parent/reply navigation. Non-text
        payloads remain in the source attachment. Up to 256 conversations and
        2,000 nodes per conversation are accepted. Use native backup for
        complete history and trash.
      </p>
      <NotionCapturePanel
        workspace={workspace}
        namespace={namespace}
        onPlan={(value) => {
          setPlan(value as Plan);
          void refresh();
        }}
      />
      {inventoryProgress && (
        <div role="status">
          <p>
            {inventoryProgress.stage === "reading"
              ? "Reading source bytes"
              : inventoryProgress.stage === "inventory"
                ? "Inventorying archive members"
                : "Parsing content and rebuilding hierarchy"}{" "}
            · {inventoryProgress.completed.toLocaleString()}
            {inventoryProgress.total
              ? ` / ${inventoryProgress.total.toLocaleString()}`
              : ""}
          </p>
          <progress
            value={
              inventoryProgress.total ? inventoryProgress.completed : undefined
            }
            max={inventoryProgress.total || 1}
            aria-label="Import compatibility preview progress"
          />
        </div>
      )}
      {error && (
        <p role="alert" className="import-error">
          {error}
        </p>
      )}
      {notice && <p role="status">{notice}</p>}
      {recent.length > 0 && (
        <details>
          <summary>Recent imports and resumable plans</summary>
          <ul>
            {recent.map((r) => (
              <li key={r.planId}>
                <button
                  disabled={busy || running}
                  onClick={() =>
                    act(async () =>
                      setPlan(
                        await request<Plan>("imports.plan", {
                          planId: r.planId,
                        }),
                      ),
                    )
                  }
                >
                  {r.sourceName} · {r.state} · {r.cursor}/{r.total}
                </button>
              </li>
            ))}
          </ul>
        </details>
      )}
      {plan && (
        <>
          <div className="import-heading">
            <h3>{plan.sourceName}</h3>
            <a href={fileURL(plan.sourceFileId)} download>
              Original source
            </a>
          </div>
          <div className="import-outcomes">
            {Object.entries(labels).map(([key, label]) => (
              <span key={key} className={`import-outcome ${key}`}>
                {label}:{" "}
                <strong>
                  {plan.items.filter((i) => i.outcome === key).length}
                </strong>
              </span>
            ))}
          </div>
          <p>
            {plan.items.length} discovered items · {plan.items.length} accounted
            for · {plan.cursor} committed
          </p>
          <progress
            value={plan.cursor}
            max={Math.max(plan.items.length, 1)}
            aria-label="Import progress"
          />
          <details open>
            <summary>Source completeness: {plan.sourceCompleteness}</summary>
            <h4>Known source omissions</h4>
            {plan.sourceOmissions.length ? (
              <ul>
                {plan.sourceOmissions.map((s, i) => (
                  <li key={i}>{s}</li>
                ))}
              </ul>
            ) : (
              <p>
                No omission statement was supplied. This does not establish
                completeness.
              </p>
            )}
            <h4>Unknown completeness</h4>
            <ul>
              {plan.unknownCompleteness.map((s, i) => (
                <li key={i}>{s}</li>
              ))}
            </ul>
          </details>
          {!plan.initialized &&
            plan.items
              .filter((i) => i.columns)
              .map((item) => (
                <details key={item.sourceLocator} open>
                  <summary>CSV properties · {item.sourceLocator}</summary>
                  <p>
                    The first column is the page title. Empty cells remain
                    unset. Formula and view definitions are never inferred from
                    display values.
                  </p>
                  {item.kind === "csv" ? (
                    <label>
                      Stable row key
                      <select
                        aria-label={`Row key for ${item.sourceLocator}`}
                        value=""
                        disabled={busy || running}
                        onChange={(event) =>
                          configure({
                            csvKeys: [
                              {
                                sourceLocator: item.sourceLocator,
                                index: Number(event.target.value),
                              },
                            ],
                          })
                        }
                      >
                        <option value="">
                          Choose a unique source key to enable future merges…
                        </option>
                        {item.columns!.slice(1).map((column, index) => (
                          <option key={column.propertyId} value={index + 1}>
                            {column.name}
                          </option>
                        ))}
                      </select>
                      <small>
                        Review column types first. Keys must be unique and
                        nonempty. Titles and row positions never identify
                        existing records.
                      </small>
                    </label>
                  ) : item.csvKeyColumn ? (
                    <p>
                      Retained row key: <strong>{item.csvKeyColumn}</strong>.
                      Re-imports match that source key even when rows move or
                      titles change.
                    </p>
                  ) : null}
                  <table>
                    <thead>
                      <tr>
                        <th>Property</th>
                        <th>Import as</th>
                        <th>Sample source values</th>
                      </tr>
                    </thead>
                    <tbody>
                      {item.columns!.map((column, index) => (
                        <tr key={column.propertyId}>
                          <td>
                            {column.name}
                            {column.warning && <small>{column.warning}</small>}
                          </td>
                          <td>
                            {index === 0 ? (
                              "Page title"
                            ) : item.kind !== "csv" ? (
                              column.type
                            ) : (
                              <select
                                aria-label={`Type for ${column.name}`}
                                value={column.type}
                                disabled={busy || running}
                                onChange={(e) =>
                                  configure({
                                    columns: [
                                      {
                                        sourceLocator: item.sourceLocator,
                                        index,
                                        type: e.target.value,
                                      },
                                    ],
                                  })
                                }
                              >
                                {[
                                  "text",
                                  "number",
                                  "checkbox",
                                  "date",
                                  "url",
                                  "email",
                                ].map((type) => (
                                  <option key={type}>{type}</option>
                                ))}
                              </select>
                            )}
                          </td>
                          <td>{column.samples.join(" · ") || "(blank)"}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </details>
              ))}
          {plan.items.some((i) => i.conflict) && (
            <details open>
              <summary>
                Review re-import conflicts ({unresolved} unresolved)
              </summary>
              {plan.items
                .filter((i) => i.conflict)
                .map((item) => (
                  <div className="import-conflict" key={item.sourceLocator}>
                    <strong>{item.sourceLocator}</strong>
                    <p>{item.conflict}</p>
                    {item.merge && (
                      <details open>
                        <summary>
                          Field review ·{" "}
                          {item.merge.baselineStatus === "retained"
                            ? "retained source baseline"
                            : "baseline unavailable"}
                        </summary>
                        <p>
                          Unchanged fields and one-sided edits merge
                          automatically. Choose content, parent and order
                          together when deleting a block. JSON null and absent
                          values are different.
                        </p>
                        <table>
                          <thead>
                            <tr>
                              <th>Field / outcome</th>
                              <th>Base</th>
                              <th>Local</th>
                              <th>Incoming</th>
                              <th>Decision</th>
                            </tr>
                          </thead>
                          <tbody>
                            {item.merge.fields
                              .filter((field) => field.outcome !== "unchanged")
                              .map((field) => (
                                <tr key={field.path}>
                                  <td>
                                    {describeImportPath(
                                      field.path,
                                      item,
                                      plan.items,
                                      workspace,
                                    )}
                                    {field.path.startsWith("/schema/") ||
                                    field.path.startsWith("/values/") ? (
                                      <small className="import-field-path">
                                        {field.path}
                                      </small>
                                    ) : null}
                                    <br />
                                    {field.outcome}
                                    {field.path.startsWith("/records/") &&
                                      field.local.present &&
                                      typeof field.local.value === "string" &&
                                      onOpenPage && (
                                        <button
                                          type="button"
                                          onClick={() =>
                                            onOpenPage(
                                              field.local.value as string,
                                            )
                                          }
                                        >
                                          Inspect local record
                                        </button>
                                      )}
                                  </td>
                                  <td>
                                    <pre>{describeField(field.base)}</pre>
                                  </td>
                                  <td>
                                    <pre>{describeField(field.local)}</pre>
                                  </td>
                                  <td>
                                    <pre>{describeField(field.incoming)}</pre>
                                  </td>
                                  <td>
                                    <select
                                      aria-label={`Resolution for ${item.sourceLocator} ${field.path}`}
                                      disabled={
                                        busy ||
                                        running ||
                                        plan.initialized ||
                                        item.merge?.localDeleted
                                      }
                                      value={
                                        item.merge?.resolutions.find(
                                          (choice) =>
                                            choice.path === field.path,
                                        )?.choice ?? ""
                                      }
                                      onChange={(event) =>
                                        configure({
                                          resolutions: [
                                            {
                                              sourceLocator: item.sourceLocator,
                                              fields: [
                                                {
                                                  path: field.path,
                                                  choice: event.target.value,
                                                },
                                              ],
                                            },
                                          ],
                                        })
                                      }
                                    >
                                      <option value="" disabled>
                                        {field.outcome === "conflict"
                                          ? "Choose explicitly…"
                                          : "Automatic merge"}
                                      </option>
                                      <option value="keep_local">
                                        Keep local
                                      </option>
                                      <option value="take_incoming">
                                        {(field.path.startsWith("/records/") ||
                                          field.path === "/exists") &&
                                        !field.incoming.present
                                          ? "Accept source deletion"
                                          : "Take incoming"}
                                      </option>
                                      {item.merge?.resolutions.find(
                                        (choice) => choice.path === field.path,
                                      )?.choice === "merged" && (
                                        <option value="merged">
                                          Edited value
                                        </option>
                                      )}
                                    </select>
                                    <EditedImportField
                                      value={field.local}
                                      disabled={
                                        busy ||
                                        running ||
                                        plan.initialized ||
                                        !!item.merge?.localDeleted
                                      }
                                      onResolve={(value) =>
                                        configure({
                                          resolutions: [
                                            {
                                              sourceLocator: item.sourceLocator,
                                              fields: [
                                                {
                                                  path: field.path,
                                                  choice: "merged",
                                                  value,
                                                },
                                              ],
                                            },
                                          ],
                                        })
                                      }
                                    />
                                  </td>
                                </tr>
                              ))}
                          </tbody>
                        </table>
                      </details>
                    )}
                    <select
                      aria-label={`Conflict choice for ${item.sourceLocator}`}
                      value={item.choice ?? ""}
                      disabled={busy || running || plan.initialized}
                      onChange={(e) =>
                        configure({
                          choices:
                            (plan.notionGraph || plan.typedDatabaseMerge) &&
                            notionGraphKinds.has(item.kind) &&
                            e.target.value !== "merge"
                              ? plan.items
                                  .filter((i) => notionGraphKinds.has(i.kind))
                                  .map((i) => ({
                                    sourceLocator: i.sourceLocator,
                                    choice: e.target.value,
                                  }))
                              : [
                                  {
                                    sourceLocator: item.sourceLocator,
                                    choice: e.target.value,
                                  },
                                ],
                        })
                      }
                    >
                      <option value="" disabled>
                        Choose explicitly…
                      </option>
                      <option value="keep_local">
                        Keep local; retain incoming source
                      </option>
                      {item.merge && !item.merge.localDeleted && (
                        <option value="merge">
                          Apply reviewed field merge
                        </option>
                      )}
                      {(!plan.notionGraph ||
                        !notionGraphKinds.has(item.kind)) &&
                        item.kind !== "csv" &&
                        item.kind !== "portable_collection" &&
                        item.kind !== "source_deleted_page" && (
                          <option value="replace">
                            Replace the page body with incoming content
                          </option>
                        )}
                      {item.kind !== "source_deleted_page" && (
                        <option value="duplicate">
                          Import a separate copy
                        </option>
                      )}
                    </select>
                  </div>
                ))}
            </details>
          )}
          <div className="import-controls">
            {plan.jobId && plan.state !== "complete" && (
              <p>
                Queued in Jobs. Use Jobs to cancel or retry this import; reopen
                this plan to refresh its report.
              </p>
            )}
            {plan.state !== "complete" && !running && !plan.jobId && (
              <button
                disabled={busy || unresolved > 0}
                onClick={() =>
                  act(async () => {
                    await request("imports.enqueue", {
                      planId: plan.planId,
                      planHash: plan.planHash,
                    });
                    setPlan(
                      await request<Plan>("imports.plan", {
                        planId: plan.planId,
                      }),
                    );
                    setNotice(
                      "Reviewed import queued. Open Jobs for progress, cancellation, and retry.",
                    );
                  })
                }
              >
                Commit in background
              </button>
            )}
            {plan.state !== "complete" &&
              !plan.jobId &&
              (running ? (
                <button
                  onClick={() => {
                    stop.current = true;
                  }}
                >
                  Pause after current item
                </button>
              ) : (
                <button disabled={busy || unresolved > 0} onClick={run}>
                  {plan.initialized
                    ? "Resume import"
                    : "Commit reviewed import"}
                </button>
              ))}
            {plan.initialized && (
              <button onClick={() => onOpenPage?.(plan.rootPageId)}>
                Open imported pages
              </button>
            )}
          </div>
          <details open>
            <summary>Complete item inventory and report</summary>
            <div className="import-inventory">
              <table>
                <thead>
                  <tr>
                    <th>Discovered item</th>
                    <th>Outcome</th>
                    <th>Details</th>
                    <th>Evidence</th>
                  </tr>
                </thead>
                <tbody>
                  {plan.items.map((item, index) => (
                    <tr key={`${item.sourceLocator}-${index}`}>
                      <td>
                        {item.pageId && item.committed ? (
                          <button onClick={() => onOpenPage?.(item.pageId!)}>
                            {item.sourceLocator}
                          </button>
                        ) : (
                          item.sourceLocator
                        )}
                      </td>
                      <td>{labels[item.outcome]}</td>
                      <td>
                        {item.reason}
                        {item.committed ? " · Committed" : " · Planned"}
                      </td>
                      <td>
                        {item.retainedFileId && (
                          <a href={fileURL(item.retainedFileId)} download>
                            Original
                          </a>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </details>
        </>
      )}
    </section>
  );
}
