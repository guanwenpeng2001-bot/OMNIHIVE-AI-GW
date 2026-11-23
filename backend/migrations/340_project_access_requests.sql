CREATE TABLE project_access_requests (
    id bigserial PRIMARY KEY,
    workspace_id bigint NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    membership_id bigint NOT NULL,
    requested_project_id bigint,
    note text NOT NULL CHECK (char_length(note) BETWEEN 1 AND 500 AND note = btrim(note)),
    state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','approved','declined')),
    version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
    created_at timestamptz NOT NULL DEFAULT now(),
    request_key text NOT NULL CHECK (char_length(request_key) BETWEEN 1 AND 200),
    fingerprint bytea NOT NULL,
    approved_project_id bigint,
    decision_actor_id bigint REFERENCES users(id),
    decided_at timestamptz,
    decision_fingerprint bytea,
    FOREIGN KEY (workspace_id,membership_id) REFERENCES workspace_members(workspace_id,id) ON DELETE CASCADE,
    FOREIGN KEY (workspace_id,requested_project_id) REFERENCES projects(workspace_id,id),
    FOREIGN KEY (workspace_id,approved_project_id) REFERENCES projects(workspace_id,id),
    UNIQUE(membership_id,request_key),
    CHECK ((state = 'approved') = (approved_project_id IS NOT NULL)),
    CHECK ((state = 'pending') = (decision_actor_id IS NULL)),
    CHECK ((state = 'pending') = (decided_at IS NULL)),
    CHECK ((state = 'pending') = (decision_fingerprint IS NULL))
);
CREATE INDEX project_access_requests_review ON project_access_requests(workspace_id,state,id);
CREATE INDEX project_access_requests_workspace_page ON project_access_requests(workspace_id,id);
CREATE INDEX project_access_requests_mine ON project_access_requests(membership_id,id);
