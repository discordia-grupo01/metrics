ALTER TABLE incidents
    ADD COLUMN platform_status TEXT NOT NULL DEFAULT 'degraded'
    CHECK (platform_status IN ('operational', 'degraded', 'maintenance'));
