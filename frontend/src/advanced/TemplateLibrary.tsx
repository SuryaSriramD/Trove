import { useEffect, useState } from "react";
import type { D, S, WorkspaceSnapshot } from "../api";
import {
  applyTemplate,
  previewTemplate,
  saveTemplate,
  templates,
  type Template,
} from "./service";

/** Both database and page controls use the same kernel capture/preview/apply path. */
export function TemplateLibrary({
  workspace: w,
  sourcePages,
  collectionId,
  disabled = false,
  flush,
  onCommitted,
  onError,
  openPage,
}: {
  workspace: WorkspaceSnapshot;
  sourcePages: D.Page[];
  collectionId?: string;
  disabled?: boolean;
  flush?: () => Promise<boolean>;
  onCommitted: () => Promise<void>;
  onError: (error: unknown) => void;
  openPage?: (id: string) => void;
}) {
  const [library, setLibrary] = useState<Template[]>([]),
    [source, setSource] = useState(""),
    [selected, setSelected] = useState(""),
    [title, setTitle] = useState(""),
    [destination, setDestination] = useState(collectionId ?? ""),
    [busy, setBusy] = useState(false),
    [preview, setPreview] = useState<S.TemplatePreviewResult>(),
    [decisions, setDecisions] = useState<
      Record<string, { target: string; value: string }>
    >({}),
    [message, setMessage] = useState("");
  const refresh = () =>
    templates(w).then((result) => setLibrary(result.templates));
  useEffect(() => {
    let live = true;
    void templates(w)
      .then((result) => {
        if (live) setLibrary(result.templates);
      })
      .catch(onError);
    return () => {
      live = false;
    };
  }, [w.workspaceId, w.snapshotCursor]);
  useEffect(() => {
    setDestination(collectionId ?? "");
    setPreview(undefined);
    setDecisions({});
  }, [collectionId]);
  const run = async (action: () => Promise<void>) => {
    setBusy(true);
    setMessage("");
    try {
      await action();
    } catch (error) {
      setPreview(undefined);
      onError(error);
    } finally {
      setBusy(false);
    }
  };
  const reset = () => {
    setPreview(undefined);
    setDecisions({});
    setMessage("");
  };
  const capture = () =>
    run(async () => {
      if (flush && !(await flush())) return;
      const page = sourcePages.find(
        (p) => p.id === (source || sourcePages[0]?.id),
      );
      if (!page) throw new Error("Choose a source page.");
      const name = prompt("Template name", page.title);
      if (!name) return;
      await saveTemplate(w, page, name);
      await refresh();
      setMessage(
        "Saved property defaults and page body as an immutable template.",
      );
    });
  const review = () =>
    run(async () => {
      if (flush && !(await flush())) return;
      const mappings: S.TemplatePropertyDecision[] = Object.entries(
        decisions,
      ).flatMap<S.TemplatePropertyDecision>(([sourcePropertyId, d]) =>
        d.target === "omit"
          ? [{ sourcePropertyId, action: "omit" as const }]
          : d.target
            ? [
                {
                  sourcePropertyId,
                  action: "map" as const,
                  targetPropertyId: d.target,
                  value: JSON.parse(d.value),
                },
              ]
            : [],
      );
      const result = await previewTemplate(
        w,
        selected,
        title.trim(),
        destination || undefined,
        mappings,
      );
      setPreview(result);
      setDecisions((previous) => ({
        ...Object.fromEntries(
          result.propertyIssues.map((issue) => [
            issue.sourceProperty.id,
            { target: "", value: JSON.stringify(issue.value, null, 2) },
          ]),
        ),
        ...previous,
      }));
    });
  return (
    <fieldset disabled={disabled || busy} className="template-library">
      <legend>Templates: property defaults and page body</legend>
      <label>
        Template source{" "}
        <select
          aria-label="Template source"
          value={source || sourcePages[0]?.id || ""}
          onChange={(event) => setSource(event.target.value)}
        >
          {!sourcePages.length && <option value="">No source records</option>}
          {sourcePages.map((page) => (
            <option key={page.id} value={page.id}>
              {page.title}
            </option>
          ))}
        </select>
      </label>
      <button disabled={!sourcePages.length} onClick={() => void capture()}>
        Save combined template
      </button>
      <label>
        Saved template{" "}
        <select
          aria-label="Saved template"
          value={selected}
          onChange={(event) => {
            const id = event.target.value;
            setSelected(id);
            const template = library.find((t) => t.id === id);
            setTitle(template ? `${template.name} copy` : "");
            if (!collectionId) {
              setDestination(
                w.records.find((r) => r.pageId === template?.sourcePageId)
                  ?.collectionId ?? "",
              );
            }
            reset();
          }}
        >
          <option value="">Choose a template</option>
          {library.map((template) => (
            <option key={template.id} value={template.id}>
              {template.name}
            </option>
          ))}
        </select>
      </label>
      {!collectionId && (
        <label>
          Template destination{" "}
          <select
            value={destination}
            onChange={(event) => {
              setDestination(event.target.value);
              reset();
            }}
          >
            <option value="">New page</option>
            {w.collections.map((c) => (
              <option key={c.id} value={c.id}>
                {c.title}
              </option>
            ))}
          </select>
        </label>
      )}
      <label>
        New title{" "}
        <input
          value={title}
          onChange={(event) => {
            setTitle(event.target.value);
            reset();
          }}
        />
      </label>
      <button
        disabled={!selected || !title.trim()}
        onClick={() => void review()}
      >
        Preview template
      </button>
      {preview && (
        <section aria-label="Template preview">
          <p>
            {preview.valueCount} property defaults · {preview.blockCount} body
            blocks · {preview.taskCount} independent tasks
          </p>
          <p>
            Each copy receives new page, record, block and owned task
            identities. Explicit links keep their reference behavior.
          </p>
          {preview.warnings.map((warning, index) => (
            <p key={index}>{warning}</p>
          ))}
          {preview.propertyIssues.map((issue) => {
            const decision = decisions[issue.sourceProperty.id] ?? {
              target: "",
              value: JSON.stringify(issue.value, null, 2),
            };
            const change = (value: Partial<typeof decision>) => {
              setDecisions((previous) => ({
                ...previous,
                [issue.sourceProperty.id]: { ...decision, ...value },
              }));
              setPreview((current) =>
                current ? { ...current, canApply: false } : current,
              );
            };
            return (
              <fieldset key={issue.sourceProperty.id}>
                <legend>Map default: {issue.sourceProperty.name}</legend>
                <p>{issue.reason}</p>
                <label>
                  Destination property{" "}
                  <select
                    aria-label={`Map ${issue.sourceProperty.name}`}
                    value={decision.target}
                    onChange={(event) => change({ target: event.target.value })}
                  >
                    <option value="">Choose an action</option>
                    <option value="omit">Omit this default</option>
                    {preview.targetProperties.map((property) => (
                      <option key={property.id} value={property.id}>
                        {property.name} ({property.type})
                      </option>
                    ))}
                  </select>
                </label>
                {decision.target && decision.target !== "omit" && (
                  <label>
                    Current typed value (JSON)
                    <textarea
                      aria-label={`Value for ${issue.sourceProperty.name}`}
                      value={decision.value}
                      onChange={(event) =>
                        change({ value: event.target.value })
                      }
                    />
                  </label>
                )}
              </fieldset>
            );
          })}
          {!preview.canApply && (
            <p>
              Resolve the defaults, then preview again. No content has been
              created.
            </p>
          )}
          <button
            disabled={!preview.canApply}
            onClick={() =>
              void run(async () => {
                const pageId = await applyTemplate(w, preview);
                setPreview(undefined);
                await onCommitted();
                setMessage("Created the combined template copy.");
                openPage?.(pageId);
              })
            }
          >
            Create from reviewed template
          </button>
        </section>
      )}
      {message && <p role="status">{message}</p>}
    </fieldset>
  );
}
