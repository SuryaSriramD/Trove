import { test, expect } from "@playwright/test";
const url =
  (process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4191") +
  "/tests/connectors-fixture.html";
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
