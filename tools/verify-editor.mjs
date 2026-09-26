import { watch } from "chokidar";
import { spawn } from "node:child_process";
import { createHash } from "node:crypto";
import { existsSync, readFileSync, realpathSync } from "node:fs";
import { dirname, extname, isAbsolute, relative, resolve, sep } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

class RegistrationCancelled extends Error {
  constructor() {
    super("native watch closed before ready");
  }
}

export function connect(
  child,
  onFailure = () => {},
  onRequest = async (method) => {
    if (method !== "window/workDoneProgress/create")
      throw new Error(`unsupported client request: ${method}`);
    return null;
  },
) {
  let sequence = 0;
  let buffer = Buffer.alloc(0);
  let failure;
  let exitSent = false;
  const pending = new Map();
  function fail(error, fromProcessExit = false) {
    if (failure) {
      if (!fromProcessExit) onFailure(error, false);
      return;
    }
    failure = error;
    for (const waiter of pending.values()) {
      waiter.cleanup();
      waiter.reject(failure);
    }
    pending.clear();
    onFailure(error, fromProcessExit);
  }
  function send(message) {
    if (failure) throw failure;
    if (exitSent) throw new Error("native LSP connection has exited");
    const body = Buffer.from(JSON.stringify({ jsonrpc: "2.0", ...message }));
    child.stdin.write(Buffer.concat([Buffer.from(`Content-Length: ${body.length}\r\n\r\n`), body]));
    if (message.method === "exit") exitSent = true;
  }
  child.stdout.on("data", (chunk) => {
    try {
      buffer = Buffer.concat([buffer, chunk]);
      for (;;) {
        const end = buffer.indexOf("\r\n\r\n");
        if (end < 0) break;
        const lengths = [
          ...buffer
            .subarray(0, end)
            .toString("ascii")
            .matchAll(/^Content-Length: (\d+)\r?$/gim),
        ];
        if (lengths.length !== 1) throw new Error("LSP frame has no unique Content-Length");
        const length = Number(lengths[0][1]);
        if (!Number.isSafeInteger(length)) throw new Error("invalid LSP Content-Length");
        if (buffer.length < end + 4 + length) break;
        const message = JSON.parse(buffer.subarray(end + 4, end + 4 + length).toString("utf8"));
        buffer = buffer.subarray(end + 4 + length);
        if (message.jsonrpc !== "2.0") throw new Error("invalid LSP JSON-RPC version");
        if (message.method) {
          if (message.id !== undefined && !exitSent) {
            Promise.resolve()
              .then(() => {
                if (!exitSent) return onRequest(message.method, message.params);
              })
              .then(
                (result) => {
                  if (!exitSent) send({ id: message.id, result });
                },
                (error) => {
                  if (!exitSent)
                    send({
                      id: message.id,
                      error: {
                        code: error instanceof RegistrationCancelled ? -32800 : -32603,
                        message: String(error),
                      },
                    });
                  if (!(error instanceof RegistrationCancelled)) fail(error);
                },
              )
              .catch(fail);
          }
          continue;
        }
        const waiter = pending.get(message.id);
        if (!waiter) throw new Error(`unexpected LSP response: ${message.id}`);
        pending.delete(message.id);
        waiter.cleanup();
        if (message.error) waiter.reject(new Error(JSON.stringify(message.error)));
        else waiter.resolve(message.result);
      }
    } catch (error) {
      fail(error);
    }
  });
  child.on("error", fail);
  child.stdin.on("error", fail);
  child.stdout.on("error", fail);
  child.on("exit", (code, signal) =>
    fail(new Error(`native LSP exited ${code} (${signal})`), true),
  );
  return {
    notify(method, params) {
      send({ method, params });
    },
    request(method, params, signal) {
      if (failure) return Promise.reject(failure);
      if (exitSent) return Promise.reject(new Error("native LSP connection has exited"));
      if (signal?.aborted) return Promise.reject(signal.reason);
      const id = ++sequence;
      return new Promise((resolve, reject) => {
        const aborted = () => {
          try {
            send({ method: "$/cancelRequest", params: { id } });
          } catch (error) {
            fail(error);
          }
          reject(signal.reason);
        };
        const cleanup = () => signal?.removeEventListener("abort", aborted);
        pending.set(id, { resolve, reject, cleanup });
        signal?.addEventListener("abort", aborted, { once: true });
        try {
          send({ id, method, params });
        } catch (error) {
          fail(error);
        }
      });
    },
  };
}

