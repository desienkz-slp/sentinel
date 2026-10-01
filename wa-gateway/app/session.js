import path from 'path';
import fs from 'fs';
import { fileURLToPath } from 'url';
import pkg from 'pg';
const { Pool } = pkg;
import {
  makeWASocket,
  DisconnectReason,
  useMultiFileAuthState,
  fetchLatestBaileysVersion,
  Browsers
} from '@whiskeysockets/baileys';
import pino from 'pino';
import { qrManager } from './qr.js';
import { webhookDispatcher } from './webhook.js';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const AUTH_DIR = process.env.AUTH_DIR || path.resolve(__dirname, '../data/auth');
const DB_HOST = process.env.DB_HOST || '127.0.0.1';
const DB_PORT = parseInt(process.env.DB_PORT || '5432', 10);
const DB_NAME = process.env.DB_NAME || 'noc_sentinel';
const DB_USER = process.env.DB_USER || 'noc_admin';
// Tanpa password default: koneksi DB gateway harus dikonfigurasi eksplisit.
// Fallback kosong membuat pool gagal menginisialisasi (bukan memakai kredensial
// lemah), yang aman karena DB ini opsional bagi fungsi WhatsApp.
const DB_PASS = process.env.DB_PASS || '';

/**
 * Tentukan identitas pengirim (nomor telepon) dari sebuah pesan WhatsApp.
 *
 * WhatsApp kini mengirim sebagian pesan dengan identitas @lid (Linked ID):
 * deretan angka panjang, BUKAN nomor telepon. Kalau LID dipakai apa adanya,
 * nomor tidak cocok dengan allowlist dan pesan pelanggan ikut ditolak.
 *
 * Nomor telepon asli tersedia di participantPn (grup) / senderPn (pribadi),
 * yang diisi baileys dari atribut stanza sender_pn / participant_pn.
 *
 * @param {object} key   msg.key dari baileys
 * @param {object} opsi  { nomorGateway, petaLid } — untuk pesan dari diri
 *                       sendiri dan pemetaan LID yang sudah pernah tercatat
 * @returns {{nomor: string, lid: string, sumber: string, jid: string}}
 */
function resolusiIdentitas(key = {}, opsi = {}) {
  const petaLid = opsi.petaLid || new Map();
  const nomorGateway = opsi.nomorGateway || '';
  const jid = key.remoteJid || '';

  // Pisahkan nomor dari JID: "628xx@s.whatsapp.net" -> "628xx"
  const bersihkan = (v) => String(v || '').split('@')[0].split(':')[0];

  const lidAsli = key.senderLid || key.participantLid || '';
  const pnAsli = key.participantPn || key.senderPn || '';

  let nomor = bersihkan(pnAsli);
  let sumber = 'pn';

  if (!nomor) {
    if (key.fromMe && nomorGateway) {
      // Pesan dari diri sendiri ("pesan tersimpan"/catatan operator): tidak ada
      // senderPn, jadi pakai nomor gateway sendiri supaya dikenali operator.
      nomor = bersihkan(nomorGateway);
      sumber = 'gateway';
    } else {
      nomor = bersihkan(key.participant || jid);
      sumber = 'jid';
    }
  }

  // Masih berupa LID? Pakai pemetaan yang pernah tercatat.
  if (nomor && petaLid.has(nomor)) {
    nomor = petaLid.get(nomor);
    sumber = 'peta';
  }

  return { nomor, lid: bersihkan(lidAsli), sumber, jid };
}

