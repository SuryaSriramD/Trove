import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./tests",
  testMatch: ["browser.spec.ts", "*.browser.spec.ts"],
  testIgnore: "**/._*",
  workers: 1,
  retries: 0,
  timeout: 30000,
  use: {
    headless: true,
    viewport: { width: 1360, height: 950 },
    launchOptions: {
      executablePath:
        process.env.CHROME_PATH ??
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
      args: ["--no-sandbox"],
    },
  },
  reporter: [
    ["list"],
    ["json", { outputFile: process.env.TROVE_BROWSER_REPORT ?? "../evidence/phase1-browser-tests.json" }],
  ],
});
