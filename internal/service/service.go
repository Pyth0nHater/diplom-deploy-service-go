package service

import "github.com/docker/docker/client"

const defaultTraefikNetwork = "web_network"

type BuilderService struct {
	dockerCli *client.Client
}

type EventLevel string

const (
	EventLevelInfo    EventLevel = "info"
	EventLevelSuccess EventLevel = "success"
	EventLevelError   EventLevel = "error"
)

type DeployParams struct {
	RepoURL     string
	AccessToken string
	ImageName   string
	Domain      string
	Branch      string
	AppType     string
	NodeVersion string
}

type DeployResult struct {
	ImageName     string
	ContainerName string
	Domain        string
}

type EventFn func(level EventLevel, stage, message string, result *DeployResult) error

func NewBuilderService() (*BuilderService, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}
	return &BuilderService{dockerCli: cli}, nil
}

func ContainerName(imageName string) string {
	return sanitizeName(imageName) + "-cnt"
}

func DockerImageName(imageName string) string {
	return sanitizeName(imageName)
}
