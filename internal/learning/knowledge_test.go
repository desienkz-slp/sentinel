package learning

import (
	"testing"
	"time"
)

func TestCandidateTidakDapatDipakaiSebelumDitinjauManusia(t *testing.T) {
	store := NewKnowledgeStore()
	candidate, err := store.Propose(CandidateInput{
		Signature: SigDNS,
		Title:     "Periksa DNS resolver untuk keluhan situs tidak terbuka",
		Pattern: ResolutionPattern{
			Diagnosis:  "DNS tidak dapat melakukan resolusi",
			Steps:      []string{"uji DNS", "uji HTTP"},
			Resolution: "eskalasi ke NOC Senior bila resolver tidak sehat",
		},
		Source:     "hasil diagnosis tervalidasi",
		ProposerID: "system-learning",
	})
	if err != nil {
		t.Fatalf("Propose() error: %v", err)
	}
	if candidate.Status != StatusCandidate {
		t.Fatalf("status = %s, mau %s", candidate.Status, StatusCandidate)
	}
	if got := store.Production(SigDNS); len(got) != 0 {
		t.Fatalf("candidate tidak boleh masuk knowledge produksi: %+v", got)
	}

	if _, err := store.SubmitFeedback(FeedbackInput{KnowledgeID: candidate.ID, Outcome: FeedbackHelpful}); err != nil {
		t.Fatalf("SubmitFeedback() error: %v", err)
	}
	if got := store.Production(SigDNS); len(got) != 0 {
		t.Fatalf("feedback tidak boleh mempromosikan knowledge: %+v", got)
	}
}

func TestHanyaReviewerManusiaDapatMenyetujuiKnowledge(t *testing.T) {
	store := NewKnowledgeStoreWithAuthority(StaticReviewAuthority{"noc-senior-01": true})
	candidate := mustCandidate(t, store)

	if _, err := store.RequestReview(candidate.ID); err != nil {
		t.Fatalf("RequestReview() error: %v", err)
	}
	if _, err := store.Approve(candidate.ID, Reviewer{ID: "agent-learning"}, "terlihat baik", time.Now()); err == nil {
		t.Fatal("identitas yang tidak diotorisasi tidak boleh menyetujui knowledge")
	}
	if got := store.Production(SigDNS); len(got) != 0 {
		t.Fatalf("knowledge non-disetujui tidak boleh produksi: %+v", got)
	}

	approved, err := store.Approve(candidate.ID, Reviewer{ID: "noc-senior-01"}, "pola telah diverifikasi", time.Now())
	if err != nil {
		t.Fatalf("Approve() error: %v", err)
	}
	if approved.Status != StatusApproved || approved.Review == nil || approved.Review.ReviewerID != "noc-senior-01" {
		t.Fatalf("approval tidak menyimpan human review: %+v", approved)
	}
	if got := store.Production(SigDNS); len(got) != 1 || got[0].ID != candidate.ID {
		t.Fatalf("knowledge approved harus tersedia untuk produksi: %+v", got)
	}
}

func TestPengusulTidakDapatMenyetujuiCandidateMiliknyaSendiri(t *testing.T) {
	store := NewKnowledgeStoreWithAuthority(StaticReviewAuthority{"noc-senior-01": true, "noc-senior-02": true})
	candidate, err := store.Propose(CandidateInput{
		Signature:  SigDNS,
		Title:      "Pola DNS usulan NOC Senior",
		ProposerID: "noc-senior-01",
		Pattern: ResolutionPattern{
			Diagnosis:  "resolver DNS tidak sehat",
			Steps:      []string{"uji DNS"},
			Resolution: "eskalasi resolver",
		},
		Source: "case-002",
	})
	if err != nil {
		t.Fatalf("Propose() error: %v", err)
	}
	if _, err := store.RequestReview(candidate.ID); err != nil {
		t.Fatalf("RequestReview() error: %v", err)
	}
	if _, err := store.Approve(candidate.ID, Reviewer{ID: "noc-senior-01"}, "meninjau usulan sendiri", time.Now()); err == nil {
		t.Fatal("pengusul tidak boleh menyetujui candidate miliknya sendiri")
	}
	if _, err := store.Approve(candidate.ID, Reviewer{ID: "noc-senior-02"}, "diverifikasi reviewer kedua", time.Now()); err != nil {
		t.Fatalf("reviewer kedua harus dapat menyetujui: %v", err)
	}
}

