// Package escalation contains deterministic human-authority routing. It only
// prepares handoff facts; delivery/acknowledgement uses the durable outbox layer.
package escalation

import (
	"sort"
	"strings"
)

type Role string

const (
	RoleNOCSenior Role = "noc_senior"
	RoleAdmin     Role = "admin"
)

type Payload struct {
	CaseID     string            `json:"case_id"`
	Domain     string            `json:"domain"`
	TargetRole Role              `json:"target_role"`
	Evidence   map[string]string `json:"evidence"`
	Missing    []string          `json:"missing,omitempty"`
}

func Target(domain string) Role {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "network", "networking", "internet", "radius", "mikrotik", "genieacs":
		return RoleNOCSenior
	default:
		return RoleAdmin
	}
}

func BuildPayload(caseID, domain string, evidence map[string]string, missing []string) Payload {
	out := make(map[string]string, len(evidence)+len(missing))
	for k, v := range evidence {
		out[k] = v
	}
	for _, key := range missing {
		if strings.TrimSpace(key) != "" {
			if _, exists := out[key]; !exists {
				out[key] = "UNAVAILABLE"
			}
		}
	}
	return Payload{CaseID: strings.TrimSpace(caseID), Domain: strings.TrimSpace(domain), TargetRole: Target(domain), Evidence: out, Missing: append([]string(nil), missing...)}
}

// Context adalah payload handoff yang dipahami manusia tanpa mengulang diagnosis.
// Nilai yang belum tersedia harus kosong atau UNAVAILABLE, tidak boleh direka.
type Context struct {
	CaseID              string   `json:"case_id"`
	Domain              string   `json:"domain"`
	Customer            string   `json:"customer"`
	Complaint           string   `json:"complaint"`
	ConversationSummary string   `json:"conversation_summary"`
	Timeline            string   `json:"timeline"`
	BillingStatus       string   `json:"billing_status"`
	RadiusStatus        string   `json:"radius_status"`
	RouterStatus        string   `json:"router_status"`
	GenieACSStatus      string   `json:"genieacs_status"`
	Diagnostics         []string `json:"diagnostics"`
	Evidence            []string `json:"evidence"`
	Hypothesis          string   `json:"hypothesis"`
	Confidence          string   `json:"confidence"`
	ActionsPerformed    []string `json:"actions_performed"`
	ActionsNotPerformed []string `json:"actions_not_performed"`
	Recommendation      string   `json:"recommendation"`
	Urgency             string   `json:"urgency"`
	AffectedScope       string   `json:"affected_scope"`
	Reason              string   `json:"reason"`
	HumanNeed           string   `json:"human_need"`
}

// MissingRequired menjelaskan konteks yang harus dilengkapi, tanpa mengubah
// ketiadaan data menjadi fakta. Daftar ini dapat diteruskan bersama handoff.
func (c Context) MissingRequired() []string {
	checks := []struct {
		name  string
		value string
	}{
		{"case_id", c.CaseID}, {"customer", c.Customer}, {"complaint", c.Complaint},
		{"conversation_summary", c.ConversationSummary}, {"timeline", c.Timeline},
		{"hypothesis", c.Hypothesis}, {"confidence", c.Confidence}, {"recommendation", c.Recommendation},
		{"urgency", c.Urgency}, {"affected_scope", c.AffectedScope}, {"reason", c.Reason}, {"human_need", c.HumanNeed},
	}
	if Target(c.Domain) == RoleNOCSenior {
		checks = append(checks,
			struct{ name, value string }{"billing_status", c.BillingStatus},
			struct{ name, value string }{"radius_status", c.RadiusStatus},
			struct{ name, value string }{"router_status", c.RouterStatus},
			struct{ name, value string }{"genieacs_status", c.GenieACSStatus},
		)
	}
	var missing []string
	for _, check := range checks {
		if strings.TrimSpace(check.value) == "" {
			missing = append(missing, check.name)
		}
	}
	if len(c.Diagnostics) == 0 {
		missing = append(missing, "diagnostics")
	}
	if len(c.Evidence) == 0 {
		missing = append(missing, "evidence")
	}
	if len(c.ActionsPerformed) == 0 {
		missing = append(missing, "actions_performed")
	}
	if len(c.ActionsNotPerformed) == 0 {
		missing = append(missing, "actions_not_performed")
	}
	return missing
}

// Staff menyimpan availability dan prioritas yang diperlukan untuk routing.
// Integrasi persistence/delivery eksternal tetap berada di luar package ini.
type Staff struct {
	ID       string `json:"id"`
	Role     Role   `json:"role"`
	Active   bool   `json:"active"`
	OnCall   bool   `json:"on_call"`
	Priority int    `json:"priority"`
}

