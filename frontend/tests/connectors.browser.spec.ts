import { test, expect } from "@playwright/test";
const url =
  (process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4191") +
  "/tests/connectors-fixture.html";

test("Drive export format and MCP binary acquisition preserve reviewed source revisions", async ({
  page,
}) => {
  for (const kind of ["google_drive", "mcp"]) {
    await page.goto(url + "?kind=" + kind);
    await page.getByRole("button", { name: "Search", exact: true }).click();
    await page.getByRole("button", { name: /Remote issue/ }).click();
    if (kind === "google_drive")
      await page
        .getByLabel("Acquisition format")
        .selectOption("application/pdf");
    else
      await expect(
        page.getByRole("button", { name: "Review import into Trove" }),
      ).toBeDisabled();
    await page.getByRole("button", { name: "Acquire this file" }).click();
    await expect(page.getByText(/42 bytes/)).toBeVisible();
    const r = await page.evaluate(
      () =>
        (window as any).connectorCalls.find(
          (c: any) => c.method === "connectors.native.acquire",
        ).request,
    );
    expect(r.expectedRevision).toBe("r1");
    expect(r.remoteId).toBe("issue:1");
    expect(r.format).toBe(kind === "google_drive" ? "application/pdf" : "");
  }
});

test("Gmail acquires only a displayed attachment and reviews all recipient fields", async ({
  page,
}) => {
  await page.goto(url + "?kind=gmail");
  await page.getByRole("button", { name: "Gmail · Example" }).click();
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page.getByRole("button", { name: /Remote issue/ }).click();
  await page.getByRole("button", { name: "Acquire Meeting notes.pdf" }).click();
  await expect(page.getByText(/42 bytes/)).toBeVisible();
  const acquired = await page.evaluate(
    () =>
      (window as any).connectorCalls.find(
        (c: any) => c.method === "connectors.native.acquire",
      ).request,
  );
  expect(acquired.remoteId).toMatch(/^gmail-attachment:/);
  await page.getByText("Prepare an external change", { exact: true }).click();
  await page.getByLabel("Selected destination").selectOption("org/repo");
  await page
    .getByLabel("To (comma separated)", { exact: true })
    .fill("to@example.test");
  await page
    .getByLabel("Cc (comma separated)", { exact: true })
    .fill("cc@example.test");
  await page
    .getByLabel("Bcc (comma separated)", { exact: true })
    .fill("bcc@example.test");
  await page.getByLabel("Subject", { exact: true }).fill("Local draft");
  await page
    .getByRole("button", { name: "Prepare review", exact: true })
    .click();
  await expect(page.getByLabel("External write review")).toContainText(
    "bcc@example.test",
  );
  const args = await page.evaluate(
    () =>
      (window as any).connectorCalls.find(
        (c: any) => c.method === "connectors.native.prepare",
      ).request.action.arguments,
  );
  expect(args.to).toEqual(["to@example.test"]);
  expect(args.cc).toEqual(["cc@example.test"]);
  expect(args.bcc).toEqual(["bcc@example.test"]);
  expect(
    await page.evaluate(
      () =>
        (window as any).connectorCalls.filter(
          (c: any) => c.method === "connectors.native.execute",
        ).length,
    ),
  ).toBe(0);
});

test("GitHub accepts the source identifier shown in previews for reviewed updates", async ({
  page,
}) => {
  await page.goto(url);
  await page.getByRole("button", { name: "GitHub · Example" }).click();
  await page.getByRole("button", { name: "Choose sources" }).click();
  await page.getByLabel("Selected repository").check();
  await page.getByRole("button", { name: "Save source selection" }).click();
  await page.getByText("Prepare an external change", { exact: true }).click();
  await page
    .getByRole("combobox", { name: "Action", exact: true })
    .selectOption("issue.update");
  await page.getByLabel("Selected destination").selectOption("org/repo");
  await page.getByLabel("Existing item ID").fill("issue:123");
  await page.getByLabel("Title", { exact: true }).fill("Reviewed update");
  await page
    .getByRole("button", { name: "Prepare review", exact: true })
    .click();
  await expect(page.getByLabel("External write review")).toContainText(
    "Item: 123",
  );
});

test("manual reconciliation records inspection without resending an uncertain write", async ({
  page,
}) => {
  await page.goto(url);
  await page.getByRole("button", { name: "GitHub · Example" }).click();
  await page.getByRole("button", { name: "Choose sources" }).click();
  await page.getByLabel("Selected repository").check();
  await page.getByRole("button", { name: "Save source selection" }).click();
  await page.getByText("Prepare an external change", { exact: true }).click();
  await page.getByLabel("Selected destination").selectOption("org/repo");
  await page
    .getByLabel("Title", { exact: true })
    .fill("Inspect remote outcome");
  await page
    .getByRole("button", { name: "Prepare review", exact: true })
    .click();
  await page
    .getByRole("button", { name: "Approve and send this exact change" })
    .click();
  await page
    .getByText("Record a manually verified outcome", { exact: true })
    .click();
  await expect(
    page.getByRole("button", { name: "Record manual resolution", exact: true }),
  ).toBeDisabled();
  await page
    .getByLabel("Evidence from your inspection")
    .fill("I inspected issue 42 and matched the reviewed content and marker.");
  await page
    .getByLabel("I inspected this exact change in the connected account.")
    .check();
  await page
    .getByRole("button", { name: "Record manual resolution", exact: true })
    .click();
  await expect(page.getByLabel("Manual resolution record")).toContainText(
    "provider outcome remains unverified",
  );
  await expect(
    page.getByRole("button", { name: "Approve and send this exact change" }),
  ).toHaveCount(0);
  const calls = await page.evaluate(() => (window as any).connectorCalls);
  expect(
    calls.filter((c: any) => c.method === "connectors.native.execute"),
  ).toHaveLength(1);
  expect(
    calls.find((c: any) => c.method === "connectors.native.resolve").request,
  ).toMatchObject({
    hash: "a".repeat(64),
    expectedRevision: "a2",
    observation: "applied",
  });
});
test("external writes require selection and review; unknown is reconciled without resend", async ({
  page,
}) => {
  await page.goto(url);
  await page.getByRole("button", { name: "GitHub · Example" }).click();
  await page.getByRole("button", { name: "Choose sources" }).click();
  await page.getByLabel("Selected repository").check();
  await page.getByRole("button", { name: "Save source selection" }).click();
  await page.getByText("Prepare an external change", { exact: true }).click();
  await page.getByLabel("Selected destination").selectOption("org/repo");
  await page.getByLabel("Title", { exact: true }).fill("Reviewed title");
  await page.getByLabel("Message or description").fill("Reviewed body");
  await page
    .getByRole("button", { name: "Prepare review", exact: true })
    .click();
  await expect(page.getByLabel("External write review")).toContainText(
    "reviewed marker",
  );
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (window as any).connectorCalls.filter(
            (c: any) => c.method === "connectors.native.execute",
          ).length,
      ),
    )
    .toBe(0);
  await page
    .getByRole("button", { name: "Approve and send this exact change" })
    .click();
  await expect(
    page.getByRole("button", { name: "Reconcile outcome" }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Approve and send this exact change" }),
  ).toHaveCount(0);
  await page.getByRole("button", { name: "Reconcile outcome" }).click();
  await expect(page.getByLabel("External write review")).toContainText(
    "Existing remote result found",
  );
  await expect
    .poll(() =>
      page.evaluate(
        () =>
          (window as any).connectorCalls.filter(
            (c: any) => c.method === "connectors.native.execute",
          ).length,
      ),
    )
    .toBe(1);
});
test("preview requires fresh import and late workspace response is discarded", async ({
  page,
}) => {
  await page.goto(url);
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page.getByRole("button", { name: /Remote issue/ }).click();
  await expect(page.getByLabel("Authenticated external preview")).toContainText(
    "Private evidence",
  );
  await page.getByRole("button", { name: "Review import into Trove" }).click();
  await expect(page.getByRole("alert")).toContainText(
    "External content changed",
  );
  await page.evaluate(() => ((window as any).holdConnectorPreview = true));
  await page.getByRole("button", { name: /Remote issue/ }).click();
  await page.getByRole("button", { name: "Switch fixture workspace" }).click();
  await page.evaluate(() => (window as any).resolveConnectorPreview());
  await expect(page.getByLabel("Authenticated external preview")).toHaveCount(
    0,
  );
});
test("native authorization can be cancelled", async ({ page }) => {
  await page.goto(url);
  await page
    .getByRole("combobox", { name: "Service", exact: true })
    .selectOption("google_drive");
  await page.getByLabel("Application client ID").fill("registered-desktop-id");
  await page.getByRole("button", { name: "Authorize connection" }).click();
  await expect(page.getByLabel("Authorization status")).toContainText(
    "waiting",
  );
  await page.getByRole("button", { name: "Cancel authorization" }).click();
  await expect(page.getByLabel("Authorization status")).toContainText(
    "cancelled",
  );
  const request = await page.evaluate(
    () =>
      (window as any).connectorCalls.find(
        (c: any) => c.method === "connectors.native.authBegin",
      ).request,
  );
  expect(request.registration.kind).toBe("google_drive");
  expect(JSON.stringify(request)).not.toContain("password");
});

