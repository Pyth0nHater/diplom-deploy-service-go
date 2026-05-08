package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

const bootstrapWorkflowPath = ".github/workflows/deploy.yml"

func (s *BuilderService) BootstrapRepository(ctx context.Context, params DeployParams, emit EventFn) (*DeployResult, error) {
	baseRoute := resolveRoute(params, sanitizeName(params.ImageName))
	route := baseRoute
	result := &DeployResult{
		ImageName:     params.ImageName,
		ContainerName: ContainerName(params.ImageName),
		Domain:        route.AccessTarget,
	}

	workDir := filepath.Join("tmp", sanitizeName(params.ImageName)+"-bootstrap")
	_ = os.RemoveAll(workDir)
	defer os.RemoveAll(workDir)

	if err := emit(EventLevelInfo, "clone", "Cloning repository for bootstrap", result); err != nil {
		return nil, err
	}

	repo, err := cloneRepository(workDir, params, result, emit)
	if err != nil {
		return nil, err
	}

	branchName, err := resolveBranchName(repo, params.Branch)
	if err != nil {
		return nil, err
	}

	profile, profileMessage, err := resolveAppProfile(workDir, params.AppType)
	if err != nil {
		return nil, fmt.Errorf("resolve app profile: %w", err)
	}
	route = routeForProfile(baseRoute, profile)
	if err := emit(EventLevelInfo, "files", profileMessage, result); err != nil {
		return nil, err
	}
	nodeRuntime, nodeMessage, err := resolveNodeRuntime(workDir, params)
	if err != nil {
		return nil, fmt.Errorf("resolve node runtime: %w", err)
	}
	if err := emit(EventLevelInfo, "files", nodeMessage, result); err != nil {
		return nil, err
	}

	if err := emit(EventLevelInfo, "files", "Generating deployment files", result); err != nil {
		return nil, err
	}

	created, err := s.writeBootstrapFiles(workDir, branchName, params, profile, nodeRuntime, result, emit)
	if err != nil {
		return nil, err
	}

	wt, err := repo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("get git worktree: %w", err)
	}

	for _, relPath := range created {
		if _, err := wt.Add(relPath); err != nil {
			return nil, fmt.Errorf("git add %s: %w", relPath, err)
		}
	}

	status, err := wt.Status()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	if status.IsClean() {
		if err := emit(EventLevelSuccess, "done", "Repository already contains Docker/CD/Traefik files; nothing to commit", result); err != nil {
			return nil, err
		}
		return result, nil
	}

	if err := emit(EventLevelInfo, "commit", "Committing bootstrap changes", result); err != nil {
		return nil, err
	}

	// Save HEAD before commit so we can soft-reset on push failure.
	preCommitHead, headErr := repo.Head()

	commitOpts := &git.CommitOptions{
		Author: &object.Signature{
			Name:  "deploy-service",
			Email: "deploy-service@local",
			When:  time.Now(),
		},
	}
	if _, err := wt.Commit("chore: bootstrap docker cd and traefik deployment", commitOpts); err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	if err := emit(EventLevelInfo, "push", fmt.Sprintf("Pushing bootstrap changes to branch %s", branchName), result); err != nil {
		return nil, err
	}

	pushErr := repo.PushContext(ctx, &git.PushOptions{
		Auth: authFromToken(params.RepoURL, params.AccessToken),
	})
	if pushErr != nil && pushErr != git.NoErrAlreadyUpToDate {
		if !isWorkflowScopeError(pushErr) {
			return nil, fmt.Errorf("git push: %w", pushErr)
		}

		// Token lacks `workflow` scope — retry without the workflow file.
		if err := emit(EventLevelInfo, "push", "Token missing 'workflow' scope; retrying without .github/workflows/deploy.yml", result); err != nil {
			return nil, err
		}

		if headErr == nil {
			_ = wt.Reset(&git.ResetOptions{Commit: preCommitHead.Hash(), Mode: git.SoftReset})
		}

		workflowAbs := filepath.Join(workDir, filepath.FromSlash(bootstrapWorkflowPath))
		_ = os.Remove(workflowAbs)
		_, _ = wt.Remove(bootstrapWorkflowPath)

		status2, err := wt.Status()
		if err != nil {
			return nil, fmt.Errorf("git status after workflow removal: %w", err)
		}

		if !status2.IsClean() {
			if _, err := wt.Commit("chore: bootstrap docker cd and traefik deployment", commitOpts); err != nil {
				return nil, fmt.Errorf("git commit (no workflow): %w", err)
			}
			if err := repo.PushContext(ctx, &git.PushOptions{
				Auth: authFromToken(params.RepoURL, params.AccessToken),
			}); err != nil && err != git.NoErrAlreadyUpToDate {
				return nil, fmt.Errorf("git push: %w", err)
			}
		}

		if err := emit(EventLevelInfo, "push", "Add 'workflow' scope to your GitHub token and re-run bootstrap to push .github/workflows/deploy.yml", result); err != nil {
			return nil, err
		}
	}

	if err := emit(EventLevelInfo, "secrets", "Required GitHub secrets: SSH_HOST, SSH_USER, SSH_KEY, optional SSH_PORT, optional REPO_ACCESS_TOKEN", result); err != nil {
		return nil, err
	}
	if err := emit(EventLevelSuccess, "done", fmt.Sprintf("Bootstrap completed and pushed to %s", branchName), result); err != nil {
		return nil, err
	}

	return result, nil
}

