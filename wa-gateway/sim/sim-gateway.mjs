// Gateway WhatsApp LOKAL (simulasi transport) untuk uji komunikasi di PC.
//
// Kontrak sama dengan gateway Baileys asli (app/server.js + app/webhook.js):
//   masuk  : POST {message_id,chat_id,sender,sender_name,message,timestamp,type}
//            ke NOC_WEBHOOK_URL; bila respons memuat "reply", dikirim balik.
//   keluar : POST /api/whatsapp/send {to,message}  (dipecah <=500 karakter,
//            memakai splitReply yang SAMA dengan gateway produksi).
//   status : GET  /api/whatsapp/status
// Tambahan khusus uji:
//   POST /sim/inbound  {from,name,text}  -> seolah pesan WA masuk
//   GET  /sim/outbox?to=  -> semua pesan yang "terkirim"
//   POST /sim/reset
// TIDAK memakai jaringan WhatsApp, TIDAK membaca sesi/auth apa pun.
import express from 'express';
import axios from 'axios';
import { splitReply, MAX_REPLY_CHARS } from '../app/split.js';

const PORT = parseInt(process.env.PORT || '3011', 10);
const WEBHOOK = process.env.NOC_WEBHOOK_URL || 'http://127.0.0.1:8190/api/wa/webhook';
const app = express();
app.use(express.json());

let outbox = []; // {to, text, ts, part, parts, via}
let seq = 0;
const nowId = (p) => `${p}_${Date.now()}_${++seq}`;

async function deliver(to, text, via) {
  const parts = splitReply(text);
  if (parts.length === 0) throw new Error('Cannot send message: empty text');
  const ids = [];
  parts.forEach((p, i) => {
    if (p.length > MAX_REPLY_CHARS) throw new Error(`bagian ${i + 1} melebihi ${MAX_REPLY_CHARS} karakter`);
    const id = nowId('simout');
    outbox.push({ id, to, text: p, ts: new Date().toISOString(), part: i + 1, parts: parts.length, via });
    ids.push(id);
  });
  return { success: true, message_id: ids[ids.length - 1], parts: parts.length, message_ids: ids };
}

app.get('/api/whatsapp/status', (_q, r) => r.json({ status: 'CONNECTED', connected: true, simulated: true, phone: 'sim-local' }));
app.get('/api/whatsapp/health', (_q, r) => r.json({ ok: true, simulated: true }));
app.get('/api/whatsapp/qr', (_q, r) => r.json({ status: 'CONNECTED', qr: null }));
app.post('/api/whatsapp/connect', (_q, r) => r.json({ success: true, status: 'CONNECTED' }));
app.post('/api/whatsapp/reconnect', (_q, r) => r.json({ success: true, status: 'CONNECTED' }));

app.post('/api/whatsapp/send', async (q, r) => {
  const { to, message } = q.body || {};
  if (!to || !message) return r.status(400).json({ error: 'Fields "to" and "message" are required.' });
  try { r.json(await deliver(String(to), String(message), 'api-send')); }
  catch (e) { r.status(500).json({ error: e.message }); }
});

app.post('/sim/inbound', async (q, r) => {
  const { from, name, text } = q.body || {};
  if (!from || !text) return r.status(400).json({ error: 'from & text wajib' });
  const payload = {
    message_id: nowId('siminb'), chat_id: String(from), sender: String(from),
    sender_name: name || 'Sim User', message: String(text),
    timestamp: new Date().toISOString(), type: 'text',
  };
  const t0 = Date.now();
  try {
    const resp = await axios.post(WEBHOOK, payload, {
      headers: { 'Content-Type': 'application/json', 'X-AI-NOC-Source': 'whatsapp-gateway' },
      timeout: 180000,
    });
    let sent = null;
    if (resp.data && resp.data.reply) sent = await deliver(payload.chat_id, resp.data.reply, 'auto-reply');
    r.json({ ok: true, ms: Date.now() - t0, accepted: resp.data?.accepted, engine: resp.data?.engine,
             note: resp.data?.note, had_reply: !!resp.data?.reply, report: resp.data?.report, sent });
  } catch (e) {
    r.status(502).json({ ok: false, error: e.message, details: e.response?.data || null });
  }
});

app.get('/sim/outbox', (q, r) => r.json(q.query.to ? outbox.filter((m) => m.to === q.query.to) : outbox));
app.post('/sim/reset', (_q, r) => { outbox = []; r.json({ ok: true }); });

app.listen(PORT, '127.0.0.1', () => console.log(`[SIM-WA] gateway lokal di http://127.0.0.1:${PORT} -> ${WEBHOOK}`));
