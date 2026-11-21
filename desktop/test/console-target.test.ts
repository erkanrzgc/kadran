import assert from "node:assert/strict";
import { test } from "node:test";

import { nextTarget, setRunning, type ConsoleTarget } from "../src/renderer/lib/consoleTarget.ts";

const idle = (server: string): ConsoleTarget => ({ server, pending: null, running: false });

test("use without connection changes console server", () => {
  const t = nextTarget(idle(""), { kind: "use", server: "hetzner" });
  assert.equal(t.server, "hetzner");
  assert.equal(t.pending, null);
});

test("gui null keeps console server", () => {
  const t = nextTarget(idle("hetzner"), { kind: "gui", server: null });
  assert.deepEqual(t, idle("hetzner"));
  // Çalışırken de bekleyen bir değişiklik doğmaz.
  const busy = nextTarget({ server: "hetzner", pending: null, running: true }, { kind: "gui", server: null });
  assert.equal(busy.pending, null);
});

test("change while running waits for done", () => {
  let t: ConsoleTarget = { server: "hetzner", pending: null, running: true };
  t = nextTarget(t, { kind: "gui", server: "gcp" });
  assert.equal(t.server, "hetzner");
  assert.equal(t.pending, "gcp");
  t = nextTarget(t, { kind: "done" });
  assert.equal(t.server, "gcp");
  assert.equal(t.pending, null);
  assert.equal(t.running, false);
});

test("use while running waits for done", () => {
  let t = setRunning(idle("hetzner"), true);
  t = nextTarget(t, { kind: "use", server: "totp" });
  assert.equal(t.server, "hetzner");
  assert.equal(setRunning(t, false).server, "totp");
});

test("done without pending keeps server", () => {
  const t = nextTarget({ server: "gcp", pending: null, running: true }, { kind: "done" });
  assert.deepEqual(t, idle("gcp"));
});
