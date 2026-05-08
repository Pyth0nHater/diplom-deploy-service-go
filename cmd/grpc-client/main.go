package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"time"

	"deploy-service/pkg/deploypb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	addr := flag.String("addr", "localhost:50051", "gRPC server address")
	method := flag.String("method", "bootstrap", "method to call: bootstrap or deploy")
	repoURL := flag.String("repo-url", "", "repository URL")
	token := flag.String("token", "", "repository access token")
	imageName := flag.String("image-name", "", "image name")
	domain := flag.String("domain", "", "deprecated route field; base domain is configured globally")
	branch := flag.String("branch", "main", "repository branch")
	appType := flag.String("app-type", "auto", "application type: auto, static, nextjs")
	nodeVersion := flag.String("node-version", "", "Node.js version override, e.g. 20, 20.18.0, node:20-alpine")
	timeout := flag.Duration("timeout", 10*time.Minute, "request timeout")
	flag.Parse()

	if *repoURL == "" || *imageName == "" {
		log.Fatal("flags -repo-url and -image-name are required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Fatalf("connect to gRPC server: %v", err)
	}
	defer conn.Close()

	client := deploypb.NewDeployServiceClient(conn)

	switch *method {
	case "bootstrap":
		stream, err := client.BootstrapRepository(ctx, &deploypb.BootstrapRepositoryRequest{
			RepoUrl:     *repoURL,
			AccessToken: *token,
			ImageName:   *imageName,
			Domain:      *domain,
			Branch:      *branch,
			AppType:     *appType,
			NodeVersion: *nodeVersion,
		})
		if err != nil {
			log.Fatalf("call BootstrapRepository: %v", err)
		}
		printEvents(stream)
	case "deploy":
		stream, err := client.Deploy(ctx, &deploypb.DeployRequest{
			RepoUrl:     *repoURL,
			AccessToken: *token,
			ImageName:   *imageName,
			Domain:      *domain,
			Branch:      *branch,
			AppType:     *appType,
			NodeVersion: *nodeVersion,
		})
		if err != nil {
			log.Fatalf("call Deploy: %v", err)
		}
		printEvents(stream)
	default:
		log.Fatalf("unsupported method %q, use bootstrap or deploy", *method)
	}
}

func printEvents(stream grpc.ServerStreamingClient[deploypb.DeployEvent]) {
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			return
		}
		if err != nil {
			log.Fatalf("receive stream event: %v", err)
		}

		fmt.Printf("[%s] stage=%s image=%s domain=%s container=%s message=%s\n",
			event.GetLevel().String(),
			event.GetStage(),
			event.GetImageName(),
			event.GetDomain(),
			event.GetContainerName(),
			event.GetMessage(),
		)
	}
}
