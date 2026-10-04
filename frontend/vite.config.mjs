import { defineConfig, searchForWorkspaceRoot } from "vite";
import { realpathSync } from "node:fs";
import { fileURLToPath } from "node:url";
const root = fileURLToPath(new URL(".", import.meta.url));
export default defineConfig({
  server: {
    hmr: process.env.TROVE_TEST_URL ? false : undefined,
    fs: {
      allow: [
        searchForWorkspaceRoot(root),
        realpathSync(new URL("./node_modules", import.meta.url)),
      ],
    },
  },
});
