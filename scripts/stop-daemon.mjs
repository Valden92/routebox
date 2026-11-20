import { readFile, readlink, unlink } from "node:fs/promises";
import { basename, join, resolve } from "node:path";
import { pathToFileURL } from "node:url";

async function optionalRead(path) {
  try { return await readFile(path, "utf8"); }
  catch (error) { if (error.code === "ENOENT" || error.code === "ESRCH") return null; throw error; }
}
export async function processIdentity(pid, procRoot = "/proc") {
  if (!Number.isSafeInteger(pid) || pid <= 1) return null;
  try {
    const executable = (await readlink(join(procRoot, String(pid), "exe"))).replace(/ \(deleted\)$/, "");
    const raw = await readFile(join(procRoot, String(pid), "stat"), "utf8");
    const fields = raw.slice(raw.lastIndexOf(")") + 1).trim().split(/\s+/);
    if (fields[0] === "Z" || !fields[19]) return null;
    return { pid, executable, started: fields[19] };
  } catch (error) { if (["ENOENT", "ESRCH"].includes(error.code)) return null; throw error; }
}
export async function stopDaemon(root, { runtimeDir = `/run/user/${process.getuid()}`, procRoot = "/proc", signal = (pid) => process.kill(pid, "SIGTERM"), wait = () => new Promise(r => setTimeout(r, 100)), attempts = 600 } = {}) {
  const record = await optionalRead(join(runtimeDir, "routebox-daemon.lock"));
  const legacyPath = join(root, ".daemon.pid");
  const legacy = await optionalRead(legacyPath);
  let expected = null;
  if (record) {
    try { expected = JSON.parse(record); } catch { /* A startup may still be writing its record; never guess a PID. */ }
  }
  const matches = (actual, owner) => actual && owner && actual.pid === owner.pid && actual.executable === owner.executable && (!owner.started || actual.started === owner.started) && basename(actual.executable) === "vpn-router-daemon";
  let actual = expected && await processIdentity(expected.pid, procRoot);
  if (!matches(actual, expected)) {
    const pid = /^\d+\s*$/.test(legacy || "") ? Number(legacy.trim()) : 0;
    actual = await processIdentity(pid, procRoot);
    expected = { pid, executable: join(resolve(root), "bin/vpn-router-daemon") };
    if (!matches(actual, expected)) return false;
    // Even for the old PID file, pin this process's start time before stopping.
    expected.started = actual.started;
  }
  try { signal(expected.pid); }
  catch (error) { if (error.code !== "ESRCH") throw error; }
  for (let i = 0; i < attempts; i++) {
    if (!matches(await processIdentity(expected.pid, procRoot), expected)) {
      if (legacy !== null && await optionalRead(legacyPath) === legacy) await unlink(legacyPath);
      return true;
    }
    await wait();
  }
  throw new Error("Daemon has not stopped gracefully; no force kill was sent");
}
if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try { await stopDaemon(resolve(process.argv[2] || ".")); }
  catch (error) { console.error(error.message); process.exitCode = 1; }
}
