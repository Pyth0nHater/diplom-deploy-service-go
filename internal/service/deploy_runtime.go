package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/archive"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
)

func (s *BuilderService) Deploy(ctx context.Context, params DeployParams, emit EventFn) (*DeployResult, error) {
	imageName := DockerImageName(params.ImageName)
	baseRoute := resolveRoute(params, sanitizeName(params.ImageName))
	route := baseRoute
	result := &DeployResult{
		ImageName:     params.ImageName,
		ContainerName: ContainerName(params.ImageName),
		Domain:        route.AccessTarget,
	}

	workDir := filepath.Join("tmp", sanitizeName(params.ImageName))
	_ = os.RemoveAll(workDir)
	defer os.RemoveAll(workDir)

	if err := emit(EventLevelInfo, "clone", "Cloning repository", result); err != nil {
		return nil, err
	}

	cloneWriter := newEventWriter("clone", result, emit)
	defer cloneWriter.Flush()

	cloneOpts := &git.CloneOptions{
		URL:      params.RepoURL,
		Progress: cloneWriter,
	}
	if branch := strings.TrimSpace(params.Branch); branch != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		cloneOpts.SingleBranch = true
	}
	if token := strings.TrimSpace(params.AccessToken); token != "" {
		cloneOpts.Auth = authFromToken(params.RepoURL, token)
	}

	if _, err := git.PlainClone(workDir, false, cloneOpts); err != nil {
		return nil, fmt.Errorf("git clone error: %w", err)
	}

	profile, profileMessage, err := resolveAppProfile(workDir, params.AppType)
	if err != nil {
		return nil, fmt.Errorf("resolve app profile: %w", err)
	}
	route = routeForProfile(baseRoute, profile)
	if err := emit(EventLevelInfo, "dockerfile", profileMessage, result); err != nil {
		return nil, err
	}
	nodeRuntime, nodeMessage, err := resolveNodeRuntime(workDir, params)
	if err != nil {
		return nil, fmt.Errorf("resolve node runtime: %w", err)
	}
	if err := emit(EventLevelInfo, "dockerfile", nodeMessage, result); err != nil {
		return nil, err
	}

	if err := emit(EventLevelInfo, "dockerfile", "Generating Dockerfile", result); err != nil {
		return nil, err
	}

	if err := os.WriteFile(workDir+"/Dockerfile", []byte(renderDockerfile(profile, nodeRuntime.Image, baseRoute.StripPrefix)), 0644); err != nil {
		return nil, err
	}
	if profile.Name == appTypeNextJS {
		helperPath := filepath.Join(workDir, nextConfigHelperPath)
		if err := os.MkdirAll(filepath.Dir(helperPath), 0755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(helperPath), err)
		}
		if err := os.WriteFile(helperPath, []byte(nextBasePathHelperScript()), 0644); err != nil {
			return nil, fmt.Errorf("write %s: %w", helperPath, err)
		}
	}

	if err := emit(EventLevelInfo, "build", "Building Docker image", result); err != nil {
		return nil, err
	}

	tar, err := archive.TarWithOptions(workDir, &archive.TarOptions{})
	if err != nil {
		return nil, fmt.Errorf("archive build context: %w", err)
	}
	defer tar.Close()

	buildResp, err := s.dockerCli.ImageBuild(ctx, tar, types.ImageBuildOptions{
		Tags:   []string{imageName},
		Remove: true,
	})
	if err != nil {
		return nil, err
	}
	defer buildResp.Body.Close()

	buildWriter := newEventWriter("build", result, emit)
	defer buildWriter.Flush()
	if err := jsonmessage.DisplayJSONMessagesStream(buildResp.Body, buildWriter, 0, false, nil); err != nil {
		if summary := buildWriter.Summary(); summary != "" {
			return nil, fmt.Errorf("read docker build logs: %w; last build logs: %s%s", err, summary, buildFailureHint(summary))
		}
		return nil, fmt.Errorf("read docker build logs: %w", err)
	}

	if err := emit(EventLevelInfo, "run", "Starting container with Traefik labels", result); err != nil {
		return nil, err
	}

	_ = s.dockerCli.ContainerRemove(ctx, result.ContainerName, container.RemoveOptions{Force: true})
	routerName := sanitizeName(params.ImageName)
	networkName := envOrDefault("DEPLOY_NETWORK", defaultTraefikNetwork)
	labels := map[string]string{
		"traefik.enable":         "true",
		"traefik.docker.network": networkName,
		fmt.Sprintf("traefik.http.routers.%s.rule", routerName):                      route.Rule,
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", routerName): profile.InternalPort,
	}
	if route.UseStripPrefix {
		labels[fmt.Sprintf("traefik.http.routers.%s.middlewares", routerName)] = route.MiddlewareName
		labels[fmt.Sprintf("traefik.http.middlewares.%s.stripprefix.prefixes", route.MiddlewareName)] = route.StripPrefix
	}

	cc, err := s.dockerCli.ContainerCreate(ctx, &container.Config{
		Image:  imageName,
		Labels: labels,
	}, &container.HostConfig{
		NetworkMode:   container.NetworkMode(networkName),
		RestartPolicy: container.RestartPolicy{Name: "always"},
	}, nil, nil, result.ContainerName)
	if err != nil {
		return nil, err
	}

	if err := s.dockerCli.ContainerStart(ctx, cc.ID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("start container: %w%s", err, s.containerLogSuffix(ctx, cc.ID))
	}

	if err := s.verifyStartedContainer(ctx, cc.ID); err != nil {
		return nil, fmt.Errorf("%w%s", err, s.containerLogSuffix(ctx, cc.ID))
	}

	if err := emit(EventLevelSuccess, "done", fmt.Sprintf("Deployment finished for %s", AccessURL(params)), result); err != nil {
		return nil, err
	}

	return result, nil
}

