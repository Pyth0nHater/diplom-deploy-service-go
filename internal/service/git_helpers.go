package service

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
)

func cloneRepository(workDir string, params DeployParams, result *DeployResult, emit EventFn) (*git.Repository, error) {
	cloneWriter := newEventWriter("clone", result, emit)
	defer cloneWriter.Flush()

	cloneOpts := &git.CloneOptions{
		URL:      normalizeRepoURL(params.RepoURL),
		Progress: cloneWriter,
	}
	if branch := strings.TrimSpace(params.Branch); branch != "" {
		cloneOpts.ReferenceName = plumbing.NewBranchReferenceName(branch)
		cloneOpts.SingleBranch = true
	}
	if token := strings.TrimSpace(params.AccessToken); token != "" {
		cloneOpts.Auth = authFromToken(params.RepoURL, token)
	}

	repo, err := git.PlainClone(workDir, false, cloneOpts)
	if err != nil {
		return nil, fmt.Errorf("git clone error: %w", err)
	}

	return repo, nil
}

func resolveBranchName(repo *git.Repository, requested string) (string, error) {
	if branch := strings.TrimSpace(requested); branch != "" {
		return branch, nil
	}

	head, err := repo.Head()
	if err != nil {
		return "", fmt.Errorf("resolve branch name: %w", err)
	}
	if !head.Name().IsBranch() {
		return "", fmt.Errorf("unsupported HEAD reference %s", head.Name().String())
	}

	return head.Name().Short(), nil
}

func normalizeRepoURL(repoURL string) string {
	repoURL = strings.TrimSpace(repoURL)
	repoURL = strings.TrimSuffix(repoURL, "/")
	if strings.HasPrefix(repoURL, "https://") {
		if strings.HasSuffix(repoURL, ".git") {
			return repoURL
		}
		return repoURL + ".git"
	}
	return repoURL
}

func authFromToken(repoURL, token string) *http.BasicAuth {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}

	return &http.BasicAuth{
		Username: authUsernameForRepo(repoURL),
		Password: token,
	}
}

func authUsernameForRepo(repoURL string) string {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return "git"
	}

	parsed, err := url.Parse(repoURL)
	if err != nil {
		return "git"
	}

	if parsed.User != nil {
		if username := strings.TrimSpace(parsed.User.Username()); username != "" {
			return username
		}
	}

	host := strings.ToLower(strings.TrimPrefix(parsed.Hostname(), "www."))
	switch {
	case strings.Contains(host, "github"):
		return "x-access-token"
	case strings.Contains(host, "gitlab"):
		return "oauth2"
	case strings.Contains(host, "bitbucket"):
		return "x-token-auth"
	default:
		return "git"
	}
}

var invalidNamePattern = regexp.MustCompile(`[^a-zA-Z0-9-]+`)

func sanitizeName(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "_", "-")
	value = strings.ReplaceAll(value, ".", "-")
	value = invalidNamePattern.ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if value == "" {
		return "deployment"
	}
	return strings.ToLower(value)
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
