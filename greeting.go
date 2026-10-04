package main

import (
	"strings"

	"ainoc/internal/config"
	"ainoc/internal/directory"
)

// customerReply adds an operator-configured greeting only to a first customer
// reply. It is pure string formatting: templates never execute code or tools.
func customerReply(cfg *config.Config, caller directory.Caller, lanjutan bool, reply string) string {
	if cfg == nil || caller.IsStaff || !cfg.GreetingFirstMessage || lanjutan || strings.TrimSpace(reply) == "" {
		return reply
	}
	template := strings.TrimSpace(cfg.GreetingTemplate)
	if template == "" {
		return reply
	}
	name := strings.TrimSpace(caller.Name)
	if name == "" {
		name = "Pelanggan"
	}
	org := strings.TrimSpace(cfg.Org)
	if org == "" {
		org = "NetLayer"
	}
	role := strings.TrimSpace(string(caller.Role))
	if role == "" {
		role = "customer"
	}
	// strings.NewReplacer makes one deterministic pass, so placeholder-looking
	// caller data remains literal and cannot cause recursive expansion.
	rendered := strings.NewReplacer(
		"{{name}}", name,
		"{{org}}", org,
		"{{role}}", role,
	).Replace(template)
	return rendered + "\n\n" + reply
}
