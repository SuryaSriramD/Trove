import { OperationHistory } from "./OperationHistory";
import { PageFreshness, ReviewQueue } from "./PageFreshness";
import SupportBundlePanel from "./SupportBundlePanel";
import { useEffect, useState, useCallback, useMemo, useRef } from "react";
import {
  snapshot,
  ids,
  batch,
  expectedPage,
  pageGuards,
  fileGuard,
  attachFile,
  dispatch,
  envelope,
  fileURL,
  type WorkspaceSnapshot,
  type D,
  type S,
} from "./api";
import { PageTitleInput } from "./PageTitleInput";
import ModelSettings from "./ModelSettings";
import DatabaseView from "./database/DatabaseView";
import {
  createTask as createCanonicalTask,
  setTaskDone,
} from "./database/service";
import RecoveryPanel from "./recovery/RecoveryPanel";
import UpdateRecoveryControls from "./recovery/UpdateRecoveryControls";
import { PageEditor } from "./editor/Editor";
import { readable } from "./editor/codec";
import { focusSourceLocation } from "./sourceNavigation";
import { parseDeepLink } from "./deepLinks";
import { SearchPanel } from "./search/SearchPanel";
import { MeetingPanel } from "./meetings/MeetingPanel";
import { ImportExportPanel } from "./imports/ImportExportPanel";
import { JobsPanel } from "./JobsPanel";
import { AIPanel } from "./ai/AIPanel";
import { P2IntelligencePanel } from "./ai/P2IntelligencePanel";
import AutomationPanel from "./automation/AutomationPanel";
import { CapturePanel } from "./capture/CapturePanel";
import { ProviderPanel } from "./providers/ProviderPanel";
type Surface =
  | "Home"
  | "Inbox"
  | "Pages"
  | "Tasks"
  | "Meetings"
  | "Search"
  | "Import"
  | "Jobs"
  | "AI"
  | "Intelligence"
  | "Automations"
  | "Capture"
  | "Providers"
  | "Settings";
const nav: [Surface, string][] = [
  ["Home", "⌂"],
  ["Inbox", "▣"],
  ["Pages", "▤"],
  ["Tasks", "✓"],
  ["Meetings", "◉"],
  ["Search", "⌕"],
  ["Import", "↓"],
  ["Jobs", "↻"],
  ["AI", "✧"],
  ["Providers", "⇄"],
  ["Capture", "⊕"],
  ["Automations", "◷"],
  ["Intelligence", "◇"],
  ["Settings", "⚙"],
];
function stored<T>(key: string, value: T): T {
  try {
    return JSON.parse(localStorage.getItem(`trove.${key}`) ?? "null") ?? value;
  } catch {
    return value;
  }
}
function pref(key: string, value: unknown) {
  localStorage.setItem(`trove.${key}`, JSON.stringify(value));
}
const fmt = (date: string) =>
  new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric" }).format(
    new Date(date),
  );
