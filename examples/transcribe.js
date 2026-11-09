#!/usr/bin/env node
// Клиент voice2text на fetch (Node 18+, без зависимостей). Показывает
// полный набор навыков работы с HTTP API:
//
//   node transcribe.js http://127.0.0.1:8080 lecture.mp4 [ru]
//
// - загрузка файла multipart/form-data (Blob из fs)
// - ретраи на 429 (Retry-After + экспоненциальный backoff, максимум 5 попыток)
// - отказ ретраить 500
// - таймаут через AbortSignal.timeout
// - валидация тела ответа: 200 не гарантирует корректный JSON
//   (сервер имитирует: ./serve.sh -emulate-broken-json always)

const fs = require("fs");

const MAX_ATTEMPTS = 5;
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

function fail(msg) {
  console.error(`ошибка: ${msg}`);
  process.exit(1);
}

async function main() {
  const [base, path, lang = "ru"] = process.argv.slice(2);
  if (!base || !path) {
    console.error(`usage: ${process.argv[1]} <server-url> <file> [lang]`);
    process.exit(1);
  }

  const audio = new Blob([fs.readFileSync(path)]);
  const form = new FormData();
  form.append("file", audio, path);
  form.append("language", lang);

  for (let attempt = 1; attempt <= MAX_ATTEMPTS; attempt++) {
    let resp;
    try {
      // AbortSignal.timeout: без таймаута зависший сервер подвесит клиента.
      resp = await fetch(`${base.replace(/\/$/, "")}/v1/audio/transcriptions`, {
        method: "POST",
        body: form,
        signal: AbortSignal.timeout(300_000),
        headers: { "X-Request-Id": crypto.randomUUID().slice(0, 8) },
      });
    } catch (e) {
      fail(`сервер недоступен: ${e.cause?.code || e.message}`);
    }

    if (resp.status === 429) {
      // Rate limit: Retry-After от сервера + экспоненциальный запас.
      const ra = parseInt(resp.headers.get("retry-after") || "5", 10);
      const wait = ((Number.isNaN(ra) ? 5 : ra) + 2 ** attempt) * 1000;
      if (attempt === MAX_ATTEMPTS) {
        fail(`исчерпаны ${MAX_ATTEMPTS} попыток на 429`);
      }
      console.error(`429, попытка ${attempt}/${MAX_ATTEMPTS}, ждём ${wait / 1000}с...`);
      await sleep(wait);
      continue;
    }
    // 500 и прочие: повтор обычно бессмыслен, ломаемся сразу.
    if (!resp.ok) {
      let msg = `HTTP ${resp.status}`;
      try {
        msg = (await resp.json()).error.message;
      } catch {}
      fail(`${msg} (HTTP ${resp.status})`);
    }

    console.error(`X-Request-Id: ${resp.headers.get("x-request-id") || "?"}`);
    const raw = await resp.text();
    // Валидация тела: 200 не гарантирует корректный JSON, а JSON — ключ text.
    let parsed;
    try {
      parsed = JSON.parse(raw);
    } catch {
      fail("сервер вернул 200 с некорректным JSON — проверьте -emulate-broken-json");
    }
    if (typeof parsed.text !== "string") {
      fail(`в ответе нет поля text: ${raw.slice(0, 80)}`);
    }
    console.log(parsed.text);
    return;
  }
}

main();
