package main

import (
	"context"

	"github.com/beskosorg/beskos/backend/internal/db"
	"github.com/beskosorg/beskos/backend/internal/provisioning"
	"github.com/beskosorg/beskos/backend/internal/storage"
)

// startEditionWorkers is the extension point for edition-specific background jobs.
func startEditionWorkers(context.Context, *db.Queries, *storage.Service) {}

func configureEditionProvisioning(*db.Queries, *provisioning.Machine) {}
