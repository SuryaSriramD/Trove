export type DeepLink = { pageId: string; blockId?: string; workspaceId?: string };
const uuid = "[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}";
const path = new RegExp(`^/(${uuid})(?:/block/(${uuid}))?$`);
const id = new RegExp(`^${uuid}$`);

// Deep links only name canonical identities. They never select a filesystem path
// or implicitly open a different workspace.
export function parseDeepLink(value: string): DeepLink {
  if (value.length > 512) throw new Error("This Trove link is too long.");
  let url: URL;
  try { url = new URL(value); } catch { throw new Error("This Trove link is invalid."); }
  const match = path.exec(url.pathname);
  if (url.protocol !== "trove:" || url.hostname !== "page" || url.port ||
      url.username || url.password || !match ||
      (url.hash && (match[2] || !id.test(url.hash.slice(1)))) ||
      [...url.searchParams.keys()].some(key => key !== "workspace") ||
      url.searchParams.getAll("workspace").length > 1) {
    throw new Error("This Trove link is invalid.");
  }
  const workspaceId = url.searchParams.get("workspace");
  if (workspaceId !== null && !id.test(workspaceId)) throw new Error("This Trove workspace link is invalid.");
  const blockId = match[2] || url.hash.slice(1);
  return { pageId: match[1], ...(blockId ? { blockId } : {}),
    ...(workspaceId ? { workspaceId } : {}) };
}
