package learning

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// KnowledgeStatus adalah status lifecycle knowledge. Hanya APPROVED yang boleh
// dipakai dalam konteks produksi.
type KnowledgeStatus string

const (
	StatusCandidate KnowledgeStatus = "CANDIDATE"
	StatusReview    KnowledgeStatus = "IN_REVIEW"
	StatusApproved  KnowledgeStatus = "APPROVED"
	StatusRejected  KnowledgeStatus = "REJECTED"
	StatusRetired   KnowledgeStatus = "RETIRED"
)

// ResolutionPattern berisi pola diagnosis dan resolusi yang dapat ditinjau.
// Field ini bersifat panduan; tidak pernah menjadi izin untuk action eksternal.
type ResolutionPattern struct {
	Diagnosis  string   `json:"diagnosis"`
	Steps      []string `json:"steps"`
	Resolution string   `json:"resolution"`
}

// Review menyimpan keputusan eksplisit dari reviewer manusia.
type Review struct {
	ReviewerID string    `json:"reviewer_id"`
	Reason     string    `json:"reason"`
	ReviewedAt time.Time `json:"reviewed_at"`
}

// Knowledge adalah satu entri basis pengetahuan tervalidasi atau kandidatnya.
type Knowledge struct {
	ID              string            `json:"id"`
	Signature       Signature         `json:"signature"`
	Title           string            `json:"title"`
	Pattern         ResolutionPattern `json:"pattern"`
	Source          string            `json:"source"`
	ProposerID      string            `json:"proposer_id,omitempty"`
	Status          KnowledgeStatus   `json:"status"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	Review          *Review           `json:"review,omitempty"`
	SupersedesID    string            `json:"supersedes_id,omitempty"`
	FeedbackCount   int               `json:"feedback_count"`
	HelpfulCount    int               `json:"helpful_count"`
	NotHelpfulCount int               `json:"not_helpful_count"`
}

// CandidateInput adalah data calon knowledge dari diagnosis atau koreksi.
type CandidateInput struct {
	Signature  Signature
	Title      string
	Pattern    ResolutionPattern
	Source     string
	ProposerID string
}

// Reviewer menyatakan identitas peninjau. Otorisasi human tidak berasal dari
// input pemanggil, tetapi dari ReviewAuthority yang dipercaya oleh store.
type Reviewer struct {
	ID string
}

// ReviewAuthority menghubungkan knowledge lifecycle ke sumber identitas/RBAC
// tepercaya. Implementasi production harus mengambil keputusan dari sesi operator
// terautentikasi, bukan dari nilai request yang dapat dipalsukan pemanggil.
type ReviewAuthority interface {
	CanReviewKnowledge(reviewerID string) bool
}

// StaticReviewAuthority berguna untuk konfigurasi eksplisit dan test. Daftar
// identitas ini dibuat saat store diinisialisasi, tidak disuplai pada Approve.
type StaticReviewAuthority map[string]bool

func (a StaticReviewAuthority) CanReviewKnowledge(reviewerID string) bool {
	return a[strings.TrimSpace(reviewerID)]
}

type denyReviewAuthority struct{}

func (denyReviewAuthority) CanReviewKnowledge(string) bool { return false }

// CorrectionInput membuat versi candidate baru dari knowledge yang sudah ada.
type CorrectionInput struct {
	Author  Reviewer
	Pattern ResolutionPattern
	Reason  string
}

// FeedbackOutcome menyatakan kegunaan knowledge pada kasus nyata.
type FeedbackOutcome string

const (
	FeedbackHelpful    FeedbackOutcome = "HELPFUL"
	FeedbackNotHelpful FeedbackOutcome = "NOT_HELPFUL"
)

// FeedbackInput menyimpan umpan balik tanpa mengubah lifecycle knowledge.
type FeedbackInput struct {
	KnowledgeID string
	Outcome     FeedbackOutcome
	Comment     string
	AuthorID    string
}

// Feedback merupakan record feedback loop. Feedback hanya bahan review, bukan
// mekanisme promosi otomatis.
type Feedback struct {
	KnowledgeID string          `json:"knowledge_id"`
	Outcome     FeedbackOutcome `json:"outcome"`
	Comment     string          `json:"comment,omitempty"`
	AuthorID    string          `json:"author_id,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

// KnowledgeStore adalah penyimpanan in-memory untuk fase awal. Persistence
// PostgreSQL dapat ditambahkan tanpa mengubah aturan lifecycle ini.
type KnowledgeStore struct {
	mu        sync.RWMutex
	nextID    uint64
	entries   map[string]Knowledge
	feedback  []Feedback
	now       func() time.Time
	authority ReviewAuthority
}

func NewKnowledgeStore() *KnowledgeStore {
	return NewKnowledgeStoreWithAuthority(denyReviewAuthority{})
}

// NewKnowledgeStoreWithAuthority membuat store dengan sumber otorisasi review
// tepercaya. Tanpa authority eksplisit, approval ditolak secara deny-by-default.
func NewKnowledgeStoreWithAuthority(authority ReviewAuthority) *KnowledgeStore {
	if authority == nil {
		authority = denyReviewAuthority{}
	}
	return &KnowledgeStore{
		entries:   map[string]Knowledge{},
		now:       time.Now,
		authority: authority,
	}
}

// Propose hanya membuat kandidat. Tidak ada jalur otomatis dari kandidat ke
// knowledge produksi.
func (s *KnowledgeStore) Propose(input CandidateInput) (Knowledge, error) {
	if err := validateCandidate(input); err != nil {
		return Knowledge{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	now := s.now().UTC()
	knowledge := Knowledge{
		ID:         fmt.Sprintf("KB-%06d", s.nextID),
		Signature:  normalizeSignature(input.Signature),
		Title:      strings.TrimSpace(input.Title),
		Pattern:    clonePattern(input.Pattern),
		Source:     strings.TrimSpace(input.Source),
		ProposerID: strings.TrimSpace(input.ProposerID),
		Status:     StatusCandidate,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	s.entries[knowledge.ID] = knowledge
	return cloneKnowledge(knowledge), nil
}

// RequestReview memindahkan kandidat ke antrean review manusia.
func (s *KnowledgeStore) RequestReview(id string) (Knowledge, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	knowledge, ok := s.entries[id]
	if !ok {
		return Knowledge{}, fmt.Errorf("knowledge %q tidak ditemukan", id)
	}
	if knowledge.Status != StatusCandidate {
		return Knowledge{}, fmt.Errorf("knowledge %q berstatus %s, bukan CANDIDATE", id, knowledge.Status)
	}
	knowledge.Status = StatusReview
	knowledge.UpdatedAt = s.now().UTC()
	s.entries[id] = knowledge
	return cloneKnowledge(knowledge), nil
}

// Approve memerlukan reviewer manusia dan candidate yang sudah berada di review.
func (s *KnowledgeStore) Approve(id string, reviewer Reviewer, reason string, reviewedAt time.Time) (Knowledge, error) {
	if err := s.validateReviewer(reviewer); err != nil {
		return Knowledge{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return Knowledge{}, errors.New("alasan approval wajib diisi")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	knowledge, ok := s.entries[id]
	if !ok {
		return Knowledge{}, fmt.Errorf("knowledge %q tidak ditemukan", id)
	}
	if knowledge.Status != StatusReview {
		return Knowledge{}, fmt.Errorf("knowledge %q berstatus %s, bukan IN_REVIEW", id, knowledge.Status)
	}
	if knowledge.ProposerID != "" && knowledge.ProposerID == strings.TrimSpace(reviewer.ID) {
		return Knowledge{}, errors.New("pengusul knowledge tidak boleh menyetujui candidate miliknya sendiri")
	}
	if reviewedAt.IsZero() {
		reviewedAt = s.now()
	}
	knowledge.Status = StatusApproved
	knowledge.Review = &Review{ReviewerID: strings.TrimSpace(reviewer.ID), Reason: strings.TrimSpace(reason), ReviewedAt: reviewedAt.UTC()}
	knowledge.UpdatedAt = s.now().UTC()
	s.entries[id] = knowledge
	return cloneKnowledge(knowledge), nil
}

// Reject hanya dapat dilakukan reviewer manusia dan menyimpan alasan review.
func (s *KnowledgeStore) Reject(id string, reviewer Reviewer, reason string, reviewedAt time.Time) (Knowledge, error) {
	if err := s.validateReviewer(reviewer); err != nil {
		return Knowledge{}, err
	}
	if strings.TrimSpace(reason) == "" {
		return Knowledge{}, errors.New("alasan penolakan wajib diisi")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	knowledge, ok := s.entries[id]
	if !ok {
		return Knowledge{}, fmt.Errorf("knowledge %q tidak ditemukan", id)
	}
	if knowledge.Status != StatusReview {
		return Knowledge{}, fmt.Errorf("knowledge %q berstatus %s, bukan IN_REVIEW", id, knowledge.Status)
	}
	if reviewedAt.IsZero() {
		reviewedAt = s.now()
	}
	knowledge.Status = StatusRejected
	knowledge.Review = &Review{ReviewerID: strings.TrimSpace(reviewer.ID), Reason: strings.TrimSpace(reason), ReviewedAt: reviewedAt.UTC()}
	knowledge.UpdatedAt = s.now().UTC()
	s.entries[id] = knowledge
	return cloneKnowledge(knowledge), nil
}

// Correct tidak memodifikasi knowledge approved. Koreksi manusia selalu menjadi
// candidate baru agar perubahan juga melewati review gate.
func (s *KnowledgeStore) Correct(id string, input CorrectionInput) (Knowledge, error) {
	if err := s.validateReviewer(input.Author); err != nil {
		return Knowledge{}, err
	}
	if strings.TrimSpace(input.Reason) == "" {
		return Knowledge{}, errors.New("alasan koreksi wajib diisi")
	}
	s.mu.RLock()
	original, ok := s.entries[id]
	s.mu.RUnlock()
	if !ok {
		return Knowledge{}, fmt.Errorf("knowledge %q tidak ditemukan", id)
	}
	candidate, err := s.Propose(CandidateInput{
		Signature:  original.Signature,
		Title:      original.Title,
		Pattern:    input.Pattern,
		Source:     "koreksi manusia " + strings.TrimSpace(input.Author.ID) + ": " + strings.TrimSpace(input.Reason),
		ProposerID: strings.TrimSpace(input.Author.ID),
	})
	if err != nil {
		return Knowledge{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	candidate = s.entries[candidate.ID]
	candidate.SupersedesID = original.ID
	candidate.UpdatedAt = s.now().UTC()
	s.entries[candidate.ID] = candidate
	return cloneKnowledge(candidate), nil
}

// SubmitFeedback mencatat feedback loop tanpa mengubah status atau mempromosikan
// knowledge. Kegunaan dan koreksi tetap diputuskan lewat review manusia.
func (s *KnowledgeStore) SubmitFeedback(input FeedbackInput) (Feedback, error) {
	if input.Outcome != FeedbackHelpful && input.Outcome != FeedbackNotHelpful {
		return Feedback{}, fmt.Errorf("outcome feedback %q tidak valid", input.Outcome)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	knowledge, ok := s.entries[input.KnowledgeID]
	if !ok {
		return Feedback{}, fmt.Errorf("knowledge %q tidak ditemukan", input.KnowledgeID)
	}
	feedback := Feedback{KnowledgeID: knowledge.ID, Outcome: input.Outcome, Comment: strings.TrimSpace(input.Comment), AuthorID: strings.TrimSpace(input.AuthorID), CreatedAt: s.now().UTC()}
	knowledge.FeedbackCount++
	if input.Outcome == FeedbackHelpful {
		knowledge.HelpfulCount++
	} else {
		knowledge.NotHelpfulCount++
	}
	knowledge.UpdatedAt = s.now().UTC()
	s.entries[knowledge.ID] = knowledge
	s.feedback = append(s.feedback, feedback)
	return feedback, nil
}

// Production hanya mengembalikan knowledge yang telah di-approve manusia.
func (s *KnowledgeStore) Production(signature Signature) []Knowledge {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Knowledge, 0)
	for _, knowledge := range s.entries {
		if knowledge.Status == StatusApproved && knowledge.Signature == normalizeSignature(signature) {
			out = append(out, cloneKnowledge(knowledge))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Get mengambil snapshot satu knowledge.
func (s *KnowledgeStore) Get(id string) (Knowledge, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	knowledge, ok := s.entries[id]
	return cloneKnowledge(knowledge), ok
}

func validateCandidate(input CandidateInput) error {
	if strings.TrimSpace(input.Title) == "" {
		return errors.New("judul knowledge wajib diisi")
	}
	if strings.TrimSpace(input.Pattern.Diagnosis) == "" || strings.TrimSpace(input.Pattern.Resolution) == "" || len(input.Pattern.Steps) == 0 {
		return errors.New("diagnosis, langkah, dan resolusi wajib diisi")
	}
	for _, step := range input.Pattern.Steps {
		if strings.TrimSpace(step) == "" {
			return errors.New("langkah resolusi tidak boleh kosong")
		}
	}
	if strings.TrimSpace(input.Source) == "" {
		return errors.New("sumber knowledge wajib diisi")
	}
	if strings.TrimSpace(input.ProposerID) == "" {
		return errors.New("identitas pengusul knowledge wajib diisi")
	}
	return nil
}

func (s *KnowledgeStore) validateReviewer(reviewer Reviewer) error {
	if strings.TrimSpace(reviewer.ID) == "" {
		return errors.New("identitas reviewer manusia wajib diisi")
	}
	if s.authority == nil || !s.authority.CanReviewKnowledge(reviewer.ID) {
		return errors.New("approval knowledge hanya dapat dilakukan reviewer manusia terotorisasi")
	}
	return nil
}

func normalizeSignature(signature Signature) Signature {
	if signature == "" {
		return SigUmum
	}
	return signature
}

func clonePattern(pattern ResolutionPattern) ResolutionPattern {
	pattern.Diagnosis = strings.TrimSpace(pattern.Diagnosis)
	pattern.Resolution = strings.TrimSpace(pattern.Resolution)
	pattern.Steps = append([]string(nil), pattern.Steps...)
	for i := range pattern.Steps {
		pattern.Steps[i] = strings.TrimSpace(pattern.Steps[i])
	}
	return pattern
}

func cloneKnowledge(knowledge Knowledge) Knowledge {
	knowledge.Pattern = clonePattern(knowledge.Pattern)
	if knowledge.Review != nil {
		review := *knowledge.Review
		knowledge.Review = &review
	}
	return knowledge
}