export function watchScope(pattern) {
  let directory;
  let fileTarget;
  if (typeof pattern === "string" && pattern.endsWith("/**/*"))
    directory = pattern.slice(0, -5) || "/";
  else if (pattern?.pattern === "**/*")
    directory = fileURLToPath(
      typeof pattern.baseUri === "string" ? pattern.baseUri : pattern.baseUri.uri,
    );
  else if (typeof pattern === "string" && isAbsolute(pattern) && !/[*?{}\[\]]/.test(pattern)) {
    fileTarget = resolve(pattern);
    directory = dirname(fileTarget);
  } else throw new Error(`unsupported native watch pattern: ${JSON.stringify(pattern)}`);
  if (!isAbsolute(directory)) throw new Error("native watch directory must be absolute");
  const requested = resolve(directory);
  const canonical = !fileTarget && existsSync(requested) ? realpathSync(requested) : requested;
  if (!fileTarget && dirname(canonical) === canonical)
    throw new Error("native recursive watch cannot cover a filesystem root");
  return { requested, fileTarget };
}

export function watchRegistration(registration, notify, fail) {
  if (registration.method === "workspace/didChangeConfiguration")
    return { ready: Promise.resolve(), close: async () => {} };
  if (registration.method !== "workspace/didChangeWatchedFiles")
    throw new Error(`unsupported native registration: ${registration.method}`);
  const scopes = registration.registerOptions.watchers.map((spec) => ({
    ...watchScope(spec.globPattern),
    kind: spec.kind ?? 7,
  }));
  const handles = [];
  let active = true;
  let closing;
  const readiness = [];
  const rejectors = [];
  const close = () => {
    if (!closing) {
      active = false;
      for (const reject of rejectors) reject(new RegistrationCancelled());
      closing = Promise.all(handles.map((handle) => handle.close()));
    }
    return closing;
  };
  try {
    for (const { requested, fileTarget, kind } of scopes) {
      const handle = watch(requested, {
        depth: fileTarget ? 0 : undefined,
        ignored: (path, stats) => {
          if (fileTarget) {
            const within = relative(resolve(path), fileTarget);
            return within === ".." || within.startsWith(".." + sep) || isAbsolute(within);
          }
          return (
            resolve(path) !== requested &&
            stats !== undefined &&
            !stats.isFile() &&
            !stats.isDirectory() &&
            !stats.isSymbolicLink()
          );
        },
        followSymlinks: true,
        ignoreInitial: true,
        atomic: false,
      });
      handles.push(handle);
      readiness.push(
        new Promise((resolve, reject) => {
          rejectors.push(reject);
          handle.once("ready", () => {
            if (active) resolve();
            else reject(new RegistrationCancelled());
          });
          handle.on("error", (error) => {
            if (!active) return;
            if (error.code === "ELOOP" && typeof error.path === "string") {
              process.stderr.write(`${error}\n`);
              return;
            }
            reject(error);
            fail(error);
          });
        }),
      );
      handle.on("all", (event, path) => {
        if (!active) return;
        try {
          const file = resolve(path);
          if (fileTarget && file !== fileTarget) return;
          const within = relative(requested, file);
          if (within === ".." || within.startsWith("../") || isAbsolute(within)) return;
          const types =
            event === "change"
              ? [2]
              : event === "add" || event === "addDir"
                ? [1, 2]
                : event === "unlink" || event === "unlinkDir"
                  ? [3]
                  : [];
          const changes = types
            .filter((type) => (kind & (1 << (type - 1))) !== 0)
            .map((type) => ({ uri: pathToFileURL(file).href, type }));
          if (changes.length) notify("workspace/didChangeWatchedFiles", { changes });
        } catch (error) {
          fail(error);
        }
      });
    }
  } catch (error) {
    void close();
    for (const ready of readiness) void ready.catch(() => {});
    throw error;
  }
  return { ready: Promise.all(readiness), close };
}

export function languageId(file) {
  switch (extname(file)) {
    case ".tsx":
      return "typescriptreact";
    case ".jsx":
      return "javascriptreact";
    case ".ts":
    case ".mts":
    case ".cts":
      return "typescript";
    case ".js":
    case ".mjs":
    case ".cjs":
      return "javascript";
    default:
      throw new Error(`unsupported probe source extension: ${file}`);
  }
}

