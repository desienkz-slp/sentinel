import QRCode from 'qrcode';

class QRManager {
  constructor() {
    this.rawQR = null;
    this.dataURL = null;
    this.generatedAt = null;
    this.ttlMs = 45000; // 45 seconds default WhatsApp QR refresh cycle
  }

  async setQR(qrString) {
    this.rawQR = qrString;
    this.generatedAt = Date.now();
    try {
      this.dataURL = await QRCode.toDataURL(qrString, {
        width: 320,
        margin: 2,
        color: {
          dark: '#020617',
          light: '#ffffff'
        }
      });
    } catch (err) {
      console.error('[QR] Failed to render QR image:', err.message);
      this.dataURL = null;
    }
  }

  getQR() {
    if (!this.rawQR || !this.generatedAt) {
      return {
        available: false,
        qr: null,
        expires_in: 0,
        is_expired: true
      };
    }

    const elapsed = Date.now() - this.generatedAt;
    const remaining = Math.max(0, Math.floor((this.ttlMs - elapsed) / 1000));
    const isExpired = remaining <= 0;

    return {
      available: !isExpired && !!this.dataURL,
      qr: !isExpired ? this.dataURL : null,
      raw: !isExpired ? this.rawQR : null,
      expires_in: remaining,
      is_expired: isExpired,
      generated_at: new Date(this.generatedAt).toISOString()
    };
  }

  clear() {
    this.rawQR = null;
    this.dataURL = null;
    this.generatedAt = null;
  }
}

export const qrManager = new QRManager();