class SessionManager {
  constructor() {
    this.sock = null;
    this.status = 'DISCONNECTED'; // CONNECTED, CONNECTING, DISCONNECTED, AUTH_REQUIRED, ERROR
    this.phone = null;
    this.name = 'AI-NOC Sentinel';
    this.sessionId = 'ai-noc';
    this.connectedAt = null;
    this.lastMessageAt = null;
    this.lastIncoming = null;
    this.lastOutgoing = null;
    this.lastError = null;
    this.reconnectAttempts = 0;
    this.isReconnecting = false;
    // ID pesan yang DIKIRIM oleh gateway ini. Dipakai untuk membedakan
    // "balasan kita sendiri" dari "pesan yang operator ketik sendiri".
    // Tanpa ini, memproses pesan fromMe akan membuat loop balasan tak berujung.
    this.sentIds = new Map(); // id -> timestamp (ms)
    // Pemetaan LID (Linked ID) -> nomor telepon asli. WhatsApp memakai LID
    // untuk sebagian pengirim; tanpa pemetaan ini, pesan mereka tidak bisa
    // dicocokkan dengan allowlist dan ikut ditolak.
    this.lidToPhone = new Map(); // lid -> nomor telepon
    this.phoneNumber = null; // nomor gateway sendiri (diisi saat sesi tersambung)
    this.recentLogs = [];
    this.dbPool = null;

    this.initDb();
    this.ensureAuthDir();
  }

  ensureAuthDir() {
    if (!fs.existsSync(AUTH_DIR)) {
      fs.mkdirSync(AUTH_DIR, { recursive: true });
    }
  }

  initDb() {
    try {
      this.dbPool = new Pool({
        host: DB_HOST,
        port: DB_PORT,
        database: DB_NAME,
        user: DB_USER,
        password: DB_PASS,
        max: 5,
        connectionTimeoutMillis: 3000
      });
      this.dbPool.on('error', (err) => {
        this.addLog(`DB Pool notice: ${err.message}`, 'WARN');
      });
    } catch (err) {
      this.addLog(`DB Init failed: ${err.message}`, 'WARN');
    }
  }

  addLog(message, level = 'INFO') {
    const timestamp = new Date().toISOString();
    // Strict Sanitization: Never log tokens, private keys, or raw base64 credentials
    let cleanMessage = String(message)
      .replace(/key|token|secret|password|cred/gi, (match) => match)
      .replace(/[A-Za-z0-9+/=]{80,}/g, '[REDACTED_BINARY]');

    const entry = { timestamp, level, message: cleanMessage };
    this.recentLogs.unshift(entry);
    if (this.recentLogs.length > 50) {
      this.recentLogs.pop();
    }
    console.log(`[${timestamp}] [${level}] ${cleanMessage}`);
  }

  async persistSessionState() {
    if (!this.dbPool) return;
    try {
      const query = `
        INSERT INTO whatsapp_sessions (session_id, phone, name, status, connected_at, last_seen_at, updated_at)
        VALUES ($1, $2, $3, $4, $5, NOW(), NOW())
        ON CONFLICT (session_id) DO UPDATE SET
          phone = EXCLUDED.phone,
          name = EXCLUDED.name,
          status = EXCLUDED.status,
          connected_at = CASE WHEN EXCLUDED.status = 'CONNECTED' AND whatsapp_sessions.status != 'CONNECTED' THEN NOW() ELSE whatsapp_sessions.connected_at END,
          last_seen_at = NOW(),
          updated_at = NOW()
      `;
      await this.dbPool.query(query, [
        this.sessionId,
        this.phone,
        this.name,
        this.status,
        this.connectedAt
      ]);
    } catch (err) {
      // Non-blocking DB logging
    }
  }

  async recordEvent(eventType, payload) {
    if (!this.dbPool) return;
    try {
      await this.dbPool.query(
        'INSERT INTO whatsapp_events (session_id, event_type, payload_json) VALUES ($1, $2, $3)',
        [this.sessionId, eventType, JSON.stringify(payload)]
      );
    } catch (err) {
      // Non-blocking
    }
  }

