import { useEffect, useRef, useState } from "react";
import { useRequestLifetime } from "../requestLifetime";
import { ImageUnderstanding } from "./ImageUnderstanding";
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

import {
  GeneratedReviewCard,
  ReviewGenerationOptions,
  newReviewGeneration,
  type ReviewGeneration,
  type GeneratedReviewRun,
} from "./GenerativeReview";

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
  generative?: ReviewGeneration;
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
  generative?: ReviewGeneration;
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
  languages?: string[];
  limitations?: string;
  maxBytes: number;
  maxPixels: number;
  timeoutSeconds: number;
  minimumConfidence: number;
};
type OCR = {
  persisted?: boolean;
  indexingState?: "ready" | "pending";
  engine: string;
  extractorVersion: string;
  modelSha256?: string;
  languages?: string[];
  sourceRevision: string;
  fileId: string;
  text: string;
  warning: string;
  acceptedLines: number;
  rejectedLines: number;
};
export function P2IntelligencePanel(
  props: Parameters<typeof P2IntelligenceContent>[0],
) {
  return (
    <P2IntelligenceContent
      key={`${props.workspace.workspaceId}:${props.workspace.scopeGrantId}`}
      {...props}
    />
  );
}
function P2IntelligenceContent({
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
  const [graphResult, setGraphResult] = useState<{
      key: string;
      value: Graph;
    } | null>(null),
    [semantic, setSemantic] = useState(false),
    [query, setQuery] = useState("");
  const [reviewResult, setReviewResult] = useState<{
      key: string;
      value: Reviews;
    } | null>(null),
    [reviewName, setReviewName] = useState("Personal review"),
    [time, setTime] = useState("09:00"),
    [frequency, setFrequency] = useState("daily"),
    [missedPolicy, setMissedPolicy] = useState("skip");
  const [generative, setGenerative] = useState(false),
    [generation, setGeneration] = useState(newReviewGeneration),
    [consent, setConsent] = useState(false),
    [includeMemory, setIncludeMemory] = useState(false),
    [weekday, setWeekday] = useState(1),
    [otherSources, setOtherSources] = useState<string[]>([]);
  const [zone, setZone] = useState(
      Intl.DateTimeFormat().resolvedOptions().timeZone,
    ),
    [source, setSource] = useState(focus);
  const [ocrStatus, setOCRStatus] = useState<OCRStatus | null>(null),
    [ocr, setOCR] = useState<OCR | null>(null),
    [imageId, setImageId] = useState("");
  const scopeKey = `${workspace.workspaceId}:${workspace.scopeGrantId}:${workspace.snapshotCursor}`;
  const currentScope = useRequestLifetime(scopeKey);
  const graphKey = JSON.stringify([scopeKey, focus, semantic, query]);
  const currentGraph = useRequestLifetime(graphKey);
  const graph = graphResult?.key === graphKey ? graphResult.value : null;
  const reviews = reviewResult?.key === scopeKey ? reviewResult.value : null;
  const graphRequest = useRef(0),
    reviewRequest = useRef(0),
    actionRequest = useRef(0);
  const [ocrPending, setOCRPending] = useState("");
  const [ocrNotice, setOCRNotice] = useState("");
  const [ocrCancelling, setOCRCancelling] = useState(false);
  const ocrFlight = useRef<
    { id: string; workspace: WorkspaceSnapshot } | undefined
  >(undefined);
  const ocrRequest = useRef(0);
  useEffect(() => {
    actionRequest.current++;
    setBusy(false);
  }, [scopeKey]);
  useEffect(
    () => () => {
      const flight = ocrFlight.current;
      if (flight)
        void dispatch("ocr.cancel", {
          ...envelope(flight.workspace),
          runRequestId: flight.id,
        }).catch(() => {});
    },
    [],
  );
  useEffect(() => {
    ocrRequest.current += 1;
    setOCR(null);
    setOCRNotice("");
    return () => {
      ocrRequest.current += 1;
    };
  }, [
    workspace.workspaceId,
    workspace.scopeGrantId,
    workspace.snapshotCursor,
    imageId,
  ]);
  const extractOCR = async () => {
    if (ocrFlight.current) return;
    const request = ++ocrRequest.current;
    const input = { ...envelope(workspace), fileId: imageId };
    ocrFlight.current = { id: input.requestId, workspace };
    setOCRPending(input.requestId);
    setOCRNotice("Extracting image text locally…");
    setOCRCancelling(false);
    try {
      const result = await dispatch<OCR>("ocr.run", input);
      if (currentScope() && request === ocrRequest.current) {
        setOCR(result);
        setOCRNotice(
          result.indexingState === "pending"
            ? "Image text saved. Search indexing is pending."
            : "Image text extracted and saved.",
        );
      }
    } catch (error) {
      if (currentScope() && request === ocrRequest.current) {
        if (error instanceof Error && error.name === "CANCELLED")
          setOCRNotice(
            "Extraction cancelled. Earlier extracted text is preserved.",
          );
        else {
          setOCRNotice(
            "Extraction failed. Earlier extracted text is preserved.",
          );
          onError(error);
        }
      }
    } finally {
      if (ocrFlight.current?.id === input.requestId) {
        ocrFlight.current = undefined;
        setOCRPending("");
        setOCRCancelling(false);
      }
    }
  };
  const cancelOCR = async () => {
    const flight = ocrFlight.current;
    if (!flight) return;
    setOCRCancelling(true);
    try {
      await dispatch("ocr.cancel", {
        ...envelope(flight.workspace),
        runRequestId: flight.id,
      });
    } catch (error) {
      if (currentScope()) {
        setOCRCancelling(false);
        onError(error);
      }
    }
  };
  const page = workspace.pages.find((p) => p.id === focus);
  const imageFiles = workspace.files.filter((f) =>
    ["image/png", "image/jpeg"].includes(f.mediaType),
  );
  const run = async (fn: () => Promise<void>) => {
    const request = ++actionRequest.current;
    setBusy(true);
    try {
      await fn();
    } catch (error) {
      if (currentScope() && request === actionRequest.current) onError(error);
    } finally {
      if (currentScope() && request === actionRequest.current) setBusy(false);
    }
  };
  const loadReviews = async () => {
    if (!currentScope()) return;
    const request = ++reviewRequest.current;
    try {
      const value = await dispatch<Reviews>("review.list", envelope(workspace));
      if (currentScope() && request === reviewRequest.current)
        setReviewResult({ key: scopeKey, value });
    } catch (error) {
      if (currentScope() && request === reviewRequest.current) throw error;
    }
  };
  useEffect(() => {
    void loadReviews().catch(onError);
    void dispatch<OCRStatus>("ocr.status", envelope(workspace))
      .then((status) => {
        if (currentScope()) setOCRStatus(status);
      })
      .catch((error) => {
        if (currentScope()) onError(error);
      });
  }, [scopeKey]);
  useEffect(() => {
    if (pageId) {
      setFocus(pageId);
      setSource(pageId);
    }
  }, [pageId]);
  const loadGraph = async () => {
    const request = ++graphRequest.current;
    try {
      const value = await dispatch<Graph>("graph.query", {
        ...envelope(workspace),
        pageId: focus,
        includeSemantic: semantic,
        query,
        limit: 60,
      });
      if (currentGraph() && request === graphRequest.current)
        setGraphResult({ key: graphKey, value });
    } catch (error) {
      if (currentGraph() && request === graphRequest.current) throw error;
    }
  };
  useEffect(() => {
    if (!reviews?.runs.some((r) => ["queued", "generating"].includes(r.status)))
      return;
    const timer = setInterval(() => {
      void loadReviews().catch(onError);
    }, 2000);
    return () => clearInterval(timer);
  }, [reviews?.runs, scopeKey]);
  const createPlan = async () => {
    if (
      !page ||
      !source ||
      (generative &&
        (!consent || !generation.modelPackId || !generation.categories.length))
    )
      return;
    const [blockId] = await ids(workspace, 1);
    const [hour, minute] = time.split(":").map(Number);
    const plan: Plan = {
      type: "trove.personal-review/v1",
      name: reviewName,
      enabled: true,
      sourcePageIds: [...new Set([source, ...otherSources])].slice(0, 32),
      query,
      includeMemory,
      ...(generative ? { generative: generation } : {}),
      schedule: {
        type: "schedule",
        timezone: zone,
        frequency,
        hour,
        minute,
        weekday,
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
          ["vision", "Image questions"],
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
                        setGraphResult(null);
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
      {tab === "vision" && (
        <ImageUnderstanding
          workspace={workspace}
          pageId={focus}
          onNavigate={onNavigate}
          onError={onError}
        />
      )}
      {tab === "ocr" && (
        <>
          <p>
            {ocrStatus?.available
              ? `${ocrStatus.engine} ready. ${ocrStatus.languages?.join(", ") || "Language support unavailable"}.`
              : "Local OCR helper is unavailable in this bundle."}{" "}
            PNG/JPEG only, up to 20 MiB and 20 megapixels; one operation at a
            time, 60-second deadline.
          </p>
          {ocrStatus?.limitations && <p>{ocrStatus.limitations}</p>}
          <label>
            Attached image{" "}
            <select
              value={imageId}
              disabled={Boolean(ocrPending)}
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
            disabled={
              busy || Boolean(ocrPending) || !ocrStatus?.available || !imageId
            }
            onClick={() => void extractOCR()}
          >
            Extract image text locally
          </button>
          {ocrPending && (
            <button disabled={ocrCancelling} onClick={() => void cancelOCR()}>
              {ocrCancelling
                ? "Cancelling extraction…"
                : "Cancel image text extraction"}
            </button>
          )}
          {ocrNotice && <p role="status">{ocrNotice}</p>}
          <p>
            Text below 0.5 confidence is omitted. Verify handwriting, columns
            and low contrast text against the image. Image description models
            are a separate compatible-pack capability.
          </p>
          {ocr && (
            <>
              <p role="status">
                {ocr.engine} · {ocr.languages?.join(", ")}. {ocr.warning}{" "}
                Accepted {ocr.acceptedLines} lines; omitted {ocr.rejectedLines}.
              </p>
              <pre style={{ whiteSpace: "pre-wrap" }}>{ocr.text}</pre>
              <p>
                Source revision: {ocr.sourceRevision}. Extractor:{" "}
                {ocr.extractorVersion}.
              </p>
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
            <select
              value={source}
              onChange={(e) => {
                setSource(e.target.value);
                setConsent(false);
              }}
            >
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
            Additional source pages
            <select
              multiple
              value={otherSources}
              onChange={(e) => {
                setConsent(false);
                setOtherSources(
                  Array.from(e.target.selectedOptions, (o) => o.value).slice(
                    0,
                    31,
                  ),
                );
              }}
            >
              {workspace.pages
                .filter((p) => !p.deletedAt && p.id !== source)
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.title || "Untitled"}
                  </option>
                ))}
            </select>
          </label>
          <label>
            <input
              type="checkbox"
              checked={includeMemory}
              onChange={(e) => {
                setConsent(false);
                setIncludeMemory(e.target.checked);
              }}
            />
            Include approved relevant memories
          </label>
          <label>
            <input
              type="checkbox"
              checked={generative}
              onChange={(e) => {
                setGenerative(e.target.checked);
                setConsent(false);
              }}
            />
            Enable local model analysis for this plan
          </label>
          {generative && (
            <ReviewGenerationOptions
              workspace={workspace}
              value={generation}
              onChange={setGeneration}
              consent={consent}
              onConsent={setConsent}
              onError={onError}
            />
          )}
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
              <option value="weekly">Weekly</option>
            </select>
          </label>
          {frequency === "weekly" && (
            <label>
              Review weekday
              <select
                value={weekday}
                onChange={(e) => setWeekday(Number(e.target.value))}
              >
                {[
                  "Sunday",
                  "Monday",
                  "Tuesday",
                  "Wednesday",
                  "Thursday",
                  "Friday",
                  "Saturday",
                ].map((day, i) => (
                  <option key={day} value={i}>
                    {day}
                  </option>
                ))}
              </select>
            </label>
          )}
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
              busy ||
              !page ||
              !source ||
              !reviewName.trim() ||
              !time ||
              !zone ||
              (generative &&
                (!consent ||
                  !generation.modelPackId ||
                  !generation.categories.length))
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
                  {p.plan.generative && (
                    <p>
                      Local model: {p.plan.generative.modelPackId}. Categories:{" "}
                      {p.plan.generative.categories.join(", ")}. Enabled
                      proposed changes:{" "}
                      {p.plan.generative.allowedActions.join(", ") || "none"}.
                      Destinations:{" "}
                      {p.plan.generative.actionPageIds
                        .map(
                          (id) =>
                            workspace.pages.find((page) => page.id === id)
                              ?.title || "Unavailable page",
                        )
                        .join(", ") || "none"}
                      .{" "}
                      {p.plan.includeMemory
                        ? "Approved relevant memories included."
                        : "Memories excluded."}{" "}
                      Activation permits scheduled read-only analysis; every
                      write needs a separate approval.
                    </p>
                  )}
                  <button
                    disabled={
                      busy || Boolean(p.plan.generative && !p.activated)
                    }
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
                    {p.activated
                      ? "Pause"
                      : p.plan.generative
                        ? "Allow and activate local model review"
                        : "Activate locally"}
                  </button>
                </>
              )}
            </article>
          ))}
          <button disabled={busy} onClick={() => void run(loadReviews)}>
            Refresh review drafts
          </button>
          {reviews?.runs.map((r) =>
            r.generative ? (
              <GeneratedReviewCard
                key={r.id}
                workspace={workspace}
                review={r as GeneratedReviewRun}
                onReload={loadReviews}
                onRefresh={refresh}
                onNavigate={onNavigate}
                onDismiss={() => decide(r, false)}
                onError={(error) => {
                  if (currentScope()) onError(error);
                }}
              />
            ) : (
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
            ),
          )}
        </>
      )}
    </section>
  );
}
