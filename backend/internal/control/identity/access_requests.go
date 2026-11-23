package identity

import (
	"strconv"

	"github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/core/fault"
	domain "github.com/guanwenpeng2001-bot/OMNIHIVE-AI-GW/backend/internal/modules/identity"
)

func (h *Handler) requestAccess(r request) (any, error) {
	var b struct {
		RequestedProjectID *string `json:"requested_project_id"`
		Note               string  `json:"note"`
	}
	if err := r.body(&b); err != nil {
		return nil, err
	}
	in := domain.RequestAccess{Note: b.Note}
	if b.RequestedProjectID != nil {
		id, err := identifier(*b.RequestedProjectID)
		if err != nil {
			return nil, fault.Invalid("/requested_project_id")
		}
		in.RequestedProjectID = &id
	}
	out, err := h.store.RequestAccess(r.Request.Context(), r.actor, r.w, in, r.GetHeader("Idempotency-Key"))
	if err == nil {
		r.Header("ETag", strconv.Quote(strconv.FormatInt(out.Version, 10)))
	}
	return out, err
}

func (h *Handler) accessRequests(r request) (any, error) {
	view := r.DefaultQuery("view", "mine")
	if view != "mine" && view != "review" {
		return nil, fault.Invalid("/view")
	}
	kind := "requests:" + view
	page, err := h.page(r, kind)
	if err != nil {
		return nil, err
	}
	rows, err := h.store.AccessRequests(r.Request.Context(), r.actor, r.w, view, page)
	if err != nil {
		return nil, err
	}
	return pageResult(h, r, kind, page, rows, func(x domain.AccessRequest) int64 { return x.ID }), nil
}

func (h *Handler) decideAccess(r request) (any, error) {
	id, err := identifier(r.Param("id"))
	if err != nil {
		return nil, err
	}
	version, err := r.version()
	if err != nil {
		return nil, err
	}
	var b struct {
		Decision      string   `json:"decision"`
		Project       *string  `json:"project_id"`
		MemberVersion *string  `json:"member_version"`
		AddActions    []string `json:"add_actions"`
	}
	if err := r.body(&b); err != nil {
		return nil, err
	}
	in := domain.DecideAccess{Decision: b.Decision, AddActions: b.AddActions}
	if b.Project != nil {
		in.ProjectID, err = identifier(*b.Project)
		if err != nil {
			return nil, fault.Invalid("/project_id")
		}
	}
	if b.MemberVersion != nil {
		in.MemberVersion, err = identifier(*b.MemberVersion)
		if err != nil {
			return nil, fault.Invalid("/member_version")
		}
	}
	out, err := h.store.DecideAccess(r.Request.Context(), r.actor, r.w, id, version, in, r.Writer.Header().Get("X-Request-ID"))
	if err == nil {
		r.Header("ETag", strconv.Quote(strconv.FormatInt(out.Version, 10)))
	}
	return out, err
}
