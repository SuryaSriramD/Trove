import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: ".",
  testMatch: "database.browser.spec.ts",
  workers: 1,
  retries: 0,
  timeout: 30000,
  use: {
    headless: true,
    viewport: { width: 1440, height: 1100 },
    launchOptions: {
      executablePath:
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
      args: ["--no-sandbox"],
    },
  },
  reporter: [
    ["list"],
    [
      "json",
      {
        outputFile:
          (globalThis as any).process?.env?.TROVE_BROWSER_REPORT ??
          "test-results/database-browser-report.json",
      },
    ],
  ],
});
