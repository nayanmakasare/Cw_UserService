package main

import (
	"Cw_UserService/apihandler"
	pb "Cw_UserService/proto"
	"context"
	"fmt"
	codecs "github.com/amsokol/mongo-go-driver-protobuf"
	"github.com/go-redis/redis"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"log"
	"net"
	"net/http"
	"strings"
	"time"
)

const (
	defaultHost          = "mongodb://nayan:tlwn722n@cluster0-shard-00-00-8aov2.mongodb.net:27017,cluster0-shard-00-01-8aov2.mongodb.net:27017,cluster0-shard-00-02-8aov2.mongodb.net:27017/test?ssl=true&replicaSet=Cluster0-shard-0&authSource=admin&retryWrites=true&w=majority"
	schedularMongoHost = "mongodb://192.168.1.143:27017"
	schedularRedisHost = "192.168.1.143:6379"
	developmentMongoHost = "mongodb://dev-uni.cloudwalker.tv:6592"
)

// private type for Context keys
type contextKey int

const (
	clientIDKey contextKey = iota
)

func credMatcher(headerName string) (mdName string, ok bool) {
	if headerName == "Login" || headerName == "Password" {
		return headerName, true
	}
	return "", false
}

// authenticateAgent check the client credentials
func authenticateClient(ctx context.Context, s *apihandler.Server) (string, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		clientLogin := strings.Join(md["login"], "")
		clientPassword := strings.Join(md["password"], "")
		if clientLogin != "nayan" {
			return "", fmt.Errorf("unknown user %s", clientLogin)
		}
		if clientPassword != "makasare" {
			return "", fmt.Errorf("bad password %s", clientPassword)
		}
		log.Printf("authenticated client: %s", clientLogin)
		return "42", nil
	}
	return "", fmt.Errorf("missing credentials")
}


func unaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	s, ok := info.Server.(*apihandler.Server)
	if !ok {
		return nil, fmt.Errorf("unable to cast the server")
	}
	clientID , err := authenticateClient(ctx, s)
	if err != nil {
		return nil, err
	}
	ctx = context.WithValue(ctx, clientIDKey, clientID)
	return handler(ctx, req)
}


func startGRPCServer(address, certFile, keyFile string) error {
	// create a listener on TCP port
	lis, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen: %v", err)
	}  // create a server instance
	s := apihandler.Server{
		getRedisClient(schedularRedisHost),
		getMongoCollection("cloudwalker", "users", developmentMongoHost),
	}  // Create the TLS credentials
	creds, err := credentials.NewServerTLSFromFile(certFile, keyFile)
	if err != nil {
		return fmt.Errorf("could not load TLS keys: %s", err)
	}  // Create an array of gRPC options with the credentials


	//opts := []grpc.ServerOption{grpc.Creds(creds), grpc.UnaryInterceptor(unaryInterceptor)}
	_ = []grpc.ServerOption{grpc.Creds(creds), grpc.UnaryInterceptor(unaryInterceptor)}



	// create a gRPC server object
	//grpcServer := grpc.NewServer(opts...)  // attach the Ping service to the server



	grpcServer := grpc.NewServer()  // attach the Ping service to the server
	pb.RegisterUserServiceServer(grpcServer, &s)  // start the server
	log.Printf("starting HTTP/2 gRPC server on %s", address)
	if err := grpcServer.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve: %s", err)
	}
	return nil
}

func startRESTServer(address, grpcAddress, certFile string) error {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	mux := runtime.NewServeMux(runtime.WithIncomingHeaderMatcher(credMatcher))


	_ , err := credentials.NewClientTLSFromFile(certFile, "")
	if err != nil {
		return fmt.Errorf("could not load TLS certificate: %s", err)
	}  // Setup the client gRPC options
	//opts := []grpc.DialOption{grpc.WithTransportCredentials(creds)}  // Register ping


	opts := []grpc.DialOption{grpc.WithInsecure()}  // Register ping

	err = pb.RegisterUserServiceHandlerFromEndpoint(ctx, mux, grpcAddress, opts)

	if err != nil {
		return fmt.Errorf("could not register service Ping: %s", err)
	}

	log.Printf("starting HTTP/1.1 REST server on %s", address)
	http.ListenAndServe(address, mux)
	return nil
}

func getMongoCollection(dbName, collectionName, mongoHost string )  *mongo.Collection {
	// Register custom codecs for protobuf Timestamp and wrapper types
	reg := codecs.Register(bson.NewRegistryBuilder()).Build()
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	mongoClient, err :=  mongo.Connect(ctx, options.Client().ApplyURI(mongoHost), options.Client().SetRegistry(reg))
	if err != nil {
		log.Fatal(err)
	}
	return mongoClient.Database(dbName).Collection(collectionName)
}

func getRedisClient(redisHost string ) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:     redisHost,
		Password: "", // no password set
		DB:       0,  // use default DB
	})
	_, err := client.Ping().Result()
	if err != nil {
		log.Fatalf("Could not connect to redis %v", err)
	}
	return client
}

func main()  {
	//grpcAddress := fmt.Sprintf("%s:%d", "cloudwalker.services.tv", 7771)
	//restAddress := fmt.Sprintf("%s:%d", "cloudwalker.services.tv", 7772)

	grpcAddress := fmt.Sprintf("%s:%d", "192.168.1.143", 7769)
	restAddress := fmt.Sprintf("%s:%d", "192.168.1.143", 7770)
	certFile := "cert/server.crt"
	keyFile := "cert/server.key"

	// fire the gRPC server in a goroutine
	go func() {
		err := startGRPCServer(grpcAddress, certFile, keyFile)
		if err != nil {
			log.Fatalf("failed to start gRPC server: %s", err)
		}
	}()


	// fire the REST server in a goroutine
	go func() {
		err := startRESTServer(restAddress, grpcAddress, certFile)
		if err != nil {
			log.Fatalf("failed to start gRPC server: %s", err)
		}
	}()  // infinite loop
	log.Printf("Entering infinite loop")
	select {}
}


//func syncToMongo(analyticCollection mongo.Collection, redisClient *redis.Client) error {
//		for _, emac := range redisClient.SMembers("todaysUser").Val() {
//			//adding tile clicked
//			for _, value := range redisClient.SMembers(fmt.Sprintf("%s:clicked", emac)).Val() {
//				var tileInfo pb.TileInfo
//				err := proto.Unmarshal([]byte(value), &tileInfo)
//				if err != nil {
//
//				}
//			}
//		}
//}