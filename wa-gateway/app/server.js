import express from 'express';
import cors from 'cors';
import path from 'path';
import { fileURLToPath } from 'url';
import axios from 'axios';
import { sessionManager } from './session.js';
import { qrManager } from './qr.js';
import { webhookDispatcher } from './webhook.js';
import { healthMonitor } from './health.js';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

const app = express();
const PORT = parseInt(process.env.PORT || '3001', 10);
const PUBLIC_DIR = path.resolve(__dirname, '../public');

app.use(cors());
app.use(express.json());
app.use(express.urlencoded({ extended: true }));

// Serve static frontend assets
app.use(express.static(PUBLIC_DIR));

// -------------------------------------------------------------
// UI ROUTES
// -------------------------------------------------------------
app.get('/whatsapp', (req, res) => {
  res.sendFile(path.join(PUBLIC_DIR, 'index.html'));
});

app.get('/', (req, res) => {
  res.redirect('/whatsapp');
});

// -------------------------------------------------------------
// MANAGEMENT API ENDPOINTS (Strict specification)
// -------------------------------------------------------------

// 1. GET /api/whatsapp/status
app.get('/api/whatsapp/status', (req, res) => {
  res.json(sessionManager.getStatus());
});

// 2. GET /api/whatsapp/qr
app.get('/api/whatsapp/qr', (req, res) => {
  const qrData = qrManager.getQR();
  res.json({
    status: sessionManager.status,
    ...qrData
  });
});

// 3. POST /api/whatsapp/connect
app.post('/api/whatsapp/connect', async (req, res) => {
  try {
    sessionManager.connect();
    res.json({ success: true, message: 'Connection initiated', status: sessionManager.status });
  } catch (err) {
    res.status(500).json({ success: false, error: err.message });
  }
});

// 4. POST /api/whatsapp/reconnect
app.post('/api/whatsapp/reconnect', async (req, res) => {
  try {
    const result = await sessionManager.reconnect();
    res.json(result);
  } catch (err) {
    res.status(500).json({ success: false, error: err.message });
  }
});

// 5. POST /api/whatsapp/logout
app.post('/api/whatsapp/logout', async (req, res) => {
  try {
    const result = await sessionManager.logout();
    res.json(result);
  } catch (err) {
    res.status(500).json({ success: false, error: err.message });
  }
});

// 6. GET /api/whatsapp/session
app.get('/api/whatsapp/session', (req, res) => {
  res.json(sessionManager.getSessionMetadata());
});

// 7. GET /api/whatsapp/logs
app.get('/api/whatsapp/logs', (req, res) => {
  res.json({ logs: sessionManager.getLogs() });
});

// 8. GET /api/whatsapp/health
app.get('/api/whatsapp/health', (req, res) => {
  res.json(healthMonitor.getHealthReport());
});

// 9. POST /api/whatsapp/send
app.post('/api/whatsapp/send', async (req, res) => {
  const { to, message } = req.body;
  if (!to || !message) {
    return res.status(400).json({ error: 'Fields "to" and "message" are required.' });
  }

  try {
    const sendResult = await sessionManager.sendMessage(to, message);
    res.json(sendResult);
  } catch (err) {
    res.status(500).json({ error: err.message });
  }
});

// 10. POST /api/whatsapp/simulate-query
// Allows testing the complete end-to-end Hermes -> Diagnostic pipeline directly from Web UI
app.post('/api/whatsapp/simulate-query', async (req, res) => {
  const { query, sender } = req.body;
  if (!query) {
    return res.status(400).json({ error: 'Query is required' });
  }

  const payload = {
    message_id: `sim_${Date.now()}`,
    chat_id: sender || 'simulated_operator',
    sender: sender || 'operator',
    sender_name: 'Simulated Operator',
    message: query,
    timestamp: new Date().toISOString(),
    type: 'text'
  };

  try {
    // Call the AI-NOC pipeline orchestrator
    const orchUrl = process.env.ORCHESTRATOR_URL || 'http://127.0.0.1:8000/pipeline/whatsapp';
    const response = await axios.post(orchUrl, payload, { timeout: 180000 }); // pipeline diagnosis bisa 45-90s
    res.json(response.data);
  } catch (err) {
    res.status(500).json({
      error: `Failed to execute pipeline: ${err.message}`,
      details: err.response?.data || null
    });
  }
});

// Process-level crash guards
process.on('unhandledRejection', (reason, promise) => {
  console.error('[AI-NOC] Unhandled Rejection caught:', reason?.message || reason);
});
process.on('uncaughtException', (err) => {
  console.error('[AI-NOC] Uncaught Exception caught:', err.message);
});

// -------------------------------------------------------------
// SERVER INITIALIZATION
// -------------------------------------------------------------
app.listen(PORT, '0.0.0.0', () => {
  console.log(`[AI-NOC] WhatsApp Gateway listening on http://0.0.0.0:${PORT}`);
  console.log(`[AI-NOC] Web UI accessible at http://0.0.0.0:${PORT}/whatsapp`);

  // Start background health monitor
  healthMonitor.start(30000);

  // Auto-connect on startup
  sessionManager.connect();
});
