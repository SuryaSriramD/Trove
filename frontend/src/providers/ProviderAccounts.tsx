import { useEffect, useState } from "react";
import { dispatch, envelope, type WorkspaceSnapshot } from "../api";
import type * as S from "../../../contracts/generated/service";

/** Account selection is explicit; sign-in never starts a provider run. */
export function ProviderAccounts({
  workspace,
  selectedRegistrationId,
  onSelect,
}: {
  workspace: WorkspaceSnapshot;
  selectedRegistrationId: string;
  onSelect: (registrationId: string) => void;
}) {
  const [accounts, setAccounts] = useState<S.ProviderAuthRegistration[]>([]);
  const [availability, setAvailability] = useState<string | null>(null);
  const [attempt, setAttempt] = useState<S.ProviderAuthAttempt | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  async function load() {
    const result = await dispatch<S.ProviderAuthListResult>(
      "providers.auth.list",
      envelope(workspace),
    );
    setAccounts(result.registrations);
    setAvailability(result.available ? "" : result.reason);
  }
  useEffect(() => {
    void load().catch((e) => setError(String(e)));
  }, [workspace.workspaceId]);
  useEffect(() => {
    if (!attempt || !["waiting", "exchanging"].includes(attempt.state)) return;
    let stopped = false;
    const timer = setInterval(() => {
      void dispatch<S.ProviderAuthAttempt>("providers.auth.attempt", {
        ...envelope(workspace),
        attemptId: attempt.id,
      })
        .then(async (next) => {
          if (stopped) return;
          setAttempt(next);
          if (next.state === "completed") {
            await load();
            setNotice("Signed in. Select the account you want to use below.");
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
  }, [workspace.workspaceId, attempt?.id, attempt?.state]);
  async function act(action: () => Promise<void>) {
    setBusy(true);
    setError("");
    setNotice("");
    try {
      await action();
      await load();
    } catch (e) {
      setError(String(e));
      // A failed local cleanup still disables account access in the host.
      // Refresh that state so the picker cannot present stale usable controls.
      try {
        await load();
      } catch {
        /* Preserve the original operation error. */
      }
    } finally {
      setBusy(false);
    }
  }
  const pending =
    !!attempt && ["waiting", "exchanging"].includes(attempt.state);
  return (
    <section aria-label="ChatGPT accounts">
      <h3>ChatGPT accounts</h3>
      <p>
        Choose a saved account or sign in with ChatGPT. Account credentials stay
        in this device’s protected credential store. ChatGPT plan usage must be
        authorized separately from your identity.
      </p>
      {availability === null ? (
        <p>Checking account storage…</p>
      ) : availability ? (
        <p role="status">{availability}</p>
      ) : (
        <>
          <button
            disabled={busy || pending}
            onClick={() =>
              act(async () =>
                setAttempt(
                  await dispatch<S.ProviderAuthAttempt>(
                    "providers.auth.begin",
                    envelope(workspace),
                  ),
                ),
              )
            }
          >
            Continue with ChatGPT
          </button>
          {attempt && (
            <div role="status">
              <p>{attempt.message}</p>
              {pending && (
                <button
                  disabled={busy}
                  onClick={() =>
                    act(async () => {
                      await dispatch("providers.auth.cancel", {
                        ...envelope(workspace),
                        attemptId: attempt.id,
                      });
                      setAttempt({
                        ...attempt,
                        state: "cancelled",
                        message: "Sign-in cancelled",
                      });
                    })
                  }
                >
                  Cancel sign-in
                </button>
              )}
              {attempt.state === "retry_required" && (
                <button
                  disabled={busy}
                  onClick={() =>
                    act(async () =>
                      setAttempt(
                        await dispatch<S.ProviderAuthAttempt>(
                          "providers.auth.retry",
                          { ...envelope(workspace), attemptId: attempt.id },
                        ),
                      ),
                    )
                  }
                >
                  Retry this sign-in
                </button>
              )}
            </div>
          )}
          {accounts.length === 0 && <p>No saved ChatGPT accounts.</p>}
          {accounts.map((account) => (
            <article key={account.id}>
              <strong>
                {account.identity.email ||
                  account.identity.name ||
                  "ChatGPT account"}{" "}
                · {account.id.slice(0, 8)}
              </strong>
              <p>
                {account.state === "signed_in"
                  ? account.planAuthorized
                    ? "Signed in · ChatGPT plan authorized"
                    : "Signed in · ChatGPT plan usage disabled"
                  : account.state === "signed_out"
                    ? "Signed out"
                    : account.state === "sign_out_incomplete"
                      ? "Local credential cleanup required"
                      : "Sign-in required"}
                {selectedRegistrationId === account.id
                  ? " · Selected account"
                  : ""}
              </p>
              <button
                disabled={
                  busy ||
                  account.state !== "signed_in" ||
                  !account.planAuthorized ||
                  selectedRegistrationId === account.id
                }
                onClick={() => onSelect(account.id)}
              >
                Use this account
              </button>
              <button
                disabled={
                  busy || pending || account.state === "sign_out_incomplete"
                }
                onClick={() =>
                  act(async () =>
                    setAttempt(
                      await dispatch<S.ProviderAuthAttempt>(
                        "providers.auth.begin",
                        { ...envelope(workspace), registrationId: account.id },
                      ),
                    ),
                  )
                }
              >
                {account.state === "signed_in" && !account.planAuthorized
                  ? "Enable ChatGPT plan usage"
                  : "Sign in again"}
              </button>
              <button
                disabled={busy || account.state === "signed_out"}
                onClick={() =>
                  act(async () => {
                    const result = await dispatch<S.ProviderAuthSignOutResult>(
                      "providers.auth.signOut",
                      { ...envelope(workspace), registrationId: account.id },
                    );
                    if (selectedRegistrationId === account.id) onSelect("");
                    setNotice(
                      result.remoteRevoked
                        ? "Signed out. This account’s renewable session was revoked."
                        : "Signed out locally. Remote revocation was not confirmed; disconnect Trove in ChatGPT Settings to confirm remote access is removed.",
                    );
                  })
                }
              >
                Sign out this account
              </button>
            </article>
          ))}
          <p>
            Account authorization does not verify model eligibility. A completed
            inference request verifies access for that request. API billing is
            never selected automatically.
          </p>
          <a
            href="https://chatgpt.com/settings/usage"
            target="_blank"
            rel="noreferrer"
          >
            Manage ChatGPT usage and app access
          </a>
        </>
      )}
      {notice && <p role="status">{notice}</p>}
      {error && <p role="alert">{error}</p>}
    </section>
  );
}
