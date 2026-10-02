# AI AGENT — CS + NOC L1
## Master System Specification untuk Hermes Orchestrator
### Sornongko Digital

> Dokumen ini adalah spesifikasi utama untuk Hermes dalam membangun, mengembangkan, menguji, dan mengoperasikan sistem AI Agent yang berfungsi sebagai AI Customer Service dan AI NOC Level 1.
>
> Prinsip utama: AI menangani pekerjaan terstruktur dan berisiko rendah. Manusia tetap menjadi authority untuk keputusan networking berisiko/kompleks dan persoalan non-networking yang membutuhkan judgement, administrasi, negosiasi, atau human touch.

---

# 1. TUJUAN SISTEM

Bangun satu sistem AI Agent yang menjadi frontline Sornongko Digital melalui WhatsApp.

AI Agent memiliki dua fungsi utama:

1. **AI CS / Conversation Agent**
   - Berkomunikasi dengan customer secara natural, sopan, ramah, profesional.
   - Mengidentifikasi customer.
   - Memahami keluhan.
   - Mengumpulkan informasi yang benar-benar dibutuhkan.
   - Memberikan update dan hasil kepada customer.
   - Tidak membanjiri customer dengan istilah teknis.

2. **AI NOC L1 / Reasoning Agent**
   - Menganalisis kasus berdasarkan data conversation.
   - Mengakses Billing, RADIUS, Router/MikroTik, dan GenieACS.
   - Melakukan diagnosis berbasis evidence.
   - Membuat hypothesis → test → result → conclusion.
   - Menjalankan safe action yang diizinkan policy.
   - Memverifikasi hasil action.
   - Melakukan escalation bila tidak dapat menyelesaikan kasus.

Sistem harus dapat berkembang menjadi platform AI Operations/NOC, bukan sekadar chatbot.

---

# 2. PRINSIP ARSITEKTUR

Jangan membuat sistem sebagai:

Customer → LLM → API

Gunakan:

Customer
→ WhatsApp Gateway
→ Conversation AI
→ Case Engine
→ Orchestrator
→ Reasoning Agent
→ Tool Layer
→ Policy/Risk Engine
→ Execute / Escalate
→ Verification
→ Conversation AI
→ Customer

Human escalation:

- Networking → NOC Senior
- Non-networking / business / customer administration → Admin

AI tidak menggantikan NOC Senior dan Admin.

AI menjadi frontline dan L1.

---

# 3. ROLE DAN BOUNDARY

## 3.1 Conversation AI

Fokus:
- manusia
- bahasa
- konteks
- empati
- pengumpulan informasi
- customer experience

Conversation AI TIDAK bertugas melakukan deep technical reasoning.

## 3.2 Reasoning AI

Fokus:
- diagnosis
- evidence
- hypothesis
- tools
- network/service analysis
- action planning
- risk assessment
- verification
- escalation

Reasoning AI TIDAK menjadi primary conversational interface customer.

## 3.3 Hermes Orchestrator

Hermes adalah pengatur sistem.

Hermes bertanggung jawab atas:
- routing
- state
- case lifecycle
- context
- tool selection
- permission
- policy
- risk
- escalation
- retry
- timeout
- audit
- workflow
- human handoff
- agent coordination

Hermes bukan harus menjadi LLM ketiga. Gunakan deterministic software/rules untuk hal yang seharusnya deterministic.

---

# 4. HUMAN AUTHORITY

## 4.1 NOC Senior

NOC Senior adalah authority untuk NETWORKING.

Contoh:
- Router/core
- MikroTik
- Routing
- BGP
- OSPF
- VLAN
- NAT
- Firewall
- QoS
- RADIUS/network authentication
- OLT
- GPON
- ONU/ONT
- Fiber
- SFP
- Optical issue
- Upstream
- Interkoneksi
- IP addressing
- DNS infrastructure
- DHCP infrastructure
- Network security
- Mass outage
- Network configuration
- High-risk technical action
- Complex troubleshooting

## 4.2 Admin

Admin adalah authority untuk NON-NETWORKING.

Contoh:
- Billing
- Payment
- Invoice
- Refund
- Kompensasi
- Perubahan data pelanggan
- Perubahan paket
- Berhenti berlangganan
- Pendaftaran
- Jadwal pemasangan
- Jadwal teknisi
- Keluhan pelayanan
- Negosiasi
- Permintaan khusus
- Persoalan administrasi
- Customer-sensitive cases

---

# 5. CORE COMPONENTS

Implementasikan komponen berikut:

