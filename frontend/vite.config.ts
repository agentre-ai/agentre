/// <reference types="vitest/config" />
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

const wailsAppMock = fileURLToPath(
  new URL("./src/__tests__/mocks/wailsApp.ts", import.meta.url),
);
const wailsModelsMock = fileURLToPath(
  new URL("./src/__tests__/mocks/wailsModels.ts", import.meta.url),
);
const wailsImportPrefixes = [
  "../",
  "../../",
  "../../../",
  "../../../../",
  "../../../../../",
  "@/../",
];
const wailsTestAliases = Object.fromEntries(
  wailsImportPrefixes.flatMap((prefix) => [
    [`${prefix}wailsjs/go/app/App`, wailsAppMock],
    [`${prefix}wailsjs/go/models`, wailsModelsMock],
  ]),
);

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  test: {
    environment: "happy-dom",
    setupFiles: ["./src/__tests__/setup.ts"],
    alias: {
      // wailsjs/ is gitignored and only generated during `wails build`.
      // All relative import paths that point into wailsjs/ are aliased here
      // so tests can run without a generated wailsjs/ directory.
      ...wailsTestAliases,
    },
  },
});
