import assert from "node:assert/strict";
import { test } from "node:test";

import { validateAppUpdate } from "../src/main/management-payload.ts";
import { draftToSpec, newAppDraft, validateAppDraft } from "../src/renderer/lib/management.ts";

// Aynı örnek tablosu hem arayüz (metin biçimi) hem ana süreç (yük) doğrulayıcısına
// verilir; ikisi de sunucunun internal/api/volumevalidate.go kuralıyla aynı olmalı.
interface Row { text: string; volumes: Array<{ name: string; mount_path: string; read_only: boolean }>; ok: boolean; why: string }
const v = (name: string, mount_path: string, read_only = false) => ({ name, mount_path, read_only });
const many = Array.from({ length: 17 }, (_, i) => v(`v${i}`, `/m${i}`));
const long4096 = "/" + "a".repeat(4095);
const accept = (why: string, text: string, volumes: Row["volumes"]): Row => ({ text, volumes, ok: true, why });
const reject = (why: string, text: string, volumes: Row["volumes"]): Row => ({ text, volumes, ok: false, why });

const TABLE: Row[] = [
  accept("düz yol", "data=/data", [v("data", "/data")]),
  accept("salt okunur", "c=/a/b:ro", [v("c", "/a/b", true)]),
  accept("boşluklu yol sunucuda geçerli", "x=/with space/dir", [v("x", "/with space/dir")]),
  accept("iki nokta içeren yol sunucuda geçerli", "d=/a:b", [v("d", "/a:b")]),
  accept("64 karakterlik ad", `${"a".repeat(64)}=/m`, [v("a".repeat(64), "/m")]),
  accept("tam 4096 bayt", `big=${long4096}`, [v("big", long4096)]),
  accept("/proc ile aynı önekli ama kök değil", "p=/procx", [v("p", "/procx")]),
  accept("16 hacim", many.slice(0, 16).map((x) => `${x.name}=${x.mount_path}`).join("\n"), many.slice(0, 16)),
  accept("kardeş yollar çakışmaz", "a=/app/a\nb=/app/b", [v("a", "/app/a"), v("b", "/app/b")]),
  reject("kök dizin", "r=/", [v("r", "/")]),
  reject("/proc", "p=/proc", [v("p", "/proc")]),
  reject("/proc altı", "p=/proc/x", [v("p", "/proc/x")]),
  reject("/sys", "p=/sys", [v("p", "/sys")]),
  reject("/dev altı", "p=/dev/shm", [v("p", "/dev/shm")]),
  reject("sondaki eğik çizgi", "a=/a/", [v("a", "/a/")]),
  reject("çift eğik çizgi", "a=//a", [v("a", "//a")]),
  reject("ortada çift eğik çizgi", "a=/a//b", [v("a", "/a//b")]),
  reject("nokta parçası", "a=/a/./b", [v("a", "/a/./b")]),
  reject("üst dizin parçası", "a=/a/../b", [v("a", "/a/../b")]),
  reject("göreli yol", "a=app", [v("a", "app")]),
  reject("boş yol", "a=", [v("a", "")]),
  reject("büyük harfli ad", "Data=/d", [v("Data", "/d")]),
  reject("65 karakterlik ad", `${"a".repeat(65)}=/m`, [v("a".repeat(65), "/m")]),
  reject("tireyle başlayan ad", "-a=/m", [v("-a", "/m")]),
  reject("iç içe bağlama", "a=/a\nb=/a/b", [v("a", "/a"), v("b", "/a/b")]),
  reject("aynı yol", "a=/a\nb=/a", [v("a", "/a"), v("b", "/a")]),
  reject("aynı ad", "a=/a\na=/b", [v("a", "/a"), v("a", "/b")]),
  reject("17 hacim", many.map((x) => `${x.name}=${x.mount_path}`).join("\n"), many),
  reject("4097 bayt", `big=${long4096}a`, [v("big", `${long4096}a`)]),
  reject("2 baytlık karakterlerle 4097 bayt", `big=/${"é".repeat(2048)}`, [v("big", `/${"é".repeat(2048)}`)]),
];

test("arayüz hacim metni doğrulayıcısı sunucu kuralını birebir izler", () => {
  for (const row of TABLE) {
    const draft = { ...newAppDraft(), appId: "hello", gitOwner: "team", gitRepo: "site", volumesText: row.text };
    assert.equal(validateAppDraft(draft), row.ok ? null : "form.invalid_volumes", row.why);
    if (row.ok) assert.deepEqual(draftToSpec(draft).volumes, row.volumes, row.why);
  }
});

test("ana süreç yük doğrulayıcısı aynı tabloda aynı kararı verir", () => {
  for (const row of TABLE) {
    const run = () => validateAppUpdate({ volumes: row.volumes });
    if (row.ok) assert.doesNotThrow(run, row.why); else assert.throws(run, row.why);
  }
});
