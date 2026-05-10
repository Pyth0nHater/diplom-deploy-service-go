package handler

import (
	"context"
	"deploy-service/internal/service"
	"strings"

	"deploy-service/pkg/deploypb"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type DeployHandler struct {
	deploypb.UnimplementedDeployServiceServer
	builder *service.BuilderService
}

func NewDeployHandler(b *service.BuilderService) *DeployHandler {
	return &DeployHandler{builder: b}
}

func (h *DeployHandler) Deploy(req *deploypb.DeployRequest, stream deploypb.DeployService_DeployServer) error {
	if strings.TrimSpace(req.GetRepoUrl()) == "" {
		return status.Error(codes.InvalidArgument, "repo_url is required")
	}
	if strings.TrimSpace(req.GetImageName()) == "" {
		return status.Error(codes.InvalidArgument, "image_name is required")
	}

	params := service.DeployParams{
		RepoURL:     req.GetRepoUrl(),
		AccessToken: req.GetAccessToken(),
		ImageName:   req.GetImageName(),
		Domain:      req.GetDomain(),
		Branch:      req.GetBranch(),
		AppType:     req.GetAppType(),
		NodeVersion: req.GetNodeVersion(),
	}

	emit := func(level service.EventLevel, stage, message string, result *service.DeployResult) error {
		event := &deploypb.DeployEvent{
			Level:         mapLevel(level),
			Stage:         stage,
			Message:       message,
			ImageName:     params.ImageName,
			ContainerName: result.ContainerName,
			Domain:        result.Domain,
		}

		if event.ContainerName == "" {
			event.ContainerName = service.ContainerName(params.ImageName)
		}
		if event.Domain == "" {
			event.Domain = service.AccessTarget(params)
		}

		return stream.Send(event)
	}

	if _, err := h.builder.Deploy(stream.Context(), params, emit); err != nil {
		_ = emit(service.EventLevelError, "deploy", err.Error(), &service.DeployResult{
			ContainerName: service.ContainerName(params.ImageName),
			Domain:        service.AccessTarget(params),
		})
		return status.Errorf(codes.Internal, "deploy failed: %v", err)
	}

	return nil
}

func (h *DeployHandler) BootstrapRepository(req *deploypb.BootstrapRepositoryRequest, stream deploypb.DeployService_BootstrapRepositoryServer) error {
	if strings.TrimSpace(req.GetRepoUrl()) == "" {
		return status.Error(codes.InvalidArgument, "repo_url is required")
	}
	if strings.TrimSpace(req.GetImageName()) == "" {
		return status.Error(codes.InvalidArgument, "image_name is required")
	}

	params := service.DeployParams{
		RepoURL:     req.GetRepoUrl(),
		AccessToken: req.GetAccessToken(),
		ImageName:   req.GetImageName(),
		Domain:      req.GetDomain(),
		Branch:      req.GetBranch(),
		AppType:     req.GetAppType(),
		NodeVersion: req.GetNodeVersion(),
	}

	emit := func(level service.EventLevel, stage, message string, result *service.DeployResult) error {
		event := &deploypb.DeployEvent{
			Level:         mapLevel(level),
			Stage:         stage,
			Message:       message,
			ImageName:     params.ImageName,
			ContainerName: result.ContainerName,
			Domain:        result.Domain,
		}

		if event.ContainerName == "" {
			event.ContainerName = service.ContainerName(params.ImageName)
		}
		if event.Domain == "" {
			event.Domain = service.AccessTarget(params)
		}

		return stream.Send(event)
	}

	if _, err := h.builder.BootstrapRepository(stream.Context(), params, emit); err != nil {
		_ = emit(service.EventLevelError, "bootstrap", err.Error(), &service.DeployResult{
			ContainerName: service.ContainerName(params.ImageName),
			Domain:        service.AccessTarget(params),
		})
		return status.Errorf(codes.Internal, "bootstrap failed: %v", err)
	}

	return nil
}

func (h *DeployHandler) Undeploy(ctx context.Context, req *deploypb.UndeployRequest) (*deploypb.UndeployResponse, error) {
	if strings.TrimSpace(req.GetImageName()) == "" {
		return nil, status.Error(codes.InvalidArgument, "image_name is required")
	}
	if err := h.builder.Undeploy(ctx, req.GetImageName()); err != nil {
		return &deploypb.UndeployResponse{Success: false, Message: err.Error()}, nil
	}
	return &deploypb.UndeployResponse{Success: true, Message: "container stopped and removed"}, nil
}

func mapLevel(level service.EventLevel) deploypb.DeployEvent_Level {
	switch level {
	case service.EventLevelSuccess:
		return deploypb.DeployEvent_LEVEL_SUCCESS
	case service.EventLevelError:
		return deploypb.DeployEvent_LEVEL_ERROR
	default:
		return deploypb.DeployEvent_LEVEL_INFO
	}
}