test("source cancellation targets the active request and creates no import", async ({
  page,
}) => {
  await page.goto(url);
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page.evaluate(() => {
    (window as any).holdConnectorPreview = true;
  });
  await page.getByRole("button", { name: /Remote issue/ }).click();
  await page.getByRole("button", { name: "Cancel source operation" }).click();
  await expect(
    page.getByRole("status").filter({ hasText: "Cancellation requested" }),
  ).toBeVisible();
  const calls = await page.evaluate(() => (window as any).connectorCalls);
  expect(
    calls.find((c: any) => c.method === "connectors.native.cancel").request
      .operationId,
  ).toBe(
    calls.find((c: any) => c.method === "connectors.native.preview").request
      .requestId,
  );
  expect(calls.some((c: any) => c.method === "connectors.native.import")).toBe(
    false,
  );
  await page.evaluate(() => (window as any).resolveConnectorPreview());
});

test("Notion preview offers explicit descendant capture with the selected root", async ({
  page,
}) => {
  await page.goto(url + "?kind=notion");
  await page.getByRole("button", { name: "Search", exact: true }).click();
  await page.getByRole("button", { name: /Remote issue/ }).click();
  const capture = page.getByRole("region", {
    name: "Capture Notion descendants",
  });
  await capture
    .getByText("Optional authenticated Notion capture", { exact: true })
    .click();
  await expect(capture.getByLabel("Root type")).toHaveValue("page");
  await expect(capture.getByLabel("Authorized root IDs")).toHaveValue(
    "019a1234-5678-7000-8000-000000000010",
  );
  await expect(capture.getByLabel("Integration token")).toHaveValue("");
  await expect(
    capture.getByRole("button", { name: "Capture selected roots" }),
  ).toBeDisabled();
  expect(
    await page.evaluate(
      () =>
        (window as any).connectorCalls.filter(
          (c: any) => c.method === "imports.notion.start",
        ).length,
    ),
  ).toBe(0);
});
