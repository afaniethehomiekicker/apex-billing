import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import dotenv from "dotenv";
import path from "path";
import { fileURLToPath } from "url";
import { defineConfig } from "vite";

// The dev server (npm run dev, port 3001) sends /api calls to the Go server
// on PORT from the project's .env (default 8090). In production the Go
// program serves these built pages itself, so no proxy is involved.
const __dirname = path.dirname(fileURLToPath(import.meta.url));
dotenv.config({ path: path.resolve(__dirname, "../.env") });
const target = process.env.VITE_PROXY_TARGET || `http://localhost:${process.env.PORT || "8090"}`;

export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: { port: 3001, host: true, proxy: { "/api": { target, changeOrigin: true } } },
  preview: { port: 4174, host: true, proxy: { "/api": { target, changeOrigin: true } } },
  build: {
    rollupOptions: { output: { manualChunks: { vendor: ["react", "react-dom", "lucide-react"] } } },
  },
});
