import { defineConfig } from "@playwright/test";

const testPort = process.env.POCKETBASE_BROWSER_PORT || "8097";
const lockedPort = process.env.POCKETBASE_BROWSER_LOCKED_PORT || "8099";

export default defineConfig({
    testDir: "./tests/browser",
    workers: 1,
    timeout: 30_000,
    use: { baseURL: `http://127.0.0.1:${testPort}`, screenshot: "only-on-failure", trace: "retain-on-failure" },
    projects: [
        { name: "plugins", testIgnore: "**/schemalock.spec.js" },
        { name: "schemalock", testMatch: ["**/appmetrica.spec.js", "**/schemalock.spec.js", "**/partnerlinks.spec.js", "**/push-mcp.spec.js", "**/currencyrates.spec.js", "**/typedconfig.spec.js"], use: { baseURL: `http://127.0.0.1:${lockedPort}` } },
    ],
    webServer: [{
        command: "node tests/browser/server.mjs",
        url: `http://127.0.0.1:${testPort}/api/health`,
        timeout: 120_000,
        reuseExistingServer: false,
    }, {
        command: "node tests/browser/server.mjs --locked",
        url: `http://127.0.0.1:${lockedPort}/api/health`,
        timeout: 120_000,
        reuseExistingServer: false,
    }],
});