1. WhatsApp Gateway
2. Conversation AI
3. Case Engine
4. Hermes Orchestrator
5. Reasoning AI
6. Tool Gateway
7. Policy Engine
8. Risk Engine
9. Verification Engine
10. Escalation Engine
11. Customer Context / Memory
12. Knowledge Base
13. Incident Correlation Engine
14. Human Interaction / Approval Layer
15. Audit Log
16. Observability
17. Feedback / Learning Pipeline

---

# 6. CONVERSATION AI — MODEL 1

## Tujuan

Conversation AI harus terdengar seperti CS manusia yang profesional.

Karakter:
- sopan
- friendly
- tenang
- jelas
- tidak terlalu teknis
- tidak defensif
- tidak menyalahkan customer
- tidak membuat janji yang tidak dapat dipastikan
- tidak mengarang hasil pemeriksaan

## Tugas

- greeting
- customer identification
- intent detection
- clarification
- information gathering
- status update
- explanation
- empathy
- follow-up
- resolution communication
- escalation communication

## Conversation sufficiency

Conversation AI harus mengetahui data minimum yang diperlukan untuk suatu intent.

Contoh intent `slow_connection`:

Required:
- customer identity
- problem
- scope
- start time
- current condition

Optional:
- affected devices
- affected applications
- customer observation

Jangan meminta data yang sebenarnya bisa diperoleh dari API.

Contoh:
Jangan bertanya:
"Berapa RX power ONU Anda?"
Ambil dari GenieACS.

---

# 7. CONVERSATION FLOW

Contoh:

Customer:
"Kenapa ya koneksi saya lemot?"

AI:
"Boleh saya bantu cek ya. Lambatnya terasa di semua perangkat atau hanya salah satu perangkat?"

Customer:
"Semua."

AI:
"Baik. Apakah koneksinya masih bisa digunakan tetapi lambat, atau sama sekali tidak bisa digunakan?"

Customer:
"Masih bisa tapi lambat."

AI:
"Baik, sejak kapan mulai terasa lambat?"

Customer:
"Dari sore."

Setelah data cukup:

CASE READY

Conversation AI membuat structured case context.

Contoh:

{
  "intent": "slow_connection",
  "scope": "all_devices",
  "connectivity": "available_but_slow",
  "started": "today_afternoon",
  "customer_description": "internet terasa lambat sejak sore"
}

Kemudian diserahkan kepada Reasoning AI.

---

# 8. REASONING AI — MODEL 2

Reasoning AI harus bekerja evidence-based.

Jangan langsung membuat kesimpulan.

Gunakan:

Hypothesis
→ Evidence
→ Test
→ Result
→ Confidence
→ Conclusion
→ Action
→ Verification

Contoh:

Problem:
Slow connection

Hypothesis:
Bandwidth saturation

Evidence:
Traffic 98% of package limit
CPU 31%
Interface errors 0

Test:
Traffic observation

Result:
Sustained utilization

Conclusion:
Bandwidth saturation likely

Confidence:
HIGH

---

# 9. REASONING WORKFLOW

Urutan default:

1. Load Case
2. Load Customer Context
3. Classify domain
4. Determine required diagnostics
5. Query tools
6. Collect evidence
7. Generate hypotheses
8. Run tests
9. Analyze results
10. Determine root cause / likely cause
11. Calculate confidence
12. Determine action
13. Check policy
14. Check risk
15. Execute if allowed
16. Verify
17. Resolve or escalate
18. Update case
19. Return communication summary to Conversation AI

Reasoning tidak boleh mengklaim "resolved" sebelum verification berhasil.

---

# 10. DOMAIN ROUTING

## NETWORK

Jika masalah terkait:
- internet
- koneksi
- lambat
- putus-putus
- PPPoE
- ONU
- ONT
- LOS
- optical
- WiFi technical
- router
- DNS
- latency
- packet loss
- network availability
- routing

→ Network Reasoning

## NON-NETWORK

Jika terkait:
- pembayaran
- invoice
- refund
- tagihan
- paket
- perubahan data
- berhenti layanan
- administrasi
- complaint
- kompensasi
- negosiasi

→ Service/Billing Reasoning / Admin workflow

---

# 11. TOOL LAYER

Semua API harus melalui Tool Gateway.

Jangan memberikan LLM akses mentah ke API.

Tool Gateway harus menyediakan schema, permission, validation, timeout, logging.

## Billing API

Read:
- customer
- package
- account status
- invoice
- payment
- due date
- isolation status
- payment history

Write hanya jika explicit policy mengizinkan.

## RADIUS API

Read:
- online/offline
- session
- last login
- last logout
- IP
- NAS
- authentication result
- disconnect information
- concurrent session