  async connect() {
    if (this.isReconnecting) {
      this.addLog('Connect called while reconnection already in progress.', 'WARN');
      return;
    }

    this.isReconnecting = true;
    this.status = 'CONNECTING';
    this.addLog('Initializing WhatsApp Multi-Device session (Baileys)...');
    await this.persistSessionState();

    try {
      this.ensureAuthDir();
      const { state, saveCreds } = await useMultiFileAuthState(AUTH_DIR);
      const { version } = await fetchLatestBaileysVersion();

      // Silent pino logger to prevent credential leakage into standard stdout
      const logger = pino({ level: 'silent' });

      this.sock = makeWASocket({
        version,
        logger,
        printQRInTerminal: false,
        auth: state,
        browser: Browsers.ubuntu('Chrome'),
        connectTimeoutMs: 60000,
        defaultQueryTimeoutMs: 60000,
        keepAliveIntervalMs: 25000,
        emitOwnEvents: false,
        syncFullHistory: false
      });

      // Credential Update Event
      this.sock.ev.on('creds.update', async () => {
        try {
          await saveCreds();
        } catch (e) {
          this.addLog(`Error saving auth credentials: ${e.message}`, 'ERROR');
        }
      });

      // Connection Update Event
      this.sock.ev.on('connection.update', async (update) => {
        const { connection, lastDisconnect, qr } = update;

        if (qr) {
          this.status = 'AUTH_REQUIRED';
          this.addLog('New QR Code generated. Awaiting operator scan...');
          await qrManager.setQR(qr);
          await this.persistSessionState();
          await this.recordEvent('qr_generated', { expires_in: 45 });
        }

        if (connection === 'close') {
          qrManager.clear();
          const statusCode = lastDisconnect?.error?.output?.statusCode;
          const shouldReconnect = statusCode !== DisconnectReason.loggedOut;
          this.lastError = lastDisconnect?.error?.message || 'Connection closed';

          this.addLog(`Connection closed (code: ${statusCode || 'unknown'}). Reason: ${this.lastError}`, 'WARN');

          if (statusCode === DisconnectReason.loggedOut) {
            this.status = 'DISCONNECTED';
            this.phone = null;
            this.connectedAt = null;
            this.addLog('Session logged out by WhatsApp server. Auth reset.', 'WARN');
            await this.persistSessionState();
            await this.recordEvent('logout', { reason: 'server_disconnect' });
            this.isReconnecting = false;
          } else if (shouldReconnect) {
            this.status = 'CONNECTING';
            if (this.reconnectTimer) {
              clearTimeout(this.reconnectTimer);
              this.reconnectTimer = null;
            }
            this.reconnectAttempts++;
            const delay = Math.min(this.reconnectAttempts * 3000, 15000);
            this.addLog(`Scheduling auto-reconnect in ${delay / 1000}s (Attempt ${this.reconnectAttempts})...`);
            await this.persistSessionState();
            this.reconnectTimer = setTimeout(() => {
              this.reconnectTimer = null;
              this.isReconnecting = false;
              this.connect();
            }, delay);
          } else {
            this.status = 'DISCONNECTED';
            this.isReconnecting = false;
            await this.persistSessionState();
          }
        } else if (connection === 'open') {
          if (this.reconnectTimer) {
            clearTimeout(this.reconnectTimer);
            this.reconnectTimer = null;
          }
          this.status = 'CONNECTED';
          this.reconnectAttempts = 0;
          this.isReconnecting = false;
          this.connectedAt = new Date().toISOString();
          qrManager.clear();

          const userJid = this.sock?.user?.id || '';
          this.phone = userJid.split(':')[0].replace(/[^0-9]/g, '');
          // Saat auto-reconnect cepat, sock.user kadang belum terisi padahal
          // kredensial masih ada di creds.json. Fallback: baca 'me' dari
          // creds.json supaya status tidak menampilkan nomor kosong.
          if (!this.phone) {
            try {
              const creds = JSON.parse(fs.readFileSync(path.join(AUTH_DIR, 'creds.json'), 'utf8'));
              if (creds?.me?.id) {
                this.phone = creds.me.id.split(':')[0].replace(/[^0-9]/g, '');
              }
            } catch (e) {
              // abaikan: creds belum ada / rusak
            }
          }
          this.name = this.sock?.user?.name || 'AI-NOC Sentinel';

          this.addLog(`WhatsApp connected successfully. Account: ${this.phone} (${this.name})`);
          await this.persistSessionState();
          await this.recordEvent('connection_update', {
            status: 'CONNECTED',
            phone: this.phone,
            name: this.name
          });
        }
      });

      // Inbound Messages Event
      this.sock.ev.on('messages.upsert', async ({ messages, type }) => {
        if (type !== 'notify') return;

        for (const msg of messages) {
          // Seluruh pemrosesan satu pesan dibungkus try/catch. Tanpa ini, satu
          // exception (mis. field yang tidak ada di versi baileys baru) membuat
          // SELURUH pesan dalam batch dilewati diam-diam: pesan masuk tercatat
          // di log, tetapi tidak pernah diteruskan ke webhook dan tidak ada
          // pesan error sama sekali. Itulah gejala "bot tidak membalas".
          try {
          // Abaikan status broadcast.
          if (msg.key.remoteJid === 'status@broadcast') continue;

          const msgId0 = msg.key.id || '';

          // Pesan dari DIRI SENDIRI (fromMe) ada dua jenis:
          //   (a) balasan yang baru saja kita kirim -> HARUS diabaikan, kalau
          //       tidak akan terjadi loop balasan tak berujung.
          //   (b) pesan yang OPERATOR ketik sendiri dari HP (mode uji/catatan)
          //       -> boleh diproses, supaya operator bisa mencoba bot dari
          //       nomor gateway itu sendiri.
          // Pembedanya: id pesan yang kita kirim tercatat di this.sentIds.
          if (msg.key.fromMe) {
            const sekarang = Date.now();
            // Bersihkan catatan lama (lebih dari 10 menit) agar tidak menumpuk.
            for (const [id, t] of this.sentIds) {
              if (sekarang - t > 10 * 60 * 1000) this.sentIds.delete(id);
            }
            if (this.sentIds.has(msgId0)) {
              this.sentIds.delete(msgId0);
              continue; // ini balasan kita sendiri
            }
            // Bukan kiriman kita -> pesan yang operator ketik sendiri.
            console.log(`[WA-INBOUND-SELF] operator mengetik sendiri message_id=${msgId0} (diproses)`);
          }

          const text = msg.message?.conversation ||
                       msg.message?.extendedTextMessage?.text ||
                       msg.message?.imageMessage?.caption ||
                       '';

          if (!text.trim()) continue;

          const senderJid = msg.key.remoteJid || '';

          // Resolusi identitas: ubah @lid (Linked ID) menjadi nomor telepon.
          // Tanpa ini, pesan dari pelanggan yang dikirim WhatsApp sebagai @lid
          // tidak cocok dengan allowlist dan ikut ditolak.
          const ident = resolusiIdentitas(msg.key, {
            nomorGateway: this.phone,
            petaLid: this.lidToPhone
          });
          let senderPhone = ident.nomor;

          // Bila hasilnya masih berupa LID, tanyakan ke resolver RESMI baileys
          // (getPNForLID) — ini membaca pemetaan dari sesi WhatsApp sendiri,
          // bukan menebak dari bentuk angka. Hanya tersedia di baileys v7+.
          const masihLid = ident.sumber === 'jid' && senderJid.endsWith('@lid');
          if (masihLid) {
            const lidJid = senderJid;
            try {
              const pn = await this.sock?.signalRepository?.lidMapping?.getPNForLID(lidJid);
              if (pn) {
                const nomorAsli = String(pn).split('@')[0].split(':')[0];
                if (nomorAsli) {
                  senderPhone = nomorAsli;
                  console.log(`[WA-LID] resolver resmi: ${lidJid} -> ${nomorAsli}`);
                }
              } else {
                console.log(`[WA-LID] resolver resmi tidak mengenal ${lidJid}`);
              }
            } catch (e) {
              console.log(`[WA-LID] resolver resmi gagal untuk ${lidJid}: ${e.message}`);
            }
          }

          // Catat pemetaan LID -> nomor telepon supaya pesan berikutnya dari
          // pengirim yang sama tetap dikenali tanpa perlu tanya ulang.
          if (ident.lid && senderPhone && ident.lid !== senderPhone) {
            this.lidToPhone.set(ident.lid, senderPhone);
          }

          // Diagnostik: tampilkan field identitas mentah untuk pesan @lid,
          // supaya pemetaannya bisa diverifikasi langsung dari log.
          if (senderJid.endsWith('@lid') || ident.lid) {
            console.log(`[WA-LID] remote_jid=${senderJid} senderPn=${msg.key.senderPn || '-'} participantPn=${msg.key.participantPn || '-'} senderLid=${msg.key.senderLid || '-'} -> nomor=${senderPhone || 'TIDAK DIKETAHUI'} (sumber=${ident.sumber})`);
          }

          const senderName = msg.pushName || senderPhone;
          const msgId = msg.key.id || `msg_${Date.now()}`;
          const timestamp = new Date(Number(msg.messageTimestamp) * 1000 || Date.now()).toISOString();

          this.lastMessageAt = timestamp;
          this.lastIncoming = {
            message_id: msgId,
            chat_id: senderJid,
            sender: senderPhone,
            sender_name: senderName,
            message: text.trim(),
            timestamp,
            type: 'text'
          };

          console.log(`[WA-INBOUND] message_id=${msgId} remote_jid=${senderJid} from_me=${msg.key.fromMe || false} message_type=text text="${text.trim().substring(0, 50)}" timestamp=${timestamp}`);
          this.addLog(`Inbound message from ${senderPhone} ("${senderName}"): "${text.trim().substring(0, 40)}..."`);

          await this.recordEvent('incoming_message', this.lastIncoming);

          // Dispatch to n8n webhook and fallback orchestrator
          await webhookDispatcher.dispatchInbound(this.lastIncoming, this);
          } catch (msgErr) {
            // Jangan biarkan satu pesan bermasalah membatalkan sisanya, dan
            // JANGAN telan errornya: inilah satu-satunya jejak saat pesan masuk
            // tapi tidak pernah diproses lebih lanjut.
            console.error(`[WA-ERROR] gagal memproses pesan ${msg?.key?.id || '?'}: ${msgErr?.message}`);
            console.error(msgErr?.stack || '');
            this.addLog(`Gagal memproses pesan ${msg?.key?.id || '?'}: ${msgErr?.message}`, 'ERROR');
          }
        }
      });

      this.isReconnecting = false;
    } catch (err) {
      this.status = 'ERROR';
      this.lastError = err.message;
      this.isReconnecting = false;
      this.addLog(`Fatal error in Baileys init: ${err.message}`, 'ERROR');
      await this.persistSessionState();
    }
  }

