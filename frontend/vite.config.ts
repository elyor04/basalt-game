import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import wails from "@wailsio/runtime/plugins/vite";

// Older Android System WebViews and iOS 15 Safari ignore media queries in the
// newer range syntax, so pin the output targets explicitly.
const targets = ["chrome107", "safari15", "edge107", "firefox110"];

export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9247,
    strictPort: true,
  },
  build: {
    target: targets,
    cssTarget: targets,
  },
  plugins: [react(), wails("./bindings")],
});