async function withOwnedProcess({ executable, args, cwd, signal, stderr = "pipe" }, callback) {
  signal?.throwIfAborted();
  const child = spawn(executable, args, {
    cwd,
    detached: process.platform !== "win32",
    stdio: ["pipe", "pipe", stderr],
  });
  const closed = new Promise((resolve) =>
    child.once("close", (code, signal) => resolve({ code, signal })),
  );
  let rejectFailure;
  const failure = new Promise((_, reject) => {
    rejectFailure = reject;
  });
  failure.catch(() => {});
  let stopping = false;
  function fail(error) {
    rejectFailure(error);
    if (stopping) return;
    stopping = true;
    if (child.pid) {
      try {
        if (process.platform === "win32") child.kill();
        else process.kill(-child.pid, "SIGKILL");
      } catch (killError) {
        if (killError.code !== "ESRCH") rejectFailure(killError);
      }
    }
  }
  child.on("error", fail);
  const aborted = () => fail(signal.reason);
  signal?.addEventListener("abort", aborted, { once: true });
  if (signal?.aborted) aborted();
  const wait = (operation) => Promise.race([failure, operation]);
  try {
    const result = await callback({ child, closed, fail, wait });
    await wait(closed);
    return result;
  } catch (error) {
    fail(error);
    throw error;
  } finally {
    await closed;
    signal?.removeEventListener("abort", aborted);
  }
}

export async function runCompiler(executable, args, signal, cwd = process.cwd()) {
  return withOwnedProcess({ executable, args, cwd, signal }, async ({ child, closed }) => {
    let stdout = "";
    let stderr = "";
    child.stdout.setEncoding("utf8");
    child.stderr.setEncoding("utf8");
    child.stdout.on("data", (chunk) => {
      stdout += chunk;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk;
    });
    const { code, signal } = await closed;
    return { status: code, signal, stdout, stderr };
  });
}

export async function withCommandSignals(callback) {
  const controller = new AbortController();
  const interrupt = () => controller.abort(new Error("editor command canceled by SIGINT"));
  const terminate = () => controller.abort(new Error("editor command canceled by SIGTERM"));
  process.on("SIGINT", interrupt);
  process.on("SIGTERM", terminate);
  try {
    return await callback(controller.signal);
  } finally {
    process.removeListener("SIGINT", interrupt);
    process.removeListener("SIGTERM", terminate);
  }
}

