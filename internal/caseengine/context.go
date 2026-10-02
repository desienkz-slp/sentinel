package caseengine

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// CustomerContext adalah proyeksi terstruktur dari fakta pelanggan yang telah
// diverifikasi oleh sumber sistem. Isi percakapan pelanggan sengaja tidak
// disimpan di sini agar tidak bercampur dengan system truth.
type CustomerContext struct {
	CustomerID string
	Identity   string
	Name       string
	Source     string
	Services   []CustomerService
	Devices    []CustomerDevice
	OpenCaseID []ID
	UpdatedAt  time.Time
}

// CustomerService menyimpan identitas layanan yang dibutuhkan diagnosis.
type CustomerService struct {
	ServiceID string
	Username  string
	Package   string
	Status    string
	Router    string
	Radius    string
}

// CustomerDevice menyimpan perangkat yang terikat ke pelanggan.
type CustomerDevice struct {
	DeviceID string
	Type     string
	Serial   string
	Status   string
}

// Validate menolak konteks yang tidak dapat dikaitkan ke identitas pelanggan.
func (c CustomerContext) Validate() error {
	if strings.TrimSpace(c.CustomerID) == "" {
		return fmt.Errorf("customer context tanpa customer_id")
	}
	if strings.TrimSpace(c.Identity) == "" {
		return fmt.Errorf("customer context tanpa identity")
	}
	if strings.TrimSpace(c.Source) == "" {
		return fmt.Errorf("customer context tanpa source of truth")
	}
	return nil
}

// StaffRole adalah peran yang berwenang menerima handoff pada fondasi Phase 1.
type StaffRole string

const (
	StaffRoleNOCSenior StaffRole = "noc_senior"
	StaffRoleAdmin     StaffRole = "admin"
)

// StaffMember menyimpan metadata operasional, bukan hanya nomor WhatsApp.
type StaffMember struct {
	ID              string
	Name            string
	Role            StaffRole
	Department      string
	Phone           string
	WhatsAppJID     string
	Active          bool
	OnCall          bool
	WorkingHours    string
	Skills          []string
	EscalationLevel int
	Priority        int
	UpdatedAt       time.Time
}

// StaffDirectory adalah fallback in-memory deterministik sampai repository
// PostgreSQL/operator directory dihubungkan. Semua hasil dikembalikan sebagai salinan.
type StaffDirectory struct {
	mu      sync.RWMutex
	members map[string]StaffMember
}

func NewStaffDirectory() *StaffDirectory {
	return &StaffDirectory{members: make(map[string]StaffMember)}
}

// Upsert menambah atau memperbarui metadata staff berdasarkan ID yang stabil.
func (d *StaffDirectory) Upsert(member StaffMember) {
	if d == nil || strings.TrimSpace(member.ID) == "" {
		return
	}
	member.ID = strings.TrimSpace(member.ID)
	member.Name = strings.TrimSpace(member.Name)
	member.WhatsAppJID = strings.TrimSpace(member.WhatsAppJID)
	member.Skills = append([]string(nil), member.Skills...)
	if member.UpdatedAt.IsZero() {
		member.UpdatedAt = time.Now().UTC()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.members[member.ID] = member
}

// ActiveOnCall memilih staff yang aktif dan on-call dengan prioritas terkecil,
// lalu ID stabil sebagai pemutus seri. Aturan fallback lanjutan tetap Phase 4.
func (d *StaffDirectory) ActiveOnCall(role StaffRole) (StaffMember, bool) {
	if d == nil {
		return StaffMember{}, false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	candidates := make([]StaffMember, 0)
	for _, member := range d.members {
		if member.Role == role && member.Active && member.OnCall {
			member.Skills = append([]string(nil), member.Skills...)
			candidates = append(candidates, member)
		}
	}
	if len(candidates) == 0 {
		return StaffMember{}, false
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Priority != candidates[j].Priority {
			return candidates[i].Priority < candidates[j].Priority
		}
		return candidates[i].ID < candidates[j].ID
	})
	return candidates[0], true
}
