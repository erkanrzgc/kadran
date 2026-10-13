/** IPC hata önekleri temizlenir; sunucunun metni olduğu gibi kalır. */

import { test } from "node:test";
import assert from "node:assert/strict";

import { errorText } from "../src/renderer/lib/errors.ts";

test("Electron IPC öneki ve Error: öneki gider", () => {
  assert.equal(
    errorText(new Error("Error invoking remote method 'kadran:status': SidecarError: could not connect: refused")),
    "could not connect: refused",
  );
  assert.equal(errorText(new Error("Error: boom")), "boom");
  assert.equal(errorText("düz metin"), "düz metin");
  assert.equal(errorText(undefined), "unknown error");
});
