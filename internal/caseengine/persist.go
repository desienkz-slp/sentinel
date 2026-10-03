package caseengine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Persistensi tracker ke JSON. Case menyimpan state privat agar invarian tidak
// bisa dilewati, jadi simpan/pulihkan lewat bentuk data khusus di bawah.
// Pemulihan TIDAK menjalankan Transition: state dipercaya apa adanya dari
// berkas yang kita tulis sendiri, tetapi nilai yang tak dikenal ditolak.

type caseData struct {
	ID            ID             `json:"id"`
	Channel       string         `json:"channel"`
	Identity      string         `json:"identity"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	State         State          `json:"state"`
	Version       int64          `json:"version"`
	Verifications []Verification `json:"verifications,omitempty"`
	Events        []Event        `json:"events,omitempty"`
}

type trackerFile struct {
	Version int        `json:"version"`
	Active  []caseData `json:"active"`
	Archive []caseData `json:"archive"`
}

var validStates = map[State]bool{
	StateNew: true, StateIdentifying: true, StateConversation: true, StateInformationGathering: true,
	StateWaitingCustomer: true, StateReadyForDiagnosis: true, StateReasoning: true, StateInvestigation: true,
	StateActionProposed: true, StatePolicyCheck: true, StateExecuting: true, StateVerifying: true,
	StateFailed: true, StateEscalation: true, StateHumanHandling: true, StateResolved: true,
}

func toData(c *Case) caseData {
	return caseData{ID: c.ID, Channel: c.Channel, Identity: c.Identity, CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt, State: c.state, Version: c.version,
		Verifications: append([]Verification(nil), c.verifications...),
		Events:        append([]Event(nil), c.events...)}
}

func fromData(d caseData) (*Case, error) {
	if !validStates[d.State] {
		return nil, fmt.Errorf("state tidak dikenal %q pada %s", d.State, d.ID)
	}
	if d.State == StateResolved {
		ok := false
		for _, v := range d.Verifications {
			if v.Passed {
				ok = true
			}
		}
		if !ok {
			return nil, fmt.Errorf("case %s RESOLVED tanpa verifikasi lulus", d.ID)
		}
	}
	return &Case{ID: d.ID, Channel: d.Channel, Identity: d.Identity, CreatedAt: d.CreatedAt,
		UpdatedAt: d.UpdatedAt, state: d.State, version: d.Version,
		verifications: d.Verifications, events: d.Events}, nil
}

// SetPath mengaktifkan penyimpanan otomatis. Path kosong = hanya memori.
func (t *Tracker) SetPath(path string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.path = path
	t.mu.Unlock()
}

// Save menulis seluruh tracker secara atomik (tulis temp lalu rename).
// Tanpa path = no-op. Aman dipanggil berulang dan dari banyak goroutine.
func (t *Tracker) Save() error {
	if t == nil {
		return nil
	}
	t.mu.RLock()
	path := t.path
	f := trackerFile{Version: 1}
	for _, c := range t.cases {
		f.Active = append(f.Active, toData(c))
	}
	for _, c := range t.archive {
		f.Archive = append(f.Archive, toData(c))
	}
	t.mu.RUnlock()
	if path == "" {
		return nil
	}
	sort.Slice(f.Active, func(i, j int) bool { return f.Active[i].ID < f.Active[j].ID })
	sort.Slice(f.Archive, func(i, j int) bool { return f.Archive[i].ID < f.Archive[j].ID })
	b, err := json.MarshalIndent(f, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	t.saveMu.Lock()
	defer t.saveMu.Unlock()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load memulihkan tracker dari berkas. Berkas tak ada = tracker kosong (bukan
// error). Berkas rusak = error dan tracker TIDAK diubah; berkas lama dicadangkan
// agar tidak tertimpa Save berikutnya.
func (t *Tracker) Load(path string) error {
	if t == nil || path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var f trackerFile
	if err := json.Unmarshal(b, &f); err != nil {
		_ = os.Rename(path, path+".rusak-"+time.Now().UTC().Format("20060102150405"))
		return fmt.Errorf("berkas case rusak (dicadangkan): %w", err)
	}
	cases := map[string]*Case{}
	archive := map[string]*Case{}
	for _, d := range f.Active {
		c, err := fromData(d)
		if err != nil {
			return err
		}
		cases[normalizeIdentity(c.Identity)] = c
	}
	for _, d := range f.Archive {
		c, err := fromData(d)
		if err != nil {
			return err
		}
		archive[string(c.ID)] = c
	}
	t.mu.Lock()
	t.cases, t.archive, t.path = cases, archive, path
	t.mu.Unlock()
	return nil
}

// Record = bentuk data satu case untuk penyimpanan lain (mis. PostgreSQL).
type Record = caseData

// Record mengembalikan salinan data case (tidak membuka mutasi).
func (c *Case) Record() Record {
	if c == nil {
		return Record{}
	}
	return toData(c)
}