func isWorkflowScopeError(err error) bool {
	return strings.Contains(err.Error(), "workflow")
}

func (s *BuilderService) writeBootstrapFiles(workDir, branchName string, params DeployParams, profile appProfile, nodeRuntime nodeRuntime, result *DeployResult, emit EventFn) ([]string, error) {
	baseRoute := resolveRoute(params, sanitizeName(params.ImageName))
	files := map[string]string{
		"Dockerfile":                bootstrapDockerfile(profile, nodeRuntime, baseRoute.StripPrefix),
		".dockerignore":             bootstrapDockerignore(),
		"docker-compose.deploy.yml": bootstrapCompose(params, profile),
		filepath.Join(".github", "workflows", "deploy.yml"): bootstrapWorkflow(branchName, params),
	}
	if profile.Name == appTypeNextJS {
		files[nextConfigHelperPath] = nextBasePathHelperScript()
	}

	created := make([]string, 0, len(files))
	for relPath, content := range files {
		absPath := filepath.Join(workDir, relPath)
		if _, err := os.Stat(absPath); err == nil {
			if err := emit(EventLevelInfo, "files", fmt.Sprintf("Skipping existing file %s", relPath), result); err != nil {
				return nil, err
			}
			continue
		} else if !os.IsNotExist(err) {
			return nil, fmt.Errorf("stat %s: %w", relPath, err)
		}

		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return nil, fmt.Errorf("mkdir %s: %w", filepath.Dir(relPath), err)
		}
		if err := os.WriteFile(absPath, []byte(content), 0644); err != nil {
			return nil, fmt.Errorf("write %s: %w", relPath, err)
		}
		if err := emit(EventLevelInfo, "files", fmt.Sprintf("Created %s", relPath), result); err != nil {
			return nil, err
		}
		created = append(created, relPath)
	}

	return created, nil
}

func bootstrapDockerfile(profile appProfile, nodeRuntime nodeRuntime, basePath string) string {
	return renderDockerfile(profile, nodeRuntime.Image, basePath)
}

func bootstrapDockerignore() string {
	return strings.TrimSpace(`
.git
.github
node_modules
dist
.DS_Store
npm-debug.log*
yarn-error.log*
.env
.env.*
.deploy-service/*.original.*
`) + "\n"
}

