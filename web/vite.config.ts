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
            // scheme. The bundled entry has no external ESM imports, so load it
            // as a deferred classic script in production builds.
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
          theme_color: "#f5f5f6",
          background_color: "#ffffff",
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
          runtimeCaching: [
            {
              urlPattern: ({ url }) => url.pathname.startsWith("/api/v1/entries"),
              handler: "NetworkFirst",
              options: {
                cacheName: "reflow-entry-api",
                expiration: { maxEntries: 120, maxAgeSeconds: 60 * 60 * 24 * 7 },
                networkTimeoutSeconds: 4,
              },
            },
          ],
        },
        devOptions: { enabled: true },
      }),
    ],
    server: {
      host: "127.0.0.1",
      port: Number.isFinite(devPort) ? devPort : 4173,
      proxy: {
        "/api": devAPITarget,
        "/healthz": devAPITarget,
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
