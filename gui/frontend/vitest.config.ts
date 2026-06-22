import { defineConfig } from "vitest/config";

// Vitest configuration for the frontend resilience tests. The render pipeline
// relies on DOMPurify and DOM APIs, so tests run in a jsdom environment.
export default defineConfig({
  test: {
    environment: "jsdom",
    include: ["src/**/*.test.ts"],
  },
});
