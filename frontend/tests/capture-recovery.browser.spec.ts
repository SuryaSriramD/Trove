import { test, expect } from "@playwright/test";
test.beforeEach(async ({ page }) => {
  await page.goto(
    `${process.env.TROVE_TEST_URL ?? "http://127.0.0.1:4173"}/tests/capture-recovery-fixture.html`,
  );
});
const preview = async (page: any) => {
  await page
    .getByLabel("Shared selection", { exact: true })
    .fill("Captured source");
  await page.getByRole("button", { name: "Preview clip", exact: true }).click();
};
test("refuses a capture commit that makes no progress", async ({ page }) => {
  await preview(page);
  await page
    .getByRole("button", { name: "Save reviewed capture", exact: true })
    .click();
  await expect(page.getByRole("alert")).toContainText("no progress");
  expect(await page.evaluate(() => (window as any).commitCalls)).toBe(1);
});
test("workspace switch discards a delayed capture preview", async ({
  page,
}) => {
  await page.evaluate(() => {
    (window as any).delayCapturePreview = true;
  });
  await preview(page);
  await expect
    .poll(() =>
      page.evaluate(() => typeof (window as any).resolveCapturePreview),
    )
    .toBe("function");
  await page
    .getByRole("button", { name: "Switch workspace", exact: true })
    .click();
  await page.evaluate(async () => {
    (window as any).resolveCapturePreview();
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  await expect(
    page.getByRole("button", { name: "Save reviewed capture", exact: true }),
  ).toHaveCount(0);
  await expect(page.getByText("Old capture", { exact: false })).toHaveCount(0);
});
test("workspace switch discards delayed native browser drafts", async ({
  page,
}) => {
  await page.evaluate(() => {
    (window as any).delayBrowserDrafts = true;
  });
  await page
    .getByRole("button", { name: "Refresh browser drafts", exact: true })
    .click();
  await expect
    .poll(() =>
      page.evaluate(() => typeof (window as any).resolveBrowserDrafts),
    )
    .toBe("function");
  await page.evaluate(() => {
    (window as any).savedOldBrowserDrafts = (
      window as any
    ).resolveBrowserDrafts;
    (window as any).delayBrowserDrafts = false;
  });
  await page
    .getByRole("button", { name: "Switch workspace", exact: true })
    .click();
  await page.evaluate(async () => {
    (window as any).savedOldBrowserDrafts();
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  await expect(
    page.getByRole("button", { name: "Old browser draft", exact: true }),
  ).toHaveCount(0);
});
test("cancel stops after the current commit batch and retains a resumable review", async ({
  page,
}) => {
  await preview(page);
  await page.evaluate(() => {
    (window as any).delayCaptureCommit = true;
  });
  await page
    .getByRole("button", { name: "Save reviewed capture", exact: true })
    .click();
  await expect
    .poll(() =>
      page.evaluate(() => typeof (window as any).resolveCaptureCommit),
    )
    .toBe("function");
  await page
    .getByRole("button", { name: "Cancel capture save", exact: true })
    .click();
  await page.evaluate(() => (window as any).resolveCaptureCommit());
  await expect(
    page.getByRole("button", { name: "Resume reviewed capture", exact: true }),
  ).toBeEnabled();
  expect(await page.evaluate(() => (window as any).commitCalls)).toBe(1);
  expect(await page.evaluate(() => (window as any).captureChanged)).toBe(1);
});

test("browser capture cancellation retains the last committed review", async ({
  page,
}) => {
  await page
    .getByRole("button", { name: "Old browser draft", exact: true })
    .click();
  await page.evaluate(() => {
    (window as any).delayCaptureCommit = true;
  });
  await page
    .getByRole("button", { name: "Save reviewed browser capture", exact: true })
    .click();
  await expect
    .poll(() =>
      page.evaluate(() => typeof (window as any).resolveCaptureCommit),
    )
    .toBe("function");
  await page
    .getByRole("button", { name: "Cancel browser capture save", exact: true })
    .click();
  await page.evaluate(() => (window as any).resolveCaptureCommit());
  await expect(
    page.getByRole("button", {
      name: "Resume reviewed browser capture",
      exact: true,
    }),
  ).toBeEnabled();
  expect(await page.evaluate(() => (window as any).commitCalls)).toBe(1);
  expect(await page.evaluate(() => (window as any).captureChanged)).toBe(1);
});
test("workspace switch prevents another commit after the in-flight batch", async ({
  page,
}) => {
  await preview(page);
  await page.evaluate(() => {
    (window as any).delayCaptureCommit = true;
  });
  await page
    .getByRole("button", { name: "Save reviewed capture", exact: true })
    .click();
  await expect
    .poll(() =>
      page.evaluate(() => typeof (window as any).resolveCaptureCommit),
    )
    .toBe("function");
  await page
    .getByRole("button", { name: "Switch workspace", exact: true })
    .click();
  await page.evaluate(async () => {
    (window as any).resolveCaptureCommit();
    await new Promise(requestAnimationFrame);
    await new Promise(requestAnimationFrame);
  });
  expect(await page.evaluate(() => (window as any).commitCalls)).toBe(1);
  await expect(
    page.getByRole("button", { name: "Resume reviewed capture", exact: true }),
  ).toHaveCount(0);
});

// These cases exercise the real panels with controlled service responses. They
// establish request/continuation behavior, not native or canonical persistence.
const captureID = (n: number) =>
  `01990000-0000-7000-8000-${String(n).padStart(12, "0")}`;
async function expectCaptureRequests(page: any, count: number, planId: string) {
  const calls = await page.evaluate(() =>
    (window as any).captureCalls.filter(
      (call: any) => call.method === "imports.commit",
    ),
  );
  expect(calls).toHaveLength(count);
  const requestIDs: string[] = [];
  for (const { request } of calls) {
    expect(request).toEqual({
      contractVersion: 1,
      requestId: expect.stringMatching(
        /^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
      ),
      workspaceId: captureID(1),
      planId,
      planHash: "hash",
    });
    // The query envelope binds workspace identity. The execution grant is held
    // by the native/host context and is not a query request field.
    expect(request).not.toHaveProperty("scopeGrantId");
    requestIDs.push(request.requestId);
  }
  expect(new Set(requestIDs).size).toBe(count);
}

async function expectCaptureProgressTrace(page: any, planId: string, calls: number, itemCount: number) {
  const actual = await page.evaluate(() => (window as any).captureReturnedPlans.map((p: any) => ({
    planId: p.planId, planHash: p.planHash, state: p.state,
    initialized: p.initialized, cursor: p.cursor,
    committed: p.items.map((item: any) => item.committed),
  })));
  expect(actual).toEqual(Array.from({ length: calls }, (_, cursor) => ({
    planId,
    planHash: "hash",
    state: cursor === itemCount ? "complete" : "running",
    initialized: true,
    cursor,
    committed: Array.from({ length: itemCount }, (_, index) => index < cursor),
  })));
}
for (const surface of ["capture", "browser"] as const) {
  const planId = captureID(surface === "capture" ? 20 : 22);
  const saveName = surface === "capture"
    ? "Save reviewed capture"
    : "Save reviewed browser capture";
  const openName = surface === "capture"
    ? "Open capture"
    : "Open saved browser capture";
  const resumeName = surface === "capture"
    ? "Resume reviewed capture"
    : "Resume reviewed browser capture";
  const cancelName = surface === "capture"
    ? "Cancel capture save"
    : "Cancel browser capture save";
  const successNotice = (page: any) => page.getByRole("status").filter({
    hasText: "saved as offline workspace content",
  });
  const pausedNotice = (page: any) => page.getByRole("status").filter({
    hasText: /capture save paused/i,
  });
  const start = async (page: any, scenario: string, itemCount = 2, delay = false) => {
    await page.evaluate((value: { scenario: string; itemCount: number; delay: boolean }) => {
      (window as any).commitScenario = value.scenario;
      (window as any).captureItemCount = value.itemCount;
      (window as any).delayCaptureCommit = value.delay;
    }, { scenario, itemCount, delay });
    if (surface === "capture") await preview(page);
    else {
      // Regenerate the native fixture's review after configuring its inventory;
      // the selected draft and every commit response must contain the same items.
      const refresh = page.getByRole("button", { name: "Refresh browser drafts", exact: true });
      await refresh.click();
      await expect(refresh).toBeEnabled();
      await page.getByRole("button", { name: "Old browser draft", exact: true }).click();
    }
    await page.getByRole("button", { name: saveName, exact: true }).click();
  };
  test(`${surface} accepts initialization and item progress under one review hash`, async ({ page }) => {
    await start(page, "progress");
    await expect(page.getByRole("button", { name: openName, exact: true })).toBeVisible();
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(successNotice(page)).toBeVisible();
    await expectCaptureRequests(page, 3, planId);
    expect(await page.evaluate(() => (window as any).captureChanged)).toBe(1);
    expect(await page.evaluate(() => (window as any).captureReturnedPlans.map((p: any) => ({
      planId: p.planId, planHash: p.planHash, state: p.state,
      initialized: p.initialized, cursor: p.cursor,
      committed: p.items.map((item: any) => item.committed),
    })))).toEqual([
      { planId, planHash: "hash", state: "running", initialized: true, cursor: 0, committed: [false, false] },
      { planId, planHash: "hash", state: "running", initialized: true, cursor: 1, committed: [true, false] },
      { planId, planHash: "hash", state: "complete", initialized: true, cursor: 2, committed: [true, true] },
    ]);
  });
  for (const scenario of ["stalled", "changed-hash", "state-only"] as const) {
    test(`${surface} stops ${scenario} without initialization or cursor progress`, async ({ page }) => {
      await start(page, scenario);
      await expect(page.getByRole("alert")).toContainText(
        scenario === "changed-hash"
          ? "Capture review changed while saving; review the draft again."
          : "no progress",
      );
      await expect(page.getByRole("button", { name: openName, exact: true })).toHaveCount(0);
      await expect(successNotice(page)).toHaveCount(0);
      await expect(pausedNotice(page)).toHaveCount(0);
      // Rejected responses must not replace the previously reviewed preview.
      await expect(page.getByRole("button", { name: saveName, exact: true })).toBeEnabled();
      await expect(page.getByRole("button", { name: resumeName, exact: true })).toHaveCount(0);
      await expectCaptureRequests(page, 1, planId);
      expect(await page.evaluate(() => (window as any).captureChanged)).toBe(0);
      const returned = await page.evaluate(() => (window as any).captureReturnedPlans);
      expect(returned).toHaveLength(1);
      expect(returned[0]).toMatchObject({
        planId,
        planHash: scenario === "changed-hash" ? "changed-1" : "hash",
        state: scenario === "state-only" ? "running" : "preview",
        initialized: false,
        cursor: 0,
      });
      expect(returned[0].items.map((item: any) => item.committed)).toEqual([false, false]);
    });
  }
  test(`${surface} pauses at 64 calls and resumes the final item once`, async ({ page }) => {
    await start(page, "progress", 64);
    await expect(pausedNotice(page)).toBeVisible();
    await expect(successNotice(page)).toHaveCount(0);
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(page.getByRole("button", { name: resumeName, exact: true })).toBeEnabled();
    await expect(page.getByRole("button", { name: openName, exact: true })).toHaveCount(0);
    await expectCaptureRequests(page, 64, planId);
    await expectCaptureProgressTrace(page, planId, 64, 64);
    expect(await page.evaluate(() => (window as any).captureChanged)).toBe(1);

    await page.getByRole("button", { name: resumeName, exact: true }).click();
    await expect(page.getByRole("button", { name: openName, exact: true })).toBeVisible();
    await expect(successNotice(page)).toBeVisible();
    await expect(pausedNotice(page)).toHaveCount(0);
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expectCaptureRequests(page, 65, planId);
    await expectCaptureProgressTrace(page, planId, 65, 64);
    expect(await page.evaluate(() => (window as any).captureChanged)).toBe(2);
  });
  test(`${surface} completes on call 64 without a false pause`, async ({ page }) => {
    await start(page, "progress", 63);
    await expect(page.getByRole("button", { name: openName, exact: true })).toBeVisible();
    await expect(successNotice(page)).toBeVisible();
    await expect(pausedNotice(page)).toHaveCount(0);
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(page.getByRole("button", { name: resumeName, exact: true })).toHaveCount(0);
    await expectCaptureRequests(page, 64, planId);
    await expectCaptureProgressTrace(page, planId, 64, 63);
    expect(await page.evaluate(() => (window as any).captureChanged)).toBe(1);
  });
  test(`${surface} resumes after cancellation of a valid initialization batch`, async ({ page }) => {
    await start(page, "progress", 2, true);
    await expect.poll(() => page.evaluate(() => typeof (window as any).resolveCaptureCommit)).toBe("function");
    await page.getByRole("button", { name: cancelName, exact: true }).click();
    await page.evaluate(() => {
      (window as any).delayCaptureCommit = false;
      (window as any).resolveCaptureCommit();
    });
    await expect(pausedNotice(page)).toBeVisible();
    await expect(successNotice(page)).toHaveCount(0);
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expect(page.getByRole("button", { name: resumeName, exact: true })).toBeEnabled();
    await expectCaptureRequests(page, 1, planId);
    await expectCaptureProgressTrace(page, planId, 1, 2);
    expect(await page.evaluate(() => (window as any).captureChanged)).toBe(1);

    await page.getByRole("button", { name: resumeName, exact: true }).click();
    await expect(page.getByRole("button", { name: openName, exact: true })).toBeVisible();
    await expect(successNotice(page)).toBeVisible();
    await expect(pausedNotice(page)).toHaveCount(0);
    await expect(page.getByRole("alert")).toHaveCount(0);
    await expectCaptureRequests(page, 3, planId);
    await expectCaptureProgressTrace(page, planId, 3, 2);
    expect(await page.evaluate(() => (window as any).captureChanged)).toBe(2);
  });

}
