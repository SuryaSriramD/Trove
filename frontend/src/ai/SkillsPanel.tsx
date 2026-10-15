import { useEffect, useState } from "react";
import {
  batch,
  dispatch,
  envelope,
  ids,
  pageGuards,
  requestID,
  type S,
  type WorkspaceSnapshot,
} from "../api";
import type { Location } from "../../../contracts/generated/domain";
import { AIPanel } from "./AIPanel";
import { ProviderPanel } from "../providers/ProviderPanel";
export type SkillExecution = {
  reference: S.SkillRunReference;
  prompt: string;
  mode: string;
  onRetry?: (ref: S.SkillRunReference) => void;
};
type Version = {
  id: string;
  pageId: string;
  version: number;
  definition: S.SkillDefinition;
  archived: boolean;
  imported: boolean;
  actorId: string;
  createdAt: string;
};
type Preview = {
  hash: string;
  prompt: string;
  sources: { pageId: string; title: string; revision: string }[];
  skill: Version;
  provider: string;
  tools: string[];
};
const empty = (): S.SkillDefinition => ({
  name: "",
  purpose: "",
  instructions: "",
  inputDescription: "",
  expectedOutput: "",
  mode: "summarize",
  sourcePageIds: [],
  tools: ["readPage", "proposeActions"],
});
const toolNames: S.SkillDefinition["tools"] = [
  "readPage",
  "search.query",
  "queryRecords",
  "resolveCitation",
  "allocateIds",
  "proposeChanges",
  "proposeActions",
];
export function SkillsPanel({
  workspace,
  onCommitted,
  onNavigate,
}: {
  workspace: WorkspaceSnapshot;
  onCommitted: () => void | Promise<void>;
  onNavigate: (location: Location) => void;
}) {
  const [library, setLibrary] = useState<Version[]>([]),
    [selected, setSelected] = useState<Version | null>(null),
    [definition, setDefinition] = useState(empty),
    [history, setHistory] = useState<Version[]>([]);
  const [imported, setImported] = useState(false),
    [archive, setArchive] = useState(false),
    [rawImport, setRawImport] = useState(""),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const [input, setInput] = useState(""),
    [provider, setProvider] = useState<"local" | "codex" | "claude_code">(
      "local",
    ),
    [invocation, setInvocation] = useState(requestID),
    [preview, setPreview] = useState<Preview | null>(null);
  const [sourceIds, setSourceIds] = useState<string[]>([]),
    [tools, setTools] = useState<S.SkillDefinition["tools"]>([]);
  async function load() {
    const r = await dispatch<Version[]>("skills.list", envelope(workspace));
    setLibrary(r);
    return r;
  }
  useEffect(() => {
    let live = true;
    dispatch<Version[]>("skills.list", envelope(workspace))
      .then((r) => {
        if (live) setLibrary(r);
      })
      .catch((e) => {
        if (live) setError(String(e));
      });
    return () => {
      live = false;
    };
  }, [workspace.workspaceId, workspace.snapshotCursor]);
  function resetRun() {
    setPreview(null);
    setInvocation(requestID());
  }
  function select(v: Version | null) {
    setSelected(v);
    setDefinition(v?.definition ?? empty());
    setImported(v?.imported ?? false);
    setArchive(v?.archived ?? false);
    setSourceIds(v?.definition.sourcePageIds ?? []);
    setTools(v?.definition.tools ?? []);
    setHistory([]);
    setInput("");
    resetRun();
  }
  async function act(fn: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  async function save() {
    let id = selected?.id,
      pageId = selected?.pageId;
    const commands: S.Command[] = [];
    let guards: S.ExpectedRevision[] = [];
    if (!selected) {
      [id, pageId] = await ids(workspace, 2);
      commands.push({ kind: "page.create", pageId, title: definition.name });
      guards = [
        { target: { entityType: "page", entityId: pageId }, revision: "0" },
      ];
    } else {
      const page = workspace.pages.find((p) => p.id === pageId);
      if (!page) throw new Error("Reload the skill page");
      guards = pageGuards(workspace, page);
    }
    commands.push({
      kind: "skill.save",
      skillId: id!,
      pageId: pageId!,
      expectedVersion: selected?.version ?? 0,
      definition,
      archived: archive,
      imported,
    });
    await batch(workspace, commands, guards);
    await onCommitted();
    const all = await load();
    select(all.find((v) => v.id === id) ?? null);
  }
  const reference: S.SkillRunReference | undefined = selected
    ? {
        skillId: selected.id,
        version: selected.version,
        invocationId: invocation,
        input,
        sourcePageIds: sourceIds,
        tools,
      }
    : undefined;
  async function reviewRetry(ref: S.SkillRunReference) {
    const versions = await dispatch<Version[]>("skills.list", {
      ...envelope(workspace),
      skillId: ref.skillId,
    });
    const version = versions.find((v) => v.version === ref.version);
    if (!version) throw new Error("Skill version unavailable");
    select(version);
    setInput(ref.input);
    setSourceIds(ref.sourcePageIds);
    setTools(ref.tools);
    setInvocation(ref.invocationId);
    setPreview(null);
  }
  const execution =
    preview && reference
      ? {
          reference: { ...reference, previewHash: preview.hash },
          prompt: preview.prompt,
          mode: preview.skill.definition.mode,
          onRetry: (ref: S.SkillRunReference) =>
            void act(() => reviewRetry(ref)),
        }
      : undefined;
  return (
    <section aria-label="Skills library">
      <h1>Skills</h1>
      <p>
        Save reusable instructions, then review the sources and provider for
        each manual run. Skill text never grants access or executes code.
      </p>
      {error && <p role="alert">{error}</p>}
      <div className="heading-row">
        <button onClick={() => select(null)}>New skill</button>
        <label>
          Saved skill
          <select
            aria-label="Saved skill"
            value={selected?.id ?? ""}
            onChange={(e) =>
              select(library.find((v) => v.id === e.target.value) ?? null)
            }
          >
            <option value="">New skill</option>
            {library.map((v) => (
              <option key={v.id} value={v.id}>
                {v.definition.name} · v{v.version}
                {v.archived ? " · archived" : ""}
              </option>
            ))}
          </select>
        </label>
      </div>
      <fieldset disabled={busy}>
        <legend>Definition</legend>
        {(
          [
            "name",
            "purpose",
            "instructions",
            "inputDescription",
            "expectedOutput",
          ] as const
        ).map((key) => (
          <label style={{ display: "block" }} key={key}>
            {
              {
                name: "Name",
                purpose: "Purpose",
                instructions: "Instructions",
                inputDescription: "Input description",
                expectedOutput: "Expected output",
              }[key]
            }
            <textarea
              aria-label={`Skill ${key}`}
              value={definition[key]}
              onChange={(e) => {
                setDefinition({ ...definition, [key]: e.target.value });
                setPreview(null);
              }}
            />
          </label>
        ))}
        <label>
          Output mode
          <select
            aria-label="Skill mode"
            value={definition.mode}
            onChange={(e) => {
              setDefinition({
                ...definition,
                mode: e.target.value as S.SkillDefinition["mode"],
              });
              setPreview(null);
            }}
          >
            {["answer", "summarize", "generate", "workspace_agent"].map(
              (mode) => (
                <option key={mode}>{mode}</option>
              ),
            )}
          </select>
        </label>
        <fieldset>
          <legend>Available sources</legend>
          {workspace.pages
            .filter((p) => !p.deletedAt && p.id !== selected?.pageId)
            .map((p) => (
              <label key={p.id} style={{ display: "block" }}>
                <input
                  type="checkbox"
                  checked={definition.sourcePageIds.includes(p.id)}
                  onChange={(e) => {
                    setDefinition({
                      ...definition,
                      sourcePageIds: e.target.checked
                        ? [...definition.sourcePageIds, p.id]
                        : definition.sourcePageIds.filter((id) => id !== p.id),
                    });
                    setPreview(null);
                  }}
                />
                {p.title || "Untitled"}
              </label>
            ))}
        </fieldset>
        <fieldset>
          <legend>Permitted tools</legend>
          {toolNames.map((tool) => (
            <label key={tool} style={{ display: "block" }}>
              <input
                type="checkbox"
                checked={definition.tools.includes(tool)}
                onChange={(e) => {
                  setDefinition({
                    ...definition,
                    tools: e.target.checked
                      ? [...definition.tools, tool]
                      : definition.tools.filter((t) => t !== tool),
                  });
                  setPreview(null);
                }}
              />
              {tool}
            </label>
          ))}
        </fieldset>
        <label>
          <input
            type="checkbox"
            checked={archive}
            onChange={(e) => {
              setArchive(e.target.checked);
              setPreview(null);
            }}
          />
          Archived (cannot run)
        </label>
        <button onClick={() => void act(save)}>Save new version</button>
        {selected && (
          <button
            onClick={() => {
              setSelected(null);
              setDefinition({ ...definition, name: `${definition.name} copy` });
              setHistory([]);
              resetRun();
            }}
          >
            Duplicate
          </button>
        )}
        {selected && (
          <button
            onClick={() =>
              void act(async () =>
                setHistory(
                  await dispatch<Version[]>("skills.list", {
                    ...envelope(workspace),
                    skillId: selected.id,
                  }),
                ),
              )
            }
          >
            Version history
          </button>
        )}
      </fieldset>
      {history.length > 0 && (
        <details open>
          <summary>Saved versions</summary>
          {history.map((v) => (
            <article key={v.version}>
              <p>
                Version {v.version} · {new Date(v.createdAt).toLocaleString()} ·
                actor {v.actorId}
              </p>
              <pre style={{ whiteSpace: "pre-wrap" }}>
                {JSON.stringify(v.definition, null, 2)}
              </pre>
              <button
                onClick={() => {
                  setDefinition(v.definition);
                  setPreview(null);
                }}
              >
                Use version {v.version} as next draft
              </button>
            </article>
          ))}
        </details>
      )}
      <details>
        <summary>Import or export a skill definition</summary>
        <p>
          Imported definitions start archived, with no source access. Review and
          save the sources and tools before enabling them.
        </p>
        <textarea
          aria-label="Skill JSON"
          value={rawImport}
          onChange={(e) => setRawImport(e.target.value)}
        />
        <button
          onClick={() =>
            void act(async () => {
              const d = JSON.parse(rawImport) as S.SkillDefinition;
              if (
                !d ||
                typeof d !== "object" ||
                ![
                  "name",
                  "purpose",
                  "instructions",
                  "inputDescription",
                  "expectedOutput",
                ].every(
                  (k) => typeof (d as Record<string, unknown>)[k] === "string",
                ) ||
                ![
                  "answer",
                  "summarize",
                  "generate",
                  "workspace_agent",
                ].includes(d.mode) ||
                !Array.isArray(d.tools) ||
                !d.tools.every((t) => toolNames.includes(t))
              )
                throw new Error(
                  "Import a valid skill definition exported by Trove",
                );
              select(null);
              setDefinition({ ...d, sourcePageIds: [] });
              setImported(true);
              setArchive(true);
            })
          }
        >
          Review imported definition
        </button>
        <button
          onClick={() => setRawImport(JSON.stringify(definition, null, 2))}
        >
          Show export JSON
        </button>
      </details>
      {selected && !selected.archived && (
        <section aria-label="Run skill">
          <h2>
            Run {selected.definition.name} · version {selected.version}
          </h2>
          <p>{selected.definition.inputDescription}</p>
          <label>
            Run input
            <textarea
              aria-label="Skill input"
              maxLength={1000}
              value={input}
              onChange={(e) => {
                setInput(e.target.value);
                resetRun();
              }}
            />
          </label>
          <fieldset>
            <legend>Sources for this run</legend>
            {selected.definition.sourcePageIds.map((id) => (
              <label key={id} style={{ display: "block" }}>
                <input
                  type="checkbox"
                  checked={sourceIds.includes(id)}
                  onChange={(e) => {
                    setSourceIds(
                      e.target.checked
                        ? [...sourceIds, id]
                        : sourceIds.filter((s) => s !== id),
                    );
                    resetRun();
                  }}
                />
                {workspace.pages.find((p) => p.id === id)?.title ??
                  "Unavailable source"}
              </label>
            ))}
          </fieldset>
          <fieldset>
            <legend>Tools for this run</legend>
            {selected.definition.tools.map((tool) => (
              <label key={tool} style={{ display: "block" }}>
                <input
                  type="checkbox"
                  checked={tools.includes(tool)}
                  onChange={(e) => {
                    setTools(
                      e.target.checked
                        ? [...tools, tool]
                        : tools.filter((t) => t !== tool),
                    );
                    resetRun();
                  }}
                />
                {tool}
              </label>
            ))}
          </fieldset>
          <label>
            Provider
            <select
              aria-label="Skill provider"
              value={provider}
              onChange={(e) => {
                setProvider(e.target.value as typeof provider);
                setPreview(null);
              }}
            >
              <option value="local">Local (default)</option>
              <option value="codex">Codex account</option>
              <option value="claude_code">Claude Code account</option>
            </select>
          </label>
          <button
            disabled={busy}
            onClick={() =>
              void act(async () =>
                setPreview(
                  await dispatch<Preview>("skills.preview", {
                    ...envelope(workspace),
                    skill: reference,
                    provider,
                  }),
                ),
              )
            }
          >
            Review skill run
          </button>
          <button onClick={resetRun}>Start a separate invocation</button>
          {preview && execution && (
            <div>
              <h3>
                Review · {provider} · v{selected.version}
              </h3>
              <ul>
                {preview.sources.map((p) => (
                  <li key={p.pageId}>
                    {p.title} · revision {p.revision}
                  </li>
                ))}
              </ul>
              <pre style={{ whiteSpace: "pre-wrap" }}>{preview.prompt}</pre>
              <p>
                Tools: {tools.join(", ") || "No mutation tools"}. Generation
                remains manual. Retry keeps invocation {invocation}.
              </p>
              {provider === "local" ? (
                <AIPanel
                  key={invocation + preview.hash}
                  workspace={workspace}
                  skill={execution}
                  onCommitted={onCommitted}
                  onNavigate={onNavigate}
                />
              ) : (
                <ProviderPanel
                  key={provider + invocation + preview.hash}
                  workspace={workspace}
                  onCommitted={onCommitted}
                  skill={execution}
                  skillProvider={provider}
                />
              )}
            </div>
          )}
        </section>
      )}
    </section>
  );
}
