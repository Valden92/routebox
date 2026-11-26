import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  symlinkSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";
import { prepareCheckout } from "./development-checkout.mjs";

function fixture(t) {
  const base = mkdtempSync(join(tmpdir(), "routebox-checkout-test-"));
  t.after(() => rmSync(base, { recursive: true, force: true }));
  const root = join(base, "runtime");
  mkdirSync(join(root, "desktop"), { recursive: true });
  const git = (...args) =>
    execFileSync(
      "git",
      ["-C", root, "-c", "core.hooksPath=/dev/null", ...args],
      { encoding: "utf8", stdio: "pipe" },
    ).trim();
  git("init", "-b", "feature/test");
  writeFileSync(join(root, "desktop/package.json"), "{}\n");
  writeFileSync(join(root, "desktop/package-lock.json"), "{}\n");
  writeFileSync(join(root, "app.txt"), "committed\n");
  git("add", ".");
  git(
    "-c",
    "user.name=Test",
    "-c",
    "user.email=test@example.invalid",
    "commit",
    "-m",
    "fixture",
  );
  git("remote", "add", "origin", "git@example.invalid:routebox.git");
  return { base, root, git };
}

test("independent checkout preserves runtime files and uses the existing branch", (t) => {
  const { base, root, git } = fixture(t);
  writeFileSync(join(root, "app.txt"), "dirty runtime\n");
  mkdirSync(join(root, "desktop/node_modules/.vite"), { recursive: true });
  writeFileSync(
    join(root, "desktop/node_modules/dependency.txt"),
    "dependency\n",
  );
  writeFileSync(
    join(root, "desktop/node_modules/.vite/cache"),
    "runtime cache\n",
  );
  const target = join(base, "development");
  const result = prepareCheckout(root, target);
  assert.equal(result.branch, "feature/test");
  assert.equal(result.dependenciesCopied, true);
  assert.equal(readFileSync(join(root, "app.txt"), "utf8"), "dirty runtime\n");
  assert.equal(readFileSync(join(target, "app.txt"), "utf8"), "committed\n");
  assert.equal(existsSync(join(target, "desktop/node_modules/.vite")), false);
  writeFileSync(
    join(target, "desktop/node_modules/dependency.txt"),
    "changed\n",
  );
  assert.equal(
    readFileSync(join(root, "desktop/node_modules/dependency.txt"), "utf8"),
    "dependency\n",
  );
  assert.equal(git("config", "vpnrouter.developmentRoot"), target);
  assert.equal(
    execFileSync(
      "git",
      ["-C", target, "config", "--bool", "vpnrouter.development"],
      { encoding: "utf8" },
    ).trim(),
    "true",
  );
  assert.equal(
    execFileSync("git", ["-C", target, "remote", "get-url", "origin"], {
      encoding: "utf8",
    }).trim(),
    "git@example.invalid:routebox.git",
  );
});

test("existing directory and source aliases are never overwritten", (t) => {
  const { base, root } = fixture(t);
  const target = join(base, "existing");
  mkdirSync(target);
  writeFileSync(join(target, "keep"), "keep");
  assert.throws(() => prepareCheckout(root, target), /уже существует/);
  assert.equal(readFileSync(join(target, "keep"), "utf8"), "keep");
  const alias = join(base, "alias");
  symlinkSync(root, alias);
  assert.throws(() => prepareCheckout(root, alias), /отдельным каталогом/);
  assert.throws(
    () => prepareCheckout(root, join(root, "desktop/dev")),
    /отдельным каталогом/,
  );
});

test("uncommitted dependency changes are not copied into an older checkout", (t) => {
  const { base, root } = fixture(t);
  mkdirSync(join(root, "desktop/node_modules"));
  writeFileSync(
    join(root, "desktop/node_modules/marker"),
    "runtime dependency",
  );
  writeFileSync(join(root, "desktop/package.json"), '{"changed":true}\n');
  const result = prepareCheckout(root, join(base, "development"));
  assert.equal(result.dependenciesCopied, false);
});

test("dependency symlinks cannot keep development connected to runtime files", (t) => {
  const { base, root } = fixture(t);
  mkdirSync(join(root, "desktop/node_modules"));
  symlinkSync(join(root, "app.txt"), join(root, "desktop/node_modules/shared"));
  const target = join(base, "development");
  assert.equal(prepareCheckout(root, target).dependenciesCopied, false);
  assert.equal(existsSync(join(target, "desktop/node_modules")), false);
});

test("development Makefile refuses runtime targets, including its default goal", () => {
  const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
  for (const goal of [
    [],
    ["dev"],
    ["stop"],
    ["desktop"],
    ["sync"],
    ["recover-network"],
  ]) {
    const result = spawnSync(
      "make",
      ["-n", "-f", join(root, "Makefile"), ...goal],
      {
        cwd: root,
        encoding: "utf8",
        env: {
          ...process.env,
          GIT_CONFIG_COUNT: "1",
          GIT_CONFIG_KEY_0: "vpnrouter.development",
          GIT_CONFIG_VALUE_0: "true",
        },
      },
    );
    assert.notEqual(result.status, 0);
    assert.match(result.stderr, /DEV checkout/);
  }
});