export async function withNativeSession({ executable, cwd, signal }, callback) {
  return withOwnedProcess(
    { executable, args: ["--lsp", "--stdio"], cwd, signal },
    async ({ child, closed, fail, wait }) => {
      let exiting = false;
      let stderr = "";
      child.stderr.setEncoding("utf8");
      child.stderr.on("data", (chunk) => {
        stderr += chunk;
        process.stderr.write(chunk);
      });
      child.stderr.on("error", fail);
      const registrations = new Map();
      let disposed;
      function disposeRegistrations() {
        if (!disposed) {
          disposed = Promise.all(
            [...registrations.values()].map((registration) => registration.close()),
          );
          registrations.clear();
        }
        return disposed;
      }
      const { request, notify } = connect(
        child,
        (error, fromProcessExit) => {
          if (!exiting || !fromProcessExit) fail(error);
        },
        async (method, params) => {
          if (disposed) throw new Error("native session closed");
          if (method === "client/registerCapability") {
            for (const registration of params.registrations) {
              if (disposed) throw new RegistrationCancelled();
              const previous = registrations.get(registration.id);
              const watcher = watchRegistration(registration, notify, fail);
              registrations.set(registration.id, watcher);
              await Promise.all([previous?.close(), watcher.ready]);
            }
          } else if (method === "client/unregisterCapability") {
            for (const registration of params.unregisterations) {
              const watcher = registrations.get(registration.id);
              registrations.delete(registration.id);
              await watcher?.close();
            }
          } else if (method !== "window/workDoneProgress/create") {
            throw new Error(`unsupported native request: ${method}`);
          }
          return null;
        },
      );
      try {
        const initialization = await wait(
          request("initialize", {
            processId: process.pid,
            rootUri: pathToFileURL(resolve(cwd)).href,
            capabilities: {
              general: { positionEncodings: ["utf-16"] },
              textDocument: { diagnostic: {} },
              workspace: {
                didChangeWatchedFiles: { dynamicRegistration: true, relativePatternSupport: true },
              },
            },
            initializationOptions: {
              logVerbosity: 5,
              disablePushDiagnostics: true,
              userPreferences: { tsserver: { automaticTypeAcquisition: { enabled: false } } },
            },
          }),
        );
        if (initialization.serverInfo?.name !== "typescript-go")
          throw new Error("selected executable is not the native TypeScript language server");
        if ((initialization.capabilities.positionEncoding || "utf-16") !== "utf-16")
          throw new Error("native LSP did not select UTF-16");
        notify("initialized", {});
        const opened = new Map();
        const session = {
          initialization,
          processId: child.pid,
          async query({
            file,
            operation,
            position,
            includeDeclaration = true,
            signal: requestSignal,
          }) {
            requestSignal?.throwIfAborted();
            const methods = {
              completion: ["textDocument/completion", "completionProvider"],
              definition: ["textDocument/definition", "definitionProvider"],
              hover: ["textDocument/hover", "hoverProvider"],
              references: ["textDocument/references", "referencesProvider"],
              diagnostics: ["textDocument/diagnostic", "diagnosticProvider"],
            };
            const method = Object.hasOwn(methods, operation) ? methods[operation] : undefined;
            if (!method) throw new Error(`unsupported editor operation: ${operation}`);
            if (!initialization.capabilities[method[1]])
              throw new Error(`native LSP lacks ${operation} support`);
            if (
              operation !== "diagnostics" &&
              (!position ||
                !Number.isInteger(position.line) ||
                position.line < 0 ||
                !Number.isInteger(position.character) ||
                position.character < 0)
            )
              throw new Error(
                "position must contain nonnegative UTF-16 line and character integers",
              );
            if (operation === "references" && typeof includeDeclaration !== "boolean")
              throw new Error("includeDeclaration must be a boolean");
            const absolute = resolve(cwd, file);
            const uri = pathToFileURL(absolute).href;
            const source = readFileSync(absolute, "utf8");
            const document = opened.get(absolute);
            if (!document) {
              notify("textDocument/didOpen", {
                textDocument: { uri, languageId: languageId(absolute), version: 1, text: source },
              });
              opened.set(absolute, { source, version: 1 });
            } else if (document.source !== source) {
              document.source = source;
              document.version++;
              notify("textDocument/didChange", {
                textDocument: { uri, version: document.version },
                contentChanges: [{ text: source }],
              });
            }
            const project = await wait(
              request("custom/projectInfo", { textDocument: { uri } }, requestSignal),
            );
            if (!project.configFilePath || !existsSync(project.configFilePath))
              throw new Error(`no configured project for ${absolute}`);
            const params = { textDocument: { uri } };
            if (operation !== "diagnostics") params.position = position;
            if (operation === "references") params.context = { includeDeclaration };
            let result = await wait(request(method[0], params, requestSignal));
            if (operation === "diagnostics") {
              if (result.kind !== "full" || !Array.isArray(result.items))
                throw new Error("native LSP did not return full diagnostics");
              result = result.items;
            } else if (operation === "definition" || operation === "references") {
              result = (result === null ? [] : Array.isArray(result) ? result : [result]).map(
                (location) => ({
                  ...location,
                  file: fileURLToPath(location.targetUri || location.uri),
                }),
              );
            }
            return {
              file: absolute,
              project,
              operation,
              result,
              serverInfo: initialization.serverInfo,
              executable,
            };
          },
        };
        const result = await wait(callback(session));
        await wait(request("shutdown"));
        await disposeRegistrations();
        exiting = true;
        notify("exit");
        const status = await wait(closed);
        const canceledExit =
          status.code === 1 && status.signal === null && stderr === "context canceled\n";
        if (status.code !== 0 && !canceledExit) {
          throw new Error(`native LSP exited ${status.code} (${status.signal})`);
        }
        return result;
      } finally {
        await disposeRegistrations();
      }
    },
  );
}

export async function queryNative(options, query) {
  return withNativeSession(options, (session) => session.query(query));
}