export default function App() {
  const [shell, setW] = useState<WorkspaceSnapshot | null>(null);
  const [pageContent, setPageContent] = useState<{
    workspaceId: string;
    pageId: string;
    revision: string;
    blocks: D.Block[];
  } | null>(null);
  const [pageReadError, setPageReadError] = useState("");
  const [pageReadRetry, setPageReadRetry] = useState(0);
  const [surface, setSurface] = useState<Surface>("Home");
  const [active, setActive] = useState<string | null>(null);
  const w = useMemo(
    () =>
      shell
        ? {
            ...shell,
            blocks:
              shell.contentMode === "shell"
                ? pageContent?.workspaceId === shell.workspaceId &&
                  pageContent.pageId === active &&
                  pageContent.revision ===
                    shell.pages.find((p) => p.id === active)?.revision
                  ? pageContent.blocks
                  : []
                : shell.blocks,
          }
        : null,
    [shell, pageContent, active],
  );
  const [remoteBacklinks, setRemoteBacklinks] = useState<
    { pageId: string; blockId: string }[]
  >([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [tabs, setTabs] = useState<string[]>(() => stored("tabs", []));
  const [favorites, setFavorites] = useState<string[]>(() =>
    stored("favorites", []),
  );
  const [recents, setRecents] = useState<string[]>(() => stored("recents", []));
  const [switcher, setSwitcher] = useState(false);
  const [query, setQuery] = useState("");
  const [quickResults, setQuickResults] = useState<{
    key: string;
    ids: string[];
    error?: string;
  }>({ key: "", ids: [] });
  const [history, setHistory] = useState<string[]>(["Home"]);
  const [position, setPosition] = useState(0);
  const [pageMenu, setPageMenu] = useState(false);
  const [display, setDisplay] = useState<{
    pageId: string;
    fullWidth: boolean;
    presentation: boolean;
    revision: string;
  } | null>(null);
  const [displayBusy, setDisplayBusy] = useState(false);
  const [moveTo, setMoveTo] = useState("");
  const [editorKey, setEditorKey] = useState(0);
  const [taskTitle, setTaskTitle] = useState("");
  const [theme, setTheme] = useState(() => stored("theme", "light"));
  const [status, setStatus] = useState("");
  const [sourceLocation, setSourceLocation] = useState<D.Location | null>(null);
  const attachmentQueue = useRef<Promise<void>>(Promise.resolve());
  const activeRef = useRef(active);
  const flushEditor = useRef<(() => Promise<boolean>) | null>(null);
  activeRef.current = active;
  const errorHandler = useCallback(
    (e: unknown) => setError(e instanceof Error ? e.message : String(e)),
    [],
  );
  const refresh = useCallback(async () => {
    try {
      const next = await snapshot();
      for (const key of [
        "pages",
        "blocks",
        "collections",
        "properties",
        "records",
        "views",
        "files",
        "meetings",
        "taskFacets",
      ])
        if (!(next as any)[key]) (next as any)[key] = [];
      setW(next);
      setError("");
    } catch (e) {
      errorHandler(e);
    } finally {
      setLoading(false);
    }
  }, [errorHandler]);
  useEffect(() => {
    void refresh();
    const off = window.runtime?.EventsOn(
      "workspace:changed",
      () => void refresh(),
    );
    const offFile = window.runtime?.EventsOn(
      "native:fileSelected",
      (...args: unknown[]) => {
        const data = args[0] as any;
        if (data?.selectionHandle && activeRef.current) {
          const pageId = activeRef.current;
          void queueAttachment(() =>
            attachHandle(data.selectionHandle, pageId),
          );
        }
      },
    );
    return () => {
      off?.();
      offFile?.();
    };
  }, [refresh]);
  useEffect(() => {
    if (loading || !w) return;
    let second = 0;
    const first = requestAnimationFrame(() => {
      second = requestAnimationFrame(() => {
        void window.go?.main.App.ShellReady?.();
      });
    });
    return () => {
      cancelAnimationFrame(first);
      cancelAnimationFrame(second);
    };
  }, [loading, !!w]);
  useEffect(() => {
    document.documentElement.dataset.theme = theme;
    pref("theme", theme);
  }, [theme]);
  useEffect(() => {
    pref("tabs", tabs);
  }, [tabs]);
  useEffect(() => {
    pref("favorites", favorites);
  }, [favorites]);
  useEffect(() => {
    pref("recents", recents);
  }, [recents]);
  const {
    pagesById,
    visibleById,
    visible,
    blocksByPage,
    backlinksByPage,
    treePages,
    treeChildren,
  } = useMemo(() => {
    const pagesById = new Map<string, D.Page>();
    const visibleById = new Map<string, D.Page>();
    const visible: D.Page[] = [];
    const blocksByPage = new Map<string, D.Block[]>();
    const backlinksByPage = new Map<string, D.Block[]>();
    const treePages: D.Page[] = [];
    const treeChildren = new Map<string | null, D.Page[]>();
    const recordPages = new Set(w?.records.map((r) => r.pageId));
    for (const p of w?.pages ?? []) {
      pagesById.set(p.id, p);
      if (p.deletedAt || p.archivedAt) continue;
      visible.push(p);
      visibleById.set(p.id, p);
      if (recordPages.has(p.id)) continue;
      treePages.push(p);
      const siblings = treeChildren.get(p.parentPageId) ?? [];
      siblings.push(p);
      treeChildren.set(p.parentPageId, siblings);
    }
    for (const b of w?.blocks ?? []) {
      const siblings = blocksByPage.get(b.pageId) ?? [];
      siblings.push(b);
      blocksByPage.set(b.pageId, siblings);
      const target = (b.data as any)?.targetPageId;
      if (target) {
        const links = backlinksByPage.get(target) ?? [];
        links.push(b);
        backlinksByPage.set(target, links);
      }
    }
    return {
      pagesById,
      visibleById,
      visible,
      blocksByPage,
      backlinksByPage,
      treePages,
      treeChildren,
    };
  }, [w]);
  const page = active ? pagesById.get(active) : undefined;
  const pageReady =
    !w ||
    w.contentMode !== "shell" ||
    (pageContent?.workspaceId === w.workspaceId &&
      pageContent.pageId === page?.id &&
      pageContent.revision === page?.revision);
  useEffect(() => {
    let live = true;
    setPageReadError("");
    if (!w || !page || w.contentMode !== "shell") return;
    const currentWorkspace = w.workspaceId;
    void dispatch<S.ReadPageResult>("readPage", {
      ...envelope(w),
      pageId: page.id,
    })
      .then((result) => {
        if (!live) return;
        if (result.page.revision !== page.revision) {
          void refresh();
          return;
        }
        setPageContent({
          workspaceId: currentWorkspace,
          pageId: page.id,
          revision: result.page.revision,
          blocks: result.blocks,
        });
      })
      .catch((error) => {
        if (live)
          setPageReadError(
            error instanceof Error ? error.message : String(error),
          );
      });
    return () => {
      live = false;
    };
  }, [
    w?.workspaceId,
    w?.snapshotCursor,
    page?.id,
    page?.revision,
    pageReadRetry,
    refresh,
  ]);
  useEffect(() => {
    let live = true;
    setRemoteBacklinks([]);
    if (!w || !page || w.contentMode !== "shell") return;
    void dispatch<{ links: { pageId: string; blockId: string }[] }>(
      "links.backlinks",
      { ...envelope(w), pageId: page.id },
    )
      .then((result) => {
        if (live) setRemoteBacklinks(result.links);
      })
      .catch(errorHandler);
    return () => {
      live = false;
    };
  }, [w?.workspaceId, w?.snapshotCursor, page?.id, errorHandler]);
  const pageDisplay = display?.pageId === page?.id ? display : null;
  useEffect(() => {
    let current = true;
    if (!w || !page) {
      setDisplay(null);
      return;
    }
    void dispatch<{
      pageId: string;
      fullWidth: boolean;
      presentation: boolean;
      revision: string;
    }>("pageDisplay.get", { ...envelope(w), pageId: page.id })
      .then((value) => {
        if (current) setDisplay(value);
      })
      .catch(errorHandler);
    return () => {
      current = false;
    };
  }, [w?.workspaceId, page?.id, page?.revision, errorHandler]);
  async function changePageDisplay(change: {
    fullWidth?: boolean;
    presentation?: boolean;
  }) {
    if (!w || !page || !pageDisplay || displayBusy) return;
    setDisplayBusy(true);
    try {
      if (flushEditor.current && !(await flushEditor.current())) return;
      const latest = await snapshot();
      const currentPage = latest.pages.find((p) => p.id === page.id);
      if (!currentPage) return;
      const preferences = await dispatch<{
        pageId: string;
        fullWidth: boolean;
        presentation: boolean;
        revision: string;
      }>("pageDisplay.get", { ...envelope(latest), pageId: page.id });
      await batch(
        latest,
        [
          {
            kind: "page.display.set",
            pageId: page.id,
            fullWidth: change.fullWidth ?? preferences.fullWidth,
            presentation: change.presentation ?? preferences.presentation,
          },
        ],
        pageGuards(latest, currentPage),
      );
      setDisplay({ ...preferences, ...change });
      await refresh();
    } catch (e) {
      errorHandler(e);
    } finally {
      setDisplayBusy(false);
    }
  }
  const body = (active && blocksByPage.get(active)) || [];
  const backlinks =
    w?.contentMode === "shell"
      ? remoteBacklinks.map((link) => ({
          id: link.blockId,
          pageId: link.pageId,
        }))
      : (active && backlinksByPage.get(active)) || [];
  const navigate = useCallback(
    async (target: string, record = true) => {
      if (flushEditor.current && !(await flushEditor.current())) return false;
      setSourceLocation(null);
      if (nav.some((n) => n[0] === target)) {
        setSurface(target as Surface);
        setActive(null);
      } else {
        setActive(target);
        setSurface("Pages");
        setTabs((t) => (t.includes(target) ? t : [...t, target]));
        setRecents((r) =>
          [target, ...r.filter((x) => x !== target)].slice(0, 12),
        );
      }
      setPageMenu(false);
      setSwitcher(false);
      if (record) {
        setHistory((h) => [...h.slice(0, position + 1), target]);
        setPosition(position + 1);
      }
      return true;
    },
    [position],
  );
  const deepLinkHandler = useRef<(href: string) => Promise<void>>(async () => {});
  deepLinkHandler.current = async (href: string) => {
    try {
      const target = parseDeepLink(href);
      if (flushEditor.current && !(await flushEditor.current())) return;
      const current = await snapshot();
      if (target.workspaceId && target.workspaceId !== current.workspaceId)
        throw new Error("This link belongs to another workspace. Open that workspace in Settings, then follow the link again.");
      const targetPage = current.pages.find(p => p.id === target.pageId && !p.deletedAt);
      if (!targetPage) throw new Error("The linked page is unavailable in this workspace or is in Trash.");
      if (target.blockId && !current.blocks.some(b => b.id === target.blockId && b.pageId === target.pageId))
        throw new Error("The linked block is unavailable or has moved to another page.");
      setW(current);
      if (!(await navigate(target.pageId))) return;
      setSourceLocation({pageId: target.pageId, sourceRevision: targetPage.revision,
        ...(target.blockId ? {blockId: target.blockId} : {})});
      setStatus(target.blockId ? "Opened linked block." : "Opened linked page.");
    } catch (error) { errorHandler(error); }
  };
  useEffect(() => {
    // Delegate at the application boundary so links inside nested rich editors,
    // ordinary page content and imported readable fallbacks share one resolver.
    const click = (event: MouseEvent) => {
      const link = (event.target as Element | null)?.closest?.<HTMLAnchorElement>("a[href]");
      const href = link?.getAttribute("href") ?? "";
      if (!/^trove:/i.test(href)) return;
      event.preventDefault();
      event.stopPropagation();
      void deepLinkHandler.current(href);
    };
    document.addEventListener("click", click, true);
    return () => document.removeEventListener("click", click, true);
  }, []);
  useEffect(() => {
    if (loading || !w) return;
    let active = true;
    const consume = async () => {
      try {
        const href = await window.go?.main.App.PendingDeepLink?.();
        if (active && href) await deepLinkHandler.current(href);
      } catch (error) { if (active) errorHandler(error); }
    };
    const off = window.runtime?.EventsOn("native:openURL", () => void consume());
    void consume();
    return () => { active = false; off?.(); };
  }, [loading, w?.workspaceId, errorHandler]);
  async function navigateSource(location: D.Location) {
    if (!w) return;
    try {
      const resolved = await dispatch<S.ResolveCitationResult>(
        "resolveCitation",
        {
          ...envelope(w),
          location,
        },
      );
      if (resolved.state === "deleted" || resolved.state === "unavailable") {
        setError(
          "This source is deleted or unavailable. Restore it from history to inspect it.",
        );
        return;
      }
      if (flushEditor.current && !(await flushEditor.current())) return;
      await navigate(location.pageId);
      setSourceLocation(location);
      setStatus(
        resolved.state === "stale"
          ? "This source changed since it was cited. Showing its current content."
          : "Opened cited source.",
      );
    } catch (error) {
      errorHandler(error);
    }
  }
  useEffect(() => {
    if (sourceLocation && active === sourceLocation.pageId)
      return focusSourceLocation(sourceLocation);
  }, [active, sourceLocation, pageReady]);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        setSwitcher((s) => !s);
        setQuery("");
      }
      if (e.key === "Escape") {
        setSwitcher(false);
        setPageMenu(false);
      }
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "n") {
        e.preventDefault();
        void createPage();
      }
    };
    window.addEventListener("keydown", key);
    return () => window.removeEventListener("keydown", key);
  }, [w, position]);
  async function createPage(parentPageId?: string) {
    if (!w) return;
    try {
      const [pageId] = await ids(w, 1);
      await batch(
        w,
        [
          {
            kind: "page.create",
            pageId,
            title: "Untitled",
            ...(parentPageId ? { parentPageId } : {}),
          },
        ],
        [{ target: { entityType: "page", entityId: pageId }, revision: "0" }],
      );
      await refresh();
      navigate(pageId);
    } catch (e) {
      errorHandler(e);
    }
  }
  async function updatePage(
    p: D.Page,
    patch: Omit<S.UpdatePage, "kind" | "pageId">,
  ) {
    if (!w) return false;
    try {
      await batch(
        w,
        [{ kind: "page.update", pageId: p.id, ...patch }],
        pageGuards(w, p),
      );
      await refresh();
      return true;
    } catch (e) {
      errorHandler(e);
      return false;
    }
  }
  async function createTask() {
    if (!w || !taskTitle.trim()) return;
    try {
      await createCanonicalTask(w, taskTitle.trim());
      setTaskTitle("");
      await refresh();
    } catch (e) {
      errorHandler(e);
    }
  }
  async function toggleTask(recordId: string) {
    if (!w) return;
    try {
      const r = w.records.find((r) => r.id === recordId)!;
      const f = w.taskFacets.find((f) => f.recordId === recordId)!;
      const property = w.properties.find((p) => p.id === f.statusPropertyId)!;
      const cfg = property.config as any;
      const current = r.values.find(
        (v) => v.propertyId === f.statusPropertyId,
      )?.value;
      const done =
        cfg.options.find((o: any) => o.id === current)?.group === "done";
      await setTaskDone(w, recordId, !done);
      await refresh();
    } catch (e) {
      errorHandler(e);
    }
  }
  function queueAttachment(operation: () => Promise<void>) {
    const next = attachmentQueue.current.then(operation).catch(errorHandler);
    attachmentQueue.current = next;
    return next;
  }
  async function pickAttachment(pageId: string) {
    await queueAttachment(async () => {
      if (flushEditor.current && !(await flushEditor.current())) return;
      const handle = await window.go?.main.App.PickFile();
      if (handle) await attachHandle(handle, pageId);
    });
  }
  async function attachHandle(selectionHandle: string, pageId: string) {
    try {
      if (flushEditor.current && !(await flushEditor.current())) return;
      const current = await snapshot();
      const p = current.pages.find((p) => p.id === pageId);
      if (!p) throw Error("Choose a page before attaching a file.");
      const file = await dispatch<S.PrepareFileResult>("prepareFile", {
        ...envelope(current),
        selectionHandle,
      });
      const [blockId] = await ids(current, 1);
      const type = file.mediaType.startsWith("image/")
        ? "image"
        : file.mediaType.startsWith("audio/")
          ? "audio"
          : file.mediaType.startsWith("video/")
            ? "video"
            : file.mediaType === "application/pdf"
              ? "pdf"
              : "file";
      await batch(
        current,
        [
          {
            kind: "file.attach",
            fileId: file.fileId,
            ingestHandle: file.ingestHandle,
            pageId,
          },
          {
            kind: "block.insert",
            block: {
              blockId,
              pageId,
              type,
              schemaVersion: 1,
              data: { fileId: file.fileId, caption: [] },
            },
          },
        ],
        [
          ...pageGuards(current, p),
          fileGuard(current, file.fileId),
          { target: { entityType: "block", entityId: blockId }, revision: "0" },
        ],
      );
      await refresh();
      setEditorKey((k) => k + 1);
    } catch (e) {
      errorHandler(e);
    }
  }
  async function uploadFile(file: File, pageId: string) {
    const app = window.go?.main.App;
    if (!app?.StageUpload)
      throw Error("File paste requires the desktop attachment bridge.");
    if (file.size > 25 * 1024 * 1024)
      throw Error("Use Attach file for files larger than 25 MiB.");
    const data = await new Promise<string>((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result).split(",")[1]);
      reader.onerror = reject;
      reader.readAsDataURL(file);
    });
    const handle = await app.StageUpload(file.name, file.type, data);
    await attachHandle(handle, pageId);
  }
  function pageCard(p: D.Page) {
    return (
      <button className="page-card" key={p.id} onClick={() => navigate(p.id)}>
        <span className="card-icon">{p.icon ?? "▤"}</span>
        <strong>{p.title || "Untitled"}</strong>
        <span className="card-excerpt">
          {blocksByPage
            .get(p.id)
            ?.slice(0, 2)
            .map(readable)
            .join(" ")
            .slice(0, 110) || "A little space for your next idea."}
        </span>
        <small>Edited {fmt(p.updatedAt)}</small>
      </button>
    );
  }
  const pageTree = useMemo(() => {
    function tree(parent: string | null, depth = 0): React.ReactNode {
      return treeChildren.get(parent)?.map((p) => (
        <div key={p.id}>
          <button
            className={`tree-item ${p.id === active ? "selected" : ""}`}
            style={{ paddingInlineStart: 16 + depth * 15 }}
            onClick={() => navigate(p.id)}
          >
            <span>{p.icon ?? "▤"}</span>
            <span>{p.title || "Untitled"}</span>
          </button>
          {depth < 12 && tree(p.id, depth + 1)}
        </div>
      ));
    }
    return tree(null);
  }, [treeChildren, active, navigate]);
  const ancestry: D.Page[] = [];
  let ancestor = page;
  const seen = new Set<string>();
  while (ancestor && !seen.has(ancestor.id)) {
    seen.add(ancestor.id);
    ancestry.unshift(ancestor);
    ancestor = ancestor.parentPageId
      ? pagesById.get(ancestor.parentPageId)
      : undefined;
  }
  // Read block text only for an actual switcher query, once per snapshot/open.
  const searching = switcher && query.length > 0;
  const quickKey = `${w?.workspaceId}:${w?.scopeGrantId}:${w?.snapshotCursor}:${query}`;
  useEffect(() => {
    let live = true;
    if (!searching || !w || w.contentMode !== "shell") return;
    void dispatch<{
      hits: { location: { pageId: string } }[];
      partial?: boolean;
      warning?: string;
    }>("search.query", {
      ...envelope(w),
      query,
      mode: "keyword",
      limit: 200,
      kinds: ["page", "task"],
    })
      .then((result) => {
        if (live)
          setQuickResults({
            key: quickKey,
            ids: [...new Set(result.hits.map((hit) => hit.location.pageId))],
            error: result.partial
              ? (result.warning ?? "Search index is incomplete.")
              : undefined,
          });
      })
      .catch((error) => {
        if (live)
          setQuickResults({
            key: quickKey,
            ids: [],
            error: String(error.message ?? error),
          });
      });
    return () => {
      live = false;
    };
  }, [searching, w?.workspaceId, w?.scopeGrantId, w?.snapshotCursor, w?.contentMode, query]);
  const searchDocuments = useMemo(
    () =>
      searching && w?.contentMode !== "shell"
        ? visible.map((p) => ({
            page: p,
            text: `${p.title} ${(blocksByPage.get(p.id) ?? []).map(readable).join(" ")}`.toLocaleLowerCase(),
          }))
        : [],
    [searching, visible, blocksByPage, w?.contentMode],
  );
  const found = useMemo(() => {
    if (!switcher) return [];
    if (!query) return visible;
    if (w?.contentMode === "shell") {
      const needle = query.toLocaleLowerCase();
      const ids = new Set(
        quickResults.key === quickKey ? quickResults.ids : [],
      );
      return visible.filter(
        (page) =>
          page.title.toLocaleLowerCase().includes(needle) || ids.has(page.id),
      );
    }
    const needle = query.toLocaleLowerCase();
    return searchDocuments
      .filter((entry) => entry.text.includes(needle))
      .map((entry) => entry.page);
  }, [
    switcher,
    query,
    visible,
    searchDocuments,
    w?.contentMode,
    quickResults,
    quickKey,
  ]);
  return (
    <div
      className={`app${pageDisplay?.presentation ? " presentation-layout" : ""}`}
    >
      <a className="skip-link" href="#main">
        Skip to content
      </a>
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-logo">✳</span>
          <strong>Trove</strong>
          <span className="local-pill">
            {(window as any).troveHost ? "HOST" : "LOCAL"}
          </span>
        </div>
        <button className="workspace-button" onClick={() => navigate("Home")}>
          <span className="workspace-avatar">S</span>
          <span>
            My workspace<small>Room for what matters</small>
          </span>
          <span>⌄</span>
        </button>
        <button
          className="quick-search"
          onClick={() => {
            setSwitcher(true);
            setQuery("");
          }}
        >
          <span>⌕ Quick search</span>
          <kbd>⌘ K</kbd>
        </button>
        <nav aria-label="Workspace">
          {nav.map(([label, icon]) => (
            <button
              key={label}
              className={`nav-item ${surface === label && !active ? "selected" : ""}`}
              onClick={() => navigate(label)}
            >
              <span className="nav-icon">{icon}</span>
              {label}
              {label === "Tasks" && (
                <span className="count">{w?.taskFacets.length ?? 0}</span>
              )}
            </button>
          ))}
        </nav>
        <div className="sidebar-section">
          <span>FAVORITES</span>
        </div>
        {favorites
          .map((id) => visibleById.get(id))
          .filter(Boolean)
          .map((p) => (
            <button
              className="tree-item"
              key={p!.id}
              onClick={() => navigate(p!.id)}
            >
              <span>☆</span>
              <span>{p!.title}</span>
            </button>
          ))}
        {favorites.length === 0 && (
          <p className="sidebar-hint">Keep your essentials close.</p>
        )}
        <div className="sidebar-section">
          <span>YOUR PAGES</span>
          <button
            aria-label="Create page"
            title="Create page"
            onClick={() => void createPage()}
          >
            ＋
          </button>
        </div>
        <div className="page-tree" role="navigation" aria-label="Page tree">
          {pageTree}
        </div>
        <div className="sidebar-footer">
          <span className="status-dot" />{" "}
          {(window as any).troveHost
            ? "Connected to your host"
            : "On your device"}
          <span>↙</span>
        </div>
      </aside>
      <div className="main-shell">
        <header className="topbar">
          <div className="breadcrumbs">
            <button
              aria-label="Go back"
              disabled={position === 0}
              onClick={() => {
                setPosition(position - 1);
                navigate(history[position - 1], false);
              }}
            >
              ‹
            </button>
            <button
              aria-label="Go forward"
              disabled={position === history.length - 1}
              onClick={() => {
                setPosition(position + 1);
                navigate(history[position + 1], false);
              }}
            >
              ›
            </button>
            <span className="crumb-root">My workspace</span>
            <span>/</span>
            {page ? (
              ancestry.map((p, i) => (
                <span key={p.id}>
                  <button onClick={() => navigate(p.id)}>
                    {p.title || "Untitled"}
                  </button>
                  {i < ancestry.length - 1 && " / "}
                </span>
              ))
            ) : (
              <span>{surface}</span>
            )}
          </div>
          <div className="topbar-actions">
            {page && (
              <>
                <button
                  aria-label={
                    favorites.includes(page.id)
                      ? "Remove favorite"
                      : "Favorite page"
                  }
                  onClick={() =>
                    setFavorites((f) =>
                      f.includes(page.id)
                        ? f.filter((x) => x !== page.id)
                        : [...f, page.id],
                    )
                  }
                >
                  {favorites.includes(page.id) ? "★" : "☆"}
                </button>
                <button
                  aria-label="Page options"
                  onClick={() => setPageMenu(!pageMenu)}
                >
                  •••
                </button>
              </>
            )}
            <span className="profile-avatar">S</span>
          </div>
        </header>
        {tabs.length > 0 && (
          <div className="tabs" role="tablist" aria-label="Open pages">
            {tabs.map((id) => {
              const p = pagesById.get(id);
              return p && !p.deletedAt ? (
                <div key={id} className={id === active ? "active" : ""}>
                  <button
                    role="tab"
                    aria-selected={id === active}
                    onClick={() => navigate(id)}
                  >
                    ▤ {p.title}
                  </button>
                  <button
                    aria-label={`Close ${p.title}`}
                    onClick={() => {
                      setTabs((t) => t.filter((x) => x !== id));
                      if (active === id) navigate("Home");
                    }}
                  >
                    ×
                  </button>
                </div>
              ) : null;
            })}
          </div>
        )}
        {error && (
          <div className="error-banner" role="alert">
            <span>{error}</span>
            <button aria-label="Dismiss error" onClick={() => setError("")}>
              ×
            </button>
          </div>
        )}
        <main
          id="main"
          tabIndex={-1}
          onPaste={(e) => {
            const files = Array.from(e.clipboardData.files);
            if (files.length && active) {
              e.preventDefault();
              const pageId = active;
              for (const file of files)
                void queueAttachment(() => uploadFile(file, pageId));
            }
          }}
          onDragOver={(e) => {
            if (e.dataTransfer.types.includes("Files")) e.preventDefault();
          }}
          onDrop={(e) => {
            if (e.dataTransfer.files.length && active) {
              e.preventDefault();
              const pageId = active;
              for (const file of Array.from(e.dataTransfer.files))
                void queueAttachment(() => uploadFile(file, pageId));
            }
          }}
        >
          {loading ? (
            <div className="empty-state">Opening your workspace…</div>
          ) : !w ? (
            <div className="welcome">
              <span className="hero-icon">✳</span>
              <h1>A place for your thinking.</h1>
              <p>
                Bring your pages, projects, and ideas together in a private
                workspace on this Mac.
              </p>
              <div className="action-row">
                <button
                  className="primary"
                  onClick={async () => {
                    try {
                      await window.go?.main.App.CreateWorkspace?.();
                      await refresh();
                    } catch (e) {
                      errorHandler(e);
                    }
                  }}
                >
                  Create workspace
                </button>
                <button
                  onClick={async () => {
                    try {
                      await window.go?.main.App.OpenWorkspace?.();
                      await refresh();
                    } catch (e) {
                      errorHandler(e);
                    }
                  }}
                >
                  Open workspace
                </button>
              </div>
              <UpdateRecoveryControls onError={errorHandler} />
            </div>
          ) : page ? (
            <article
              className={`page-surface${pageDisplay?.fullWidth ? " full-width" : ""}`}
            >
              {pageDisplay?.presentation && (
                <button
                  className="exit-presentation"
                  disabled={displayBusy}
                  onClick={() =>
                    void changePageDisplay({ presentation: false })
                  }
                >
                  Exit presentation layout
                </button>
              )}
              {page.coverFileId && (
                <div className="page-cover">
                  <img src={fileURL(page.coverFileId)} alt="Page cover" />
                </div>
              )}
              <OperationHistory workspace={w} pageId={page.id} onChange={refresh} onError={errorHandler} />
              <PageFreshness workspace={w} page={page} onChange={refresh} onError={errorHandler} />
              <div className="page-heading">
                <button
                  className="page-icon"
                  aria-label="Set page icon"
                  onClick={() => {
                    const icon = prompt("Page icon", page.icon ?? "");
                    if (icon !== null) void updatePage(page, { icon });
                  }}
                >
                  {page.icon ?? "▤"}
                </button>
                <PageTitleInput
                  key={page.id}
                  page={page}
                  onCommit={(base, title) => updatePage(base, { title })}
                />
                <div className="page-meta">
                  Your private workspace <span>·</span> Edited{" "}
                  {fmt(page.updatedAt)}
                  <span>·</span>
                  <button onClick={() => void createPage(page.id)}>
                    ＋ Subpage
                  </button>
                  <button
                    onClick={async () => {
                      try {
                        await pickAttachment(page.id);
                      } catch (e) {
                        errorHandler(e);
                      }
                    }}
                  >
                    ↗ Attach file
                  </button>
                </div>
              </div>
              {[
                ...new Set([
                  ...w.collections
                    .filter((c) => c.owningPageId === page.id)
                    .map((c) => c.id),
                  ...w.views
                    .filter((v) => v.owningPageId === page.id)
                    .map((v) => v.collectionId),
                ]),
              ].map((id) => (
                <DatabaseView
                  key={id}
                  workspace={w}
                  collectionId={id}
                  owningPageId={page.id}
                  onCommitted={refresh}
                  onError={errorHandler}
                  openPage={navigate}
                />
              ))}
              {w.memories
                .filter((m) => m.pageId === page.id)
                .map((m) => (
                  <aside className="settings-section" key={m.id}>
                    <h2>
                      {m.kind.replaceAll("_", " ")} · {m.status}
                    </h2>
                    <p>
                      {m.content ??
                        pagesById.get(m.pageRef ?? "")?.title ??
                        "Referenced page unavailable"}
                    </p>
                    <p>Review approval and evidence in Local AI below.</p>
                  </aside>
                ))}
              {pageReady ? (
                <PageEditor
                  key={`${page.id}-${editorKey}`}
                  page={page}
                  blocks={body}
                  workspace={w}
                  onAttach={() => pickAttachment(page.id)}
                  registerFlush={(flush) => {
                    flushEditor.current = flush;
                  }}

                  onCommitted={refresh}
                  onError={errorHandler}
                  openPage={navigate}
                  toggleTask={toggleTask}
                />
              ) : (
                <div role={pageReadError ? "alert" : "status"}>
                  {pageReadError || "Loading page content…"}
                  {pageReadError && (
                    <button onClick={() => setPageReadRetry((n) => n + 1)}>
                      Retry loading page
                    </button>
                  )}
                </div>
              )}
              {w.meetings.some((m) => m.pageId === page.id) && (
                <MeetingPanel
                  workspace={w}
                  pageId={page.id}
                  onRefresh={refresh}
                  onNavigate={navigateSource}
                />
              )}
              <details className="page-ai">
                <summary>✧ Ask AI or work with this page</summary>
                <AIPanel
                  key={page.id}
                  workspace={w}
                  pageId={page.id}
                  onNavigate={navigateSource}
                  onCommitted={refresh}
                />
              </details>
              <div className="backlinks">
                <span className="eyebrow">LINKED TO THIS PAGE</span>
                {backlinks.map((b) => (
                  <button key={b.id} onClick={() => navigate(b.pageId)}>
                    ↗ {pagesById.get(b.pageId)?.title}
                  </button>
                ))}
                {!backlinks.length && (
                  <p>
                    Backlinks will appear here when another page links to this
                    one.
                  </p>
                )}
              </div>
            </article>
          ) : (
            <section className="surface">
              {surface === "Home" ? (
                <>
                  <div className="home-intro">
                    <span className="eyebrow">A LITTLE CLARITY, EVERY DAY</span>
                    <h1>Make room for your ideas.</h1>
                    <p>
                      Your thoughts, plans, and everything in between.
                      <br />
                      All here. All yours.
                    </p>
                    <div className="action-row">
                      <button
                        className="primary"
                        onClick={() => void createPage()}
                      >
                        ＋ New page
                      </button>
                      <button onClick={() => navigate("Import")}>
                        ↓ Bring something in
                      </button>
                    </div>
                    <span className="hero-drawing" aria-hidden="true">
                      ✳
                    </span>
                  </div>
                  <ReviewQueue workspace={w} onNavigate={navigate} />
                  <div className="section-title">
                    <h2>Pick up where you left off</h2>
                    <button onClick={() => navigate("Pages")}>
                      All pages ↗
                    </button>
                  </div>
                  <div className="card-grid">
                    {recents
                      .map((id) => visibleById.get(id))
                      .filter(Boolean)
                      .slice(0, 4)
                      .map((p) => pageCard(p!))}
                    {!recents.some((id) => visibleById.has(id)) &&
                      visible.slice(0, 4).map(pageCard)}
                    {visible.length === 0 && (
                      <button
                        className="page-card new-card"
                        onClick={() => void createPage()}
                      >
                        <span>＋</span>
                        <strong>Your first page</strong>
                        <small>Start with an empty canvas.</small>
                      </button>
                    )}
                  </div>
                  <div className="home-bottom">
                    <div className="soft-card">
                      <span>☀</span>
                      <h3>A calmer place to work</h3>
                      <p>
                        Capture a loose thought. Shape it into a plan. Give it a
                        home.
                      </p>
                      <button onClick={() => navigate("Inbox")}>
                        Open your inbox ↗
                      </button>
                    </div>
                    <div className="soft-card lavender">
                      <span>✓</span>
                      <h3>Small steps, real progress</h3>
                      <p>
                        {w.taskFacets.length
                          ? `${w.taskFacets.length} tasks, connected to the pages behind them.`
                          : "Keep tasks connected to the work that matters."}
                      </p>
                      <button onClick={() => navigate("Tasks")}>
                        Go to tasks ↗
                      </button>
                    </div>
                  </div>
                </>
              ) : surface === "Tasks" ? (
                <>
                  <span className="eyebrow">ONE TASK, EVERYWHERE</span>
                  <h1>Tasks</h1>
                  <p className="lede">
                    The next step, with its context close by.
                  </p>
                  <form
                    className="task-form"
                    onSubmit={(e) => {
                      e.preventDefault();
                      void createTask();
                    }}
                  >
                    <input
                      aria-label="New task title"
                      placeholder="What needs to happen?"
                      value={taskTitle}
                      onChange={(e) => setTaskTitle(e.target.value)}
                    />
                    <button className="primary">Add task</button>
                  </form>
                  <div className="task-list">
                    {w.taskFacets.map((f) => {
                      const r = w.records.find((r) => r.id === f.recordId);
                      const p = r ? visibleById.get(r.pageId) : undefined;
                      const prop = w.properties.find(
                        (p) => p.id === f.statusPropertyId,
                      );
                      const value = r?.values.find(
                        (v) => v.propertyId === f.statusPropertyId,
                      )?.value;
                      const option = (prop?.config as any)?.options?.find(
                        (o: any) => o.id === value,
                      );
                      return r && p ? (
                        <div className="task-row" key={r.id}>
                          <input
                            type="checkbox"
                            aria-label={`Complete ${p.title}`}
                            checked={option?.group === "done"}
                            onChange={() => void toggleTask(r.id)}
                          />
                          <button
                            className={option?.group === "done" ? "done" : ""}
                            onClick={() => navigate(p.id)}
                          >
                            {p.title}
                          </button>
                          <span>
                            {
                              w.collections.find((c) => c.id === r.collectionId)
                                ?.title
                            }
                          </span>
                          <small>{option?.label}</small>
                        </div>
                      ) : null;
                    })}
                  </div>
                  {!w.taskFacets.length && (
                    <div className="empty-state">
                      Every great project starts with one small step.
                    </div>
                  )}
                </>
              ) : surface === "Pages" || surface === "Inbox" ? (
                <>
                  <div className="heading-row">
                    <div>
                      <span className="eyebrow">
                        {surface === "Inbox"
                          ? "CAPTURE NOW, ORGANIZE LATER"
                          : "YOUR GROWING COLLECTION"}
                      </span>
                      <h1>{surface}</h1>
                    </div>
                    <button
                      className="primary"
                      onClick={() => void createPage()}
                    >
                      ＋ New page
                    </button>
                  </div>
                  <p className="lede">
                    {surface === "Inbox"
                      ? "Unfiled pages, ready to find their place."
                      : "A home for every idea worth keeping."}
                  </p>
                  <div className="card-grid">
                    {(surface === "Inbox"
                      ? (treeChildren.get(null) ?? [])
                      : visible
                    ).map(pageCard)}
                  </div>
                  {surface === "Pages" && (
                    <DatabaseView
                      workspace={w}

                      onCommitted={refresh}
                      onError={errorHandler}
                      openPage={navigate}
                    />
                  )}
                </>
              ) : surface === "Search" ? (
                <SearchPanel workspace={w} onNavigate={navigateSource} />
              ) : surface === "Meetings" ? (
                <MeetingPanel
                  workspace={w}
                  onRefresh={refresh}
                  onNavigate={navigateSource}
                />
              ) : surface === "Import" ? (
                <ImportExportPanel
                  workspace={w}
                  onChanged={refresh}
                  onOpenPage={navigate}
                />
              ) : surface === "AI" ? (
                <AIPanel
                  workspace={w}
                  onNavigate={navigateSource}
                  onCommitted={refresh}
                />
              ) : surface === "Intelligence" ? (
                <P2IntelligencePanel
                  workspace={w}
                  refresh={refresh}
                  onNavigate={navigate}
                  onError={errorHandler}
                />
              ) : surface === "Automations" ? (
                <AutomationPanel
                  workspace={w}
                  onCommitted={refresh}
                  onError={errorHandler}
                />
              ) : surface === "Capture" ? (
                <CapturePanel
                  workspace={w}
                  onChanged={refresh}
                  onOpenPage={navigate}
                />
              ) : surface === "Providers" ? (
                <ProviderPanel workspace={w} onCommitted={refresh} />
              ) : surface === "Jobs" ? (
                <JobsPanel workspace={w} />
              ) : (
                <>
                  <span className="eyebrow">YOUR WORKSPACE, YOUR WAY</span>
                  <h1>Settings</h1>
                  <div className="settings-section">
                    <h2>Appearance</h2>
                    <label>
                      Theme
                      <select
                        value={theme}
                        onChange={(e) => setTheme(e.target.value)}
                      >
                        <option value="light">Light</option>
                        <option value="dark">Dark</option>
                      </select>
                    </label>
                  </div>
                  <div className="settings-section">
                    <h2>Workspace</h2>
                    <p>
                      Workspace ID <code>{w.workspaceId}</code>
                    </p>
                    <p>
                      {visible.length} pages · {w.files.length} files ·{" "}
                      {w.collections.length} collections
                    </p>
                    <button
                      onClick={async () => {
                        try {
                          if (
                            flushEditor.current &&
                            !(await flushEditor.current())
                          )
                            return;
                          await attachmentQueue.current;
                          await window.go?.main.App.OpenWorkspace?.();
                          setTabs([]);
                          setRecents([]);
                          navigate("Home");
                          await refresh();
                        } catch (e) {
                          errorHandler(e);
                        }
                      }}
                    >
                      Open another workspace
                    </button>
                    <button
                      onClick={async () => {
                        try {
                          if (
                            flushEditor.current &&
                            !(await flushEditor.current())
                          )
                            return;
                          await attachmentQueue.current;
                          await window.go?.main.App.CreateWorkspace?.();
                          setTabs([]);
                          setRecents([]);
                          navigate("Home");
                          await refresh();
                        } catch (e) {
                          errorHandler(e);
                        }
                      }}
                    >
                      Create workspace
                    </button>
                  </div>
                  <div className="settings-section">
                    <h2>Archived pages</h2>
                    {w.pages
                      .filter((p) => p.archivedAt && !p.deletedAt)
                      .map((p) => (
                        <div className="settings-row" key={p.id}>
                          {p.title}
                          <button
                            onClick={() =>
                              void updatePage(p, { archived: false })
                            }
                          >
                            Unarchive
                          </button>
                        </div>
                      ))}
                  </div>
                  <ModelSettings onError={errorHandler} />
                  <UpdateRecoveryControls key={w.workspaceId} workspaceId={w.workspaceId} onError={errorHandler} />
                  <SupportBundlePanel key={w.workspaceId} workspaceId={w.workspaceId} pages={w.pages} onError={errorHandler} />
                  <RecoveryPanel
                    workspace={w}

                    onCommitted={refresh}
                    onError={errorHandler}
                  />
                  <div className="settings-section">
                    <h2>Keyboard shortcuts</h2>
                    <div className="shortcut-grid">
                      <span>Quick switcher</span>
                      <kbd>⌘ K</kbd>
                      <span>New page</span>
                      <kbd>⌘ N</kbd>
                      <span>Save</span>
                      <kbd>⌘ S</kbd>
                      <span>Find in page</span>
                      <kbd>⌘ F</kbd>
                      <span>Duplicate block</span>
                      <kbd>⌘ ⇧ D</kbd>
                      <span>Indent / outdent</span>
                      <kbd>Tab / ⇧ Tab</kbd>
                    </div>
                  </div>
                </>
              )}
            </section>
          )}
        </main>
        <footer className="app-footer">
          <span>Made for your own way of thinking.</span>
          <span>{status || "Private by default · Saved on this device"}</span>
        </footer>
      </div>
      {pageMenu && page && w && (
        <div className="page-menu" role="dialog" aria-label="Page options">
          <label>
            <input
              type="checkbox"
              aria-label="Full width"
              checked={pageDisplay?.fullWidth ?? false}
              disabled={!pageDisplay || displayBusy}
              onChange={(e) =>
                void changePageDisplay({ fullWidth: e.target.checked })
              }
            />{" "}
            Full width
          </label>
          <label>
            <input
              type="checkbox"
              aria-label="Presentation layout"
              checked={pageDisplay?.presentation ?? false}
              disabled={!pageDisplay || displayBusy}
              onChange={(e) =>
                void changePageDisplay({ presentation: e.target.checked })
              }
            />{" "}
            Presentation layout
          </label>
          <button onClick={() => void createPage(page.id)}>
            ＋ Add subpage
          </button>
          <button
            onClick={() => {
              const icon = prompt("Page icon", page.icon ?? "");
              if (icon !== null) void updatePage(page, { icon });
            }}
          >
            Change icon
          </button>
          <label>
            Cover image
            <select
              aria-label="Cover image"
              value={page.coverFileId ?? ""}
              onChange={(e) =>
                void updatePage(page, { coverFileId: e.target.value || null })
              }
            >
              <option value="">No cover</option>
              {w.files
                .filter((f) => f.mediaType.startsWith("image/"))
                .map((f) => (
                  <option key={f.id} value={f.id}>
                    {f.originalName}
                  </option>
                ))}
            </select>
          </label>
          <label>
            Move page
            <select
              aria-label="Move page parent"
              value={moveTo}
              onChange={(e) => setMoveTo(e.target.value)}
            >
              <option value="">Workspace root</option>
              {treePages
                .filter((p) => p.id !== page.id)
                .map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.title}
                  </option>
                ))}
            </select>
          </label>
          <button
            onClick={async () => {
              try {
                await batch(
                  w,
                  [
                    {
                      kind: "page.move",
                      pageId: page.id,
                      parentPageId: moveTo || null,
                    },
                  ],
                  pageGuards(w, page),
                );
                await refresh();
                setPageMenu(false);
              } catch (e) {
                errorHandler(e);
              }
            }}
          >
            Move here
          </button>
          <button
            onClick={async () => {
              await updatePage(page, { archived: true });
              navigate("Home");
            }}
          >
            Archive page
          </button>
          <button
            className="danger"
            onClick={async () => {
              try {
                await batch(
                  w,
                  [
                    {
                      kind: "page.trash",
                      pageId: page.id,
                      includeDescendants: false,
                    },
                  ],
                  pageGuards(w, page),
                );
                await refresh();
                navigate("Home");
              } catch (e) {
                errorHandler(e);
              }
            }}
          >
            Move to trash
          </button>
        </div>
      )}
      {switcher && (
        <div className="modal-backdrop" onClick={() => setSwitcher(false)}>
          <div
            className="switcher"
            role="dialog"
            aria-modal="true"
            aria-label="Quick switcher"
            onClick={(e) => e.stopPropagation()}
          >
            <div>
              <span>⌕</span>
              <input
                autoFocus
                aria-label="Find a page"
                placeholder="Where would you like to go?"
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter" && found[0]) navigate(found[0].id);
                  if (e.key === "Tab") {
                    const list = e.currentTarget
                      .closest(".switcher")
                      ?.querySelectorAll<HTMLButtonElement>("button");
                    if (list?.length) {
                      e.preventDefault();
                      list[0].focus();
                    }
                  }
                }}
              />
              <kbd>esc</kbd>
            </div>
            <p>{query ? "MATCHING PAGES" : "YOUR PAGES"}</p>
            {found.slice(0, 20).map((p) => (
              <button
                key={p.id}
                onClick={() => navigate(p.id)}
                onKeyDown={(e) => {
                  if (e.key === "ArrowDown") {
                    e.preventDefault();
                    (
                      e.currentTarget.nextElementSibling as HTMLElement
                    )?.focus();
                  }
                  if (e.key === "ArrowUp") {
                    e.preventDefault();
                    (
                      e.currentTarget.previousElementSibling as HTMLElement
                    )?.focus();
                  }
                }}
              >
                <span>{p.icon ?? "▤"}</span>
                {p.title}
                <span>↵</span>
              </button>
            ))}
            {searching &&
              w?.contentMode === "shell" &&
              quickResults.key !== quickKey && (
                <p role="status">Searching page contents…</p>
              )}
            {searching &&
              w?.contentMode === "shell" &&
              quickResults.key === quickKey &&
              quickResults.error && (
                <p role="alert">
                  Content search unavailable: {quickResults.error}
                </p>
              )}
            {!found.length &&
              !(
                searching &&
                w?.contentMode === "shell" &&
                quickResults.key !== quickKey
              ) && <p>No matching pages. Try another phrase.</p>}
          </div>
        </div>
      )}
    </div>
  );
}
