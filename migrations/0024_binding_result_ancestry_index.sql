-- Resolve result ancestry by result and organization without scanning retained execution history (ADR-0066).
-- This additive lookup index changes neither grants nor runtime table privileges and permits ambiguity detection.
create index if not exists query_executions_result_org_idx
    on public.query_executions (result_id, organization_id);
