package infrastructure

import "embed"

// ScopeUpgrade is deployment-only. Pure tenant assemblies never install it.
// The existing tenant-only SQL source remains the schema for that profile;
// scoped-queries.sql and the neutral owner catalog describe scope-aware access.
//
//go:embed migrations/*.sql
var ScopeUpgrade embed.FS
