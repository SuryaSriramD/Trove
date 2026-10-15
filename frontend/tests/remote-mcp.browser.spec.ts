import { test, expect } from "@playwright/test";
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_REMOTE_TEST_URL ?? process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173"}/tests/remote-mcp-fixture.html`,
  );
});
test("native certificate handle and explicit scoped consent precede remote access", async ({
  page,
}) => {
  await expect(
    page.getByText("Remote access is disabled.", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Enable HTTPS listener", exact: true }),
  ).toBeDisabled();
  expect(
    (await page.evaluate(() => (window as any).remoteCalls)).every(
      (c: any) => c.method === "providers.remote.status",
    ),
  ).toBe(true);
  await page
    .getByLabel("Public HTTPS origin", { exact: true })
    .fill("https://trove.example.test");
  await page
    .getByRole("button", {
      name: "Choose certificate and private key…",
      exact: true,
    })
    .click();
  await expect(
    page.getByText(/Certificate for trove.example.test/),
  ).toBeVisible();
  await page
    .getByRole("button", { name: "Enable HTTPS listener", exact: true })
    .click();
  await page
    .getByRole("checkbox", { name: "Shared research", exact: true })
    .check();
  await page
    .getByRole("button", {
      name: "Review selected remote context",
      exact: true,
    })
    .click();
  await expect(page.getByLabel("Remote context preview")).toContainText(
    "Shared research (revision 4)",
  );
  await expect(page.getByLabel("Remote context preview")).not.toContainText(
    "Private journal",
  );
  await page
    .getByRole("button", { name: "Create scoped remote URL", exact: true })
    .click();
  await expect(page.getByLabel("Connection URL", { exact: true })).toHaveValue(
    "https://trove.example.test/mcp/opaque-fixture",
  );
  await page.evaluate(() => (window as any).requestRemoteAuthorization());
  const review = page.getByRole("article", {
    name: "Remote authorization review",
    exact: true,
  });
  await expect(review).toContainText("REVIEW42");
  await expect(review).toContainText("https://client.example.test/callback");
  await expect(review).toContainText("Shared research");
  await expect(review).not.toContainText("Private journal");
  await expect(
    review.getByRole("button", {
      name: "Approve this client and context",
      exact: true,
    }),
  ).toBeDisabled();
  await review.getByRole("checkbox").check();
  await review
    .getByRole("button", {
      name: "Approve this client and context",
      exact: true,
    })
    .click();
  await expect(
    page.getByText("Authorized remote client", { exact: true }),
  ).toBeVisible();
  const calls = await page.evaluate(() => (window as any).remoteCalls);
  expect(
    calls.find((c: any) => c.method === "providers.remote.enable").request,
  ).toMatchObject({
    certificateHandle: "019a1234-5678-7000-8000-000000000005",
    publicOrigin: "https://trove.example.test",
  });
  expect(
    calls.find((c: any) => c.method === "providers.remote.createConnection")
      .request.pageIds,
  ).toEqual(["019a1234-5678-7000-8000-000000000003"]);
  expect(
    calls.find((c: any) => c.method === "providers.remote.approveAuthorization")
      .request.reviewHash,
  ).toBe("c".repeat(64));
  expect(JSON.stringify(calls)).not.toMatch(
    /privateKey|BEGIN PRIVATE|certPath|keyPath/,
  );
  await page
    .getByRole("button", {
      name: "Disable remote access and disconnect all clients",
      exact: true,
    })
    .click();
  await expect(
    page.getByText("Remote access is disabled.", { exact: true }),
  ).toBeVisible();
  await expect(
    page.getByRole("button", { name: "Enable HTTPS listener", exact: true }),
  ).toBeDisabled();
});

test("delayed status cannot undo displayed HTTPS revocation", async ({
  page,
}) => {
  await page
    .getByLabel("Public HTTPS origin", { exact: true })
    .fill("https://trove.example.test");
  await page
    .getByRole("button", {
      name: "Choose certificate and private key…",
      exact: true,
    })
    .click();
  await page
    .getByRole("button", { name: "Enable HTTPS listener", exact: true })
    .click();
  await expect(page.getByText(/HTTPS enabled at/)).toBeVisible();
  await page.evaluate(() => {
    (window as any).holdRemoteStatus = true;
  });
  await expect
    .poll(() => page.evaluate(() => typeof (window as any).resolveRemoteStatus))
    .toBe("function");
  await page
    .getByRole("button", {
      name: "Disable remote access and disconnect all clients",
      exact: true,
    })
    .click();
  await expect(
    page.getByText("Remote access is disabled.", { exact: true }),
  ).toBeVisible();
  await page.evaluate(async () => {
    (window as any).resolveRemoteStatus();
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  expect(
    await page.getByText("Remote access is disabled.", { exact: true }).count(),
  ).toBe(1);
  expect(await page.getByText(/HTTPS enabled at/).count()).toBe(0);
});