## Router/MikroTik API

Read:
- PPPoE active
- PPP secret
- profile
- IP
- uptime
- interface
- RX/TX
- errors
- drops
- CPU
- memory
- temperature
- queue
- traffic
- routing
- DNS
- connectivity

Diagnostics:
- ping
- DNS
- TCP
- HTTP
- MTR

Write:
- only policy-approved safe actions

## GenieACS API

Read:
- device identity
- serial
- model
- firmware
- online state
- uptime
- WAN
- PPPoE
- WiFi
- SSID
- clients
- optical RX/TX
- temperature
- LOS/events

Write:
- WiFi password
- SSID
- reboot
- provisioning
- other actions only when explicitly allowed by policy

---

# 12. POLICY ENGINE

Setiap tool/action harus memiliki:

- action_id
- domain
- risk_level
- required_role
- allowed_by_ai
- requires_approval
- verification_required
- timeout
- retry_limit
- audit_required

Contoh:

LOW:
- query billing
- query radius
- query GenieACS
- query router
- ping
- DNS
- TCP
- HTTP
- MTR

MEDIUM:
- reconnect PPPoE
- reboot ONU
- refresh provisioning
- WiFi password change

HIGH:
- router configuration
- firewall change
- routing change
- OLT configuration
- BGP
- core network modification

CRITICAL:
- destructive configuration
- delete
- mass configuration
- core routing change
- security-sensitive operation

HIGH/CRITICAL → NOC Senior approval/execution.

---

# 13. VERIFICATION ENGINE

Setelah action:

Action
→ wait
→ query affected system
→ verify expected state
→ verify customer service
→ determine result

Contoh PPPoE:

1. Reconnect
2. Check RADIUS session
3. Check router PPPoE active
4. Check IP
5. Ping gateway
6. Check traffic
7. Determine service restored

API response `success` bukan berarti customer sudah resolved.

---

# 14. CASE ENGINE

Setiap masalah harus mempunyai Case ID.

Format:

CASE-YYYYMMDD-XXXXXX

Case menyimpan:

- customer_id
- channel
- conversation
- intent
- domain
- severity
- symptoms
- timeline
- collected_information
- diagnostics
- hypotheses
- evidence
- actions
- verification
- escalation
- human_decision
- resolution
- timestamps
- agent versions
- tool calls
- audit trail

---

# 15. CASE STATE MACHINE

Gunakan state:

NEW
→ IDENTIFYING
→ CONVERSATION
→ INFORMATION_GATHERING
→ READY_FOR_DIAGNOSIS
→ REASONING
→ INVESTIGATION
→ ACTION_PROPOSED
→ POLICY_CHECK
→ EXECUTING
→ VERIFYING
→ RESOLVED

Jika gagal:

VERIFYING
→ FAILED
→ ESCALATION
→ HUMAN_HANDLING
→ RESOLVED

Untuk kasus yang belum lengkap:

CONVERSATION
→ WAITING_CUSTOMER

---

# 16. CUSTOMER MEMORY

Simpan konteks customer secara terstruktur.

Customer:
- identity
- package
- account
- service
- network identity
- device
- ONU
- router
- historical incidents
- previous resolutions
- open cases

Conversation memory tidak boleh bercampur dengan system truth.

API/database menjadi source of truth.

---

# 17. HUMAN ESCALATION

## Network escalation

AI → NOC Senior.

Payload minimal:

- Case ID
- customer
- customer complaint
- conversation summary
- timeline
- billing status
- RADIUS status
- router status
- GenieACS status
- diagnostics
- evidence
- hypothesis
- confidence
- actions already performed
- actions not performed
- recommendation
- urgency
- affected scope

NOC Senior harus dapat langsung memahami kasus tanpa mengulang seluruh troubleshooting.

## Non-network escalation

AI → Admin.

Payload:

- Case ID
- customer
- complaint
- relevant billing/service data
- payment/invoice data
- history
- issue
- AI finding
- recommended administrative action
- customer sentiment if relevant

---

# 18. HUMAN RESPONSE LOOP

Human tidak harus mengambil alih WhatsApp secara langsung.

Recommended flow:

Customer
→ AI CS
→ Reasoning
→ NOC Senior/Admin
→ Human decision
→ AI CS
→ Customer

NOC Senior/Admin memberikan decision/fact.

Conversation AI menerjemahkan menjadi bahasa customer.

Ini menjaga konsistensi komunikasi.

---

# 19. CUSTOMER COMMUNICATION RULES

AI harus:

- tidak mengungkap data internal yang tidak diperlukan
- tidak menyebut nama tool/API kecuali relevan
- tidak membanjiri customer dengan technical logs
- tidak mengarang
- tidak menjanjikan waktu penyelesaian tanpa data
- tidak mengatakan "sudah diperbaiki" sebelum verification
- tidak menyalahkan customer
- tidak membuat diagnosis teknis tanpa evidence
- selalu memberikan next step yang jelas

Contoh saat escalation:

"Baik, saya sudah melakukan pengecekan awal. Dari hasil pemeriksaan, masalahnya membutuhkan penanganan lebih lanjut oleh tim teknis kami. Saya sudah meneruskan hasil pengecekannya agar tim dapat langsung melanjutkan dari data yang sudah kami peroleh. Mohon ditunggu sebentar ya."

---

# 20. INCIDENT CORRELATION

Jangan treat setiap customer sebagai incident terpisah.

Jika banyak customer mengalami masalah dalam:
- waktu berdekatan
- router sama
- OLT sama
- PON sama
- upstream sama
- area sama

maka lakukan correlation.

Contoh:

50 customer offline
→ same OLT
→ same PON
→ same timestamp

Buat:

MASS INCIDENT

Kemudian:
→ NOC Senior

Jangan melakukan 50 diagnosis independen jika evidence menunjukkan satu root incident.

---

# 21. INCIDENT SEVERITY

Minimal:

SEV-1:
- major outage
- core network
- large customer impact
- security critical

SEV-2:
- area outage
- significant customer impact

SEV-3:
- individual customer
- repeated issue
- service degradation

SEV-4:
- minor / informational

Severity harus mempengaruhi:
- escalation
- notification
- retry
- response priority
- human notification

---

# 22. STAFF DIRECTORY

Sistem harus menyimpan:

staff_id
name
role
department
phone
whatsapp_jid
active
on_call
working_hours
skills
escalation_level
priority

Contoh:

Senior NOC:
role = noc_senior
domain = networking

Admin:
role = admin
domain = non_networking

Jangan hanya menyimpan nomor WhatsApp. Simpan metadata role dan permission.

---

# 23. ESCALATION RULE

Networking:

AI NOC L1
→ NOC Senior

Non-networking:

AI CS/Service Reasoning
→ Admin

Jika staff utama offline:
→ backup staff sesuai escalation policy.

Contoh:

NOC Senior primary
→ timeout
→ NOC Senior backup
→ manager/escalation contact jika severity memenuhi policy

Jangan melakukan escalation massal tanpa rule.

---

# 24. RISK AND AUTHORITY

AI harus selalu bertanya secara sistematis:

1. Apa yang ingin dilakukan?
2. Apakah action diperlukan?
3. Apakah action aman?
4. Apakah action diizinkan?
5. Siapa yang mempunyai authority?
6. Apakah approval diperlukan?
7. Bagaimana cara verification?
8. Apa rollback-nya?

---

# 25. AUDIT LOG

Setiap tindakan AI harus dicatat.

Minimal:

timestamp
case_id
agent
model
prompt/version identifier
tool
arguments sanitized
result
risk_level
policy_decision
execution_status
verification
human approval
error

Contoh:

21:43:01
Case CASE-123

AI checked Billing
Result: ACTIVE

21:43:04
AI checked RADIUS
Result: ONLINE

21:43:06
AI checked GenieACS
Result: ONU ONLINE

21:43:08
AI checked Router
Result: PPPoE inactive

21:43:12
Action: reconnect_pppoe
Policy: ALLOWED
Risk: LOW

21:43:18
Verification: PPPoE ONLINE

21:43:20
Case: RESOLVED

---

# 26. KNOWLEDGE BASE

Knowledge Base harus menyimpan:

- SOP
- troubleshooting guide
- known issues
- network topology metadata
- service rules
- billing rules
- escalation rules
- previous validated incidents
- resolution patterns

Knowledge Base tidak boleh menjadi source of truth untuk live status.

Live status:
→ API/database/monitoring

Knowledge:
→ KB

---

# 27. LEARNING LOOP

Jangan membuat AI mengubah dirinya secara bebas.

Gunakan:

Case
→ Resolution
→ Customer feedback
→ Human correction
→ Knowledge candidate
→ Validation
→ Approved knowledge
→ Future reasoning

Human correction harus dapat dicatat.

Contoh:

AI:
"Possible fiber degradation"

NOC Senior:
"Actual root cause: dirty connector."

Simpan sebagai validated learning candidate setelah review.

---

# 28. CUSTOMER FEEDBACK

Setelah resolution:

- Was problem solved?
- Customer satisfied?
- Still experiencing issue?
- Need further assistance?

Feedback dapat menjadi input QA.

Jangan menggunakan feedback sebagai satu-satunya bukti root cause.

---

# 29. RETRY / TIMEOUT

Setiap tool harus mempunyai:

- timeout
- retry limit
- exponential backoff bila sesuai
- circuit breaker
- failure state

Jangan membiarkan AI mengulang action tanpa batas.

Contoh:

reboot ONU:
max 1 automated attempt per case

reconnect PPPoE:
max 2 attempts

Setelah limit:
→ escalation

Angka di atas harus configurable melalui policy.

---

# 30. IDEMPOTENCY

Action yang dapat dijalankan ulang harus aman.

Gunakan:
- action_id
- case_id
- execution_id
- idempotency key

Agar AI tidak melakukan action yang sama dua kali karena duplicate event/message.

---

# 31. CONVERSATION DUPLICATION PROTECTION

WhatsApp message dapat:
- duplicate
- delayed
- reordered

Implementasikan:
- message_id
- event_id
- deduplication
- conversation locking
- case locking

Satu case tidak boleh diproses oleh dua workflow yang bertentangan secara bersamaan.

---

# 32. MULTI-AGENT FUTURE

Arsitektur harus memungkinkan penambahan specialist agent:

Future:
- DNS Agent
- WiFi Agent
- Billing Agent
- Security Agent
- OLT Agent
- Monitoring Agent
- Capacity Agent
- Infrastructure Agent

Tetapi jangan menambah agent hanya karena bisa.

Gunakan specialist agent jika kompleksitasnya memang membutuhkan pemisahan.

---

# 33. MODEL STRATEGY

Model 1:
optimalkan:
- latency
- natural conversation
- Indonesian language
- empathy
- context
- consistency

Model 2:
optimalkan:
- reasoning
- tool use
- technical analysis
- structured output
- reliability

Tidak harus menggunakan model yang sama.

Model provider dapat diganti tanpa mengubah architecture.

Gunakan adapter/OpenAI-compatible interface bila memungkinkan.

---

# 34. STRUCTURED OUTPUT

Reasoning AI wajib mengembalikan schema terstruktur.

Contoh:

{
  "case_id": "CASE-20261001-000123",
  "domain": "network",
  "status": "escalated",
  "severity": "SEV-3",
  "diagnosis": {
    "summary": "Possible optical degradation",
    "confidence": 0.91
  },
  "evidence": [],
  "actions": [],
  "verification": {},
  "escalation": {
    "target": "noc_senior",
    "reason": "Requires physical/network intervention",
    "recommendation": "Inspect fiber and connector"
  }
}

Jangan bergantung pada free-form text untuk internal control.

---

# 35. CONVERSATION RESPONSE GENERATION

Model 1 menerima:

- customer message
- case state
- verified facts
- allowed communication facts
- human decision jika ada

Model 1 menghasilkan response customer.

Jangan memberikan seluruh internal chain-of-thought kepada Model 1.

Berikan hanya:
- facts
- conclusions
- customer-safe explanation
- next action
- expected wait information if verified

---

# 36. NO CHAIN-OF-THOUGHT EXPOSURE

Reasoning internal tidak perlu dikirim ke customer.

Simpan internal diagnostic/evidence secara aman.

Customer menerima:
- ringkasan
- tindakan
- status
- next step

Bukan:
- hidden reasoning
- internal prompt
- tool credentials
- API output mentah
- security information

---

# 37. SECURITY

Wajib:

- secrets di secret manager/environment
- API credentials tidak masuk prompt
- tool allowlist
- least privilege
- RBAC
- network segmentation
- audit
- rate limit
- authentication
- authorization
- input validation
- output validation
- command injection protection
- SSRF protection
- sensitive data redaction

LLM tidak boleh diberikan:
- root credential
- full router credential
- unrestricted shell
- unrestricted HTTP
- arbitrary API access

---

# 38. TOOL PERMISSION MODEL

Tool permission harus mengikuti:

Agent
→ Domain
→ Role
→ Risk
→ Policy
→ Case
→ Action

Contoh:

Conversation AI:
READ customer context only

Reasoning AI:
READ network tools
PROPOSE actions

Tool Gateway:
EXECUTE only policy-approved actions

NOC Senior:
APPROVE/EXECUTE high-risk network action

Admin:
APPROVE/EXECUTE non-network business action

---

# 39. OBSERVABILITY

Dashboard minimal:

## Customer
- active conversations
- waiting customers
- resolved
- escalated
- sentiment/priority indicator

