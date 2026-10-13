/**
 * Iç içe create/update alanları: sunucunun kabul ettiği HİÇBİR şey reddedilmez
 * (daha sıkı olmak meşru uygulamaları kilitler), sunucunun reddettiği
 * biçimler ana süreçte de reddedilir.
 */

import { test } from "node:test";
import assert from "node:assert/strict";
import { requireAppPayload, requireRef } from "../src/main/management-ipc.ts";

const okSpec = () => ({
  app_id: "blog", git_host: "git.example.co.uk", git_owner: "Acme_1", git_repo: "my.repo-2", git_branch: "feature/x-1.2", dockerfile_path: "", build_args: { NODE_ENV: "production" },
  container_port: 65535, replicas: 64, health_path: "", domain: "", limits: { memory_bytes: "6291456", cpu_millis: 10, blkio_weight: 1000 },
  env: { EMPTY: "", MULTI: "line1\nline2 ünlü {x} $(y)" }, volumes: [{ name: "data", mount_path: "/app/data", read_only: true }, { name: "cache", mount_path: "/app/cache2" }],
});

test("create: valid but unusual specs the server accepts are NOT rejected (never stricter)", () => {
  assert.doesNotThrow(() => requireAppPayload(okSpec(), "create"));
  assert.doesNotThrow(() => requireAppPayload({ ...okSpec(), dockerfile_path: "docker/Prod.Dockerfile", health_path: "/a?b=c", domain: "a.b.c.example.com" }, "create"));
  // Sunucu ".." yolunu (tek başına) Clean sınamasından geçirir; ayna olarak biz de geçiriyoruz.
  assert.doesNotThrow(() => requireAppPayload({ ...okSpec(), dockerfile_path: ".." }, "create"));
  // protojson sayıları metin olarak da kabul eder.
  assert.doesNotThrow(() => requireAppPayload({ ...okSpec(), container_port: "3000", replicas: "2" }, "create"));
  assert.doesNotThrow(() => requireAppPayload({ ...okSpec(), limits: { memory_bytes: 536870912, cpu_millis: 1, blkio_weight: 10 } }, "create"));
  const manyEnv = Object.fromEntries(Array.from({ length: 200 }, (_, i) => [`K${i}`, "v"]));
  assert.doesNotThrow(() => requireAppPayload({ ...okSpec(), env: manyEnv }, "create"));
});

test("create: mirrors the server limits at their edges", () => {
  const bad = (patch: Record<string, unknown>) => assert.throws(() => requireAppPayload({ ...okSpec(), ...patch }, "create"), undefined, JSON.stringify(patch).slice(0, 120));
  bad({ git_host: "localhost" }); bad({ git_host: "a.b:22" }); bad({ git_host: "-a.b" }); bad({ git_repo: "." }); bad({ git_owner: "a b" });
  bad({ replicas: 65 }); bad({ container_port: 65536 }); bad({ domain: "a".repeat(250) + ".com" });
  bad({ dockerfile_path: "a/../b" }); bad({ dockerfile_path: "a//b" }); bad({ dockerfile_path: "a/" }); bad({ dockerfile_path: "./a" }); bad({ dockerfile_path: "." });
  bad({ health_path: "/a\tb" }); bad({ health_path: "/" + "a".repeat(512) });
  bad({ env: Object.fromEntries(Array.from({ length: 201 }, (_, i) => [`K${i}`, "v"])) });
  bad({ env: { A: "x".repeat(32 * 1024) } });
  bad({ env: { ["A".repeat(257)]: "x" } });
  bad({ build_args: { A: "a\0b" } });
  bad({ volumes: Array.from({ length: 17 }, (_, i) => ({ name: `v${i}`, mount_path: `/m${i}` })) });
  bad({ volumes: [{ name: "a", mount_path: "/a/../b" }] }); bad({ volumes: [{ name: "a", mount_path: "/" }] }); bad({ volumes: [{ name: "a", mount_path: "/sys" }] });
  bad({ volumes: [{ name: "a", mount_path: "/a/" }] }); bad({ volumes: [{ name: "A", mount_path: "/a" }] }); bad({ volumes: [{ name: "a", mount_path: "/a", extra: 1 }] });
  bad({ limits: { memory_bytes: "1", cpu_millis: 1, blkio_weight: 100, extra: 1 } });
  bad({ limits: { memory_bytes: "18446744073709551616", cpu_millis: 1, blkio_weight: 100 } });
});

test("update: server-accepted partial updates pass; conflicts and bad names do not", () => {
  assert.doesNotThrow(() => requireAppPayload({ domain: "", git_branch: "main", health_path: "", replicas: 3, env: { A: "" }, env_remove: ["B"], volumes: [{ name: "d", mount_path: "/d", read_only: false }], volume_remove: ["old"] }, "update"));
  assert.doesNotThrow(() => requireAppPayload({}, "update"));
  const bad = (update: Record<string, unknown>) => assert.throws(() => requireAppPayload(update, "update"), undefined, JSON.stringify(update));
  bad({ domain: "a b" }); bad({ git_branch: "" }); bad({ git_branch: "a b" }); bad({ replicas: 65 }); bad({ replicas: 0 });
  bad({ env: { A: "1" }, env_remove: ["A"] }); bad({ env_remove: ["a-b"] }); bad({ env_remove: "A" }); bad({ volume_remove: ["Up"] });
  bad({ volumes: [{ name: "d", mount_path: "/d" }], volume_remove: ["d"] }); bad({ app_id: "x" }); bad({ limits: { memory_bytes: "1" } });
});

test("branch rule is shared: empty ref stays allowed only for deploy refs", () => {
  assert.equal(requireRef(""), "");
  assert.throws(() => requireAppPayload({ ...okSpec(), git_branch: "" }, "create"));
});
