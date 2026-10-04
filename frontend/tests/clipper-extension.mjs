// Called by TestClipperBrowserQualification. No network page is downloaded.
import assert from "node:assert/strict";
import fs from "node:fs/promises";
import path from "node:path";
import os from "node:os";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { chromium } = require("@playwright/test");
const root = path.resolve(
  path.dirname(fileURLToPath(import.meta.url)),
  "../..",
);
const { pairing, destinationId, revokeFile } = JSON.parse(
  await fs.readFile(process.argv[2], "utf8"),
);
const id = pairing.extensionId,
  profile = await fs.mkdtemp(
    path.join(os.tmpdir(), "trove-clipper-qualified-"),
  );
const context = await chromium.launchPersistentContext(profile, {
  executablePath: process.env.TROVE_CLIPPER_BROWSER,
  headless: true,
  args: [
    "--enable-unsafe-extension-debugging",
    "--disable-extensions-except=" + path.join(root, "browser-clipper"),
    "--load-extension=" + path.join(root, "browser-clipper"),
  ],
});
const evidence = {
  browser: context.browser().version(),
  extensionId: id,
  article: false,
  selection: false,
  url: false,
  explicitImage: false,
  unavailableDraftRetained: false,
  sourceDOMUnchanged: false,
};
try {
  const cdp = await context.browser().newBrowserCDPSession();
  const sourceURL = "https://clipper-source.example.test/article",
    imageURL = "https://clipper-assets.example.test/image.png";
  const png = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a2ioAAAAASUVORK5CYII=",
    "base64",
  );
  await context.route(sourceURL, (route) =>
    route.fulfill({
      contentType: "text/html",
      body:
        '<!doctype html><title>Browser evidence</title><nav>Omit navigation</nav><article><h1>Readable article</h1><p id="selected"><strong>Selected bold text</strong> and link <a href="/related">retained source</a>.</p><script type="application/json">{"inert":"source"}</script><img src="' +
        imageURL +
        '" alt="Retained image"></article>',
    }),
  );
  await context.route(imageURL, (route) =>
    route.fulfill({
      contentType: "image/png",
      headers: { "Access-Control-Allow-Origin": "*" },
      body: png,
    }),
  );
  const source = await context.newPage();
  await source.goto(sourceURL);
  const original = await source.locator("article").innerHTML();
  async function popup() {
    await source.bringToFront();
    const tabs = await cdp.send("Target.getTargets", {
      filter: [{ type: "tab", exclude: false }],
    });
    const tab = tabs.targetInfos.find(
      (item) => item.type === "tab" && item.url === sourceURL,
    );
    assert(tab, "active browser tab exists");
    await cdp.send("Extensions.triggerAction", { id, targetId: tab.targetId });
    let popup;
    for (let n = 0; n < 40; n++) {
      popup = context
        .pages()
        .find((page) =>
          page.url().startsWith("chrome-extension://" + id + "/popup.html"),
        );
      if (popup) break;
      await new Promise((resolve) => setTimeout(resolve, 50));
    }
    // Chromium headless grants activeTab for the action but may not surface
    // its browser-chrome popup. Open the same extension page in that case.
    if (!popup) {
      popup = await context.newPage();
      await popup.goto("chrome-extension://" + id + "/popup.html");
      await source.bringToFront();
    }
    await popup.locator("#extension-id").waitFor({ state: "attached" });
    return popup;
  }
  let panel = await popup();
  await panel.locator("#endpoint").fill(pairing.endpoint);
  await panel.locator("#pair-code").fill(pairing.code);
  await panel.locator("#pair").click();
  await panel
    .locator("#status")
    .filter({ hasText: "Paired for this Trove session" })
    .waitFor();
  await panel.locator("#capture").click();
  await panel
    .locator("#preview")
    .filter({ hasText: "Readable article" })
    .waitFor();
  const local = await panel.evaluate(() => chrome.storage.local.get("draft"));
  assert(!local.draft.html.includes("script"));
  assert(!local.draft.html.includes("Omit navigation"));
  assert.equal(local.draft.images.length, 0);
  // Headless native permission prompts are not automatable. This grant boundary
  // is an explicit fixture; acquisition itself uses the real extension fetch,
  // storage, message route and canonical importer. The browser must approve the
  // exact origin in an interactive installation.
  await panel.evaluate(() => {
    chrome.permissions.request = async (details) => {
      window.requestedImageOrigins = details.origins;
      return true;
    };
    chrome.permissions.remove = async (details) => {
      window.removedImageOrigins = details.origins;
      return true;
    };
  });
  await panel.locator("#images input").check();
  await panel.locator("#acquire").click();
  await panel
    .locator("#status")
    .filter({ hasText: "Retained 1 explicitly acquired image" })
    .waitFor();
  assert.deepEqual(await panel.evaluate(() => window.requestedImageOrigins), [
    "https://clipper-assets.example.test/*",
  ]);
  assert.deepEqual(await panel.evaluate(() => window.removedImageOrigins), [
    "https://clipper-assets.example.test/*",
  ]);
  evidence.explicitImage = true;
  await panel.locator("#destination").selectOption(destinationId);
  await panel.locator("#send").click();
  await panel
    .locator("#status")
    .filter({ hasText: "Draft received" })
    .waitFor();
  evidence.article = true;
  await panel.close();
  await source.evaluate(() => {
    const range = document.createRange();
    range.selectNodeContents(document.querySelector("#selected"));
    const selection = getSelection();
    selection.removeAllRanges();
    selection.addRange(range);
  });
  panel = await popup();
  await panel.locator("#mode").selectOption("selection");
  await panel.locator("#capture").click();
  await panel
    .locator("#preview")
    .filter({ hasText: "Selected bold text" })
    .waitFor();
  await panel.locator("#destination").selectOption("inbox");
  await panel.locator("#send").click();
  await panel
    .locator("#status")
    .filter({ hasText: "Draft received" })
    .waitFor();
  evidence.selection = true;
  await panel.close();
  panel = await popup();
  await panel.locator("#mode").selectOption("url");
  await panel.locator("#capture").click();
  await panel.locator("#preview").filter({ hasText: sourceURL }).waitFor();
  await panel.locator("#send").click();
  await panel
    .locator("#status")
    .filter({ hasText: "Draft received" })
    .waitFor();
  evidence.url = true;
  await panel.locator("#capture").click();
  await panel.locator("#preview").filter({ hasText: sourceURL }).waitFor();
  await fs.writeFile(revokeFile, "revoke");
  await new Promise((resolve) => setTimeout(resolve, 150));
  await panel.locator("#send").click();
  await panel
    .locator("#error")
    .filter({ hasText: "local draft is retained" })
    .waitFor();
  await panel.reload();
  await panel.locator("#preview").filter({ hasText: sourceURL }).waitFor();
  assert((await panel.evaluate(() => chrome.storage.local.get("draft"))).draft);
  evidence.unavailableDraftRetained = true;
  evidence.sourceDOMUnchanged =
    (await source.locator("article").innerHTML()) === original;
  assert(evidence.sourceDOMUnchanged);
  console.log(JSON.stringify(evidence));
} finally {
  await context.close();
  await fs.rm(profile, { recursive: true, force: true });
}