func bootstrapCompose(params DeployParams, profile appProfile) string {
	routerName := sanitizeName(params.ImageName)
	imageName := DockerImageName(params.ImageName)
	route := routeForProfile(resolveRoute(params, routerName), profile)
	labelLines := []string{
		"      traefik.enable: \"true\"",
		fmt.Sprintf("      traefik.docker.network: \"${TRAEFIK_NETWORK:-%s}\"", defaultTraefikNetwork),
		fmt.Sprintf("      traefik.http.routers.%s.rule: \"%s\"", routerName, route.Rule),
		fmt.Sprintf("      traefik.http.routers.%s.entrypoints: \"web\"", routerName),
		fmt.Sprintf("      traefik.http.services.%s.loadbalancer.server.port: \"%s\"", routerName, profile.InternalPort),
	}
	if route.UseStripPrefix {
		labelLines = append(labelLines,
			fmt.Sprintf("      traefik.http.routers.%s.middlewares: \"%s\"", routerName, route.MiddlewareName),
			fmt.Sprintf("      traefik.http.middlewares.%s.stripprefix.prefixes: \"%s\"", route.MiddlewareName, route.StripPrefix),
		)
	}

	return fmt.Sprintf(
		"services:\n"+
			"  app:\n"+
			"    build:\n"+
			"      context: .\n"+
			"    image: %s:latest\n"+
			"    container_name: %s\n"+
			"    restart: unless-stopped\n"+
			"    labels:\n%s\n"+
			"    networks:\n"+
			"      - web\n"+
			"\n"+
			"networks:\n"+
			"  web:\n"+
			"    external: true\n"+
			"    name: \"${TRAEFIK_NETWORK:-%s}\"\n",
		imageName,
		ContainerName(params.ImageName),
		strings.Join(labelLines, "\n"),
		defaultTraefikNetwork,
	)
}

func bootstrapWorkflow(branchName string, params DeployParams) string {
	repoURL := normalizeRepoURL(params.RepoURL)
	return fmt.Sprintf(
		"name: Remote Deploy\n"+
			"\n"+
			"on:\n"+
			"  push:\n"+
			"    branches:\n"+
			"      - %s\n"+
			"\n"+
			"jobs:\n"+
			"  deploy:\n"+
			"    runs-on: ubuntu-latest\n"+
			"    steps:\n"+
			"      - name: Deploying to Server\n"+
			"        uses: appleboy/ssh-action@v1.0.3\n"+
			"        with:\n"+
			"          host: ${{ secrets.SSH_HOST }}\n"+
			"          username: ${{ secrets.SSH_USER }}\n"+
			"          key: ${{ secrets.SSH_KEY }}\n"+
			"          port: ${{ secrets.SSH_PORT }}\n"+
			"          script: |\n"+
			"            APP_DIR=\"/srv/apps/%s\"\n"+
			"            REPO_URL=\"%s\"\n"+
			"            REPO_BRANCH=\"%s\"\n"+
			"            AUTH_PREFIX=\"\"\n"+
			"\n"+
			"            if [ -n \"${{ secrets.REPO_ACCESS_TOKEN }}\" ]; then\n"+
			"              AUTH_PREFIX=\"x-access-token:${{ secrets.REPO_ACCESS_TOKEN }}@\"\n"+
			"            fi\n"+
			"\n"+
			"            AUTHED_REPO_URL=\"${REPO_URL/https:\\/\\//https://${AUTH_PREFIX}}\"\n"+
			"\n"+
			"            mkdir -p /srv/apps\n"+
			"\n"+
			"            if [ ! -d \"$APP_DIR/.git\" ]; then\n"+
			"              rm -rf \"$APP_DIR\"\n"+
			"              git clone --branch \"$REPO_BRANCH\" \"$AUTHED_REPO_URL\" \"$APP_DIR\"\n"+
			"            fi\n"+
			"\n"+
			"            cd \"$APP_DIR\" || exit 1\n"+
			"            git remote set-url origin \"$AUTHED_REPO_URL\"\n"+
			"            git fetch origin \"$REPO_BRANCH\"\n"+
			"            git reset --hard \"origin/$REPO_BRANCH\"\n"+
			"\n"+
			"            cat > .deploy.env <<'EOF'\n"+
			"            TRAEFIK_NETWORK=%s\n"+
			"            EOF\n"+
			"\n"+
			"            docker network create %s >/dev/null 2>&1 || true\n"+
			"            docker compose --env-file .deploy.env -f docker-compose.deploy.yml up -d --build --force-recreate --remove-orphans\n"+
			"            docker image prune -f\n",
		branchName,
		sanitizeName(params.ImageName),
		repoURL,
		branchName,
		defaultTraefikNetwork,
		defaultTraefikNetwork,
	)
}
