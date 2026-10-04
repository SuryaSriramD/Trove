import { useEffect, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";
import type * as S from "../../../contracts/generated/service";

const pending = (profile: S.ProviderClaudeProfile) =>
  ["waiting", "running", "cancellation_requested"].includes(
    profile.attempt?.state ?? "",
  );

/** The official vendor terminal owns every login code, token and credential. */
export function ClaudeAccounts({
  workspace,
  runtimeAvailable,
  selectedProfileId,
  onSelect,
}: {
  workspace: WorkspaceSnapshot;
  runtimeAvailable: boolean;
  selectedProfileId: string;
  onSelect: (profileId: string, billingMode: string) => void;
}) {
  const [profiles, setProfiles] = useState<S.ProviderClaudeProfile[]>([]);
  const [availability, setAvailability] = useState<string | null>(null);
  const [label, setLabel] = useState("");
  const [billing, setBilling] = useState<
    "claude_subscription" | "anthropic_api"
  >("claude_subscription");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const active = profiles.some(pending);
  async function load() {
    const result = await dispatch<S.ProviderClaudeListResult>(
      "providers.claude.list",
      envelope(workspace),
    );
    setProfiles(result.profiles);
    setAvailability(result.available ? "" : result.reason);
  }
  useEffect(() => {
    let stopped = false;
    void dispatch<S.ProviderClaudeListResult>(
      "providers.claude.list",
      envelope(workspace),
    )
      .then((result) => {
        if (!stopped) {
          setProfiles(result.profiles);
          setAvailability(result.available ? "" : result.reason);
        }
      })
      .catch((e) => {
        if (!stopped) setError(String(e));
      });
    return () => {
      stopped = true;
    };
  }, [workspace.workspaceId]);
  useEffect(() => {
    if (!active) return;
    let stopped = false;
    const timer = setInterval(() => {
      void dispatch<S.ProviderClaudeListResult>(
        "providers.claude.list",
        envelope(workspace),
      )
        .then((result) => {
          if (!stopped) {
            setProfiles(result.profiles);
            setAvailability(result.available ? "" : result.reason);
          }
        })
        .catch((e) => {
          if (!stopped) {
            setError(String(e));
            clearInterval(timer);
          }
        });
    }, 1000);
    return () => {
      stopped = true;
      clearInterval(timer);
    };
  }, [active, workspace.workspaceId]);
  async function act(action: () => Promise<void>) {
    setBusy(true);
    setError("");
    try {
      await action();
      await load();
    } catch (e) {
      setError(String(e));
      try {
        await load();
      } catch {
        /* Preserve operation failure. */
      }
    } finally {
      setBusy(false);
    }
  }
  const disabled = busy || availability !== "" || !runtimeAvailable;
  async function profileAction(
    method: string,
    profile: S.ProviderClaudeProfile,
  ) {
    if (
      selectedProfileId === profile.id &&
      method !== "providers.claude.cancel"
    )
      onSelect("", "");
    await dispatch(method, { ...envelope(workspace), profileId: profile.id });
  }
  return (
    <section aria-label="Claude accounts">
      <h3>Claude accounts</h3>
      <p>
        Each profile uses private vendor-owned state. Sign-in opens the official
        Claude Code flow in Terminal. Enter any browser code only in that
        terminal. Trove never reads terminal input or output.
      </p>
      <p>
        Choose the billing route explicitly. Claude subscriptions and Anthropic
        API billing remain separate; sign-in does not start an AI request.
      </p>
      {availability === null ? (
        <p>Checking private profile storage…</p>
      ) : availability ? (
        <p role="status">{availability}</p>
      ) : null}
      {!runtimeAvailable && (
        <p role="status">
          Native Claude startup is unavailable. Existing profiles are read-only
          until a compatible runtime passes the startup check.
        </p>
      )}
      <div>
        <label>
          Claude profile label{" "}
          <input
            value={label}
            maxLength={120}
            disabled={disabled || active}
            onChange={(e) => setLabel(e.target.value)}
          />
        </label>
        <label>
          Claude billing route{" "}
          <select
            value={billing}
            disabled={disabled || active}
            onChange={(e) => setBilling(e.target.value as typeof billing)}
          >
            <option value="claude_subscription">Claude subscription</option>
            <option value="anthropic_api">Anthropic API billing</option>
          </select>
        </label>
        <button
          disabled={disabled || active || !label.trim()}
          onClick={() =>
            void act(async () => {
              await dispatch("providers.claude.create", {
                ...envelope(workspace),
                label: label.trim(),
                billingMode: billing,
              });
              setLabel("");
            })
          }
        >
          Create Claude profile
        </button>
      </div>
      {profiles.length === 0 && availability === "" && (
        <p>No saved Claude profiles.</p>
      )}
      {profiles.map((profile) => (
        <article key={profile.id}>
          <strong>
            {profile.label} ·{" "}
            {profile.billingMode === "claude_subscription"
              ? "Claude subscription"
              : "Anthropic API billing"}
          </strong>
          <p>
            {profile.auth.loggedIn
              ? `Signed in${profile.auth.accountLabel ? ` as ${profile.auth.accountLabel}` : ""}`
              : profile.auth.method === "none"
                ? "Signed out"
                : "Status not checked"}
            {selectedProfileId === profile.id ? " · Selected profile" : ""}
          </p>
          {profile.auth.reason && <p role="status">{profile.auth.reason}</p>}
          {profile.attempt && <p role="status">{profile.attempt.message}</p>}
          <button
            disabled={
              disabled ||
              active ||
              !profile.auth.loggedIn ||
              !!profile.auth.reason ||
              profile.auth.billingMode !== profile.billingMode
            }
            onClick={() => onSelect(profile.id, profile.billingMode)}
          >
            Use {profile.label}
          </button>
          <button
            disabled={disabled || active}
            onClick={() =>
              void act(() => profileAction("providers.claude.status", profile))
            }
          >
            Refresh {profile.label} status
          </button>
          <button
            disabled={disabled || active || profile.signOutIncomplete}
            onClick={() =>
              void act(() => profileAction("providers.claude.login", profile))
            }
          >
            Sign in to {profile.label} in Terminal
          </button>
          <button
            disabled={disabled || active}
            onClick={() =>
              void act(() => profileAction("providers.claude.logout", profile))
            }
          >
            Sign out of {profile.label}
          </button>
          {pending(profile) && (
            <button
              disabled={
                busy || profile.attempt?.state === "cancellation_requested"
              }
              onClick={() =>
                void act(() =>
                  profileAction("providers.claude.cancel", profile),
                )
              }
            >
              Cancel {profile.label} sign-in
            </button>
          )}
        </article>
      ))}
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
