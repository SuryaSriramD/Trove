import { defineConfig } from "@playwright/test";
import path from "node:path";
const evidence = process.env.TROVE_REAL_UI_EVIDENCE;
const stage = process.env.TROVE_REAL_UI_STAGE;
if (!evidence || !path.isAbsolute(evidence) || !["create", "reopen"].includes(stage ?? ""))
  throw new Error("Run through scripts/run-real-ui.py with a new evidence directory.");
export default defineConfig({
  testDir: "./tests",
  testMatch: "real-storage.integration.spec.ts",
  workers: 1,
  retries: 0,
  forbidOnly: true,
  timeout: 60000,
  outputDir: path.join(evidence, stage!, "artifacts"),
  reporter: [["list"], ["json", { outputFile: path.join(evidence, stage!, "playwright.json") }]],
  use: {
    baseURL: process.env.TROVE_REAL_UI_URL,
    headless: true,
    viewport: { width: 1360, height: 950 },
    screenshot: "only-on-failure",
    trace: "off", // Authentication secrets must not enter trace archives.
    launchOptions: { executablePath: process.env.CHROME_PATH ?? "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" },
  },
});
