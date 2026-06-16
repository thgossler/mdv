import { defineConfig } from "vite";
import wails from "@wailsio/runtime/plugins/vite";

// mdv frontend build configuration. The Wails plugin wires up the generated
// bindings in ./bindings so Go services are callable from TypeScript.
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  build: {
    // Inline assets and keep chunking simple for fast cold start.
    chunkSizeWarningLimit: 4096,
    rollupOptions: {
      output: {
        manualChunks(id: string) {
          if (id.includes("node_modules/mermaid")) return "mermaid";
          if (id.includes("node_modules/katex")) return "katex";
          if (id.includes("node_modules/highlight.js")) return "hljs";
          return undefined;
        },
      },
    },
  },
  plugins: [wails("./bindings")],
});
