// Page opening fans out content, review, display, operations, backlinks, AI-run
// metadata and then editor metadata reads. Keep this client's read fan-out below
// the host's four-request gate. Two slots of headroom remain for the host bridge's
// five-second snapshot poll and user writes/uploads/control requests. Those calls
// and other clients bypass this queue, so a host 429 must still remain visible.
const hostedReads = new Set([
  "workspace.snapshot", "readPage", "readBlock", "readRecord",
  "page.freshness", "page.reviewQueue", "pageDisplay.get", "links.backlinks",
  "operations.list", "editor.lock", "editor.references", "editor.templates",
  "editor.template", "editor.button", "models.list", "ai.runs", "ai.run",
  "memory.list", "listMemory", "queryRecords", "databaseDerived", "databaseP2",
  "databaseChart", "recordLayouts.get", "history.list", "history.trash",
  "jobs.list", "getJob", "meetings.drafts", "meetings.extractions",
]);

type PendingRead = { start: () => void };
const cancelled = () => new DOMException("The workspace read was cancelled.", "AbortError");

export class HostReadAdmission {
  private active = 0;
  private pending: PendingRead[] = [];

  run<T>(send: () => Promise<T>, signal?: AbortSignal): Promise<T> {
    if (signal?.aborted) return Promise.reject(cancelled());
    return new Promise<T>((resolve, reject) => {
      let started = false, settled = false;
      const settle = (complete: () => void) => {
        if (!settled) { settled = true; complete(); }
      };
      const abort = () => {
        if (!started) {
          this.pending = this.pending.filter(item => item !== entry);
          signal?.removeEventListener("abort", abort);
        }
        settle(() => reject(cancelled()));
      };
      const entry: PendingRead = { start: () => {
        started = true;
        this.active++;
        void Promise.resolve().then(() => {
          if (signal?.aborted) throw cancelled();
          return send();
        }).then(value => settle(() => resolve(value)), error => settle(() => reject(error))).finally(() => {
          // Dispatch does not expose transport abort. An aborted in-flight read
          // keeps its slot until the real request settles; never over-admit.
          signal?.removeEventListener("abort", abort);
          this.active--;
          while (this.active < 2 && this.pending.length) this.pending.shift()!.start();
        });
      }};
      if (this.active >= 2 && this.pending.length >= 64) {
        reject(new Error("Too many pending workspace reads. Wait for current reads to finish, then retry this view."));
        return;
      }
      signal?.addEventListener("abort", abort, { once: true });
      if (this.active < 2) entry.start();
      else this.pending.push(entry);
    });
  }
}

const admission = new HostReadAdmission();
export function dispatchWithHostReadAdmission<T>(hosted: boolean, method: string, send: () => Promise<T>, signal?: AbortSignal): Promise<T> {
  // Never queue or retry mutations, uploads, inference or cancellation. Keep
  // native Wails dispatch exactly as before, including its failure behavior.
  return hosted && hostedReads.has(method) ? admission.run(send, signal) : send();
}
