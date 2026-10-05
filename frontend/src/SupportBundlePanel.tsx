import { useEffect, useRef, useState } from "react";

type Preview = {
  previewId: string;
  workspaceId: string;
  expiresAt: string;
  files: { name: string; content: string }[];
};
type Bridge = {
  PreviewSupportBundle(raw: string): Promise<string>;
  ExportSupportBundle(id: string): Promise<string>;
  CancelSupportBundle(): Promise<void>;
};
function bridge(): Bridge {
  const value = window.go?.main.App as unknown as Partial<Bridge>;
  if (!value?.PreviewSupportBundle)
    throw new Error("Support export is available in the desktop application.");
  return value as Bridge;
}
export default function SupportBundlePanel({
  workspaceId,
  pages,
  onError,
}: {
  workspaceId: string;
  pages: {
    id: string;
    title: string;
    deletedAt?: string | null;
    archivedAt?: string | null;
  }[];
  onError(error: unknown): void;
}) {
  const [titles, setTitles] = useState(false),
    [text, setText] = useState(false),
    [selected, setSelected] = useState<string[]>([]);
  const [preview, setPreview] = useState<Preview | null>(null),
    [busy, setBusy] = useState(false),
    [result, setResult] = useState("");
  const generation = useRef(0);
  useEffect(() => {
    generation.current++;
    setBusy(false);
    setPreview(null);
    setSelected([]);
    setTitles(false);
    setText(false);
    setResult("");
    return () => {
      generation.current++;
      try {
        void bridge()
          .CancelSupportBundle()
          .catch(() => {});
      } catch {
        /* Browser without native bridge. */
      }
    };
  }, [workspaceId]);
  const invalidate = () => {
    generation.current++;
    setBusy(false);
    setPreview(null);
    setResult("");
    try {
      void bridge().CancelSupportBundle().catch(onError);
    } catch (error) {
      onError(error);
    }
  };
  const run = async (fn: (current: () => boolean) => Promise<void>) => {
    const operation = ++generation.current;
    const current = () => generation.current === operation;
    setBusy(true);
    setResult("");
    try {
      await fn(current);
    } catch (error) {
      if (current()) {
        setPreview(null);
        onError(error);
      }
    } finally {
      if (current()) setBusy(false);
    }
  };
  const contentSelected = titles || text;
  return (
    <section className="settings-section" aria-label="Local support bundle">
      <h2>Local support bundle</h2>
      <p>
        By default, export only OS and app availability, schema and index
        counts, helper capabilities, and job totals with allowlisted error
        codes. No names, page content, attachments, credentials or raw logs are
        collected. Nothing is uploaded.
      </p>
      <fieldset disabled={busy}>
        <legend>Optional content — off by default</legend>
        <label>
          <input
            type="checkbox"
            checked={titles}
            onChange={(e) => {
              invalidate();
              setTitles(e.target.checked);
            }}
          />{" "}
          Include selected page titles
        </label>
        <label>
          <input
            type="checkbox"
            checked={text}
            onChange={(e) => {
              invalidate();
              setText(e.target.checked);
            }}
          />{" "}
          Include selected page text
        </label>
        {contentSelected && (
          <>
            <p>
              This content may contain secrets. Review every byte below before
              saving; Trove does not automatically redact the content you
              choose. Only text in supported paragraph, heading, list, quote,
              callout, toggle and code blocks is included. Other blocks are
              counted as omitted; attachments, transcript-linked blocks, properties and
              linked content are excluded. Limit: five pages and 256 KiB.
            </p>
            <label>
              Pages to include
              <select
                aria-label="Pages to include"
                multiple
                value={selected}
                onChange={(e) => {
                  invalidate();
                  setSelected(
                    Array.from(
                      e.target.selectedOptions,
                      (option) => option.value,
                    ),
                  );
                }}
              >
                {pages
                  .filter((p) => !p.deletedAt && !p.archivedAt)
                  .map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.title || "Untitled"}
                    </option>
                  ))}
              </select>
            </label>
            {selected.length > 5 && (
              <p role="alert">Choose at most five pages.</p>
            )}
          </>
        )}
      </fieldset>
      <button
        disabled={
          busy ||
          (contentSelected && (selected.length === 0 || selected.length > 5))
        }
        onClick={() =>
          void run(async (current) => {
            setPreview(null);
            const raw = await bridge().PreviewSupportBundle(
              JSON.stringify({
                workspaceId,
                selection: {
                  titles,
                  text,
                  pageIds: contentSelected ? selected : [],
                },
              }),
            );
            if (current() && raw) setPreview(JSON.parse(raw));
          })
        }
      >
        Preview exact bundle contents
      </button>
      {busy && <button onClick={invalidate}>Cancel support operation</button>}
      {preview && (
        <article aria-label="Support bundle preview">
          <h3>Review exact files</h3>
          <p>
            Valid until {new Date(preview.expiresAt).toLocaleTimeString()}.
            Changed pages require a new preview. Choose a new file name;
            existing files are never overwritten.
          </p>
          {preview.files.map((file) => (
            <details open key={file.name}>
              <summary>{file.name}</summary>
              <pre
                style={{
                  whiteSpace: "pre-wrap",
                  overflowWrap: "anywhere",
                  maxHeight: 320,
                  overflow: "auto",
                }}
              >
                {file.content}
              </pre>
            </details>
          ))}
          <button
            disabled={busy || preview.workspaceId !== workspaceId}
            onClick={() =>
              void run(async (current) => {
                const id = preview.previewId;
                setPreview(null);
                const raw = await bridge().ExportSupportBundle(id);
                if (current() && raw) setResult(JSON.parse(raw).message);
              })
            }
          >
            Save reviewed bundle…
          </button>
          <button disabled={busy} onClick={invalidate}>
            Discard support preview
          </button>
        </article>
      )}
      {result && <p role="status">{result}</p>}
    </section>
  );
}
