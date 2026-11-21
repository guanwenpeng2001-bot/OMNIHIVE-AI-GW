package audit

import (
	"context"
	"encoding/json"
	"time"
)

type Operation struct {
	Workspace, Project, Actor                    int64
	Action, Route, RequestID, ResourceID, Method string
	ActorEmail, ActorRole                        string
	Status                                       int
	At                                           time.Time
	Changes                                      json.RawMessage
	Reason                                       string
}

func (s *Store) Capture(ctx context.Context, operation Operation) error {
	if s == nil || s.db == nil || operation.Workspace <= 0 || operation.Project <= 0 || operation.Actor <= 0 {
		return nil
	}
	if len(operation.Changes) == 0 {
		operation.Changes = json.RawMessage("[]")
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO scoped_audit_events(workspace_id,project_id,actor_id,resource,resource_id,action,effect,at,request_id,route,method,actor_email,actor_role,status_code,changes,reason)
 VALUES($1,$2,$3,'project',NULLIF($4,''),$5,'committed',$6,NULLIF($7,''),$8,NULLIF($9,''),NULLIF($10,''),NULLIF($11,''),$12,$13,NULLIF($14,''))`, operation.Workspace, operation.Project, operation.Actor, operation.ResourceID, operation.Action, operation.At, operation.RequestID, operation.Route, operation.Method, operation.ActorEmail, operation.ActorRole, operation.Status, operation.Changes, operation.Reason)
	return err
}
