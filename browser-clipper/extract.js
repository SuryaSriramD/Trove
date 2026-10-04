// Injected only into the active tab after the toolbar action. All source DOM is
// untrusted; this returns a bounded clone and never changes the original page.
export function extract(mode) {
  if (!/^https?:$/.test(location.protocol))
    throw Error(
      "Choose an HTTP(S) page. Browser settings and local files are not captured.",
    );
  const source = location.href,
    title = document.title || location.hostname;
  if (mode === "url")
    return { title, url: source, html: "", text: source, imageURLs: [] };
  let root;
  if (mode === "selection") {
    const selection = window.getSelection();
    if (!selection || selection.isCollapsed || !selection.rangeCount)
      throw Error("Select some page content first.");
    root = document.createElement("div");
    root.append(selection.getRangeAt(0).cloneContents());
  } else
    root = (
      document.querySelector('article,main,[role="main"]') || document.body
    ).cloneNode(true);
  root
    .querySelectorAll(
      "script,style,iframe,frame,object,embed,form,input,button,textarea,select,nav,header,footer,noscript,template,canvas,svg,video,audio",
    )
    .forEach((n) => n.remove());
  const imageURLs = [];
  for (const node of root.querySelectorAll("*")) {
    const href = node.tagName === "A" ? node.getAttribute("href") : null,
      src = node.tagName === "IMG" ? node.getAttribute("src") : null;
    for (const attr of [...node.attributes])
      if (!["alt", "title", "colspan", "rowspan"].includes(attr.name))
        node.removeAttribute(attr.name);
    if (href) {
      try {
        const url = new URL(href, source);
        if (
          ["https:", "http:", "mailto:"].includes(url.protocol) &&
          !url.username &&
          !url.password
        )
          node.setAttribute("href", url.href);
      } catch {}
    }
    if (src) {
      try {
        const url = new URL(src, source);
        if (
          ["https:", "http:"].includes(url.protocol) &&
          !url.username &&
          !url.password
        ) {
          node.setAttribute("src", url.href);
          if (!imageURLs.includes(url.href)) imageURLs.push(url.href);
        }
      } catch {}
    }
  }
  const html = root.innerHTML,
    text = root.textContent || "";
  if (new TextEncoder().encode(html).length > 512 * 1024)
    throw Error("Main content exceeds 512 KiB. Capture a smaller selection.");
  if (!text.trim() && !imageURLs.length)
    throw Error("No readable content was found. Select text or save the URL.");
  return {
    title,
    url: source,
    html,
    text: text.slice(0, 512 * 1024),
    imageURLs: imageURLs.slice(0, 8),
    omittedImageCount: Math.max(0, imageURLs.length - 8),
  };
}