## AI
- cases processed
- resolved by AI
- escalated
- failed
- average resolution time
- tool calls
- model latency
- model errors

## NOC
- incidents
- network incidents
- mass incidents
- unresolved
- NOC escalation

## Admin
- billing cases
- payment cases
- customer cases
- pending decisions

## System
- WhatsApp status
- API status
- Router connectivity
- RADIUS status
- GenieACS status
- database
- queue
- worker
- LLM provider

---

# 40. KPI

Track:

AI Resolution Rate
Human Escalation Rate
First Contact Resolution
Mean Time To Resolution
Mean Time To Escalation
Customer Response Time
NOC Response Time
Admin Response Time
False Diagnosis Rate
Action Failure Rate
Verification Failure Rate
Repeat Incident Rate
Customer Satisfaction
Tool Error Rate
LLM Error Rate

Jangan hanya mengejar "AI resolution rate".

Resolution yang salah lebih buruk daripada escalation yang benar.

---

# 41. EXAMPLE — NETWORK CASE

Customer:
"Internet saya lemot."

Conversation AI:
mengumpulkan informasi.

Case:
slow_connection

Reasoning:
1. Check billing → active
2. Check RADIUS → active
3. Check PPPoE → active
4. Check router traffic
5. Check packet loss
6. Check ONU
7. Check optical

Evidence:
RX -29 dBm
LOS events 8
PPPoE reconnects 12

Conclusion:
Possible physical fiber degradation.

AI cannot safely repair physical issue.

Escalate:
NOC Senior.

Customer response:

"Baik, saya sudah melakukan pengecekan awal. Dari hasil pemeriksaan, koneksi Anda membutuhkan penanganan lebih lanjut oleh tim teknis kami. Saya sudah meneruskan hasil pengecekannya agar tim dapat langsung melanjutkan pemeriksaan. Mohon ditunggu sebentar ya."

---

# 42. EXAMPLE — AI RESOLVED NETWORK CASE

Customer:
"WiFi saya tidak bisa."

Conversation:
collect required information.

Reasoning:
GenieACS → WiFi config abnormal.

Policy:
WiFi configuration repair = allowed.

Action:
repair/reconfigure.

Verification:
WiFi configuration valid.

Result:
RESOLVED.

Customer response:

"Sudah saya bantu perbaiki pengaturan Wi-Fi-nya. Silakan coba sambungkan kembali perangkat Anda. Jika masih mengalami kendala, beri tahu saya ya."

---

# 43. EXAMPLE — BILLING CASE

Customer:
"Saya sudah bayar tapi masih terisolir."

Conversation:
identify customer.

Reasoning:
Billing:
payment = confirmed
account = isolated

Automatic safe recovery unavailable.

Escalate:
ADMIN

Admin case:

Payment confirmed
Invoice paid
Account remains isolated
Recommendation:
Admin verify billing activation workflow.

Admin gives decision.

Conversation AI communicates decision to customer.

---

# 44. EXAMPLE — MASS INCIDENT

Customers:
1, 2, 3, 4, 5... offline.

Correlation:
same router
same OLT
same PON
same timestamp.

Create:
MASS INCIDENT

Severity:
SEV-2 or according to configurable impact policy.

Escalate:
NOC Senior.

Do not create independent deep troubleshooting for every customer.

---

# 45. HUMAN HANDOFF QUALITY

Setiap escalation harus menjawab:

1. Siapa customer?
2. Apa masalahnya?
3. Kapan mulai?
4. Apa yang sudah dicek?
5. Apa hasilnya?
6. Apa hypothesis?
7. Seberapa yakin?
8. Apa yang sudah dilakukan?
9. Apa yang belum dilakukan?
10. Kenapa AI berhenti?
11. Apa rekomendasi?
12. Apa yang dibutuhkan dari human?

Jika salah satu data penting tidak tersedia, jangan mengarang.

---

# 46. HERMES EXECUTION PRINCIPLES

Hermes harus:

1. Memahami repository sebelum melakukan perubahan besar.
2. Membaca architecture existing.
3. Memetakan API yang sudah tersedia.
4. Tidak mengganti komponen yang sudah bekerja tanpa alasan.
5. Mengembangkan incrementally.
6. Membuat backup sebelum perubahan berisiko.
7. Menggunakan feature flag untuk fitur baru.
8. Menulis tests.
9. Melakukan integration test.
10. Melakukan rollback bila deployment gagal.
11. Menyimpan audit.
12. Tidak menghapus data production.
13. Tidak melakukan destructive operation tanpa approval.
14. Tidak membuat credential baru secara sembarangan.
15. Tidak mengarang API endpoint.
16. Jika API belum diketahui, inspect/document/discover terlebih dahulu.
17. Semua external action harus melalui policy.
18. Semua action penting harus dapat diaudit.