  async reconnect() {
    this.addLog('Manual reconnection requested by operator...');
    try {
      if (this.sock) {
        this.sock.end(new Error('Manual reconnect'));
      }
    } catch (e) {
      // Ignore teardown errors
    }
    this.isReconnecting = false;
    await this.connect();
    return { success: true, message: 'Reconnection sequence initiated' };
  }

  async logout() {
    this.addLog('Logout requested by operator...');
    try {
      if (this.sock) {
        await this.sock.logout();
      }
    } catch (e) {
      // Ignore logout errors
    }

    try {
      // Safely wipe auth files so a fresh QR code is generated on next connect
      if (fs.existsSync(AUTH_DIR)) {
        fs.rmSync(AUTH_DIR, { recursive: true, force: true });
        fs.mkdirSync(AUTH_DIR, { recursive: true });
      }
    } catch (e) {
      this.addLog(`Error clearing auth directory: ${e.message}`, 'WARN');
    }

    this.sock = null;
    this.status = 'DISCONNECTED';
    this.phone = null;
    this.connectedAt = null;
    qrManager.clear();

    await this.persistSessionState();
    await this.recordEvent('logout', { actor: 'operator' });
    this.addLog('Session cleared. Ready for new QR scan.');
    return { success: true, message: 'Logged out successfully' };
  }

