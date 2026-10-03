import assert from 'node:assert/strict';
import test from 'node:test';
import { splitReply, MAX_REPLY_CHARS } from './split.js';

test('pesan pendek tidak dipecah dan tanpa penanda', () => {
  assert.deepEqual(splitReply('halo'), ['halo']);
  const t = 'x'.repeat(500);
  assert.deepEqual(splitReply(t, 500), [t]);
});

test('kosong -> tidak ada pesan', () => {
  assert.deepEqual(splitReply(''), []);
  assert.deepEqual(splitReply('   \n  '), []);
  assert.deepEqual(splitReply(null), []);
});

test('tiap bagian <= 500 karakter termasuk penanda', () => {
  const lines = Array.from({ length: 80 }, (_, i) => `- 2026-${String(i % 12 + 1).padStart(2, '0')} Rp150.000 lunas #${i}`);
  const parts = splitReply(lines.join('\n'));
  assert.ok(parts.length > 1);
  for (const p of parts) assert.ok(p.length <= MAX_REPLY_CHARS, `bagian ${p.length} > ${MAX_REPLY_CHARS}`);
});

test('baris data tidak terbelah di tengah & urutan terjaga, tidak ada yang hilang', () => {
  const lines = Array.from({ length: 60 }, (_, i) => `baris-${String(i).padStart(3, '0')} data pelanggan uji`);
  const parts = splitReply(lines.join('\n'));
  const rebuilt = parts.map(p => p.replace(/ \(\d+\/\d+\)$/, '')).join('\n').split('\n');
  assert.deepEqual(rebuilt, lines);
});

test('penanda (i/n) benar dan berurutan', () => {
  const parts = splitReply(Array.from({ length: 50 }, (_, i) => `baris ${i} `.repeat(6).trim()).join('\n'));
  const n = parts.length;
  assert.ok(n > 1);
  parts.forEach((p, i) => assert.ok(p.endsWith(`(${i + 1}/${n})`), p.slice(-15)));
});

test('satu baris sangat panjang dipotong di spasi, tetap <= batas', () => {
  const words = Array.from({ length: 300 }, (_, i) => `kata${i}`).join(' ');
  const parts = splitReply(words);
  assert.ok(parts.length > 1);
  for (const p of parts) assert.ok(p.length <= MAX_REPLY_CHARS);
  const rebuilt = parts.map(p => p.replace(/ \(\d+\/\d+\)$/, '')).join(' ');
  assert.equal(rebuilt, words);
});

test('tanpa spasi sama sekali -> potong keras, tetap <= batas dan utuh', () => {
  const s = 'a'.repeat(1700);
  const parts = splitReply(s);
  for (const p of parts) assert.ok(p.length <= MAX_REPLY_CHARS);
  assert.equal(parts.map(p => p.replace(/ \(\d+\/\d+\)$/, '')).join(''), s);
});

test('CRLF dinormalkan', () => {
  const parts = splitReply(('baris\r\n').repeat(200));
  for (const p of parts) assert.ok(!p.includes('\r'));
});
