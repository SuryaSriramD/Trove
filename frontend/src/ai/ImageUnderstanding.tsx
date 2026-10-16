import { useEffect, useRef, useState } from "react";
import { useRequestLifetime } from "../requestLifetime";
import { dispatch, envelope, fileURL, type WorkspaceSnapshot } from "../api";
type Run = {
  id: string;
  pageId: string;
  fileId: string;
  modelPackId: string;
  state: string;
  text: string;
  question: string;
  pageRevision: string;
  fileRevision: string;
  contentHash: string;
  modelHash?: string;
  projectorHash?: string;
  helperHash?: string;
  preprocessingProfileId?: string;
  preprocessingProfileHash?: string;
  inputTokens?: number;
  outputTokens?: number;
  analysisMethod?: string;
  warning: string;
  error?: string;
  stale: boolean;
};
type State = {
  runs: Run[];
  truncated: boolean;
  images: { id: string; originalName: string }[];
  imagesTruncated: boolean;
};
export function ImageUnderstanding(
  props: Parameters<typeof ImageUnderstandingContent>[0],
) {
  return (
    <ImageUnderstandingContent
      key={`${props.workspace.workspaceId}:${props.workspace.scopeGrantId}:${props.pageId}`}
      {...props}
    />
  );
}
function ImageUnderstandingContent({
  workspace,
  pageId,
  onNavigate,
  onError,
}: {
  workspace: WorkspaceSnapshot;
  pageId: string;
  onNavigate: (id: string) => void;
  onError: (error: unknown) => void;
}) {
  const [storedData, setStoredData] = useState<{ key: string; value: State }>();
  const empty: State = {
    runs: [],
    truncated: false,
    images: [],
    imagesTruncated: false,
  };
  const [packs, setPacks] = useState<{ id: string; name: string }[]>([]),
    [available, setAvailable] = useState(false),
    [pack, setPack] = useState(""),
    [image, setImage] = useState(""),
    [question, setQuestion] = useState("Describe the visible content briefly."),
    [busy, setBusy] = useState(false);
  const scopeKey = `${workspace.workspaceId}:${workspace.scopeGrantId}:${pageId}:${workspace.snapshotCursor}`;
  const current = useRequestLifetime(scopeKey);
  const data = storedData?.key === scopeKey ? storedData.value : empty;
  const reads = useRef(0);
  const load = async () => {
    if (!current()) return;
    const request = ++reads.current;
    try {
      const value = await dispatch<State>("image.list", {
        ...envelope(workspace),
        pageId,
      });
      if (current() && request === reads.current)
        setStoredData({ key: scopeKey, value });
    } catch (error) {
      if (current() && request === reads.current) throw error;
    }
  };
  useEffect(() => {
    setImage("");
    setBusy(false);
    if (!pageId) return;
    void load().catch(onError);
    void dispatch<any>("models.list")
      .then((m) => {
        if (!current()) return;
        setAvailable(Boolean(m.runtime?.visionAvailable));
        const ready = m.packs
          .filter((p: any) => p.state === "ready" && p.pack.kind === "vision")
          .map((p: any) => p.pack);
        setPacks(ready);
        setPack((previous) =>
          ready.some((p: any) => p.id === previous)
            ? previous
            : (ready[0]?.id ?? ""),
        );
      })
      .catch((error) => {
        if (current()) onError(error);
      });
  }, [scopeKey]);
  useEffect(() => {
    if (!data.runs.some((r) => r.state === "running")) return;
    const timer = setInterval(() => {
      void load().catch(onError);
    }, 1500);
    return () => clearInterval(timer);
  }, [data.runs, scopeKey]);
  const action = async (method: string, input: object) => {
    setBusy(true);
    try {
      await dispatch(method, { ...envelope(workspace), ...input });
      if (current()) await load();
    } catch (error) {
      if (current()) onError(error);
    } finally {
      if (current()) setBusy(false);
    }
  };
  return (
    <section aria-label="Local image understanding">
      <p>
        Experimental English image questions. PNG/JPEG, up to 20 MiB and 20
        megapixels, normalized to a 1024-pixel longest edge. Original bytes are
        retained. One local model at a time, 512 answer tokens and a five-minute
        deadline.
      </p>
      <p>
        {available
          ? "Local image helper ready."
          : "Compatible image helper unavailable in this bundle."}{" "}
        Install the two-file vision pack in Settings → Local models. Downloads
        require your explicit action; questions run offline.
      </p>
      <label>
        Image attached to this page
        <select value={image} onChange={(e) => setImage(e.target.value)}>
          <option value="">Choose image</option>
          {data.images.map((f) => (
            <option key={f.id} value={f.id}>
              {f.originalName}
            </option>
          ))}
        </select>
      </label>
      {data.imagesTruncated && (
        <p>
          Showing the first 100 attached images. Use a page with fewer images to
          select additional files.
        </p>
      )}
      {image && (
        <figure>
          <img
            alt="Original image for the question"
            src={fileURL(image)}
            style={{ maxWidth: "100%", maxHeight: 320, objectFit: "contain" }}
          />
          <figcaption>
            Original source preview. Analysis uses encoded pixel orientation;
            check rotated camera images.
          </figcaption>
        </figure>
      )}
      <label>
        Local image model
        <select value={pack} onChange={(e) => setPack(e.target.value)}>
          <option value="">Choose installed vision pack</option>
          {packs.map((p) => (
            <option key={p.id} value={p.id}>
              {p.name}
            </option>
          ))}
        </select>
      </label>
      <label>
        Image question
        <textarea
          value={question}
          maxLength={2048}
          onChange={(e) => setQuestion(e.target.value)}
        />
      </label>
      <button
        disabled={
          busy ||
          !available ||
          !pack ||
          !image ||
          !question.trim() ||
          data.runs.some((r) => r.state === "running")
        }
        onClick={() =>
          void action("image.start", {
            pageId,
            fileId: image,
            modelPackId: pack,
            question,
          })
        }
      >
        Ask about image locally
      </button>
      <p>
        Descriptions can hallucinate objects, text or numbers. Verify the
        original before using them. Drafts stay separate from OCR, search,
        memory and page content.
      </p>
      <button disabled={busy} onClick={() => void load().catch(onError)}>
        Refresh image drafts
      </button>
      {data.truncated && (
        <p>Showing the 20 most recent image drafts for this page.</p>
      )}
      {data.runs.map((r) => (
        <article key={r.id}>
          <h3>
            {r.state}
            {r.stale ? " · source changed" : ""}
          </h3>
          <p>{r.question}</p>
          {r.state === "running" && (
            <button
              disabled={busy}
              onClick={() => void action("image.cancel", { runId: r.id })}
            >
              Cancel image analysis
            </button>
          )}
          {r.error && <p role="alert">{r.error}</p>}
          <p role="status">{r.warning}</p>
          {r.text && <pre style={{ whiteSpace: "pre-wrap" }}>{r.text}</pre>}
          <p>
            {r.analysisMethod === "pixel_measurement"
              ? "Local pixel measurement; model not run."
              : r.analysisMethod === "local_vision_model"
                ? `Local model draft · ${r.inputTokens} input tokens · ${r.outputTokens} output tokens`
                : null}
          </p>
          <button onClick={() => onNavigate(r.pageId)}>
            Open image source page
          </button>
          <a href={fileURL(r.fileId)} target="_blank" rel="noopener noreferrer">
            Open original image
          </a>
          <details>
            <summary>Source and model evidence</summary>
            <p>
              Page revision {r.pageRevision} · file revision {r.fileRevision}
            </p>
            <p>Original SHA-256: {r.contentHash}</p>
            {r.modelHash && <p>Model SHA-256: {r.modelHash}</p>}
            {r.projectorHash && <p>Projector SHA-256: {r.projectorHash}</p>}
            {r.helperHash && <p>Helper SHA-256: {r.helperHash}</p>}
            {r.preprocessingProfileId && <p>Preprocessing profile: {r.preprocessingProfileId}</p>}
            {r.preprocessingProfileHash && <p>Preprocessing SHA-256: {r.preprocessingProfileHash}</p>}
          </details>
        </article>
      ))}
    </section>
  );
}
