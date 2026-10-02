// Package caseengine menyediakan lifecycle deterministik untuk setiap keluhan pelanggan.
// Package ini tidak mengetahui HTTP, database, LLM, maupun WhatsApp sehingga aturan
// state dan keselamatan dapat diuji serta ditegakkan konsisten pada setiap adapter.
package caseengine

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"time"
)

// ID mengikuti kontrak master specification: CASE-YYYYMMDD-XXXXXX.
type ID string

// State adalah status authoritative sebuah case.
type State string

const (
	StateNew                  State = "NEW"
	StateIdentifying          State = "IDENTIFYING"
	StateConversation         State = "CONVERSATION"
	StateInformationGathering State = "INFORMATION_GATHERING"
	StateWaitingCustomer      State = "WAITING_CUSTOMER"
	StateReadyForDiagnosis    State = "READY_FOR_DIAGNOSIS"
	StateReasoning            State = "REASONING"
	StateInvestigation        State = "INVESTIGATION"
	StateActionProposed       State = "ACTION_PROPOSED"
	StatePolicyCheck          State = "POLICY_CHECK"
	StateExecuting            State = "EXECUTING"
	StateVerifying            State = "VERIFYING"
	StateFailed               State = "FAILED"
	StateEscalation           State = "ESCALATION"
	StateHumanHandling        State = "HUMAN_HANDLING"
	StateResolved             State = "RESOLVED"
)

// Verification adalah bukti hasil verifikasi yang tersimpan di dalam case.
type Verification struct {
	Passed  bool
	Source  string
	Summary string
	At      time.Time
}

// Event adalah riwayat immutable transition domain. Repositori akan
// mempersist-nya sebagai case_event dalam transaksi yang sama dengan case.
type Event struct {
	From   State
	To     State
	Actor  string
	Reason string
	At     time.Time
}

// Case adalah aggregate lifecycle. State, version, event, dan verification
// disimpan privat agar invariant tidak dapat dibypass oleh package pemanggil.
type Case struct {
	ID        ID
	Channel   string
	Identity  string
	CreatedAt time.Time
	UpdatedAt time.Time

	state         State
	version       int64
	verifications []Verification
	events        []Event
}

func New(channel, identity string, now time.Time) *Case {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return &Case{
		ID:        newID(now),
		Channel:   strings.TrimSpace(channel),
		Identity:  strings.TrimSpace(identity),
		state:     StateNew,
		version:   1,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// RecordVerification menyimpan fakta hanya ketika case sedang VERIFYING.
// Hanya bukti lulus yang membuka transisi ke RESOLVED; klaim sukses API semata
// tidak cukup.
func (c *Case) RecordVerification(v Verification) error {
	if c == nil || c.state != StateVerifying {
		return fmt.Errorf("verification hanya boleh dicatat saat case VERIFYING")
	}
	if v.At.IsZero() {
		v.At = time.Now().UTC()
	}
	c.verifications = append(c.verifications, v)
	c.UpdatedAt = v.At
	return nil
}

func (c *Case) hasPassedVerification() bool {
	for _, v := range c.verifications {
		if v.Passed {
			return true
		}
	}
	return false
}

// Transition menerapkan graph lifecycle dan invariant resolution master spec.
func (c *Case) Transition(next State, actor, reason string) error {
	if c == nil {
		return fmt.Errorf("case tidak boleh nil")
	}
	if !allowed(c.state, next) {
		return fmt.Errorf("transisi case ilegal: %s -> %s", c.state, next)
	}
	if next == StateResolved && !c.hasPassedVerification() {
		return fmt.Errorf("case tidak boleh RESOLVED sebelum verification berhasil")
	}
	now := time.Now().UTC()
	c.events = append(c.events, Event{From: c.state, To: next, Actor: strings.TrimSpace(actor), Reason: strings.TrimSpace(reason), At: now})
	c.state = next
	c.version++
	c.UpdatedAt = now
	return nil
}

// State mengembalikan status authoritative tanpa membuka mutasi langsung.
func (c *Case) State() State {
	if c == nil {
		return ""
	}
	return c.state
}

// Version mengembalikan nomor optimistic-locking saat ini.
func (c *Case) Version() int64 {
	if c == nil {
		return 0
	}
	return c.version
}

// Events mengembalikan salinan event lifecycle agar audit tidak dapat diubah caller.
func (c *Case) Events() []Event {
	if c == nil {
		return nil
	}
	return append([]Event(nil), c.events...)
}

func allowed(from, to State) bool {
	if from == to {
		return false
	}
	switch from {
	case StateNew:
		return to == StateIdentifying
	case StateIdentifying:
		return to == StateConversation || to == StateInformationGathering || to == StateEscalation
	case StateConversation:
		return to == StateInformationGathering || to == StateWaitingCustomer || to == StateReadyForDiagnosis || to == StateEscalation
	case StateInformationGathering:
		return to == StateWaitingCustomer || to == StateReadyForDiagnosis || to == StateEscalation
	case StateWaitingCustomer:
		return to == StateConversation || to == StateEscalation
	case StateReadyForDiagnosis:
		return to == StateReasoning || to == StateEscalation
	case StateReasoning:
		return to == StateInvestigation || to == StateActionProposed || to == StateEscalation || to == StateFailed
	case StateInvestigation:
		return to == StateActionProposed || to == StateVerifying || to == StateEscalation || to == StateFailed
	case StateActionProposed:
		return to == StatePolicyCheck || to == StateEscalation
	case StatePolicyCheck:
		return to == StateExecuting || to == StateEscalation || to == StateFailed
	case StateExecuting:
		return to == StateVerifying || to == StateFailed || to == StateEscalation
	case StateVerifying:
		return to == StateResolved || to == StateFailed || to == StateEscalation
	case StateFailed:
		return to == StateEscalation || to == StateHumanHandling
	case StateEscalation:
		return to == StateHumanHandling
	case StateHumanHandling:
		return to == StateVerifying || to == StateResolved || to == StateFailed
	}
	return false
}

func newID(now time.Time) ID {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// Format enam karakter membatasi entropy suffix. Unique constraint pada
		// repository adalah pengaman final dan repository wajib retry saat collision.
		return ID(fmt.Sprintf("CASE-%s-%06X", now.UTC().Format("20060102"), now.UnixNano()&0xFFFFFF))
	}
	suffix := strings.TrimRight(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw[:]), "=")
	return ID(fmt.Sprintf("CASE-%s-%s", now.UTC().Format("20060102"), suffix[:6]))
}