func TestKoreksiManusiaMembuatCandidateBaruDanTidakMengubahKnowledgeDisetujui(t *testing.T) {
	store := NewKnowledgeStoreWithAuthority(StaticReviewAuthority{"noc-senior-01": true, "noc-senior-02": true})
	approved := mustApproved(t, store)

	correction, err := store.Correct(approved.ID, CorrectionInput{
		Author: Reviewer{ID: "noc-senior-02"},
		Pattern: ResolutionPattern{
			Diagnosis:  "DNS upstream gagal",
			Steps:      []string{"uji DNS upstream", "cek resolver cadangan"},
			Resolution: "alih-rutekan resolver melalui prosedur approval",
		},
		Reason: "langkah resolver cadangan lebih akurat",
	})
	if err != nil {
		t.Fatalf("Correct() error: %v", err)
	}
	if correction.Status != StatusCandidate || correction.SupersedesID != approved.ID {
		t.Fatalf("koreksi harus menjadi candidate baru: %+v", correction)
	}
	production := store.Production(SigDNS)
	if len(production) != 1 || production[0].ID != approved.ID {
		t.Fatalf("koreksi belum ditinjau tidak boleh mengganti produksi: %+v", production)
	}
}

func TestFeedbackDapatDibacaUntukLoopPerbaikanTanpaMengubahStatus(t *testing.T) {
	store := NewKnowledgeStoreWithAuthority(StaticReviewAuthority{"noc-senior-01": true})
	approved := mustApproved(t, store)

	feedback, err := store.SubmitFeedback(FeedbackInput{
		KnowledgeID: approved.ID,
		Outcome:     FeedbackNotHelpful,
		Comment:     "langkah kedua belum sesuai kondisi perangkat",
		AuthorID:    "case-123",
	})
	if err != nil {
		t.Fatalf("SubmitFeedback() error: %v", err)
	}
	if feedback.KnowledgeID != approved.ID || feedback.Outcome != FeedbackNotHelpful {
		t.Fatalf("feedback tidak tersimpan benar: %+v", feedback)
	}
	got, ok := store.Get(approved.ID)
	if !ok || got.Status != StatusApproved || got.FeedbackCount != 1 || got.NotHelpfulCount != 1 {
		t.Fatalf("feedback tidak boleh mengubah approval tetapi harus dihitung: %+v", got)
	}
}

func mustCandidate(t *testing.T, store *KnowledgeStore) Knowledge {
	t.Helper()
	knowledge, err := store.Propose(CandidateInput{
		Signature: SigDNS,
		Title:     "Pola resolusi DNS",
		Pattern: ResolutionPattern{
			Diagnosis:  "DNS bermasalah",
			Steps:      []string{"uji DNS"},
			Resolution: "eskalasi bila bukti belum cukup",
		},
		Source:     "case-001",
		ProposerID: "system-learning",
	})
	if err != nil {
		t.Fatalf("Propose() error: %v", err)
	}
	return knowledge
}

func mustApproved(t *testing.T, store *KnowledgeStore) Knowledge {
	t.Helper()
	candidate := mustCandidate(t, store)
	if _, err := store.RequestReview(candidate.ID); err != nil {
		t.Fatalf("RequestReview() error: %v", err)
	}
	approved, err := store.Approve(candidate.ID, Reviewer{ID: "noc-senior-01"}, "terverifikasi", time.Now())
	if err != nil {
		t.Fatalf("Approve() error: %v", err)
	}
	return approved
}
