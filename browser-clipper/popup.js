import { extract } from "./extract.js";
const $ = (id) => document.getElementById(id);
let pairing = null,
  draft = null,
  destinations = [],
  busy = false;
const status = (s) => {
  $("status").textContent = s;
  $("error").textContent = "";
};
const report = (e) => {
  $("error").textContent = String(e.message || e);
};
function endpoint(value) {
  const u = new URL(value);
  if (
    u.protocol !== "http:" ||
    u.hostname !== "127.0.0.1" ||
    !u.port ||
    u.username ||
    u.password ||
    u.pathname !== "/" ||
    u.search ||
    u.hash
  )
    throw Error("Use the exact loopback endpoint shown in Trove.");
  return u.origin;
}
async function call(path, body, credentials = pairing) {
  if (!credentials) throw Error("Pair with Trove first.");
  let response;
  try {
    response = await fetch(endpoint(credentials.endpoint) + path, {
      method: "POST",
      mode: "cors",
      headers: {
        "Content-Type": "application/json",
        ...(credentials.token
          ? { Authorization: "Bearer " + credentials.token }
          : {}),
      },
      body: JSON.stringify(body),
      credentials: "omit",
      redirect: "error",
      cache: "no-store",
      signal: AbortSignal.timeout(30000),
    });
  } catch {
    throw Error(
      "Trove is unavailable or this session was revoked. Your local draft is retained; open Trove and pair again.",
    );
  }
  const data = await response.json();
  if (!response.ok || !data.ok)
    throw Error(data.error || "Trove rejected this request.");
  return data;
}
async function act(fn) {
  if (busy) return;
  busy = true;
  for (const button of document.querySelectorAll("button"))
    button.disabled = true;
  try {
    await fn();
  } catch (e) {
    report(e);
  } finally {
    busy = false;
    for (const button of document.querySelectorAll("button"))
      button.disabled = false;
  }
}
function showDraft() {
  const exists = !!draft;
  $("title").value = draft?.title || "";
  $("source").textContent = draft?.url || "";
  $("preview").textContent = draft?.text?.slice(0, 1200) || draft?.url || "";
  $("retry").style.display = exists ? "inline-block" : "none";
  $("discard").style.display = exists ? "inline-block" : "none";
  $("images").replaceChildren();
  for (const url of draft?.imageURLs || []) {
    const label = document.createElement("label"),
      check = document.createElement("input");
    check.type = "checkbox";
    check.value = url;
    label.append(check, document.createTextNode(url));
    $("images").append(label);
  }
  $("image-choices").hidden = !exists || !draft.imageURLs?.length;
}
async function keepDraft() {
  if (draft) {
    if (
      new TextEncoder().encode(JSON.stringify(draft)).length >
      4 * 1024 * 1024
    )
      throw Error(
        "Draft exceeds 4 MiB. Discard it and capture a smaller selection.",
      );
    await chrome.storage.local.set({ draft });
  } else await chrome.storage.local.remove("draft");
  showDraft();
}
async function refresh() {
  const data = await call("/destinations", { version: 1 });
  destinations = data.destinations;
  $("destination").replaceChildren(
    ...destinations.map((d) => {
      const option = document.createElement("option");
      option.value = d.id;
      option.textContent = d.title;
      return option;
    }),
  );
  if (
    draft?.destinationId &&
    destinations.some((d) => d.id === draft.destinationId)
  )
    $("destination").value = draft.destinationId;
}
$("extension-id").textContent = chrome.runtime.id;
$("pair").onclick = () =>
  act(async () => {
    const target = endpoint($("endpoint").value.trim()),
      data = await call(
        "/pair",
        { version: 1, code: $("pair-code").value.trim() },
        { endpoint: target },
      );
    pairing = { endpoint: target, token: data.token };
    await chrome.storage.session.set({ pairing });
    $("pair-code").value = "";
    $("pairing").open = false;
    await refresh();
    status(
      "Paired for this Trove session. Captures still require final review in the app.",
    );
  });
$("destinations").onclick = () => act(refresh);
$("capture").onclick = () =>
  act(async () => {
    if (draft)
      throw Error(
        "Send or discard the retained draft before reading another page.",
      );
    const [tab] = await chrome.tabs.query({
      active: true,
      currentWindow: true,
    });
    if (!tab?.id) throw Error("No active page");
    const mode = $("mode").value;
    const results = await chrome.scripting.executeScript({
      target: { tabId: tab.id },
      func: extract,
      args: [mode],
    });
    const source = results[0]?.result;
    if (!source) throw Error("The page did not provide a capture.");
    draft = {
      ...source,
      version: 1,
      mode,
      requestId: crypto.randomUUID(),
      capturedAt: new Date().toISOString(),
      resourceConsent: false,
      images: [],
    };
    await keepDraft();
    status(
      "Content retained locally. Choose a destination and send it for review.",
    );
  });
