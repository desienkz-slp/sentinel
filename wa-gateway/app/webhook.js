import axios from 'axios';

class WebhookDispatcher {
  constructor() {
    this.n8nWebhookUrl = process.env.N8N_WEBHOOK_URL || 'http://127.0.0.1:5678/webhook/whatsapp-inbound';
    this.orchestratorUrl = process.env.ORCHESTRATOR_URL || 'http://127.0.0.1:8000/pipeline/whatsapp';
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

    // 1. Attempt dispatch to n8n webhook
    try {
      const resp = await axios.post(this.n8nWebhookUrl, payload, {
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

      // If n8n returns a direct reply in body, dispatch back to WhatsApp
      if (resp.data && resp.data.reply && sessionManager) {
        sessionManager.addLog(`Balasan diterima dari AI-NOC (${resp.data.reply.length} chars) -> mengirim ke ${payload.chat_id}`);
        try {
          await sessionManager.sendMessage(payload.chat_id, resp.data.reply);
        } catch (sendErr) {
          // Jangan telan error kirim: tanpa log ini, pesan yang gagal terkirim
          // tampak seperti "bot tidak membalas" padahal balasannya sudah dibuat.
          console.error(`[Webhook] GAGAL mengirim balasan ke ${payload.chat_id}: ${sendErr.message}`);
          sessionManager.addLog(`GAGAL mengirim balasan ke ${payload.chat_id}: ${sendErr.message}`, 'ERROR');
          return { success: false, via: 'n8n', status: resp.status, error: sendErr.message };
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

      return { success: true, via: 'n8n', status: resp.status, data: resp.data };
    } catch (err) {
      this.lastLatencyMs = Date.now() - startT;
      this.lastStatusCode = err.response ? err.response.status : 503;
      this.lastError = err.message;
      this.status = 'failing';
      this.failCount++;
      console.warn(`[Webhook] n8n webhook unreachable (${err.message}). Triggering fallback orchestrator...`);

      // 2. Fallback: Trigger direct AI-NOC orchestrator so diagnostic pipeline always runs
      try {
        const orchResp = await axios.post(this.orchestratorUrl, payload, {
          headers: { 'Content-Type': 'application/json' },
          timeout: 180000 // fallback orchestrator, sama seperti jalur n8n
        });

        if (orchResp.data && orchResp.data.report && sessionManager) {
          await sessionManager.sendMessage(payload.chat_id, orchResp.data.report);
        }

        return { success: true, via: 'orchestrator_fallback', data: orchResp.data };
      } catch (orchErr) {
        console.error(`[Webhook] Fallback orchestrator also failed: ${orchErr.message}`);
        return { success: false, error: orchErr.message };
      }
    }
  }

  getStatus() {
    return {
      status: this.status,
      target_url: this.n8nWebhookUrl,
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
