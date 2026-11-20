import { copyFile, chmod, mkdir, readFile, rename, stat, writeFile } from "node:fs/promises";
import { homedir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { spawnSync } from "node:child_process";
import { pathToFileURL } from "node:url";

export const serviceName = "routebox-daemon.service";
const marker = "# Managed by Router BOX";
export function servicePaths(env = process.env) {
  const home = env.HOME || homedir();
  const config = env.XDG_CONFIG_HOME || join(home, ".config");
  return { unit: join(config, "systemd/user", serviceName), binary: join(home, ".local/lib/routebox/vpn-router-daemon"), config };
}
function quote(value, expandDollar = false) {
  if (/[\r\n\0]/.test(value)) throw new Error("Invalid service path");
  if (expandDollar) value = value.replaceAll("$", () => "$$");
  return `"${value.replaceAll("\\", "\\\\").replaceAll('"', '\\"').replaceAll("%", "%%")}"`;
}
export function renderServiceUnit(env = process.env) {
  const paths = servicePaths(env);
  return `${marker}
[Unit]
Description=Router BOX routing daemon
StartLimitIntervalSec=60
StartLimitBurst=3

[Service]
Type=exec
ExecStart=${quote(paths.binary, true)} --listen 127.0.0.1:47891
Environment=${quote("XDG_CONFIG_HOME=" + paths.config)}
UMask=0077
Restart=on-failure
RestartSec=5
RestartPreventExitStatus=73
KillMode=mixed
TimeoutStopSec=60

[Install]
WantedBy=default.target
`;
}
export async function isInstalled(env = process.env) {
  try { return (await stat(servicePaths(env).unit)).isFile(); }
  catch (error) { if (error.code === "ENOENT") return false; throw error; }
}
function systemctl(args) {
  const result = spawnSync("systemctl", ["--user", ...args], { stdio: "inherit" });
  if (result.error) throw result.error;
  if (result.status !== 0) throw new Error(`systemctl --user ${args[0]} failed`);
}
export async function installService(binary, { env = process.env, control = systemctl } = {}) {
  const paths = servicePaths(env);
  const unit = renderServiceUnit(env);
  try {
    if (!(await readFile(paths.unit, "utf8")).startsWith(marker + "\n")) throw new Error("Existing service is not managed by Router BOX");
  } catch (error) { if (error.code !== "ENOENT") throw error; }
  await mkdir(dirname(paths.binary), { recursive: true, mode: 0o700 });
  await mkdir(dirname(paths.unit), { recursive: true, mode: 0o700 });
  const suffix = `.tmp-${process.pid}-${Date.now()}`;
  await copyFile(binary, paths.binary + suffix);
  await chmod(paths.binary + suffix, 0o700);
  await rename(paths.binary + suffix, paths.binary);
  await writeFile(paths.unit + suffix, unit, { mode: 0o600 });
  await rename(paths.unit + suffix, paths.unit);
  control(["daemon-reload"]);
  // Installation never starts/restarts/enables routing or enables lingering.
}
export async function serviceAction(action, args = []) {
  if (action === "installed") return await isInstalled() ? 0 : 1;
  if (action === "install") {
    if (args.length !== 1) throw new Error("Expected daemon binary path");
    await installService(args[0]); return 0;
  }
  if (!["start", "restart", "stop", "status"].includes(action)) throw new Error("Unknown service action");
  if (!await isInstalled()) throw new Error("Run make service-install first");
  systemctl([action, serviceName]); return 0;
}
if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try { process.exitCode = await serviceAction(process.argv[2], process.argv.slice(3)); }
  catch (error) { console.error(error.message); process.exitCode = 1; }
}
