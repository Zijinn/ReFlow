/// <reference types="vitest/config" />

import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"
import { VitePWA } from "vite-plugin-pwa"

const devAPITarget = process.env.REFLOW_DEV_API ?? "http://127.0.0.1:7381"
const devPort = Number(process.env.REFLOW_DEV_PORT ?? 4173)

export default defineConfig(({ mode }) => {
  const desktopBuild = mode === "desktop"
  return {
    plugins: [
      react(),
      {
        name: "reflow-desktop-compatible-entry",
        apply: "build",
        transformIndexHtml: {
          order: "post",
          handler(html) {
            // WKWebView does not request Vite's module entry over the wails://
            // scheme, so the desktop bundle (format iife, no ESM syntax) loads
            // as a deferred classic script. The default build stays ESM:
            // stripping type="module" there makes the entry throw as a classic
            // script, and reflow-server's static web/dist renders nothing.
            if (!desktopBuild) return html
            return html
              .replace(/<script type="module" crossorigin([^>]*)>/g, "<script defer$1>")
              .replace(/<link rel="stylesheet" crossorigin([^>]*)>/g, '<link rel="stylesheet"$1>')
          },
        },
      },
      VitePWA({
        registerType: "autoUpdate",
        includeAssets: [
          "icons/reflow-32.png",
          "icons/reflow-180.png",
          "icons/reflow-192.png",
          "icons/reflow-512.png",
        ],
        manifest: {
          name: "ReFlow",
          short_name: "ReFlow",
          description: "A private reading home for the open web.",
          lang: "zh-CN",
          // 对齐有色画布的上段（--canvas-wash 顶端 #cfe0f6 与第二段 #e3eaf3 之间）：
          // 装机启动时系统状态栏与闪屏不再是"白纸接蓝纸"的那道接缝。
          theme_color: "#e9eff7",
          background_color: "#e3eaf3",
          display: "standalone",
          orientation: "any",
          start_url: "/",
          scope: "/",
          icons: [
            {
              src: "/icons/reflow-192.png",
              sizes: "192x192",
              type: "image/png",
            },
            {
              src: "/icons/reflow-512.png",
              sizes: "512x512",
              type: "image/png",
            },
            {
              src: "/icons/reflow-maskable-512.png",
              sizes: "512x512",
              type: "image/png",
              purpose: "maskable",
            },
          ],
        },
        workbox: {
          navigateFallback: "/index.html",
          globPatterns: ["**/*.{js,css,html,png,woff2}"],
          // Authenticated API responses are cached in IndexedDB with explicit
          // device-scoped lifecycle handling. Do not mirror them into a second,
          // URL-keyed CacheStorage bucket that survives re-pairing/revocation.
        },
      }),
    ],
    server: {
      host: "127.0.0.1",
      port: Number.isFinite(devPort) ? devPort : 4173,
      strictPort: true,
      proxy: {
        // Keep the browser's Host header: WebKit/Chromium send Origin on POST,
        // and rewriting only Host makes the API server see a cross-origin
        // request (Origin 4173 vs Host 7381). The Go server is the only target,
        // so virtual-host routing is not needed here.
        "/api": { target: devAPITarget, changeOrigin: false },
        "/healthz": { target: devAPITarget, changeOrigin: false },
      },
    },
    test: {
      environment: "jsdom",
      setupFiles: ["./src/test/setup.ts"],
      css: true,
    },
    build: desktopBuild
      ? {
          chunkSizeWarningLimit: 800,
          rollupOptions: {
            output: {
              inlineDynamicImports: true,
              format: "iife",
            },
          },
        }
      : undefined,
  }
})