// Directory adalah direktori staff in-memory deterministik untuk handoff.
type Directory struct{ staff []Staff }

func NewDirectory(staff ...Staff) *Directory {
	copyStaff := append([]Staff(nil), staff...)
	return &Directory{staff: copyStaff}
}

func (d *Directory) available(role Role, attempted map[string]struct{}) []Staff {
	if d == nil {
		return nil
	}
	candidates := make([]Staff, 0, len(d.staff))
	for _, staff := range d.staff {
		if staff.Role != role || !staff.Active || !staff.OnCall || strings.TrimSpace(staff.ID) == "" {
			continue
		}
		if _, alreadyTried := attempted[staff.ID]; alreadyTried {
			continue
		}
		candidates = append(candidates, staff)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		return candidates[i].ID < candidates[j].ID
	})
	return candidates
}

type Status string

const (
	StatusPending      Status = "PENDING"
	StatusAcknowledged Status = "ACKNOWLEDGED"
	StatusExhausted    Status = "EXHAUSTED"
)

type Attempt struct {
	StaffID      string `json:"staff_id"`
	AssignedUnix int64  `json:"assigned_unix"`
	DeadlineUnix int64  `json:"deadline_unix"`
}

type Request struct {
	Payload    Context   `json:"payload"`
	TargetRole Role      `json:"target_role"`
	Status     Status    `json:"status"`
	Attempts   []Attempt `json:"attempts"`
}

type RequestInput struct {
	Payload Context
	NowUnix int64
}

// Policy dapat dipetakan ke aturan operator. Satuan timeout adalah detik agar
// mudah diuji dan tidak mengikat package pada scheduler/delivery tertentu.
type Policy struct{ ResponseTimeout int64 }

type Engine struct {
	directory *Directory
	policy    Policy
}

func NewEngine(directory *Directory, policy Policy) *Engine {
	if policy.ResponseTimeout <= 0 {
		policy.ResponseTimeout = 300
	}
	return &Engine{directory: directory, policy: policy}
}

type Result struct {
	Request       Request `json:"request"`
	AssignedStaff Staff   `json:"assigned_staff,omitempty"`
}

// Start memilih staff aktif/on-call sesuai authority domain. TargetRole selalu
// dihitung ulang dari Domain agar pemanggil tidak dapat mengalihkan network ke Admin.
func (e *Engine) Start(input RequestInput) Result {
	request := Request{Payload: input.Payload, TargetRole: Target(input.Payload.Domain), Status: StatusPending}
	return e.assign(request, input.NowUnix)
}

// AdvanceOnTimeout hanya memilih backup setelah deadline dan hanya dalam role
// authority yang sama. Permintaan acknowledged tidak pernah difallback.
func (e *Engine) AdvanceOnTimeout(request Request, nowUnix int64) Result {
	request.TargetRole = Target(request.Payload.Domain)
	if request.Status != StatusPending || len(request.Attempts) == 0 {
		return e.result(request)
	}
	current := request.Attempts[len(request.Attempts)-1]
	if nowUnix < current.DeadlineUnix {
		return e.result(request)
	}
	return e.assign(request, nowUnix)
}

// Acknowledge menerima pengakuan hanya dari staff yang menjadi assignment aktif.
func (e *Engine) Acknowledge(request Request, staffID string, _ int64) Result {
	if request.Status != StatusPending || len(request.Attempts) == 0 {
		return e.result(request)
	}
	current := request.Attempts[len(request.Attempts)-1]
	if strings.TrimSpace(staffID) == current.StaffID {
		request.Status = StatusAcknowledged
	}
	return e.result(request)
}

func (e *Engine) assign(request Request, nowUnix int64) Result {
	attempted := make(map[string]struct{}, len(request.Attempts))
	for _, attempt := range request.Attempts {
		attempted[attempt.StaffID] = struct{}{}
	}
	candidates := e.directory.available(request.TargetRole, attempted)
	if len(candidates) == 0 {
		request.Status = StatusExhausted
		return Result{Request: request}
	}
	chosen := candidates[0]
	request.Attempts = append(request.Attempts, Attempt{StaffID: chosen.ID, AssignedUnix: nowUnix, DeadlineUnix: nowUnix + e.policy.ResponseTimeout})
	request.Status = StatusPending
	return Result{Request: request, AssignedStaff: chosen}
}

func (e *Engine) result(request Request) Result {
	if len(request.Attempts) == 0 || e == nil || e.directory == nil {
		return Result{Request: request}
	}
	currentID := request.Attempts[len(request.Attempts)-1].StaffID
	for _, staff := range e.directory.staff {
		if staff.ID == currentID {
			return Result{Request: request, AssignedStaff: staff}
		}
	}
	return Result{Request: request}
}