func (s *BuilderService) verifyStartedContainer(ctx context.Context, containerID string) error {
	time.Sleep(1200 * time.Millisecond)

	inspection, err := s.dockerCli.ContainerInspect(ctx, containerID)
	if err != nil {
		return fmt.Errorf("inspect container: %w", err)
	}
	if inspection.ContainerJSONBase == nil || inspection.ContainerJSONBase.State == nil {
		return nil
	}

	state := inspection.ContainerJSONBase.State
	if state.Running {
		return nil
	}

	if state.Error != "" {
		return fmt.Errorf("container exited after start: %s", state.Error)
	}

	return fmt.Errorf("container exited after start with code %d", state.ExitCode)
}

func (s *BuilderService) containerLogSuffix(ctx context.Context, containerID string) string {
	logs, err := s.tailContainerLogs(ctx, containerID, "20")
	if err != nil || logs == "" {
		return ""
	}
	return "; container logs: " + logs
}

func (s *BuilderService) tailContainerLogs(ctx context.Context, containerID, tail string) (string, error) {
	reader, err := s.dockerCli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
	})
	if err != nil {
		return "", err
	}
	defer reader.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, reader); err != nil {
		raw, readErr := io.ReadAll(reader)
		if readErr != nil {
			return "", err
		}
		return sanitizeLogLine(string(raw)), nil
	}

	lines := make([]string, 0, 8)
	for _, chunk := range []string{stdout.String(), stderr.String()} {
		for _, line := range strings.Split(chunk, "\n") {
			line = sanitizeLogLine(line)
			if line == "" {
				continue
			}
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return "", nil
	}
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	return strings.Join(lines, " | "), nil
}

func buildFailureHint(summary string) string {
	summary = strings.ToLower(summary)
	if strings.Contains(summary, "node.js version") || strings.Contains(summary, "for next.js, node.js version") {
		return "; hint: set node_version in the request, or add .nvmrc/.node-version/package.json engines.node, or configure DEPLOY_DEFAULT_NODE_VERSION on the deploy service"
	}
	return ""
}
