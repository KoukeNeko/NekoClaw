package web

import "embed"

// DistFS embeds the built frontend assets from web/dist/.
// Build with: cd web && npm run build
//
//go:embed dist/*
var DistFS embed.FS
