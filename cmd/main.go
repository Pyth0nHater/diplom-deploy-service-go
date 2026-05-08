package main

import (
	"deploy-service/internal/handler"
	"deploy-service/internal/service"
	"log"
	"net"
	"os"

	"deploy-service/pkg/deploypb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	builderSvc, err := service.NewBuilderService()
	if err != nil {
		log.Fatalf("create builder service: %v", err)
	}

	deployHandler := handler.NewDeployHandler(builderSvc)
	grpcServer := grpc.NewServer()
	deploypb.RegisterDeployServiceServer(grpcServer, deployHandler)
	reflection.Register(grpcServer)

	addr := envOrDefault("GRPC_ADDR", ":50051")
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("listen %s: %v", addr, err)
	}

	log.Printf("gRPC deploy service started on %s", addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("serve gRPC: %v", err)
	}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
