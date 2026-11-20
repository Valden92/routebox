import test from "node:test";
import assert from "node:assert/strict";
import { mkdtemp, readFile, writeFile, stat, mkdir, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { installService, isInstalled, renderServiceUnit, servicePaths } from "./daemon-service.mjs";

test("service installation is private, independent of checkout and never activates routing", async t => {
  const root = await mkdtemp(join(tmpdir(), "routebox-service-test-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const env = { HOME: join(root, "home space"), XDG_CONFIG_HOME: join(root, "config space") };
  const binary = join(root, "compiled-daemon");
  await writeFile(binary, "first binary");
  const controls = [];
  await installService(binary, { env, control: args => controls.push(args) });
  const paths = servicePaths(env);
  assert.equal(await isInstalled(env), true);
  assert.equal(await readFile(paths.binary, "utf8"), "first binary");
  assert.equal((await stat(paths.binary)).mode & 0o777, 0o700);
  assert.equal((await stat(paths.unit)).mode & 0o777, 0o600);
  assert.deepEqual(controls, [["daemon-reload"]]);
  const unit = await readFile(paths.unit, "utf8");
  assert.ok(unit.includes('ExecStart="' + paths.binary + '" --listen 127.0.0.1:47891'));
  assert.ok(unit.includes("KillMode=mixed\n"));
  assert.ok(unit.includes("RestartPreventExitStatus=73\n"));
  assert.ok(unit.includes("StartLimitBurst=3\n"));
  assert.ok(!unit.includes(binary));
  await writeFile(binary, "next binary");
  await installService(binary, { env, control: args => controls.push(args) });
  assert.equal(await readFile(paths.binary, "utf8"), "next binary");
  assert.deepEqual(controls, [["daemon-reload"], ["daemon-reload"]]);
});

test("does not overwrite a manually managed unit", async t => {
  const root = await mkdtemp(join(tmpdir(), "routebox-custom-unit-"));
  t.after(() => rm(root, { recursive: true, force: true }));
  const env = { HOME: root };
  const paths = servicePaths(env);
  await mkdir(dirname(paths.unit), { recursive: true });
  await writeFile(paths.unit, "custom unit");
  await assert.rejects(installService("missing", { env, control: () => assert.fail("called systemctl") }), /not managed/);
  assert.equal(await readFile(paths.unit, "utf8"), "custom unit");
});

test("systemd paths escape specifiers, variables and quotes and reject line injection", () => {
  const unit = renderServiceUnit({ HOME: '/fixture $HOME %x "name"', XDG_CONFIG_HOME: "/custom/%n" });
  assert.ok(unit.includes('$$HOME %%x \\"name\\"'));
  assert.ok(unit.includes('Environment="XDG_CONFIG_HOME=/custom/%%n"'));
  assert.throws(() => renderServiceUnit({ HOME: "/tmp/fixture\nExecStart=bad" }), /Invalid service path/);
});
