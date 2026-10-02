import axios from 'axios';

// Ingress tunggal untuk pesan masuk: aplikasi NOC Sentinel (Go) di
// /api/wa/webhook. Tidak ada lagi jalur n8n atau orchestrator Python lama —
// orchestration deterministik ada di sisi Go (master spec §13). Env
// N8N_WEBHOOK_URL dipertahankan sebagai alias kompatibilitas lama; env
// kanoniknya adalah NOC_WEBHOOK_URL.
function resolveIngressUrl() {
  return process.env.NOC_WEBHOOK_URL
    || process.env.N8N_WEBHOOK_URL
    || 'http://127.0.0.1:8090/api/wa/webhook';
}

class WebhookDispatcher {
  constructor() {
    this.ingressUrl = resolveIngressUrl();
    this.status = 'standby';
    this.lastDispatchedAt = null;
    this.lastLatencyMs = 0;
    this.lastStatusCode = null;
    this.lastError = null;
    this.successCount = 0;
    this.failCount = 0;
  }

  async dispatchInbound(messageData, sessionManager) {
    const payload = {
      message_id: messageData.message_id || `msg_${Date.now()}`,
      chat_id: messageData.chat_id || messageData.sender,
      sender: messageData.sender || 'unknown',
      sender_name: messageData.sender_name || 'WhatsApp User',
      message: messageData.message || '',
      timestamp: messageData.timestamp || new Date().toISOString(),
      type: messageData.type || 'text'
    };

    this.lastDispatchedAt = new Date().toISOString();
    const startT = Date.now();

    try {
      const resp = await axios.post(this.ingressUrl, payload, {
        headers: {
          'Content-Type': 'application/json',
          'X-AI-NOC-Source': 'whatsapp-gateway'
        },
        timeout: 180000 // pipeline diagnosis (beberapa probe berurutan + LLM) butuh 45-90s
      });

      this.lastLatencyMs = Date.now() - startT;
      this.lastStatusCode = resp.status;
      this.status = resp.status >= 200 && resp.status < 300 ? 'healthy' : 'degraded';
      this.lastError = null;
      this.successCount++;

      // Aplikasi NOC Sentinel mengembalikan `reply` (balasan untuk pelanggan)
      // bila auto-reply aktif. Bila ada, kirim balik ke WhatsApp.
      if (resp.data && resp.data.reply && sessionManager) {
        sessionManager.addLog(`Balasan diterima dari NOC Sentinel (${resp.data.reply.length} chars) -> mengirim ke ${payload.chat_id}`);
        try {
          await sessionManager.sendMessage(payload.chat_id, resp.data.reply);
        } catch (sendErr) {
          // Jangan telan error kirim: tanpa log ini, pesan yang gagal terkirim
          // tampak seperti "bot tidak membalas" padahal balasannya sudah dibuat.
          console.error(`[Webhook] GAGAL mengirim balasan ke ${payload.chat_id}: ${sendErr.message}`);
          sessionManager.addLog(`GAGAL mengirim balasan ke ${payload.chat_id}: ${sendErr.message}`, 'ERROR');
          return { success: false, status: resp.status, error: sendErr.message };
        }
      } else if (resp.data) {
        // Server menerima pesan tetapi tidak menyertakan balasan. Ini penyebab
        // paling umum "tidak dibalas": pesan ditolak allowlist, diabaikan
        // sebagai pesan grup, atau auto-reply dimatikan.
        sessionManager?.addLog(
          `Tidak ada balasan untuk ${payload.chat_id} (accepted=${resp.data.accepted}, note=${resp.data.note || '-'})`,
          'WARN'
        );
      }

      return { success: true, status: resp.status, data: resp.data };
    } catch (err) {
      this.lastLatencyMs = Date.now() - startT;
      this.lastStatusCode = err.response ? err.response.status : 503;
      this.lastError = err.message;
      this.status = 'failing';
      this.failCount++;
      console.error(`[Webhook] Gagal menghubungi NOC Sentinel (${this.ingressUrl}): ${err.message}`);
      return { success: false, error: err.message };
    }
  }

  getStatus() {
    return {
      status: this.status,
      target_url: this.ingressUrl,
      last_dispatched_at: this.lastDispatchedAt,
      last_latency_ms: this.lastLatencyMs,
      last_status_code: this.lastStatusCode,
      last_error: this.lastError,
      success_count: this.successCount,
      fail_count: this.failCount
    };
  }
}

export const webhookDispatcher = new WebhookDispatcher();
