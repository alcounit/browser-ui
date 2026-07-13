import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  define: {
    __BUILD_NUMBER__: JSON.stringify("test"),
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
    css: false,
    coverage: {
      provider: "v8",
      reporter: ["text", "text-summary"],
      include: ["lib/**/*.ts", "components/**/*.tsx", "pages/VNCView.tsx"],
      exclude: ["**/*.test.ts", "**/*.test.tsx"],
    },
  },
});
