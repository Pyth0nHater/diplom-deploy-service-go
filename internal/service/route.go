package service

import (
	"fmt"
	"net/url"
	"strings"
)

const defaultDeployBaseHost = "localhost"

type routeConfig struct {
	AccessTarget   string
	Rule           string
	MiddlewareName string
	StripPrefix    string
	UseStripPrefix bool
}

func resolveRoute(params DeployParams, routerName string) routeConfig {
	return defaultLocalRoute(params, routerName)
}

func defaultLocalRoute(params DeployParams, routerName string) routeConfig {
	owner, repo := repositoryRouteParts(params.RepoURL)
	if owner == "" {
		owner = "name"
	}
	if repo == "" {
		repo = sanitizeName(params.ImageName)
	}

	return localPathRoute(baseHost(), "/"+owner+"/"+repo, routerName)
}

func localPathRoute(host, pathPrefix, routerName string) routeConfig {
	pathPrefix = normalizePathPrefix(pathPrefix)
	return routeConfig{
		AccessTarget:   strings.TrimSuffix(host, "/") + pathPrefix,
		Rule:           localPathRule(host, pathPrefix),
		MiddlewareName: routerName + "-strip",
		StripPrefix:    pathPrefix,
		UseStripPrefix: true,
	}
}

func localPathRule(host, pathPrefix string) string {
	if pathPrefix == "/" {
		return fmt.Sprintf("Host(`%s`) && PathPrefix(`/`)", host)
	}

	return fmt.Sprintf("Host(`%s`) && (Path(`%s`) || PathPrefix(`%s/`))", host, pathPrefix, pathPrefix)
}

func baseHost() string {
	host := strings.TrimSpace(envOrDefault("DEPLOY_BASE_DOMAIN", defaultDeployBaseHost))
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimPrefix(host, "https://")
	host = strings.Trim(host, "/")
	if host == "" {
		return defaultDeployBaseHost
	}
	return host
}

func normalizePathPrefix(path string) string {
	path = "/" + strings.Trim(strings.TrimSpace(path), "/")
	if path == "/" {
		return path
	}
	return path
}

func repositoryRouteParts(repoURL string) (owner, repo string) {
	segments := repositoryPathSegments(repoURL)
	if len(segments) >= 2 {
		return sanitizeName(segments[len(segments)-2]), sanitizeName(segments[len(segments)-1])
	}
	if len(segments) == 1 {
		return "", sanitizeName(segments[0])
	}
	return "", ""
}

func repositoryPathSegments(repoURL string) []string {
	repoURL = strings.TrimSpace(repoURL)
	if repoURL == "" {
		return nil
	}

	if parsed, err := url.Parse(repoURL); err == nil && parsed.Host != "" {
		return cleanRepoSegments(strings.Split(strings.Trim(parsed.Path, "/"), "/"))
	}

	if idx := strings.Index(repoURL, ":"); idx >= 0 {
		return cleanRepoSegments(strings.Split(strings.Trim(repoURL[idx+1:], "/"), "/"))
	}

	return cleanRepoSegments(strings.Split(strings.Trim(repoURL, "/"), "/"))
}

func cleanRepoSegments(segments []string) []string {
	cleaned := make([]string, 0, len(segments))
	for i, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			continue
		}
		if i == len(segments)-1 {
			segment = strings.TrimSuffix(segment, ".git")
		}
		cleaned = append(cleaned, segment)
	}
	return cleaned
}

func AccessTarget(params DeployParams) string {
	return resolveRoute(params, sanitizeName(params.ImageName)).AccessTarget
}

func AccessURL(params DeployParams) string {
	return "http://" + AccessTarget(params)
}

func routeForProfile(route routeConfig, profile appProfile) routeConfig {
	if profile.Name == appTypeNextJS {
		route.UseStripPrefix = false
		route.MiddlewareName = ""
		route.StripPrefix = ""
	}
	return route
}
