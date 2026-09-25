import assert from "node:assert/strict";
import { appendFileSync, readFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const record = (value) =>
  appendFileSync(process.env.EDITOR_TEST_EVENTS, JSON.stringify(value) + "\n");
record({
  pid: process.pid,
  args: process.argv.slice(2),
  wrapper: process.env.EDITOR_TEST_WRAPPER === "1",
});
if (process.env.EDITOR_TEST_WRAPPER === "1") {
  execFileSync(process.execPath, [fileURLToPath(import.meta.url), ...process.argv.slice(2)], {
    stdio: "inherit",
    env: { ...process.env, EDITOR_TEST_WRAPPER: "0" },
  });
} else if (process.argv.includes("--version")) {
  process.stdout.write("Version fixture\n");
} else if (process.argv.includes("--noEmit")) {
  if (process.env.EDITOR_TEST_HOLD_CHECK === "1") process.stdin.resume();
} else {
  let buffer = Buffer.alloc(0);
  const sendMessage = (message) => {
    const body = Buffer.from(JSON.stringify({ jsonrpc: "2.0", ...message }));
    process.stdout.write(
      Buffer.concat([Buffer.from(`Content-Length: ${body.length}\r\n\r\n`), body]),
    );
  };
  const send = (id, result) => sendMessage({ id, result });
  process.stdin.on("data", (chunk) => {
    buffer = Buffer.concat([buffer, chunk]);
    for (;;) {
      const end = buffer.indexOf("\r\n\r\n");
      if (end < 0) return;
      const length = Number(
        buffer
          .subarray(0, end)
          .toString()
          .match(/Content-Length: (\d+)/i)[1],
      );
      if (buffer.length < end + 4 + length) return;
      const message = JSON.parse(buffer.subarray(end + 4, end + 4 + length));
      buffer = buffer.subarray(end + 4 + length);
      record({ method: message.method, params: message.params });
      if (message.method === "shutdown" || message.method === "exit") {
        assert.equal(Object.hasOwn(message, "params"), false);
      }
      if (
        message.method === "exit" ||
        (message.method === "shutdown" && process.env.EDITOR_TEST_FAIL_SHUTDOWN === "1")
      ) {
        if (message.method === "exit" && process.env.EDITOR_TEST_MALFORMED_EXIT === "1")
          process.stdout.write("Content-Length: 1\r\n\r\n{");
        process.stderr.write(process.env.EDITOR_TEST_EXIT_STDERR ?? "", () => {
          process.exit(Number(process.env.EDITOR_TEST_EXIT_STATUS ?? "0"));
        });
        return;
      }
      for (
        let index = 0;
        message.method === "shutdown" &&
        index < Number(process.env.EDITOR_TEST_WATCH_ON_SHUTDOWN ?? 0);
        index++
      ) {
        record({ lateWatch: true });
        sendMessage({
          id: "late-watch-" + index,
          method: "client/registerCapability",
          params: {
            registrations: [
              {
                id: "late-config-" + index,
                method: "workspace/didChangeWatchedFiles",
                registerOptions: {
                  watchers: [{ globPattern: process.env.EDITOR_TEST_PROJECT }],
                },
              },
            ],
          },
        });
      }
      if (message.id === undefined || message.method === undefined) continue;
      if (message.method === "shutdown" && process.env.EDITOR_TEST_HOLD_SHUTDOWN === "1") continue;
      if (message.method === "initialize") {
        assert.equal(message.params.initializationOptions.logVerbosity, 5);
        send(message.id, {
          serverInfo: { name: "typescript-go", version: "fixture" },
          capabilities: {
            diagnosticProvider: {},
            completionProvider: {},
            definitionProvider: true,
            hoverProvider: true,
            referencesProvider: true,
          },
        });
      } else if (message.method === "textDocument/completion") {
        send(message.id, { isIncomplete: false, items: [{ label: "generatedMember" }] });
      } else if (message.method === "custom/projectInfo") {
        send(message.id, { configFilePath: process.env.EDITOR_TEST_PROJECT });
      } else if (message.method === "textDocument/diagnostic") {
        send(message.id, {
          kind: "full",
          items: [
            {
              code: 6385,
              severity: 4,
              message: "retained hint",
              range: { start: { line: 0, character: 0 }, end: { line: 0, character: 1 } },
            },
          ],
        });
      } else if (
        message.method === "textDocument/definition" ||
        message.method === "textDocument/references"
      ) {
        send(message.id, []);
      } else if (message.method === "textDocument/hover") {
        send(message.id, {
          contents: {
            kind: "plaintext",
            value: readFileSync(process.env.EDITOR_TEST_PROJECT, "utf8"),
          },
        });
      } else {
        send(message.id, null);
      }
    }
  });
}
