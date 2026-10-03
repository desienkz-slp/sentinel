// splitReply memecah balasan panjang menjadi beberapa pesan WhatsApp.
//
// Aturan:
//   - tiap bagian <= max karakter (termasuk penanda "(i/n)" bila lebih dari satu);
//   - dipotong di batas paragraf, lalu baris, lalu spasi — baris data (mis.
//     "- 2026-09 Rp150.000 lunas") tidak terbelah di tengah;
//   - bila satu baris sendiri melebihi batas, baru dipotong keras;
//   - urutan dipertahankan; bagian kosong dibuang.
export const MAX_REPLY_CHARS = 500;
// Ruang cadangan untuk penanda " (12/12)" di akhir tiap bagian.
const MARKER_RESERVE = 9;

function hardSplit(s, size) {
  const out = [];
  for (let i = 0; i < s.length; i += size) out.push(s.slice(i, i + size));
  return out;
}

function splitLong(line, size) {
  // Satu baris melebihi batas: coba potong di spasi terakhir sebelum batas.
  const out = [];
  let rest = line;
  while (rest.length > size) {
    let cut = rest.lastIndexOf(' ', size);
    if (cut < size * 0.4) cut = size; // tak ada spasi yang layak -> potong keras
    out.push(rest.slice(0, cut).trimEnd());
    rest = rest.slice(cut).trimStart();
  }
  if (rest) out.push(rest);
  return out;
}

export function splitReply(text, max = MAX_REPLY_CHARS) {
  const src = String(text ?? '').replace(/\r\n/g, '\n').trim();
  if (!src) return [];
  if (src.length <= max) return [src];

  const size = Math.max(50, max - MARKER_RESERVE);
  // Pecah menjadi unit terkecil yang masih utuh: baris. Paragraf dijaga lewat
  // baris kosong yang ikut dibawa sebagai pemisah.
  const units = [];
  for (const line of src.split('\n')) {
    if (line.length <= size) units.push(line);
    else units.push(...splitLong(line, size));
  }

  const chunks = [];
  let cur = '';
  for (const u of units) {
    const next = cur === '' ? u : cur + '\n' + u;
    if (next.length <= size) {
      cur = next;
      continue;
    }
    if (cur.trim()) chunks.push(cur.trim());
    cur = u;
  }
  if (cur.trim()) chunks.push(cur.trim());

  const parts = chunks.filter(Boolean);
  if (parts.length <= 1) return parts;
  return parts.map((p, i) => `${p} (${i + 1}/${parts.length})`);
}

export { hardSplit };
