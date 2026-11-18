package dto

import (
	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/service"
	"time"
)

type ProxyGroup struct {
	ID                     int64     `json:"id"`
	Name                   string    `json:"name"`
	Description            string    `json:"description"`
	Region                 string    `json:"region"`
	TargetServices         []string  `json:"target_services"`
	Status                 string    `json:"status"`
	FallbackMode           string    `json:"fallback_mode"`
	FallbackProxyID        *int64    `json:"fallback_proxy_id"`
	BindingTTLSeconds      int       `json:"binding_ttl_seconds"`
	MemberCount            int       `json:"member_count"`
	SchedulableMemberCount int       `json:"schedulable_member_count"`
	BoundAccountCount      int64     `json:"bound_account_count"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

type ProxyGroupMember struct {
	Proxy          *Proxy     `json:"proxy"`
	CooldownUntil  *time.Time `json:"cooldown_until,omitempty"`
	CooldownReason string     `json:"cooldown_reason,omitempty"`
}

type ProxyGroupDetail struct {
	ProxyGroup
	Members []ProxyGroupMember `json:"members"`
}

func ProxyGroupFromService(view *service.ProxyGroupAdminView) ProxyGroup {
	g := view.Group
	return ProxyGroup{ID: g.ID, Name: g.Name, Description: g.Description, Region: g.Region,
		TargetServices: append([]string{}, g.TargetServices...), Status: g.Status, FallbackMode: g.FallbackMode,
		FallbackProxyID: g.FallbackProxyID, BindingTTLSeconds: g.BindingTTLSeconds,
		MemberCount: len(view.Members), SchedulableMemberCount: view.SchedulableMemberCount,
		BoundAccountCount: view.BoundAccountCount, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt}
}

func ProxyGroupDetailFromService(view *service.ProxyGroupAdminView) ProxyGroupDetail {
	out := ProxyGroupDetail{ProxyGroup: ProxyGroupFromService(view), Members: make([]ProxyGroupMember, 0, len(view.Members))}
	for _, member := range view.Members {
		item := ProxyGroupMember{Proxy: ProxyFromService(member.Proxy)}
		if member.Cooldown != nil {
			item.CooldownUntil = &member.Cooldown.ExpiresAt
			// Reasons can originate in transport errors containing credential-bearing URLs.
			item.CooldownReason = "proxy_transport_failure"
		}
		out.Members = append(out.Members, item)
	}
	return out
}
