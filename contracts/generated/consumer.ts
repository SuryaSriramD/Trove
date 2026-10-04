// A consumer compiled by the Phase 0 gate; examples do not require an app runtime.
import type { BatchRequest, Command, BatchResponse, SearchHit } from './service';
import type { WorkerOutput } from './jobs';
export const request: BatchRequest = {
  contractVersion: 1, requestId: '0199a000-0000-7000-8000-000000000003',
  workspaceId: '0199a000-0000-7000-8000-000000000001',
  mutation: { idempotencyKey: 'capture-1', scopeGrantId: '0199a000-0000-7000-8000-000000000009', expectedRevisions: [] },
  commands: [{ kind: 'page.create', pageId: '0199a000-0000-7000-8000-000000000002', title: 'Meeting notes' }]
};
export function title(command: Command): string | undefined {
  return command.kind === 'page.create' ? command.title : undefined;
}
export function status(response: BatchResponse): string {
  return response.ok ? response.result.eventCursor : response.error.code;
}
export function token(event: WorkerOutput): string | undefined {
  return event.kind === 'token' ? event.text : undefined;
}
export function seek(hit: SearchHit): number | undefined { return hit.location.transcript?.startMs; }
