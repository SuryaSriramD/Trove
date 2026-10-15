import { test, expect } from "@playwright/test";
const url =
  (process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4191") +
  "/tests/managed-providers-fixture.html?connected=1";
async function prepare(page: any, provider = "codex") {
  await page.goto(url + "&provider=" + provider);
  await page
    .getByRole("button", {
      name:
        provider === "codex"
          ? "Check installed Codex"
          : "Check installed Claude Code",
      exact: true,
    })
    .click();
  if (provider === "codex") {
    await page
      .getByRole("button", { name: "Use this account", exact: true })
      .first()
      .click();
    await page
      .getByRole("button", { name: "Load models for selected account" })
      .click();
    await page.getByLabel("Account model").selectOption("model-a");
  } else {
    await page
      .getByRole("button", { name: "Use API Claude", exact: true })
      .click();
    await page
      .getByLabel("Full Claude model identifier")
      .fill("claude-fixture-model");
  }
  await page
    .getByLabel(
      provider === "codex" ? "Request to Codex" : "Request to Claude Code",
      { exact: true },
    )
    .fill("Summarize this selected issue.");
}
for (const provider of ["codex", "claude_code"])
  test(
    provider +
      " shares exact reviewed excerpts without implicit canonical access",
    async ({ page }) => {
      await prepare(page, provider);
      const start = page.getByRole("button", {
        name: "Start with selected account and context",
      });
      await expect(start).toBeDisabled();
      await page
        .getByRole("button", { name: "Review context for this run" })
        .click();
      const review = page.getByRole("region", {
        name: "External sharing review",
      });
      await expect(review).toContainText(
        provider === "codex"
          ? "recipient-5@example.test"
          : "recipient-6@example.test",
      );
      await expect(review).toContainText(
        "Exact private excerpt: launch AMBER-27",
      );
      await expect(start).toBeDisabled();
      await page
        .getByLabel(
          "I approve sharing these excerpts with this recipient when I start the run.",
        )
        .check();
      await start.click();
      await expect(review).toHaveCount(0);
      await expect(
        page.getByText("Partial response", { exact: true }),
      ).toBeVisible();
      const calls = await page.evaluate(() => (window as any).managedCalls);
      const request = calls.find(
        (c: any) => c.method === "providers.start",
      ).request;
      expect(request.pageIds).toEqual([]);
      expect(request.tools).toEqual(["readConnectedSource"]);
      expect(request.connectedSharing).toEqual({
        id: "review-id",
        hash: "d".repeat(64),
      });
      expect(
        calls.find((c: any) => c.method === "providers.connectedPreview")
          .request.sources,
      ).toEqual([
        {
          snapshotId: "external-source",
          remoteRevision: "r1",
          contentHash: "c".repeat(64),
        },
      ]);
    },
  );
test("account switch discards recipient consent and expired reviews cannot start", async ({
  page,
}) => {
  await prepare(page);
  await page
    .getByRole("button", { name: "Review context for this run" })
    .click();
  await page
    .getByLabel(
      "I approve sharing these excerpts with this recipient when I start the run.",
    )
    .check();
  await page
    .getByRole("button", { name: "Use this account", exact: true })
    .last()
    .click();
  await expect(
    page.getByRole("region", { name: "External sharing review" }),
  ).toHaveCount(0);
  await page
    .getByRole("button", { name: "Load models for selected account" })
    .click();
  await page.getByLabel("Account model").selectOption("model-b");
  await page.evaluate(() => ((window as any).expiredSharing = true));
  await page
    .getByRole("button", { name: "Review context for this run" })
    .click();
  await expect(
    page.getByRole("region", { name: "External sharing review" }),
  ).toContainText("recipient-6@example.test");
  await expect(page.getByRole("alert")).toContainText("expired");
  await page
    .getByLabel(
      "I approve sharing these excerpts with this recipient when I start the run.",
    )
    .check();
  await expect(
    page.getByRole("button", {
      name: "Start with selected account and context",
    }),
  ).toBeDisabled();
});
for (const change of ["grant", "source revision"])
  test(
    "late sharing preview is discarded after " + change + " changes",
    async ({ page }) => {
      await prepare(page);
      await page.evaluate(() => ((window as any).holdSharing = true));
      await page
        .getByRole("button", { name: "Review context for this run" })
        .click();
      await expect
        .poll(() => page.evaluate(() => typeof (window as any).resolveSharing))
        .toBe("function");
      await page
        .getByRole("button", { name: "Switch fixture " + change, exact: true })
        .click();
      await page.evaluate(() => (window as any).resolveSharing());
      await expect(
        page.getByRole("region", { name: "External sharing review" }),
      ).toHaveCount(0);
      await expect(
        page.getByRole("button", {
          name: "Start with selected account and context",
        }),
      ).toBeDisabled();
      await expect(page.getByLabel("Account model")).toHaveValue("");
    },
  );
test("rejected start clears the consumed sharing review", async ({ page }) => {
  await prepare(page);
  await page
    .getByRole("button", { name: "Review context for this run" })
    .click();
  await page
    .getByLabel(
      "I approve sharing these excerpts with this recipient when I start the run.",
    )
    .check();
  await page.evaluate(() => ((window as any).stalePreview = true));
  await page
    .getByRole("button", { name: "Start with selected account and context" })
    .click();
  await expect(page.getByRole("alert")).toContainText(
    "Selected content changed",
  );
  await expect(
    page.getByRole("region", { name: "External sharing review" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", {
      name: "Start with selected account and context",
    }),
  ).toBeDisabled();
});

test("external source preparation cancels before a provider run exists", async ({
  page,
}) => {
  await prepare(page);
  await page.evaluate(() => ((window as any).holdSharing = true));
  await page
    .getByRole("button", { name: "Review context for this run" })
    .click();
  await expect
    .poll(() => page.evaluate(() => typeof (window as any).resolveSharing))
    .toBe("function");
  await expect(
    page.getByText("Checking selected sources and recipient…", { exact: true }),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Cancel provider preparation" })
    .click();
  await expect(page.getByRole("alert")).toContainText(/cancelled/);
  await expect(
    page.getByRole("region", { name: "External sharing review" }),
  ).toHaveCount(0);
  await expect(
    page.getByRole("button", { name: "Review context for this run" }),
  ).toBeEnabled();
  const calls = await page.evaluate(() => (window as any).managedCalls);
  expect(
    calls.find((c: any) => c.method === "providers.cancel").request.runId,
  ).toBe(
    calls.find((c: any) => c.method === "providers.connectedPreview").request
      .requestId,
  );
  expect(calls.some((c: any) => c.method === "providers.start")).toBe(false);
});