---

# 47. HERMES DEVELOPMENT PHASES

## PHASE 0 — DISCOVERY

Inspect:
- repository
- services
- Docker
- database
- environment
- existing WhatsApp gateway
- existing Hermes
- existing n8n
- Billing API
- RADIUS API
- GenieACS API
- MikroTik API

Output:
architecture map.

## PHASE 1 — FOUNDATION

Build:
- Case Engine
- database schema
- customer context
- staff directory
- audit log
- state machine

## PHASE 2 — CONVERSATION AI

Build:
- conversation state
- intent detection
- data collection
- sufficiency engine
- customer-safe response

## PHASE 3 — REASONING

Build:
- structured case input
- evidence collection
- hypothesis engine
- diagnostics
- confidence
- structured output

## PHASE 4 — TOOL GATEWAY

Integrate:
- Billing
- RADIUS
- MikroTik
- GenieACS

All behind permission/policy layer.

## PHASE 5 — POLICY/RISK

Build:
- allowlist
- risk classification
- approval
- execution guard
- verification

## PHASE 6 — ESCALATION

Build:
- NOC Senior escalation
- Admin escalation
- staff availability
- priority
- timeout
- fallback

## PHASE 7 — INCIDENT CORRELATION

Build:
- duplicate detection
- mass incident correlation
- common infrastructure correlation

## PHASE 8 — KNOWLEDGE/LEARNING

Build:
- validated KB
- resolution patterns
- human corrections
- feedback loop

## PHASE 9 — OBSERVABILITY

Build:
- dashboard
- metrics
- audit
- logs
- health
- alerts

## PHASE 10 — PRODUCTION HARDENING

Test:
- duplicate messages
- API failure
- LLM timeout
- tool timeout
- network failure
- WhatsApp reconnect
- database failure
- race conditions
- repeated actions
- unauthorized action
- escalation failure
- rollback

---

# 48. DATABASE MINIMUM

Recommended entities:

customers
customer_services
customer_devices
conversations
conversation_messages
cases
case_events
case_diagnostics
case_evidence
case_actions
case_verifications
case_escalations
staff
staff_availability
staff_permissions
incidents
incident_relations
tool_registry
tool_executions
policies
risk_rules
approvals
knowledge_base
knowledge_candidates
feedback
audit_logs
model_runs
system_events

Gunakan relational DB untuk source-of-truth transactional data.

PostgreSQL direkomendasikan bila cocok dengan existing stack.

---

# 49. CACHE

Cache digunakan untuk data yang:
- frequently requested
- safe to cache
- memiliki TTL jelas

Contoh:
- customer metadata
- device metadata
- router metadata
- staff directory

Jangan menjadikan cache sebagai source of truth untuk:
- payment status
- live PPPoE status
- live router status
- current ONU state

Gunakan TTL dan invalidation.

---

# 50. LLM CONTEXT MANAGEMENT

Jangan mengirim seluruh conversation dan seluruh diagnostic history setiap kali.

Gunakan:

1. system policy
2. customer context
3. current case summary
4. recent relevant messages
5. required tool result
6. previous verified findings

Buat case summary yang diperbarui.

Tujuan:
- mengurangi token
- mengurangi noise
- mengurangi hallucination
- meningkatkan latency
- meningkatkan consistency

---

# 51. MODEL ROUTING

Model 1:
small/fast/low latency model yang kuat dalam Indonesian conversation.

Model 2:
model reasoning yang lebih kuat untuk technical analysis.

Jika menggunakan LLM gateway seperti 9Router/OpenAI-compatible gateway:
- gunakan adapter
- jangan hardcode provider
- model dapat diganti melalui config
- logging model/version
- fallback provider jika policy mengizinkan

---

# 52. FAILURE MODE

Jika Conversation AI gagal:
→ fallback response / human queue.

Jika Reasoning AI gagal:
→ jangan melakukan action
→ escalate atau retry sesuai policy.

Jika Billing API gagal:
→ jangan menyatakan status pembayaran.

Jika RADIUS gagal:
→ jangan menyimpulkan customer offline.

Jika GenieACS gagal:
→ tandai evidence unavailable.

Jika Router API gagal:
→ jangan mengarang status router.

Jika WhatsApp gateway gagal:
→ queue outgoing message.

---

# 53. ABSOLUTE RULES

