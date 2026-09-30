import { sessionManager } from './session.js';
import { webhookDispatcher } from './webhook.js';

class HealthMonitor {
  constructor() {
    this.interval = null;
  }

  start(intervalMs = 30000) {
    if (this.interval) clearInterval(this.interval);
    this.interval = setInterval(() => this.recordHealth(), intervalMs);
  }

  getHealthReport() {
    const sessionStatus = sessionManager.status;
    const webhookStatus = webhookDispatcher.status;

    // Health calculation based on strict specifications
    let overallHealth = 'OK';
    if (sessionStatus === 'ERROR') {
      overallHealth = 'CRITICAL';
    } else if (sessionStatus === 'DISCONNECTED' || sessionStatus === 'AUTH_REQUIRED') {
      overallHealth = 'WARN';
    }

    return {
      status: sessionStatus, // CONNECTED, CONNECTING, DISCONNECTED, AUTH_REQUIRED, ERROR
      health: overallHealth,
      phone: sessionManager.phone,
      name: sessionManager.name,
      session: sessionManager.sessionId,
      connected_at: sessionManager.connectedAt,
      last_message_at: sessionManager.lastMessageAt,
      webhook: webhookStatus,
      webhook_details: webhookDispatcher.getStatus(),
      uptime_seconds: Math.floor(process.uptime()),
      timestamp: new Date().toISOString()
    };
  }

  async recordHealth() {
    if (!sessionManager.dbPool) return;
    const rep = this.getHealthReport();
    try {
      await sessionManager.dbPool.query(
        `INSERT INTO whatsapp_health (status, webhook_status, latency_ms, error_message, checked_at)
         VALUES ($1, $2, $3, $4, NOW())`,
        [rep.status, rep.webhook, rep.webhook_details.last_latency_ms, sessionManager.lastError]
      );
    } catch (e) {
      // Non-blocking
    }
  }

  stop() {
    if (this.interval) clearInterval(this.interval);
  }
}

export const healthMonitor = new HealthMonitor();
