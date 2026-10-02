package caseengine

import (
	"testing"
	"time"
)

func TestCustomerContextValidatesIdentityAndKeepsSystemTruthSeparate(t *testing.T) {
	context := CustomerContext{
		CustomerID: "customer-001",
		Identity:   "628111222333",
		Source:     "billing",
		Services: []CustomerService{{
			ServiceID: "service-001",
			Status:    "ACTIVE",
		}},
		UpdatedAt: time.Now().UTC(),
	}

	if err := context.Validate(); err != nil {
		t.Fatalf("konteks pelanggan valid ditolak: %v", err)
	}
	if context.Source != "billing" {
		t.Fatalf("source of truth berubah: %q", context.Source)
	}

	context.Identity = ""
	if err := context.Validate(); err == nil {
		t.Fatal("konteks tanpa identity harus ditolak")
	}
}

func TestStaffDirectoryReturnsOnlyActiveOnCallRole(t *testing.T) {
	directory := NewStaffDirectory()
	directory.Upsert(StaffMember{ID: "noc-off", Name: "NOC Nonaktif", Role: StaffRoleNOCSenior, Active: false, OnCall: true})
	directory.Upsert(StaffMember{ID: "noc-on", Name: "NOC Siaga", Role: StaffRoleNOCSenior, Active: true, OnCall: true, WhatsAppJID: "628111222333@s.whatsapp.net"})
	directory.Upsert(StaffMember{ID: "admin-on", Name: "Admin Siaga", Role: StaffRoleAdmin, Active: true, OnCall: true})

	staff, ok := directory.ActiveOnCall(StaffRoleNOCSenior)
	if !ok {
		t.Fatal("NOC Senior aktif dan on-call harus ditemukan")
	}
	if staff.ID != "noc-on" {
		t.Fatalf("staff terpilih = %q, ingin noc-on", staff.ID)
	}
	if _, ok := directory.ActiveOnCall(StaffRoleAdmin); !ok {
		t.Fatal("Admin aktif dan on-call harus ditemukan")
	}
}