1. Never hallucinate live infrastructure status.
2. Never fabricate API result.
3. Never fabricate payment status.
4. Never fabricate network diagnosis.
5. Never expose credentials.
6. Never execute unapproved high-risk action.
7. Never claim resolution before verification.
8. Never bypass policy.
9. Never loop indefinitely.
10. Never silently change production configuration.
11. Always log important actions.
12. Always preserve Case ID.
13. Always maintain customer-safe communication.
14. Always escalate when authority is exceeded.
15. Human decisions override AI recommendations.

---

# 54. ACCEPTANCE CRITERIA

System dianggap siap jika:

### Conversation
- Customer dapat chat natural.
- AI dapat identify customer.
- AI dapat collect required information.
- AI tidak bertanya data yang tersedia melalui API.
- AI tidak terlalu teknis.

### Reasoning
- Reasoning menerima structured case.
- Reasoning dapat query tools.
- Reasoning menghasilkan evidence.
- Reasoning memiliki confidence.
- Reasoning dapat menentukan action.
- Reasoning dapat menentukan escalation.

### Tools
- Billing integrated.
- RADIUS integrated.
- MikroTik integrated.
- GenieACS integrated.
- Tool calls logged.

### Safety
- Policy engine aktif.
- Risk engine aktif.
- High-risk action diblok.
- Approval tersedia.
- Verification tersedia.

### Human
- NOC Senior dapat menerima network case.
- Admin dapat menerima non-network case.
- Escalation memiliki complete context.
- Human dapat mengembalikan decision ke AI CS.

### Reliability
- Duplicate protection.
- Retry.
- Timeout.
- Idempotency.
- Queue.
- Audit.
- Error handling.

### Customer experience
- Bahasa natural.
- Sopan.
- Friendly.
- Tidak membocorkan technical internals.
- Tidak membuat janji palsu.
- Customer selalu mendapat status yang jelas.

---

# 55. TARGET OPERATING MODEL

Target akhir:

Customer
→ AI CS
→ AI NOC L1 / Service Reasoning
→ Auto Resolve jika aman
→ Verify
→ Customer

Jika tidak dapat:

Network:
→ NOC Senior

Non-network:
→ Admin

Human memberikan keputusan:
→ AI CS menyampaikan kepada customer.

Dengan demikian:

AI menangani volume.
AI melakukan diagnosis L1.
AI melakukan pekerjaan repetitif.
AI mengumpulkan evidence.
AI menyiapkan pekerjaan untuk manusia.

NOC Senior menangani networking kompleks dan berisiko.

Admin menangani non-networking, business, administrative, customer-sensitive decisions.

---

# 56. FINAL SYSTEM PHILOSOPHY

Jangan membangun "AI yang boleh melakukan semuanya".

Bangun:

**AI yang tahu apa yang boleh dilakukan, tahu apa yang tidak boleh dilakukan, tahu kapan harus mencari data, tahu kapan harus berhenti, dan tahu siapa manusia yang harus dilibatkan.**

Sistem harus mengoptimalkan:

- accuracy
- safety
- customer experience
- operational efficiency
- traceability
- human oversight

AI bukan pengganti manusia.

AI adalah frontline dan L1.

Human tetap menjadi authority untuk:
- high-risk networking
- complex technical decisions
- business decisions
- administrative decisions
- negotiation
- sensitive customer situations

---

# 57. INSTRUCTION UNTUK HERMES

Gunakan dokumen ini sebagai master specification.

Sebelum coding:

1. Inspect current system.
2. Identify existing capabilities.
3. Map existing APIs.
4. Map current WhatsApp workflow.
5. Map current Hermes capabilities.
6. Reuse existing components where appropriate.
7. Produce architecture gap analysis.
8. Implement incrementally.

Jangan langsung melakukan rewrite besar.

Untuk setiap phase:
- plan
- implement
- test
- verify
- document
- commit
- continue

Jika menemukan requirement yang belum jelas:
- gunakan existing architecture dan safest interpretation;
- jangan mengarang API;
- tandai assumption;
- buat implementation modular agar mudah diubah.

Semua production action harus:
- policy controlled
- auditable
- reversible bila memungkinkan
- verified

Prioritas:

1. Safety
2. Correctness
3. Customer experience
4. Reliability
5. Observability
6. Performance
7. Cost optimization

Target akhir adalah sistem AI Agent production-grade yang mampu menjadi:

**AI CUSTOMER SERVICE + AI NOC L1**

dengan:

**NOC SENIOR = NETWORKING HUMAN AUTHORITY**

dan

**ADMIN = NON-NETWORKING HUMAN AUTHORITY.**