  async sendMessage(to, text) {
    if (this.status === 'CONNECTING') {
      this.addLog('Gateway currently connecting. Waiting up to 10s for connection ready...');
      for (let i = 0; i < 20; i++) {
        await new Promise(r => setTimeout(r, 500));
        if (this.status === 'CONNECTED' && this.sock) break;
      }
    }

    if (this.status !== 'CONNECTED' || !this.sock) {
      throw new Error(`Cannot send message: Gateway is ${this.status}`);
    }

    // Format target JID properly (Preserve @s.whatsapp.net, @g.us)
    let jid = String(to).trim();
    if (!jid.includes('@')) {
      const cleanNumber = jid.replace(/[^0-9]/g, '');
      jid = `${cleanNumber}@s.whatsapp.net`;
    }

    // PENTING: jangan kirim ke JID @lid. Pengiriman ke @lid gagal karena
    // WhatsApp memerlukan nomor telepon (PN) sebagai tujuan. Bila tujuan
    // berupa LID yang sudah kita kenali nomornya, ubah ke nomor telepon.
    if (jid.endsWith('@lid')) {
      const lid = jid.split('@')[0];
      const nomor = this.lidToPhone.get(lid);
      if (nomor) {
        console.log(`[WA-OUTBOUND] tujuan @lid ${jid} -> dialihkan ke nomor ${nomor}@s.whatsapp.net`);
        jid = `${nomor}@s.whatsapp.net`;
      } else {
        // Belum tahu nomornya: coba resolver resmi baileys.
        try {
          const pn = await this.sock?.signalRepository?.lidMapping?.getPNForLID(jid);
          if (pn) {
            const n = String(pn).split('@')[0].split(':')[0];
            this.lidToPhone.set(lid, n);
            console.log(`[WA-OUTBOUND] tujuan @lid ${jid} -> ${n}@s.whatsapp.net (resolver resmi)`);
            jid = `${n}@s.whatsapp.net`;
          } else {
            console.log(`[WA-OUTBOUND] PERINGATAN: ${jid} tidak bisa diresolusi ke nomor telepon; mengirim apa adanya (kemungkinan gagal)`);
          }
        } catch (e) {
          console.log(`[WA-OUTBOUND] PERINGATAN: resolusi ${jid} gagal (${e.message}); mengirim apa adanya`);
        }
      }
    }

    const timestamp = new Date().toISOString();
    this.lastMessageAt = timestamp;
    this.lastOutgoing = {
      to: jid,
      message: text,
      timestamp
    };

    this.addLog(`Sending message to ${jid} (${text.length} chars)...`);

    try {
      const result = await this.sock.sendMessage(jid, { text });
      // Catat id pesan ini supaya saat event fromMe muncul (pesan kita sendiri
      // dikirim balik oleh WhatsApp), kita bisa mengenali dan mengabaikannya.
      // Tanpa ini, bot akan membalas balasannya sendiri tanpa henti.
      if (result?.key?.id) {
        this.sentIds.set(result.key.id, Date.now());
      }
      console.log(`[WA-OUTBOUND] remote_jid=${jid} message_id=${result?.key?.id} status=SENT`);
      await this.recordEvent('outgoing_message', {
        to: jid,
        preview: text.substring(0, 100),
        timestamp
      });
      return { success: true, message_id: result?.key?.id, timestamp };
    } catch (err) {
      console.log(`[WA-OUTBOUND] remote_jid=${jid} status=FAILED error="${err.message}"`);
      this.addLog(`Failed to send message to ${jid}: ${err.message}`, 'ERROR');
      throw err;
    }
  }

  getStatus() {
    return {
      status: this.status,
      phone: this.phone,
      name: this.name,
      session: this.sessionId,
      connected_at: this.connectedAt,
      last_message_at: this.lastMessageAt,
      last_incoming: this.lastIncoming,
      last_outgoing: this.lastOutgoing,
      webhook: webhookDispatcher.status,
      error: this.lastError,
      uptime_seconds: process.uptime()
    };
  }

  getSessionMetadata() {
    return {
      session_id: this.sessionId,
      status: this.status,
      phone: this.phone,
      display_name: this.name,
      connected_at: this.connectedAt,
      platform: 'Baileys Multi-Device (WhatsApp Web Protocol)',
      node_version: process.version,
      auth_directory: AUTH_DIR,
      reconnect_attempts: this.reconnectAttempts
    };
  }

  getLogs() {
    return this.recentLogs;
  }
}

export const sessionManager = new SessionManager();
