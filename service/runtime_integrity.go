package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/model"
)

// CollectRuntimeIntegrityReport freezes Report v1: flat, string versions "1",
// separate config/approval/product digests, and explicit PASS only after all
// observations succeed. The callback is the existing user-facing projection,
// not a second implementation of product approval or compatibility rules.
func CollectRuntimeIntegrityReport(ctx context.Context, productView func() ([]byte, error)) (model.RuntimeIntegrity, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	before, err := model.ObserveRuntimeIntegrity(ctx)
	if err != nil {
		return model.RuntimeIntegrity{}, err
	}
	view, err := productView()
	if err != nil {
		return model.RuntimeIntegrity{}, model.ErrProductViewInvalid
	}
	after, err := model.ObserveRuntimeIntegrity(ctx)
	if err != nil {
		return model.RuntimeIntegrity{}, err
	}
	if before.Report.InstanceID != after.Report.InstanceID || before.Report.ContainerID != after.Report.ContainerID || before.Report.StartedAt != after.Report.StartedAt || before.Report.BuildIdentity != after.Report.BuildIdentity {
		return model.RuntimeIntegrity{}, model.ErrProbeUnavailable
	}
	if before.Report.ConfigGeneration != after.Report.ConfigGeneration || before.Report.ConfigDigest != after.Report.ConfigDigest {
		return model.RuntimeIntegrity{}, model.ErrRuntimeConfigMismatch
	}
	if before.Report.ApprovalViewDigest != after.Report.ApprovalViewDigest {
		return model.RuntimeIntegrity{}, model.ErrApprovalViewInvalid
	}
	if before.Report.CatalogVersion != after.Report.CatalogVersion || before.ViewStateDigest != after.ViewStateDigest {
		return model.RuntimeIntegrity{}, model.ErrProductViewInvalid
	}
	if ctx.Err() != nil {
		return model.RuntimeIntegrity{}, model.ErrProbeUnavailable
	}
	report := after.Report
	report.ProductViewDigest = fmt.Sprintf("sha256:%x", sha256.Sum256(view))
	report.GeneratedAt = time.Now().UTC()
	report.Status = "PASS"
	return report, nil
}