function imageOrigin(raw) {
  const u = new URL(raw),
    h = u.hostname.toLowerCase();
  if (
    !["http:", "https:"].includes(u.protocol) ||
    u.username ||
    u.password ||
    (u.port && !["80", "443"].includes(u.port)) ||
    h === "localhost" ||
    h.endsWith(".localhost") ||
    h.endsWith(".local") ||
    !h.includes(".") ||
    /^\d+\.\d+\.\d+\.\d+$/.test(h) ||
    h.includes(":")
  )
    throw Error("Local or literal-IP image sources are not acquired.");
  return u.origin + "/*";
}
$("acquire").onclick = () =>
  act(async () => {
    if (!draft) throw Error("Read a page first.");
    const selected = [...$("images").querySelectorAll("input:checked")].map(
      (n) => n.value,
    );
    if (!selected.length) throw Error("Select the image URLs to acquire.");
    const origins = [...new Set(selected.map(imageOrigin))];
    if (!(await chrome.permissions.request({ origins })))
      throw Error(
        "Image origin access was not granted; the draft stays available without those bytes.",
      );
    try {
      const acquired = [];
      let total = 0;
      for (const url of selected) {
        const response = await fetch(url, {
          credentials: "omit",
          redirect: "error",
          cache: "no-store",
          referrerPolicy: "no-referrer",
          signal: AbortSignal.timeout(10000),
        });
        if (!response.ok || !response.body)
          throw Error("An image was unavailable; no new image set was saved.");
        const mediaType = (response.headers.get("content-type") || "").split(
          ";",
        )[0];
        if (!["image/png", "image/jpeg", "image/gif"].includes(mediaType))
          throw Error("Only PNG, JPEG and GIF bytes can be retained.");
        const reader = response.body.getReader(),
          chunks = [];
        let size = 0;
        while (true) {
          const { value, done } = await reader.read();
          if (done) break;
          size += value.length;
          total += value.length;
          if (total > 2 * 1024 * 1024) {
            await reader.cancel();
            throw Error(
              "Selected images exceed 2 MiB total. Choose fewer images.",
            );
          }
          chunks.push(value);
        }
        const bytes = new Uint8Array(size);
        let offset = 0;
        for (const chunk of chunks) {
          bytes.set(chunk, offset);
          offset += chunk.length;
        }
        let binary = "";
        for (let offset = 0; offset < bytes.length; offset += 16384)
          binary += String.fromCharCode(
            ...bytes.subarray(offset, offset + 16384),
          );
        acquired.push({ url, mediaType, base64: btoa(binary) });
      }
      draft.images = acquired;
      draft.resourceConsent = true;
      await keepDraft();
      status(
        `Retained ${acquired.length} explicitly acquired image(s) for offline use.`,
      );
    } finally {
      await chrome.permissions.remove({ origins });
    }
  });
async function send() {
  if (!draft) throw Error("Read a page first.");
  const destination = destinations.find((d) => d.id === $("destination").value);
  if (!destination) throw Error("Refresh and select an approved destination.");
  draft.title = $("title").value;
  draft.destinationId = destination.id;
  draft.destinationRevision = destination.revision;
  await keepDraft();
  const { imageURLs, omittedImageCount, ...request } = draft;
  const data = await call("/capture", request);
  draft = null;
  await keepDraft();
  status(
    `Draft received: ${data.receipt.title}. Open Trove → Capture → Browser drafts to review and save.`,
  );
}
$("send").onclick = () => act(send);
$("retry").onclick = () =>
  act(async () => {
    await refresh();
    await send();
  });
$("discard").onclick = () =>
  act(async () => {
    draft = null;
    await keepDraft();
    status("Local draft discarded. Existing Trove captures are unchanged.");
  });
$("forget").onclick = () =>
  act(async () => {
    pairing = null;
    await chrome.storage.session.remove("pairing");
    $("pairing").open = true;
    status(
      "Browser credential removed. Revoke in Trove to close its listener. Retained drafts stay local.",
    );
  });
(async () => {
  const session = await chrome.storage.session.get("pairing"),
    local = await chrome.storage.local.get("draft");
  pairing = session.pairing || null;
  draft = local.draft || null;
  showDraft();
  if (pairing) {
    $("endpoint").value = pairing.endpoint;
    $("pairing").open = false;
    try {
      await refresh();
    } catch (e) {
      report(e);
    }
  }
  if (draft)
    status(
      "A draft is retained locally. Pair or retry when Trove is available.",
    );
})().catch(report);
