import { useEffect, useState } from "react";
import {
  batch,
  dispatch,
  envelope,
  ids,
  pageGuards,
  readBlock,
  type S,
  type WorkspaceSnapshot,
} from "../api";
import type { Location } from "../../../contracts/generated/domain";

type Graph = {
  nodes: { id: string; title: string; revision: string }[];
  edges: {
    source: string;
    target: string;
    kind: string;
    location: Location;
    evidence: string;
    score?: number;
  }[];
  truncated: boolean;
  warning?: string;
};
type Plan = {
  type: string;
  name: string;
  enabled: boolean;
  sourcePageIds: string[];
  query: string;
  includeMemory: boolean;
  schedule: {
    type: string;
    timezone: string;
    frequency: string;
    hour: number;
    minute: number;
    weekday: number;
    missedPolicy: string;
    catchUpLimit: number;
  };
};
type PlanView = {
  blockId: string;
  pageId: string;
  hash: string;
  activated: boolean;
  plan: Plan;
  nextAt: string;
  error?: string;
};
type Run = {
  id: string;
  pageId: string;
  planBlockId: string;
  status: string;
  text: string;
  sources: Location[];
  createdAt: string;
};
type Reviews = {
  plans: PlanView[];
  runs: Run[];
  engine: string;
  scheduling: string;
};
type OCRStatus = {
  available: boolean;
  engine: string;
  maxBytes: number;
  maxPixels: number;
  timeoutSeconds: number;
  minimumConfidence: number;
};
type OCR = {
  fileId: string;
  text: string;
  warning: string;
  acceptedLines: number;
  rejectedLines: number;
};
export function P2IntelligencePanel({
  workspace,
  refresh,
  onNavigate,
  onError,
  pageId,
}: {
  workspace: WorkspaceSnapshot;
  refresh: () => Promise<void>;
  onNavigate: (pageId: string) => void;
  onError: (error: unknown) => void;
  pageId?: string;
}) {
  const [focus, setFocus] = useState(pageId ?? workspace.pages[0]?.id ?? "");
  const [tab, setTab] = useState("graph"),
    [busy, setBusy] = useState(false);
  const [graph, setGraph] = useState<Graph | null>(null),
    [semantic, setSemantic] = useState(false),
    [query, setQuery] = useState("");
  const [reviews, setReviews] = useState<Reviews | null>(null),
    [reviewName, setReviewName] = useState("Personal review"),
    [time, setTime] = useState("09:00"),
    [frequency, setFrequency] = useState("daily"),
    [missedPolicy, setMissedPolicy] = useState("skip");
  const [zone, setZone] = useState(
      Intl.DateTimeFormat().resolvedOptions().timeZone,
    ),
    [source, setSource] = useState(focus);
  const [ocrStatus, setOCRStatus] = useState<OCRStatus | null>(null),
    [ocr, setOCR] = useState<OCR | null>(null),
    [imageId, setImageId] = useState("");
  const page = workspace.pages.find((p) => p.id === focus);
  const imageFiles = workspace.files.filter((f) =>
    ["image/png", "image/jpeg"].includes(f.mediaType),
  );
  const run = async (fn: () => Promise<void>) => {
    setBusy(true);
    try {
      await fn();
    } catch (e) {
      onError(e);
    } finally {
      setBusy(false);
    }
  };
  const loadReviews = async () =>
    setReviews(await dispatch<Reviews>("review.list", envelope(workspace)));
  useEffect(() => {
    let active = true;
    Promise.all([
      dispatch<OCRStatus>("ocr.status", envelope(workspace)),
      dispatch<Reviews>("review.list", envelope(workspace)),
    ])
      .then(([status, r]) => {
        if (active) {
          setOCRStatus(status);
          setReviews(r);
        }
      })
      .catch(onError);
    return () => {
      active = false;
    };
  }, [workspace.workspaceId, workspace.snapshotCursor]);
  useEffect(() => {
    if (pageId) {
      setFocus(pageId);
      setSource(pageId);
    }
  }, [pageId]);
  const loadGraph = async () =>
    setGraph(
      await dispatch<Graph>("graph.query", {
        ...envelope(workspace),
        pageId: focus,
        includeSemantic: semantic,
        query,
        limit: 60,
      }),
    );
  const createPlan = async () => {
    if (!page || !source) return;
    const [blockId] = await ids(workspace, 1);
    const [hour, minute] = time.split(":").map(Number);
    const plan: Plan = {
      type: "trove.personal-review/v1",
      name: reviewName,
      enabled: true,
      sourcePageIds: [source],
      query,
      includeMemory: true,
      schedule: {
        type: "schedule",
        timezone: zone,
        frequency,
        hour,
        minute,
        weekday: 1,
        missedPolicy,
        catchUpLimit: 1,
      },
    };
    await batch(
      workspace,
      [
        {
          kind: "block.insert",
          block: {
            blockId,
            pageId: page.id,
            type: "code",
            schemaVersion: 1,
            data: {
              language: "trove-review",
              source: JSON.stringify(plan, null, 2),
            },
          },
        },
        {
          kind: "review.activate",
          pageId: page.id,
          planBlockId: blockId,
          enabled: true,
        },
      ],
      [
        ...pageGuards(workspace, page),
        { target: { entityType: "block", entityId: blockId }, revision: "0" },
      ],
    );
    await refresh();
    await loadReviews();
  };
  const togglePlan = async (view: PlanView) => {
    const block = await readBlock(workspace, view.blockId),
      owner = workspace.pages.find((p) => p.id === view.pageId);
    if (!block || !owner) return;
    await batch(
      workspace,
      [
        {
          kind: "block.update",
          blockId: block.id,
          type: "code",
          schemaVersion: 1,
          data: {
            language: "trove-review",
            source: JSON.stringify(
              { ...view.plan, enabled: !view.activated },
              null,
              2,
            ),
          },
        },
        {
          kind: "review.activate",
          pageId: owner.id,
          planBlockId: block.id,
          enabled: !view.activated,
        },
      ],
      [
        ...pageGuards(workspace, owner),
        {
          target: { entityType: "block", entityId: block.id },
          revision: block.revision,
        },
      ],
    );
    await refresh();
    await loadReviews();
  };
  const decide = async (review: Run, approve: boolean) => {
    const owner = workspace.pages.find((p) => p.id === review.pageId);
    if (!owner) return;
    const guards = pageGuards(workspace, owner);
    let command: S.Command;
    if (approve) {
      const [blockId] = await ids(workspace, 1);
      command = {
        kind: "review.accept",
        pageId: owner.id,
        runId: review.id,
        blockId,
        approved: true,
      };
      guards.push({
        target: { entityType: "block", entityId: blockId },
        revision: "0",
      });
      for (const loc of review.sources) {
        if (
          !guards.some(
            (g) =>
              g.target.entityType === "page" &&
              g.target.entityId === loc.pageId,
          )
        )
          guards.push({
            target: { entityType: "page", entityId: loc.pageId },
            revision: loc.sourceRevision,
          });
      }
    } else
      command = { kind: "review.dismiss", pageId: owner.id, runId: review.id };
    await batch(workspace, [command], guards);
    await refresh();
    await loadReviews();
  };
  const positions = new Map(
    graph?.nodes.map((n, i, all) => [
      n.id,
      {
        x: 320 + 235 * Math.cos((i * Math.PI * 2) / all.length),
        y: 190 + 140 * Math.sin((i * Math.PI * 2) / all.length),
      },
    ]) ?? [],
  );
  return (
    <section className="ai-panel" aria-label="P2 intelligence packages">
      <header>
        <h2>Local intelligence</h2>
        <p>Traceable connections, image text, and reviewed personal digests.</p>
      </header>
      <nav aria-label="Intelligence packages">
        {[
          ["graph", "Knowledge graph"],
          ["ocr", "Image text"],
          ["review", "Personal review"],
        ].map(([id, label]) => (
          <button key={id} aria-pressed={tab === id} onClick={() => setTab(id)}>
            {label}
          </button>
        ))}
      </nav>
      <label>
        Page{" "}
        <select value={focus} onChange={(e) => setFocus(e.target.value)}>
          {workspace.pages
            .filter((p) => !p.deletedAt)
            .map((p) => (
              <option key={p.id} value={p.id}>
                {p.title || "Untitled"}
              </option>
            ))}
        </select>
      </label>
      {tab === "graph" && (
        <>
          <label>
            <input
              type="checkbox"
              checked={semantic}
              onChange={(e) => setSemantic(e.target.checked)}
            />{" "}
            Include retrieval associations
          </label>
          <label>
            Related topic{" "}
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Defaults to the page title"
            />
          </label>
          <button disabled={busy || !focus} onClick={() => void run(loadGraph)}>
            Explore connections
          </button>
          {graph && (
            <>
              {graph.warning && <p role="status">{graph.warning}</p>}
              {graph.truncated && (
                <p>
                  Showing a bounded graph. Select a connected page to explore
                  its neighborhood.
                </p>
              )}
              <svg
                viewBox="0 0 640 380"
                role="img"
                aria-label="Page relationships"
                style={{ width: "100%", maxHeight: 430 }}
              >
                {graph.edges.map((edge, i) => {
                  const a = positions.get(edge.source),
                    b = positions.get(edge.target);
                  return a && b ? (
                    <line
                      key={i}
                      x1={a.x}
                      y1={a.y}
                      x2={b.x}
                      y2={b.y}
                      stroke={edge.kind === "explicit" ? "#64748b" : "#c084fc"}
                      strokeDasharray={
                        edge.kind === "explicit" ? undefined : "5 4"
                      }
                    />
                  ) : null;
                })}
                {graph.nodes.map((n) => {
                  const p = positions.get(n.id)!;
                  return (
                    <g key={n.id}>
                      <circle
                        cx={p.x}
                        cy={p.y}
                        r={n.id === focus ? 10 : 7}
                        fill={n.id === focus ? "#22c55e" : "#818cf8"}
                      />
                      <text
                        x={p.x}
                        y={p.y - 15}
                        textAnchor="middle"
                        fill="currentColor"
                        fontSize="11"
                      >
                        {n.title.slice(0, 26)}
                      </text>
                    </g>
                  );
                })}
              </svg>
              <p>
                Solid edges are explicit links. Dashed edges are retrieval
                associations and do not assert a factual relationship.
              </p>
              <ul>
                {graph.nodes.map((n) => (
                  <li key={n.id}>
                    <button onClick={() => onNavigate(n.id)}>
                      {n.title || "Untitled"}
                    </button>{" "}
                    <button
                      onClick={() => {
                        setFocus(n.id);
                        setGraph(null);
                      }}
                    >
                      Focus here
                    </button>
                  </li>
                ))}
              </ul>
              {graph.edges.map((edge, i) => (
                <details key={i}>
                  <summary>
                    {edge.kind} ·{" "}
                    {graph.nodes.find((n) => n.id === edge.source)?.title} →{" "}
                    {graph.nodes.find((n) => n.id === edge.target)?.title}
                  </summary>
                  <p>{edge.evidence}</p>
                  <p>
                    Source revision {edge.location.sourceRevision}
                    {edge.location.blockId
                      ? ` · block ${edge.location.blockId}`
                      : ""}
                  </p>
                  <button onClick={() => onNavigate(edge.location.pageId)}>
                    Open evidence
                  </button>
                </details>
              ))}
              {graph.edges.length === 0 && (
                <p>
                  No connections in this neighborhood. Add a page link or
                  include retrieval associations.
                </p>
              )}
            </>
          )}
        </>
      )}
      {tab === "ocr" && (
        <>
          <p>
            {ocrStatus?.available
              ? "Local Apple Vision helper ready."
              : "Local OCR helper is unavailable in this bundle."}{" "}
            PNG/JPEG only, up to 20 MiB and 20 megapixels; one operation at a
            time, 60-second deadline.
          </p>
          <label>
            Attached image{" "}
            <select
              value={imageId}
              onChange={(e) => setImageId(e.target.value)}
            >
              <option value="">Choose image</option>
              {imageFiles.map((f) => (
                <option key={f.id} value={f.id}>
                  {f.originalName}
                </option>
              ))}
            </select>
          </label>
          <button
            disabled={busy || !ocrStatus?.available || !imageId}
            onClick={() =>
              void run(async () =>
                setOCR(
                  await dispatch<OCR>("ocr.run", {
                    ...envelope(workspace),
                    fileId: imageId,
                  }),
                ),
              )
            }
          >
            Extract image text locally
          </button>
          <p>
            Text below 0.5 confidence is omitted. Verify handwriting, columns
            and low contrast text against the image. Image description models
            are a separate compatible-pack capability.
          </p>
          {ocr && (
            <>
              <p role="status">
                {ocr.warning} Accepted {ocr.acceptedLines} lines; omitted{" "}
                {ocr.rejectedLines}.
              </p>
              <pre style={{ whiteSpace: "pre-wrap" }}>{ocr.text}</pre>
              <p>
                Extraction is revision bound and searchable with the attached
                image. The original image remains the source.
              </p>
            </>
          )}
        </>
      )}
      {tab === "review" && (
        <>
          <p>
            {reviews?.engine}. Scheduled runs prepare drafts; accepting a draft
            appends the displayed digest to its page.
          </p>
          <label>
            Name{" "}
            <input
              value={reviewName}
              onChange={(e) => setReviewName(e.target.value)}
              maxLength={200}
            />
          </label>
          <label>
            Source page{" "}
            <select value={source} onChange={(e) => setSource(e.target.value)}>
              {workspace.pages
                .filter((p) => !p.deletedAt)
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.title || "Untitled"}
                  </option>
                ))}
            </select>
          </label>
          <label>
            Evidence query{" "}
            <input
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Blank uses current source excerpts"
            />
          </label>
          <label>
            Recurrence{" "}
            <select
              value={frequency}
              onChange={(e) => setFrequency(e.target.value)}
            >
              <option value="daily">Daily</option>
              <option value="weekly">Every Monday</option>
            </select>
          </label>
          <label>
            Local time{" "}
            <input
              type="time"
              value={time}
              onChange={(e) => setTime(e.target.value)}
            />
          </label>
          <label>
            Time zone{" "}
            <input value={zone} onChange={(e) => setZone(e.target.value)} />
          </label>
          <label>
            After downtime{" "}
            <select
              value={missedPolicy}
              onChange={(e) => setMissedPolicy(e.target.value)}
            >
              <option value="skip">Skip missed runs</option>
              <option value="catch_up">Prepare one missed review</option>
            </select>
          </label>
          <button
            disabled={
              busy || !page || !source || !reviewName.trim() || !time || !zone
            }
            onClick={() => void run(createPlan)}
          >
            Save review plan on this page
          </button>
          <p>
            {reviews?.scheduling}. Saving a changed plan starts a new schedule.
            Daylight-saving gaps are skipped; repeated wall times run once.
          </p>
          {reviews?.plans.map((p) => (
            <article key={p.blockId}>
              <h3>{p.plan?.name || "Invalid review plan"}</h3>
              {p.error ? (
                <p role="alert">{p.error}</p>
              ) : (
                <>
                  <p>
                    {p.activated
                      ? "Enabled locally"
                      : "Paused / activation required"}{" "}
                    · next{" "}
                    {p.nextAt
                      ? new Date(p.nextAt).toLocaleString()
                      : "on the next scheduler tick"}
                  </p>
                  <button
                    disabled={busy}
                    onClick={() =>
                      void run(async () => {
                        await dispatch("review.run", {
                          ...envelope(workspace),
                          planBlockId: p.blockId,
                        });
                        await loadReviews();
                      })
                    }
                  >
                    Prepare review now
                  </button>
                  <button
                    disabled={busy}
                    onClick={() => void run(() => togglePlan(p))}
                  >
                    {p.activated ? "Pause" : "Activate locally"}
                  </button>
                </>
              )}
            </article>
          ))}
          <button disabled={busy} onClick={() => void run(loadReviews)}>
            Refresh review drafts
          </button>
          {reviews?.runs.map((r) => (
            <article key={r.id}>
              <h3>
                {r.status} · {new Date(r.createdAt).toLocaleString()}
              </h3>
              <pre style={{ whiteSpace: "pre-wrap" }}>{r.text}</pre>
              <ul>
                {r.sources.map((s, i) => (
                  <li key={i}>
                    <button onClick={() => onNavigate(s.pageId)}>
                      Source · revision {s.sourceRevision}
                    </button>
                  </li>
                ))}
              </ul>
              {r.status === "pending" && (
                <>
                  <button
                    disabled={busy}
                    onClick={() => void run(() => decide(r, true))}
                  >
                    Approve and append this digest
                  </button>
                  <button
                    disabled={busy}
                    onClick={() => void run(() => decide(r, false))}
                  >
                    Dismiss
                  </button>
                </>
              )}
            </article>
          ))}
        </>
      )}
    </section>
  );
}
