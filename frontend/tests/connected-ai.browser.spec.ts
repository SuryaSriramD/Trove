import { test, expect } from "@playwright/test";
const url =
  (process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4191") +
  "/tests/connectors-fixture.html";
async function selectSource(page: any, kind = "github") {
  await page.goto(url + "?kind=" + kind);
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page.getByRole("button", { name: /Remote issue/ }).click();
  await page
    .getByRole("button", { name: "Select for local AI", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Ask local AI about selected sources" })
    .click();
}
for (const kind of ["github", "gmail", "mcp"])
  test(`${kind} selected context stays local and citations retain external identity`, async ({
    page,
  }) => {
    await selectSource(page, kind);
    await expect(page.getByLabel("AI source scope")).toHaveValue("connected");
    await expect(page.getByLabel("Use approved memory")).not.toBeChecked();
    await expect(page.getByLabel("Use approved memory")).toBeDisabled();
    await page.getByLabel("AI prompt").fill("When is the launch?");
    await page.getByRole("button", { name: "Generate locally" }).click();
    await expect(page.locator(".ai-prose")).toContainText(
      "The launch is Friday",
    );
    const r = await page.evaluate(
      () =>
        (window as any).connectorCalls.find((c: any) => c.method === "ai.start")
          .request,
    );
    expect(r.connectedSources).toEqual([
      {
        snapshotId: "source1",
        remoteRevision: "r1",
        contentHash: "a".repeat(64),
      },
    ]);
    expect(r.useMemory).toBe(false);
    expect(r.pageId).toBeUndefined();
    expect(r.projectId).toBeUndefined();
    await expect(page.locator(".ai-prose a")).toHaveAttribute(
      "href",
      "https://github.com/org/repo/issues/1",
    );
    await page.getByText("Source context · 1", { exact: true }).click();
    await expect(page.locator(".ai-source")).toContainText("Access checked");
    if (kind === "mcp")
      await expect(page.locator(".ai-source")).toContainText("partial source");
    expect(
      await page.evaluate(
        () =>
          (window as any).connectorCalls.filter(
            (c: any) =>
              c.method.startsWith("providers.") ||
              c.method === "resolveCitation",
          ).length,
      ),
    ).toBe(0);
  });
test("late connected AI response is discarded after workspace switch", async ({
  page,
}) => {
  await selectSource(page);
  await page.getByLabel("AI prompt").fill("When is the launch?");
  await page.evaluate(() => {
    (window as any).holdAIStart = true;
  });
  await page.getByRole("button", { name: "Generate locally" }).click();
  await expect
    .poll(() => page.evaluate(() => typeof (window as any).resolveAIStart))
    .toBe("function");
  await page.getByRole("button", { name: "Switch fixture workspace" }).click();
  await page.evaluate(() => {
    (window as any).resolveAIStart();
  });
  await expect(
    page.getByRole("region", { name: "Selected local AI sources" }),
  ).toHaveCount(0);
  await expect(page.locator(".ai-prose")).toHaveCount(0);
});

test("source preparation can be cancelled before the model run exists", async ({
  page,
}) => {
  await selectSource(page);
  await page.getByLabel("AI prompt").fill("When is the launch?");
  await page.evaluate(() => {
    (window as any).holdAIStart = true;
  });
  await page.getByRole("button", { name: "Generate locally" }).click();
  await expect(
    page.getByText("Preparing and checking selected sources…", { exact: true }),
  ).toBeVisible();
  await page.getByRole("button", { name: "Cancel preparation" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "Source preparation cancelled",
  );
  const calls = await page.evaluate(() => (window as any).connectorCalls);
  expect(calls.find((c: any) => c.method === "ai.cancel").request.runId).toBe(
    calls.find((c: any) => c.method === "ai.start").request.requestId,
  );
  await expect(
    page.getByRole("button", { name: "Generate locally" }),
  ).toBeEnabled();
});

test("old-source completion cannot enter a newly selected revision", async ({
  page,
}) => {
  await selectSource(page);
  await page.getByLabel("AI prompt").fill("When is the launch?");
  await page.getByRole("button", { name: "Generate locally" }).click();
  await expect(page.locator(".ai-prose")).toContainText("Friday");
  await page.evaluate(() => {
    (window as any).fixtureSource.remoteRevision = "r2";
  });
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page
    .getByRole("button", { name: /Remote issue/ })
    .first()
    .click();
  await page
    .getByRole("button", { name: "Select for local AI", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Ask local AI about selected sources" })
    .click();
  await expect(
    page.getByText("Saved conversations · 0", { exact: true }),
  ).toBeVisible();
  await page.evaluate(() => {
    (window as any).emitAIProgress((window as any).lastAIRun);
  });
  await expect(
    page.getByText("Saved conversations · 0", { exact: true }),
  ).toBeVisible();
  await expect(page.locator(".ai-prose")).toHaveCount(0);
});

test("Gmail remains available locally but cannot open external sharing", async ({
  page,
}) => {
  await selectSource(page, "gmail");
  await expect(
    page.getByRole("button", {
      name: "Review sharing with an external provider",
    }),
  ).toBeDisabled();
  await expect(
    page.getByText(
      "Gmail-derived context remains local. Remove Gmail sources to review external sharing.",
    ),
  ).toBeVisible();
  expect(
    await page.evaluate(() =>
      (window as any).connectorCalls.some((c: any) =>
        c.method.startsWith("providers."),
      ),
    ),
  ).toBe(false);
});

test("Connections opens a separate external review without starting or sharing", async ({
  page,
}) => {
  await selectSource(page, "github");
  await page
    .getByRole("button", { name: "Review sharing with an external provider" })
    .click();
  await expect(
    page.getByRole("region", { name: "Managed Codex", exact: true }),
  ).toBeVisible();
  await expect(page.getByText("No saved ChatGPT accounts.")).toBeVisible();
  await expect(page.getByLabel("Provider", { exact: true })).toHaveValue(
    "codex",
  );
  await expect(
    page.getByRole("option", { name: "Paired local MCP client" }),
  ).toHaveCount(0);
  await expect(page.getByRole("alert")).toHaveCount(0);
  expect(
    await page.evaluate(() =>
      (window as any).connectorCalls.some((c: any) =>
        [
          "providers.start",
          "providers.connectedPreview",
          "providers.connect",
        ].includes(c.method),
      ),
    ),
  ).toBe(false);
  await page
    .getByRole("button", { name: "Remove Remote issue", exact: true })
    .click();
  await expect(
    page.getByRole("region", { name: "Managed Codex", exact: true }),
  ).toHaveCount(0);
});
