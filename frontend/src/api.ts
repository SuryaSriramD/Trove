import type * as D from "../../contracts/generated/domain";
import type * as S from "../../contracts/generated/service";
export type { D, S };
export type WorkspaceSnapshot = D.DomainSnapshot & {
  scopeGrantId: string;
  snapshotCursor: string;
};
declare global {
  interface Window {
    go?: {
      main: {
        App: {
          Dispatch(method: string, raw: string): Promise<string>;
          PickFile(): Promise<string>;
          ExportPortable?(): Promise<string>;
          PickModelPack?(packId: string): Promise<void>;
          SetEditing?(active: boolean): Promise<void>;
          ShellReady?(): Promise<void>;
          PendingDeepLink?(): Promise<string>;
          StageUpload?(
            name: string,
            mediaType: string,
            base64: string,
          ): Promise<string>;
          OpenWorkspace?(): Promise<string>;
          CreateWorkspace?(): Promise<string>;
        };
      };
    };
    runtime?: {
      EventsOn(
        event: string,
        callback: (...args: unknown[]) => void,
      ): () => void;
    };
  }
}
export class ServiceError extends Error {
  constructor(public detail: S.Error) {
    super(detail.message);
    this.name = detail.code;
  }
}
// Request identities are transport correlation IDs. Content IDs are allocated by the kernel.
export function requestID() {
  const a = crypto.getRandomValues(new Uint8Array(16));
  const t = BigInt(Date.now());
  for (let i = 0; i < 6; i++) a[5 - i] = Number((t >> BigInt(i * 8)) & 255n);
  a[6] = (a[6] & 15) | 112;
  a[8] = (a[8] & 63) | 128;
  const s = Array.from(a, (b) => b.toString(16).padStart(2, "0")).join("");
  return `${s.slice(0, 8)}-${s.slice(8, 12)}-${s.slice(12, 16)}-${s.slice(16, 20)}-${s.slice(20)}`;
}
export async function dispatch<T>(
  method: string,
  request: unknown = {},
): Promise<T> {
  const bridge = window.go?.main.App;
  if (!bridge)
    throw new Error(
      "Open Trove in the desktop application to connect to a workspace.",
    );
  const response = JSON.parse(
    await bridge.Dispatch(method, JSON.stringify(request)),
  );
  if (response.ok === false) throw new ServiceError(response.error);
  return response.result ?? response;
}
export const snapshot = () => dispatch<WorkspaceSnapshot>("workspace.snapshot");
export function envelope(w: WorkspaceSnapshot) {
  return {
    contractVersion: 1 as const,
    requestId: requestID(),
    workspaceId: w.workspaceId,
  };
}
export async function ids(w: WorkspaceSnapshot, count: number) {
  return (
    await dispatch<S.AllocateIdsResult>("allocateIds", {
      ...envelope(w),
      count,
    })
  ).ids;
}
export async function batch(
  w: WorkspaceSnapshot,
  commands: S.Command[],
  expected: S.ExpectedRevision[],
) {
  return dispatch<S.BatchResult>("batch", {
    ...envelope(w),
    mutation: {
      idempotencyKey: requestID(),
      scopeGrantId: w.scopeGrantId,
      expectedRevisions: expected,
    },
    commands,
  });
}
export const expectedPage = (p: D.Page): S.ExpectedRevision => ({
  target: { entityType: "page", entityId: p.id },
  revision: p.revision,
});
export function pageGuards(
  w: WorkspaceSnapshot,
  p: D.Page,
): S.ExpectedRevision[] {
  const guards = [expectedPage(p)];
  const r = w.records.find((r) => r.pageId === p.id);
  if (r) {
    guards.push({
      target: { entityType: "record", entityId: r.id },
      revision: r.revision,
    });
    const c = w.collections.find((c) => c.id === r.collectionId);
    if (c)
      guards.push({
        target: { entityType: "collection", entityId: c.id },
        revision: c.revision,
      });
  }
  return guards;
}
export function fileGuard(
  w: WorkspaceSnapshot,
  fileId: string,
): S.ExpectedRevision {
  return {
    target: { entityType: "file", entityId: fileId },
    revision: w.files.find((f) => f.id === fileId)?.revision ?? "0",
  };
}
export function fileURL(fileId: string) {
  return `/files/${encodeURIComponent(fileId)}`;
}
export async function attachFile(w: WorkspaceSnapshot, page: D.Page) {
  const selectionHandle = await window.go!.main.App.PickFile();
  if (!selectionHandle) return;
  const file = await dispatch<S.PrepareFileResult>("prepareFile", {
    ...envelope(w),
    selectionHandle,
  });
  const [blockId] = await ids(w, 1);
  const kind = file.mediaType.startsWith("image/")
    ? "image"
    : file.mediaType.startsWith("audio/")
      ? "audio"
      : file.mediaType.startsWith("video/")
        ? "video"
        : file.mediaType === "application/pdf"
          ? "pdf"
          : "file";
  await batch(
    w,
    [
      {
        kind: "file.attach",
        fileId: file.fileId,
        ingestHandle: file.ingestHandle,
        pageId: page.id,
      },
      {
        kind: "block.insert",
        block: {
          blockId,
          pageId: page.id,
          type: kind,
          schemaVersion: 1,
          data: { fileId: file.fileId, caption: [] },
        },
      },
    ],
    [
      ...pageGuards(w, page),
      fileGuard(w, file.fileId),
      { target: { entityType: "block", entityId: blockId }, revision: "0" },
    ],
  );
  return file;
}
