import { mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { execFileSync, spawn } from "node:child_process";

const dir = mkdtempSync(join(tmpdir(), "pocket-mfo-browser-"));
const binary = join(dir, "pocketbase");
const data = join(dir, "pb_data");
const locked = process.argv.includes("--locked");
const port = locked ? 8099 : 8097;
try {
    execFileSync("go", ["build", "-o", binary, locked ? "./example" : "./tests/testapp"], { stdio: "inherit", env: { ...process.env, GOTOOLCHAIN: "auto" } });
    execFileSync(binary, ["migrate", "up", "--dir", data], { stdio: "inherit" });
    execFileSync("go", ["run", "./tests/browser/seed", data, locked ? "locked" : "unlocked"], { stdio: "inherit", env: { ...process.env, GOTOOLCHAIN: "auto" } });
    // Test-only credentials, created exclusively inside a disposable directory.
    execFileSync(binary, ["superuser", "create", "browser@example.test", "browser-test-password-123", "--dir", data], { stdio: "inherit" });
    const server = spawn(binary, ["serve", `--http=127.0.0.1:${port}`, "--dir", data], { stdio: "inherit" });
    for (const signal of ["SIGTERM", "SIGINT"]) process.on(signal, () => server.kill("SIGTERM"));
    server.on("exit", code => { rmSync(dir, { recursive: true, force: true }); process.exit(code || 0); });
} catch (err) {
    rmSync(dir, { recursive: true, force: true });
    throw err;
}
