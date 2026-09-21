import { defineConfig } from "@playwright/test";

export default defineConfig({
    testDir: "./tests/browser",
    workers: 1,
    timeout: 30_000,
    use: { baseURL: "http://127.0.0.1:8097", screenshot: "only-on-failure", trace: "retain-on-failure" },
    webServer: {
        command: "node tests/browser/server.mjs",
        url: "http://127.0.0.1:8097/api/health",
        timeout: 120_000,
        reuseExistingServer: false,
    },
});
