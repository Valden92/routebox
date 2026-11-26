import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, mkdir, writeFile, readFile, symlink, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { stopDaemon } from "./stop-daemon.mjs";

async function fixture(t, { storedStart = "101", processStart = "101", executable = "vpn-router-daemon", legacy = false } = {}) {
  const root = await mkdtemp(join(tmpdir(), "routebox-stop-test-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const runtimeDir = join(root, "runtime"), procRoot = join(root, "proc"), pid = 45678;
  await mkdir(join(procRoot, String(pid)), { recursive: true });
  const exe = join(root, "bin", executable);
  await symlink(exe + " (deleted)", join(procRoot, String(pid), "exe"));
  const fields = ["S", ...Array(18).fill("0"), processStart];
  await writeFile(join(procRoot, String(pid), "stat"), `${pid} (test (name)) ${fields.join(" ")}`);
  await mkdir(runtimeDir, { recursive: true });
  if (legacy) await writeFile(join(root, ".daemon.pid"), String(pid));
  else await writeFile(join(runtimeDir, "routebox-daemon.lock"), JSON.stringify({ pid, executable: exe, started: storedStart }));
  return { root, runtimeDir, procRoot, pid };
}

test("stops only the recorded owner with SIGTERM and preserves the lock inode", async t => {
  const f = await fixture(t);
  const signals = [];
  assert.equal(await stopDaemon(f.root, { ...f, signal: pid => { signals.push(pid); }, wait: () => rm(join(f.procRoot, String(f.pid)), { recursive: true }) }), true);
  assert.deepEqual(signals, [f.pid]);
  assert.ok(await readFile(join(f.runtimeDir, "routebox-daemon.lock"), "utf8"));
});

test("migrates the old checkout PID file without killing all named processes", async t => {
  const f = await fixture(t, { legacy: true });
  const signals = [];
  assert.equal(await stopDaemon(f.root, { ...f, signal: pid => { signals.push(pid); }, wait: () => rm(join(f.procRoot, String(f.pid)), { recursive: true }) }), true);
  assert.deepEqual(signals, [f.pid]);
  await assert.rejects(readFile(join(f.root, ".daemon.pid")), { code: "ENOENT" });
});

for (const [name, options] of [["reused PID", { storedStart: "99" }], ["unrelated executable", { executable: "other-app" }]]) {
  test(`does not stop ${name}`, async t => {
    const f = await fixture(t, options);
    assert.equal(await stopDaemon(f.root, { ...f, signal: () => assert.fail("sent a signal") }), false);
  });
}

test("does not force kill after graceful stop times out", async t => {
  const f = await fixture(t);
  let signals = 0;
  await assert.rejects(stopDaemon(f.root, { ...f, signal: () => signals++, wait: async () => {}, attempts: 2 }), /no force kill/);
  assert.equal(signals, 1);
});
