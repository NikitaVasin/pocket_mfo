import assert from "node:assert/strict";
import { execFileSync } from "node:child_process";

const project = `pocket-mfo-smoke-${process.pid}`;
const port = process.env.POCKETBASE_TEST_PORT || "8098";
const env = { ...process.env, POCKETBASE_PORT: port };
const args = ["compose", "-f", "example/compose.yaml", "-p", project];
const compose = (...command) => execFileSync("docker", [...args, ...command], { env, stdio: "inherit" });
const base = `http://127.0.0.1:${port}`;

async function ready() {
    for (let attempt = 0; attempt < 60; attempt++) {
        try { if ((await fetch(base + "/api/health")).ok) return; } catch {}
        await new Promise(resolve => setTimeout(resolve, 500));
    }
    throw new Error("PocketBase did not become healthy");
}

async function json(path, options = {}) {
    const response = await fetch(base + path, options);
    assert.equal(response.ok, true, `${path}: ${response.status}`);
    return response.json();
}

try {
    compose("up", "-d", "--build", "--wait", "--wait-timeout", "60");
    await ready();
    const admin = "admin@admin.com";
    const password = "123456";
    const auth = await json("/api/collections/_superusers/auth-with-password", {
        method: "POST", headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ identity: admin, password }),
    });
    const before = await json("/api/collections/comments/records?expand=subject");
    assert.equal(before.totalItems, 2);
    const record = before.items[0];
    assert.ok(record.expand.subject.title);
    assert.equal(Object.keys(record).some(key => key.startsWith("pmr_")), false);
    await json(`/api/collections/comments/records/${record.id}`, {
        method: "PATCH", headers: { "Content-Type": "application/json", Authorization: auth.token },
        body: JSON.stringify({ text: "docker-persistence-check" }),
    });
    const homepage = await json("/api/collections/demo_homepage/records");
    assert.equal(homepage.totalItems, 1, "guest sees one default singleton record");
    const home = homepage.items[0];
    await json(`/api/collections/demo_homepage/records/${home.id}`, {
        method: "PATCH", headers: { "Content-Type": "application/json", Authorization: auth.token },
        body: JSON.stringify({ title: "singleton-persistence-check" }),
    });
    const duplicate = await fetch(base + "/api/collections/demo_homepage/records", {
        method: "POST", headers: { "Content-Type": "application/json", Authorization: auth.token },
        body: JSON.stringify({ title: "duplicate", content_set: home.content_set }),
    });
    assert.equal(duplicate.status, 400, "database rejects a second singleton record");
    compose("restart");
    await ready();
    const after = await json("/api/collections/comments/records?expand=subject");
    assert.equal(after.totalItems, 2, "seed must not run twice");
    const persisted = after.items.find(item => item.id === record.id);
    assert.equal(persisted.text, "docker-persistence-check", "restart must preserve edits");
    assert.deepEqual(persisted.subject, record.subject);
    const homeAfter = await json("/api/collections/demo_homepage/records");
    assert.equal(homeAfter.totalItems, 1);
    assert.equal(homeAfter.items[0].title, "singleton-persistence-check");
    const extension = await fetch(base + "/_/extensions.js");
    const source = await extension.text();
    assert.ok(source.includes("polymorphicRelation"));
    assert.ok(source.includes("ps-record-form"));
    console.log("Docker smoke passed: health, embedded UI, expand, singleton constraint, persistent edits and one-time seed.");
} finally {
    // Only removes resources belonging to this uniquely named test project.
    compose("down", "--volumes", "--remove-orphans");
}