export function checkCompilerErrors(native, expectedError) {
  if (![0, 1, 2].includes(native.status)) throw new Error(`tsgo exited ${native.status}`);
  const hasErrors = /error TS\d+:/.test(native.stdout + "\n" + native.stderr);
  if (hasErrors !== expectedError || (native.status === 0) === hasErrors)
    throw new Error("unexpected compiler error status");
}

export async function verify(tsgo, probesFile, { signal } = {}) {
  const probes = JSON.parse(readFileSync(probesFile, "utf8"));
  const version = await runCompiler(tsgo, ["--version"], signal);
  if (version.status !== 0) throw new Error("cannot identify selected compiler");
  const identity = {
    executable: realpathSync(tsgo),
    sha256: createHash("sha256").update(readFileSync(tsgo)).digest("hex"),
    version: version.stdout.trim(),
  };
  const results = [];
  const nativeProjects = new Map();
  const projectDocuments = new Map();
  const checks = [];
  let initialization;
  try {
    await withNativeSession({ executable: tsgo, cwd: process.cwd(), signal }, async (session) => {
      initialization = session.initialization;
      if (initialization.serverInfo.version !== identity.version.replace(/^Version /, ""))
        throw new Error("native LSP and CLI compiler versions differ");
      for (const probe of probes) {
        const file = resolve(probe.file);
        const source = readFileSync(file, "utf8");
        const offset = source.indexOf(probe.symbol);
        if (offset < 0 || source.indexOf(probe.symbol, offset + 1) !== -1)
          throw new Error(`probe symbol must occur exactly once in ${file}: ${probe.symbol}`);
        const diagnostic = await session.query({ file, operation: "diagnostics" });
        const project = diagnostic.project;
        if (probe.project && resolve(project.configFilePath) !== resolve(probe.project))
          throw new Error(`wrong editor project: ${project.configFilePath}`);
        const diagnostics = diagnostic.result;
        const before = source.slice(0, offset);
        const position = {
          line: before.split("\n").length - 1,
          character: offset - before.lastIndexOf("\n") - 1,
        };
        const definitions = (await session.query({ file, operation: "definition", position }))
          .result;
        if (
          probe.definitionAbsent
            ? Boolean(definitions.length)
            : !definitions.length || definitions.some((definition) => !existsSync(definition.file))
        )
          throw new Error(`definition has no materialized file for ${file}:${probe.symbol}`);
        if (
          probe.definition &&
          !definitions.some(
            (definition) =>
              realpathSync(definition.file) === realpathSync(resolve(probe.definition)),
          )
        )
          throw new Error(`definition resolved outside canonical generated artifact for ${file}`);
        if (!nativeProjects.has(project.configFilePath))
          nativeProjects.set(
            project.configFilePath,
            await runCompiler(
              tsgo,
              ["--project", project.configFilePath, "--noEmit", "--pretty", "false"],
              signal,
            ),
          );
        const native = nativeProjects.get(project.configFilePath);
        if (![0, 1, 2].includes(native.status)) throw new Error(`tsgo exited ${native.status}`);
        results.push({
          file,
          project,
          diagnostics,
          definitions: definitions.map((definition) => ({
            ...definition,
            realFile: realpathSync(definition.file),
          })),
          native: { status: native.status, stdout: native.stdout, stderr: native.stderr },
        });
        const document = { file, diagnostics };
        const expected = [...(probe.diagnosticCodes || [])].sort((a, b) => a - b);
        const editorCodes = diagnostics
          .filter((item) => item.severity === 1)
          .map((diagnostic) => diagnostic.code)
          .sort((a, b) => a - b);
        if (JSON.stringify(editorCodes) !== JSON.stringify(expected))
          throw new Error(`unexpected editor errors for ${file}`);
        if (!projectDocuments.has(project.configFilePath))
          projectDocuments.set(project.configFilePath, new Map());
        projectDocuments.get(project.configFilePath).set(file, document);
      }
      for (const [project, documents] of projectDocuments) {
        const expectedError = [...documents.values()].some(({ diagnostics }) =>
          diagnostics.some((item) => item.severity === 1),
        );
        checkCompilerErrors(nativeProjects.get(project), expectedError);
        checks.push({ project, expectedError });
      }
    });
  } finally {
    process.stdout.write(
      JSON.stringify({ identity, initialization, results, checks }, null, 2) + "\n",
    );
  }
}
