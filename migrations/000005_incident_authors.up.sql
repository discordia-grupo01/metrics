-- NULL en los incidentes creados o resueltos antes de guardar quién lo hizo.
ALTER TABLE incidents
    ADD COLUMN created_by UUID,
    ADD COLUMN resolved_by UUID;
