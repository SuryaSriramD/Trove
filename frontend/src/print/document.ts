import katex from "katex";
import mermaid from "mermaid";
import "katex/dist/katex.min.css";
import "./print.css";
import type * as D from "../../../contracts/generated/domain";

type PageSource = {
  id: string;
  canonicalPage: D.Page;
  canonicalBlocks: D.Block[];
};
type Snapshot = {
  pages: PageSource[];
  attachments: { id: string; path: string; mediaType?: string }[];
  collections?: unknown[];
};
type Status = { ready: boolean; warnings: string[]; error?: string };
declare global {
  interface Window {
    trovePrintStatus: Status;
  }
}
window.trovePrintStatus = { ready: false, warnings: [] };
const warnings = window.trovePrintStatus.warnings;
const warn = (s: string) => {
  if (!warnings.includes(s)) warnings.push(s);
};
const colors: Record<string, string> = {
  gray: "#686868",
  brown: "#85583e",
  orange: "#b85d05",
  yellow: "#947500",
  green: "#267341",
  blue: "#2468ac",
  purple: "#7852a0",
  pink: "#a83f78",
  red: "#b33434",
};
const backgrounds: Record<string, string> = {
  gray: "#e9e9e7",
  brown: "#f1e6dd",
  orange: "#ffead5",
  yellow: "#fff2bb",
  green: "#dcf0df",
  blue: "#deedff",
  purple: "#eee2fa",
  pink: "#f9e2f0",
  red: "#fbe0df",
};
const text = (v: unknown) => (typeof v === "string" ? v : "");
function element(tag: string, value?: string, className?: string) {
  const e = document.createElement(tag);
  if (value) e.textContent = value;
  if (className) e.className = className;
  return e;
}
function safeLink(raw: string) {
  try {
    const u = new URL(raw);
    if (u.protocol === "trove:" && !u.username && !u.password) {
      const parts = u.pathname.split("/").filter(Boolean);
      if (u.hostname === "page" && parts[0]) {
        const block = parts[1] === "block" ? parts[2] : u.hash.slice(1);
        return block ? "#block-" + block : "#page-" + parts[0];
      }
      if (u.hostname === "block" && parts.length === 1)
        return "#block-" + parts[0];
      return "";
    }
    return ["https:", "http:", "mailto:"].includes(u.protocol) ? u.href : "";
  } catch {
    return "";
  }
}
function rich(value: unknown) {
  const out = document.createDocumentFragment();
  for (const span of Array.isArray(value) ? value : []) {
    let node: Node = document.createTextNode(text(span.text));
    for (const mark of span.marks ?? []) {
      const tag = (
        {
          bold: "strong",
          italic: "em",
          underline: "u",
          strike: "s",
          code: "code",
        } as Record<string, string>
      )[mark];
      if (tag) {
        const wrap = element(tag);
        wrap.append(node);
        node = wrap;
      }
    }
    const fg = colors[span.color],
      bg = backgrounds[span.highlight];
    if (fg || bg) {
      const wrap = element("span");
      if (fg) wrap.style.color = fg;
      if (bg) wrap.style.backgroundColor = bg;
      wrap.append(node);
      node = wrap;
    }
    const url = safeLink(text(span.link));
    if (url) {
      const a = element("a") as HTMLAnchorElement;
      a.href = url;
      a.append(node);
      node = a;
    }
    out.append(node);
  }
  return out;
}
let diagramSequence = 0;
async function render(snapshot: Snapshot) {
  const host = document.getElementById("document")!,
    blocks = new Map(
      snapshot.pages.flatMap((p) =>
        p.canonicalBlocks.map((b) => [b.id, b] as const),
      ),
    ),
    files = new Map(snapshot.attachments.map((f) => [f.id, f]));
  const children = new Map<string, D.Block[]>();
  for (const b of blocks.values()) {
    if (b.parentBlockId) {
      const list = children.get(b.parentBlockId) ?? [];
      list.push(b);
      children.set(b.parentBlockId, list);
    }
  }
  const order = (list: D.Block[]) =>
    list
      .slice()
      .sort(
        (a, b) =>
          a.sortKey.localeCompare(b.sortKey) || a.id.localeCompare(b.id),
      );
  const jobs: Promise<unknown>[] = [];
  const renderBlock = (
    b: D.Block,
    ancestry = new Set<string>(),
    copy = false,
  ): HTMLElement => {
    const d = (b.data ?? {}) as Record<string, any>,
      out = element("section", undefined, "print-block");
    if (!copy) out.id = "block-" + b.id;
    if (ancestry.has(b.id) || ancestry.size >= 32) {
      out.append(
        element(
          "p",
          "Reusable or nested content cycle; see canonical export.",
          "fallback",
        ),
      );
      warn("Some recursive content was replaced with a readable reference.");
      return out;
    }
    const next = new Set(ancestry).add(b.id),
      addChildren = (ids?: string[]) => {
        for (const child of ids
          ? (ids.map((id) => blocks.get(id)).filter(Boolean) as D.Block[])
          : order(children.get(b.id) ?? []))
          out.append(renderBlock(child, next, copy));
      };
    const tags: Record<string, string> = {
      paragraph: "p",
      heading_1: "h2",
      heading_2: "h3",
      heading_3: "h4",
      heading_4: "h5",
      quote: "blockquote",
      bulleted_list_item: "p",
      numbered_list_item: "p",
      toggle: "h4",
      toggle_heading: "h3",
      callout: "aside",
    };
    if (tags[b.type]) {
      const node = element(tags[b.type]);
      if (b.type === "bulleted_list_item")
        node.append(document.createTextNode("• "));
      if (b.type === "numbered_list_item")
        node.append(document.createTextNode("1. "));
      if (b.type === "callout")
        node.append(document.createTextNode(text(d.icon) + " "));
      node.append(rich(d.richText));
      out.append(node);
      addChildren();
      return out;
    }
    switch (b.type) {
      case "divider":
        out.append(element("hr"));
        break;
      case "code": {
        if (d.filename)
          out.append(element("div", text(d.filename), "code-label"));
        const pre = element("pre"),
          code = element("code", text(d.source));
        pre.append(code);
        out.append(pre);
        if (d.caption) out.append(rich(d.caption));
        break;
      }
      case "equation": {
        const formula = element("div", undefined, "equation");
        formula.dataset.source = text(d.latex);
        try {
          formula.innerHTML = katex.renderToString(text(d.latex), {
            displayMode: d.display !== false,
            throwOnError: true,
            trust: false,
            maxExpand: 1000,
            maxSize: 20,
          });
        } catch {
          formula.append(element("pre", text(d.latex)));
          warn("An invalid equation is printed as preserved source.");
        }
        out.append(formula);
        break;
      }
      case "mermaid": {
        const figure = element("figure", undefined, "diagram"),
          fallback = element("pre", text(d.source));
        figure.append(fallback);
        out.append(figure);
        jobs.push(
          mermaid
            .render("printdiagram" + ++diagramSequence, text(d.source))
            .then(({ svg }) => {
              figure.innerHTML = svg;
              const svgNode = figure.querySelector("svg");
              if (svgNode) {
                svgNode.removeAttribute("width");
                svgNode.removeAttribute("height");
              }
            })
            .catch(() =>
              warn("An invalid diagram is printed as preserved source."),
            ),
        );
        break;
      }
      case "table": {
        const table = element("table"),
          head = element("thead"),
          body = element("tbody");
        for (let row = 0; row < Math.min(1000, Number(d.rowCount)); row++) {
          const tr = element("tr");
          for (const cell of (d.cells ?? [])
            .filter((c: any) => c.row === row)
            .sort((a: any, b: any) => a.column - b.column)) {
            const c = document.createElement(
              row < d.headerRows || cell.column < d.headerColumns ? "th" : "td",
            );
            c.rowSpan = cell.rowSpan;
            c.colSpan = cell.colSpan;
            c.append(rich(cell.richText));
            tr.append(c);
          }
          (row < d.headerRows ? head : body).append(tr);
        }
        if (head.childNodes.length) table.append(head);
        table.append(body);
        out.append(table);
        break;
      }
      case "columns":
      case "tab_container": {
        out.classList.add(b.type === "columns" ? "columns" : "tabs");
        for (const group of d[b.type === "columns" ? "columns" : "tabs"] ??
          []) {
          const column = element("div", undefined, "layout-group");
          if (b.type === "columns")
            column.style.flex = String(
              Math.max(0.1, Math.min(10, Number(group.widthRatio) || 1)),
            );
          else column.append(element("h3", text(group.title) || "Tab"));
          for (const id of group.childBlockIds ?? []) {
            const child = blocks.get(id);
            if (child) column.append(renderBlock(child, next, copy));
            else
              column.append(
                element("p", "Unavailable child content", "fallback"),
              );
          }
          out.append(column);
        }
        break;
      }
      case "synced_block": {
        const source = blocks.get(text(d.contentSourceId));
        out.classList.add("reusable");
        if (source) out.append(renderBlock(source, next, true));
        else {
          out.append(
            element(
              "p",
              "Reusable source unavailable: " + text(d.contentSourceId),
              "fallback",
            ),
          );
          warn("An unavailable reusable source is retained as a reference.");
        }
        break;
      }
      case "image": {
        const file = files.get(text(d.fileId)),
          figure = element("figure");
        if (file) {
          const img = document.createElement("img");
          img.src = "/managed/" + file.id;
          img.alt =
            text(d.altText) || file.path.split("/").pop() || "Managed image";
          if (d.width) img.style.width = Math.min(2000, Number(d.width)) + "px";
          figure.classList.add(
            "align-" +
              (["left", "right", "center"].includes(d.alignment)
                ? d.alignment
                : "center"),
          );
          figure.append(img);
        } else {
          figure.append(
            element("p", "Image unavailable: " + text(d.fileId), "fallback"),
          );
          warn("A missing image is printed with an attachment reference.");
        }
        const caption = element("figcaption");
        caption.append(rich(d.caption));
        figure.append(caption);
        out.append(figure);
        break;
      }
      case "audio":
      case "video":
      case "pdf":
      case "file": {
        const file = files.get(text(d.fileId));
        out.append(
          element(
            "p",
            `${b.type.toUpperCase()} attachment: ${text(d.displayName) || file?.path.split("/").pop() || text(d.fileId)}`,
            "attachment",
          ),
        );
        out.append(rich(d.caption));
        warn(
          "PDF/audio/video/file attachments are listed with captions; original bytes are available through portable export.",
        );
        break;
      }
      case "bookmark": {
        const title = d.cachedSourceUrl === d.url ? text(d.title) : text(d.url);
        const a = element("a", title || text(d.url)) as HTMLAnchorElement;
        a.href = safeLink(text(d.url));
        out.append(a);
        if (d.cachedSourceUrl === d.url && d.description)
          out.append(element("p", text(d.description)));
        out.append(element("small", text(d.url)));
        break;
      }
      case "embed":
        out.append(
          element("p", text(d.offlineText) || "Online embed"),
          element("small", text(d.url)),
        );
        warn(
          "Online embeds use their retained offline text; no online player is fetched.",
        );
        break;
      case "page_mention": {
        const a = element(
          "a",
          text(d.displaySnapshot) || "Page",
        ) as HTMLAnchorElement;
        a.href = "#page-" + text(d.targetPageId);
        out.append(a);
        break;
      }
      case "date_mention":
        out.append(
          element(
            "p",
            text(d.start) +
              (d.end ? " – " + text(d.end) : "") +
              " " +
              text(d.timezone),
          ),
        );
        break;
      case "button":
        out.append(
          element("p", "Action: " + text(d.label) + " (inactive in PDF)"),
        );
        break;
      case "todo":
        out.append(element("p", "Task reference: " + text(d.taskRecordId)));
        warn(
          "Task references are retained; database task values are in the portable export.",
        );
        break;
      case "breadcrumb":
        out.append(
          element(
            "p",
            snapshot.pages.find((p) => p.id === b.pageId)?.canonicalPage
              .title || "Page",
          ),
        );
        break;
      case "table_of_contents": {
        const list = element("ul");
        for (const heading of snapshot.pages.find((p) => p.id === b.pageId)
          ?.canonicalBlocks ?? []) {
          if (
            /^heading_[1-4]$/.test(heading.type) &&
            Number(heading.type.slice(-1)) <= d.maxLevel
          ) {
            const item = element("li"),
              a = element("a") as HTMLAnchorElement;
            a.href = "#block-" + heading.id;
            a.append(rich((heading.data as any).richText));
            item.append(a);
            list.append(item);
          }
        }
        out.append(list);
        break;
      }
      case "unsupported_external":
        out.append(
          element(
            "pre",
            text(d.readableText) || JSON.stringify(d.raw, null, 2),
          ),
        );
        warn("Unsupported content is printed as retained readable evidence.");
        break;
      default:
        out.append(element("pre", JSON.stringify(d, null, 2)));
        warn("Unrecognized blocks are printed as retained canonical evidence.");
    }
    if (!["columns", "tab_container", "synced_block"].includes(b.type))
      addChildren();
    return out;
  };
  mermaid.initialize({
    startOnLoad: false,
    securityLevel: "strict",
    theme: "neutral",
    suppressErrorRendering: true,
    maxTextSize: 100000,
  });
  for (const page of snapshot.pages) {
    const article = element("article");
    article.id = "page-" + page.id;
    article.append(element("h1", page.canonicalPage.title));
    for (const b of order(
      page.canonicalBlocks.filter(
        (b) => !b.parentBlockId || !blocks.has(b.parentBlockId),
      ),
    ))
      article.append(renderBlock(b));
    host.append(article);
  }
  if (!snapshot.pages.length)
    host.append(element("p", "This workspace has no active pages."));
  await Promise.all(jobs);
  await Promise.all(
    [...document.images].map(async (img) => {
      try {
        await img.decode();
        if (img.naturalWidth * img.naturalHeight > 64_000_000)
          throw Error("oversized");
      } catch {
        const fallback = element(
          "p",
          "Image could not be rendered: " + img.alt,
          "fallback",
        );
        img.replaceWith(fallback);
        warn(
          "An unsupported, missing or oversized image is printed as a readable fallback.",
        );
      }
    }),
  );
  await document.fonts.ready;
  // WebKit's PDF print operation does not repeat table headers. Materialize
  // bounded row groups with cloned headers, without cutting across row spans.
  for (const table of [...host.querySelectorAll("table")]) {
    if (table.getBoundingClientRect().height <= 850) continue;
    if (
      table.tHead &&
      [...table.tHead.rows].some((row, index) =>
        [...row.cells].some(
          (cell) => index + cell.rowSpan > table.tHead!.rows.length,
        ),
      )
    ) {
      warn(
        "A table with a header merge spanning into body rows remains continuous to preserve its cells.",
      );
      continue;
    }
    const rows = [...table.tBodies[0].rows],
      headHeight = table.tHead?.getBoundingClientRect().height ?? 0;
    let chunk = table.cloneNode(false) as HTMLTableElement,
      body = document.createElement("tbody"),
      height = headHeight,
      end = 0;
    const begin = () => {
      chunk = table.cloneNode(false) as HTMLTableElement;
      chunk.classList.add("table-chunk");
      if (table.tHead) chunk.append(table.tHead.cloneNode(true));
      body = document.createElement("tbody");
      chunk.append(body);
      height = headHeight;
    };
    begin();
    rows.forEach((row, index) => {
      const rowHeight = row.getBoundingClientRect().height;
      if (height + rowHeight > 850 && body.rows.length && index >= end) {
        table.before(chunk);
        begin();
      }
      for (const cell of [...row.cells])
        end = Math.max(end, index + cell.rowSpan);
      body.append(row.cloneNode(true));
      height += rowHeight;
    });
    table.before(chunk);
    table.remove();
  }
  for (const formula of [...host.querySelectorAll<HTMLElement>(".equation")]) {
    const math = formula.querySelector<HTMLElement>(".katex-html");
    if (math && math.scrollWidth > formula.clientWidth) {
      const scale = formula.clientWidth / math.scrollWidth;
      formula.style.fontSize = `${Math.max(0.3, scale)}em`;
      if (scale < 0.3) {
        formula.replaceChildren(element("pre", formula.dataset.source));
        warn(
          "An unusually wide equation is printed as preserved source for readable output.",
        );
      }
    }
  }
  if (warnings.length) {
    const section = element("aside", undefined, "export-notes");
    section.append(element("h2", "Export notes"));
    for (const message of warnings) section.append(element("p", message));
    host.append(section);
  }
  window.trovePrintStatus.ready = true;
}
fetch("/snapshot.json", { credentials: "omit", cache: "no-store" })
  .then((r) => {
    if (!r.ok) throw Error("Snapshot unavailable");
    return r.json();
  })
  .then(render)
  .catch((e) => {
    window.trovePrintStatus.error = String(e);
    document.getElementById("document")!.textContent =
      "PDF preparation failed.";
  });
