import { useEffect, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";
type Pairing = {
  endpoint: string;
  code: string;
  extensionId: string;
  expiresAt: string;
  sessionOnly: boolean;
};
type Plan = {
  planId: string;
  planHash: string;
  sourceName: string;
  state: string;
  rootPageId: string;
  captureDestinationTitle: string;
  sourceOmissions: string[];
  unknownCompleteness: string[];
  items: {
    sourceLocator: string;
    outcome: string;
    reason: string;
    pageId?: string;
    kind?: string;
    committed: boolean;
    blocks?: { type: string; data: Record<string, unknown> }[];
  }[];
};
type Bridge = {
  RefreshClipperDestination: (id: string, hash: string) => Promise<string>;
  ShowClipperExtension: () => Promise<void>;
  BeginClipperPairing: (
    extension: string,
    destinations: string[],
  ) => Promise<string>;
  RevokeClipperPairing: () => Promise<void>;
  ClipperStatus: () => Promise<string>;
  ClipperReviewPlans: () => Promise<string>;
};
export function ClipperPanel({
  workspace,
  onChanged,
  onOpenPage,
}: {
  workspace: WorkspaceSnapshot;
  onChanged: () => Promise<void> | void;
  onOpenPage?: (id: string) => void;
}) {
  const [extension, setExtension] = useState(""),
    [inbox, setInbox] = useState(true),
    [destination, setDestination] = useState(""),
    [pairing, setPairing] = useState<Pairing | null>(null),
    [paired, setPaired] = useState(false),
    [listening, setListening] = useState(false),
    [plans, setPlans] = useState<Plan[]>([]),
    [selected, setSelected] = useState<Plan | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState("");
  const native = window.go?.main.App as unknown as Partial<Bridge> | undefined;
  const supported = !!native?.BeginClipperPairing;
  const refresh = async () => {
    if (!supported) return;
    const [status, items] = await Promise.all([
      native!.ClipperStatus!(),
      native!.ClipperReviewPlans!(),
    ]);
    const current = JSON.parse(status);
    setPaired(current.paired);
    setListening(current.listening);
    if (current.paired) setPairing(null);
    setPlans(JSON.parse(items));
  };
  useEffect(() => {
    setPairing(null);
    setSelected(null);
    setPlans([]);
    if (supported) void refresh().catch((e) => setError(String(e)));
    const timer = setInterval(() => {
      if (supported) void refresh().catch((e) => setError(String(e)));
    }, 5000);
    return () => clearInterval(timer);
  }, [workspace.workspaceId, supported]);
  const act = async (fn: () => Promise<void>) => {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await fn();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  };
  const begin = () =>
    act(async () => {
      const ids = [
        ...(inbox ? ["inbox"] : []),
        ...(destination ? [destination] : []),
      ];
      setPairing(
        JSON.parse(await native!.BeginClipperPairing!(extension.trim(), ids)),
      );
      await refresh();
    });
  const save = () =>
    act(async () => {
      if (!selected) return;
      let plan = selected;
      let attempts = 0;
      while (plan.state !== "complete") {
        if (++attempts > 64)
          throw Error(
            "Capture save did not complete; refresh the draft before retrying.",
          );
        const previousHash = plan.planHash;
        plan = await dispatch<Plan>("imports.commit", {
          ...envelope(workspace),
          planId: plan.planId,
          planHash: plan.planHash,
        });
        setSelected(plan);
        if (plan.state !== "complete" && plan.planHash === previousHash)
          throw Error(
            "Capture save made no progress; refresh the draft before retrying.",
          );
      }
      await onChanged();
      await refresh();
      setNotice("Reviewed browser capture saved as offline workspace content.");
    });
  return (
    <section aria-label="Browser clipper" className="browser-clipper-panel">
      <h3>Browser clipper</h3>
      {supported && (
        <button
          disabled={busy}
          onClick={() =>
            act(async () => {
              await native!.ShowClipperExtension!();
            })
          }
        >
          Show bundled extension folder
        </button>
      )}
      <p>
        Install the bundled Trove Web Clipper extension in Chrome or a
        compatible Chromium browser, then approve its ID and destinations.
        Pairing lasts for this open app/workspace session. Captures remain
        drafts until you review and save them here.
      </p>
      {!supported ? (
        <p>Pairing requires the native desktop app.</p>
      ) : (
        <>
          <div className="import-controls">
            <label>
              Extension ID
              <input
                aria-label="Clipper extension ID"
                value={extension}
                onChange={(e) => setExtension(e.target.value)}
                placeholder="32 letters from the extension popup"
              />
            </label>
            <label>
              <input
                type="checkbox"
                checked={inbox}
                onChange={(e) => setInbox(e.target.checked)}
              />
              Allow Inbox
            </label>
            <label>
              Additional approved page
              <select
                aria-label="Clipper approved page"
                value={destination}
                onChange={(e) => setDestination(e.target.value)}
              >
                <option value="">None</option>
                {workspace.pages
                  .filter((p) => !p.deletedAt)
                  .map((p) => (
                    <option key={p.id} value={p.id}>
                      {p.title}
                    </option>
                  ))}
              </select>
            </label>
            <button
              disabled={
                busy ||
                !/^[a-p]{32}$/.test(extension.trim()) ||
                (!inbox && !destination)
              }
              onClick={begin}
            >
              Create one-time pairing code
            </button>
          </div>
          {pairing && (
            <div
              className="import-conflict"
              aria-label="Clipper pairing details"
            >
              <p>
                Paste these into the extension. The code expires at{" "}
                {new Date(pairing.expiresAt).toLocaleTimeString()} and is usable
                once.
              </p>
              <label>
                Local endpoint
                <input
                  aria-label="Clipper local endpoint"
                  readOnly
                  value={pairing.endpoint}
                />
              </label>
              <label>
                Pairing code
                <input
                  aria-label="Clipper pairing code"
                  readOnly
                  value={pairing.code}
                />
              </label>
            </div>
          )}
          <p role="status">
            {paired
              ? "Browser paired for this session."
              : listening
                ? "Waiting for the approved extension to pair."
                : "Browser clipper is stopped."}
          </p>
          <button
            disabled={busy || !listening}
            onClick={() =>
              act(async () => {
                await native!.RevokeClipperPairing!();
                setPairing(null);
                await refresh();
                setNotice(
                  "Pairing revoked and listener closed. Existing drafts remain available for review.",
                );
              })
            }
          >
            Revoke browser pairing
          </button>
          <button disabled={busy} onClick={() => act(refresh)}>
            Refresh browser drafts
          </button>
        </>
      )}
      <h4>Browser drafts</h4>
      {!plans.length ? (
        <p>No browser capture drafts yet.</p>
      ) : (
        <ul>
          {plans.map((plan) => (
            <li key={plan.planId}>
              <button disabled={busy} onClick={() => setSelected(plan)}>
                {plan.sourceName}
              </button>{" "}
              · {plan.captureDestinationTitle || "Inbox"} ·{" "}
              {plan.state === "complete" ? "Saved" : "Awaiting review"}
            </li>
          ))}
        </ul>
      )}
      {selected && (
        <section aria-label="Browser capture review">
          <h4>Review {selected.sourceName}</h4>
          <p>
            Destination: {selected.captureDestinationTitle || "Inbox"}. The
            capture becomes a new child clip; existing page content is
            preserved.
          </p>
          <pre
            style={{ whiteSpace: "pre-wrap", maxHeight: 260, overflow: "auto" }}
          >
            {selected.items
              .flatMap((item) =>
                (item.blocks ?? []).map((block) => {
                  const data = block.data;
                  return Array.isArray(data.richText)
                    ? data.richText.map((span: any) => span.text ?? "").join("")
                    : typeof data.readableText === "string"
                      ? data.readableText
                      : typeof data.source === "string"
                        ? data.source
                        : `[${block.type}]`;
                }),
              )
              .join("\n")
              .slice(0, 12000)}
          </pre>
          <ul>
            {selected.items.map((item, i) => (
              <li key={i}>
                {item.sourceLocator} · {item.outcome}: {item.reason}
              </li>
            ))}
          </ul>
          {selected.sourceOmissions?.map((item, i) => (
            <p key={i}>{item}</p>
          ))}
          {selected.unknownCompleteness?.map((item, i) => (
            <p key={i}>{item}</p>
          ))}
          {selected.state !== "complete" ? (
            <>
              <button
                disabled={busy}
                onClick={() =>
                  act(async () => {
                    setSelected(
                      JSON.parse(
                        await native!.RefreshClipperDestination!(
                          selected.planId,
                          selected.planHash,
                        ),
                      ),
                    );
                    setNotice(
                      "Destination refreshed. Review again before saving.",
                    );
                  })
                }
              >
                Refresh destination for review
              </button>
              <button disabled={busy} onClick={save}>
                Save reviewed browser capture
              </button>
            </>
          ) : (
            <button
              onClick={() =>
                onOpenPage?.(
                  selected.items.find((item) => item.committed && item.pageId)
                    ?.pageId || selected.rootPageId,
                )
              }
            >
              Open saved browser capture
            </button>
          )}
        </section>
      )}
      {error && (
        <p role="alert" className="import-error">
          {error}
        </p>
      )}
      {notice && <p role="status">{notice}</p>}
    </section>
  );
}
