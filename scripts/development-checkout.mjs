import { execFileSync } from "node:child_process";
import {
  cpSync,
  existsSync,
  lstatSync,
  readFileSync,
  readdirSync,
  readlinkSync,
  realpathSync,
} from "node:fs";
import {
  basename,
  dirname,
  isAbsolute,
  relative,
  resolve,
  sep,
} from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

function git(root, ...args) {
  return execFileSync("git", ["-C", root, ...args], {
    encoding: "utf8",
  }).trim();
}

function canonicalDestination(path) {
  if (existsSync(path)) return realpathSync(path);
  const parent = dirname(path);
  return resolve(canonicalDestination(parent), basename(path));
}

function hasSharedDependencyLinks(directory, root = directory) {
  if (lstatSync(directory).isSymbolicLink()) return true;
  for (const entry of readdirSync(directory, { withFileTypes: true })) {
    if ([".vite", ".vite-temp", ".cache"].includes(entry.name)) continue;
    const path = resolve(directory, entry.name);
    if (entry.isSymbolicLink()) {
      if (isAbsolute(readlinkSync(path))) return true;
      try {
        if (!realpathSync(path).startsWith(root + sep)) return true;
      } catch {
        return true;
      }
    } else if (entry.isDirectory() && hasSharedDependencyLinks(path, root)) {
      return true;
    }
  }
  return false;
}

export function prepareCheckout(source, destination, copyDependencies = true) {
  const root = realpathSync(source);
  const target = resolve(destination);
  const canonical = canonicalDestination(target);
  const inside = relative(root, canonical);
  if (
    canonical === root ||
    root.startsWith(canonical + sep) ||
    (!inside.startsWith(".." + sep) &&
      !isAbsolute(inside) &&
      !inside.startsWith(".tmp" + sep))
  ) {
    throw new Error(
      "Dev-checkout должен быть отдельным каталогом вне исходников приложения (или внутри .tmp/)",
    );
  }
  if (existsSync(target)) {
    throw new Error(
      `Каталог уже существует, его содержимое сохранено: ${target}`,
    );
  }
  const branch = git(root, "symbolic-ref", "--short", "HEAD");
  const origin = git(root, "remote", "get-url", "origin");
  if (/^https?:\/\/[^/]*@/i.test(origin)) {
    throw new Error(
      "origin с учётными данными в URL не копируется; используйте SSH или credential helper",
    );
  }

  execFileSync(
    "git",
    [
      "-c",
      "core.hooksPath=/dev/null",
      "clone",
      "--local",
      "--no-hardlinks",
      "--branch",
      branch,
      "--",
      root,
      target,
    ],
    { stdio: "pipe" },
  );
  git(target, "remote", "set-url", "origin", origin);
  git(target, "config", "vpnrouter.development", "true");
  git(target, "config", "vpnrouter.runtimeRoot", root);
  git(root, "config", "vpnrouter.developmentRoot", target);

  let dependenciesCopied = false;
  const packagesMatch = ["package.json", "package-lock.json"].every((name) => {
    const original = resolve(root, "desktop", name);
    const cloned = resolve(target, "desktop", name);
    return (
      existsSync(original) &&
      existsSync(cloned) &&
      readFileSync(original).equals(readFileSync(cloned))
    );
  });
  if (
    copyDependencies &&
    packagesMatch &&
    existsSync(resolve(root, "desktop/node_modules")) &&
    !hasSharedDependencyLinks(resolve(root, "desktop/node_modules"))
  ) {
    cpSync(
      resolve(root, "desktop/node_modules"),
      resolve(target, "desktop/node_modules"),
      {
        recursive: true,
        verbatimSymlinks: true,
        filter: (path) =>
          ![".vite", ".vite-temp", ".cache"].includes(basename(path)),
      },
    );
    dependenciesCopied = true;
  }
  return { target, branch, dependenciesCopied };
}

if (
  process.argv[1] &&
  import.meta.url === pathToFileURL(resolve(process.argv[1])).href
) {
  const root = fileURLToPath(new URL("../", import.meta.url));
  const target = process.argv[2] || resolve(root, ".tmp/development");
  try {
    const result = prepareCheckout(root, target);
    console.log(`Dev-checkout: ${result.target}\nВетка: ${result.branch}`);
    console.log(
      "Скопирован закоммиченный HEAD. Рабочие конфиги и бинарники не переносились.",
    );
    console.log(
      result.dependenciesCopied
        ? "Node-зависимости скопированы отдельно, без установки."
        : "При необходимости установите Node-зависимости только в dev-checkout.",
    );
    console.log(
      "Правки, сборки и проверки выполняйте там. Команды управления сетью по умолчанию заблокированы.",
    );
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}
