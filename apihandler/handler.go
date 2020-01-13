package apihandler

import (
	pb "Cw_UserService/proto"
	"fmt"
	"github.com/go-redis/redis"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/net/context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"io"
	"log"
	"strings"
	"time"
)

type Server struct {
	RedisConnection  *redis.Client
	UserCollection *mongo.Collection
}

//Helper function
func(h *Server) ValidateUserInformation(user *pb.User) bool {
	if(len(user.GoogleId) > 0 && len(user.Genre) > 0 && len(user.Language) > 0 && len(user.ContentType) > 0 && len(user.Email) > 0 && len(user.PhoneNumber) > 0){
		return true
	}else {
		return false
	}
}

func (h *Server) CreateUser(ctx context.Context, req *pb.User) (*pb.CreateReponse, error) {
	findResult := h.UserCollection.FindOne(context.Background(), bson.D{{"googleid", req.GoogleId}})
	if findResult.Err() != nil {
		// means user is not present so add user in DB
		if h.ValidateUserInformation(req) {
			ts, _ := ptypes.TimestampProto(time.Now())
			req.CreatedAt = ts
			_, err := h.UserCollection.InsertOne(context.Background(), req)
			return &pb.CreateReponse{IsCreated:true}, err
		}else {
			return  nil, status.Error(codes.Internal, fmt.Sprintf("User Information not valid %s "))
		}
	}else {
		return nil, status.Error(codes.Internal, fmt.Sprintf("User already present please try update profile api instead. %s "))
	}
}

func (h *Server) GetUser(ctx context.Context, request *pb.GetRequest) (*pb.User, error) {
	var response *pb.User
	err := h.UserCollection.FindOne(context.Background(), bson.D{{"googleid", request.GetGoogleId()}}).Decode(&response)
	return response, err
}


func (h *Server) UpdateUser(ctx context.Context, req *pb.User) (*pb.UpdateResponse, error) {
	var res pb.UpdateResponse
	if !h.ValidateUserInformation(req) {
		return nil, status.Error(codes.Internal, fmt.Sprintf("User Information not valid"))
	}
	_, err := h.UserCollection.ReplaceOne(context.Background(), bson.D{{"googleid", req.GetGoogleId()}}, &req)
	if err != nil {
		res.IsUpdated = false
	}else {
		res.IsUpdated = true
	}
	return  &res, err
}

func (h *Server) DeleteUser(ctx context.Context, request *pb.DeleteRequest) ( *pb.DeleteReponse, error) {
	var response pb.DeleteReponse
	_, err := h.UserCollection.DeleteOne(ctx, bson.D{{"googleid", request.GetGoogleId()}})
	if err != nil {
		response.IsDeleted = false
	}else {
		response.IsDeleted = true
	}
	return &response, err
}

func (h *Server) LinkedTvDevice(ctx context.Context, request *pb.TvDevice ) (*pb.LinkedDeviceResponse, error) {
	var cwUser pb.User
	var response pb.LinkedDeviceResponse
	err := h.UserCollection.FindOne(ctx, bson.D{{"googleid", request.GetGoogleId()}}).Decode(&cwUser)
	if err != nil {
		return  nil,  status.Error(codes.Internal, fmt.Sprintf("User not found %s ", err.Error()))
	}
	cwUser.LinkedDevices = append(cwUser.LinkedDevices, request.LinkedDevice)
	_, err = h.UserCollection.ReplaceOne(ctx, bson.D{{"googleid", request.GetGoogleId()}}, cwUser)
	if err != nil {
		response.IsLinkedDeviceFetched = false
		return &response,  status.Error(codes.Internal, fmt.Sprintf("Error while updating user data %s ", err.Error()))
	}
	response.LinkedDevices = cwUser.LinkedDevices
	response.IsLinkedDeviceFetched = true
	return &response, nil
}


func (h *Server) RemoveTvDevice(ctx context.Context, request *pb.RemoveTvDeviceRequest ) ( *pb.RemoveTvDeviceResponse, error) {
	var cwUser pb.User
	var response pb.RemoveTvDeviceResponse
	err := h.UserCollection.FindOne(context.Background(), bson.D{{"googleid", request.GoogleId}}).Decode(&cwUser)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("Error while fetching user data %s ", err.Error()))
	}
	// Find and remove
	for i, v := range cwUser.LinkedDevices {
		if v.TvEmac == request.TvEmac {
			cwUser.LinkedDevices = append(cwUser.LinkedDevices[:i], cwUser.LinkedDevices[i+1:]...)
			_, err = h.UserCollection.ReplaceOne(ctx, bson.D{{"googleid", request.GoogleId}}, cwUser)
			if err != nil {
				return nil,  status.Error(codes.Internal, fmt.Sprintf("Error while updating user data %s ", err.Error()))
			}
			response.IsTvDeviceRemoved = true
			return &response, nil
		}
	}
	response.IsTvDeviceRemoved = false
	return nil, status.Error(codes.Internal, fmt.Sprintf("Tv device not found associated with user profile %s ", err.Error()))
}

func (h *Server) GetLinkedDevices(ctx context.Context, request *pb.GetRequest ) (*pb.LinkedDeviceResponse, error) {
	var cwUser pb.User
	var response pb.LinkedDeviceResponse
	err := h.UserCollection.FindOne(ctx, bson.D{{"googleid", request.GoogleId}}).Decode(&cwUser)
	if err != nil {
		return nil, status.Error(codes.Internal, fmt.Sprintf("Error while fetching user data %s ", err.Error()))
	}
	response.LinkedDevices = cwUser.LinkedDevices
	response.IsLinkedDeviceFetched = true
	return &response, nil
}


func (s *Server) TileClick(stream pb.UserService_TileClickServer)  error {
	log.Println("click hit ")

	for {
		tileInfo , err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return status.Error(codes.Internal, fmt.Sprintf("Error while getting data from request %s ", err.Error()))
		}
		ts, _ := ptypes.TimestampProto(time.Now())
		tileInfo.EventTime = ts
		resultByteArray, err := proto.Marshal(tileInfo)
		if err != nil {
			return  status.Error(codes.Internal, fmt.Sprintf("Error while marshaling data %s ", err.Error()))
		}
		// adding emac to major analytics
		s.RedisConnection.SAdd("todaysUser", strings.TrimSpace(strings.Replace(strings.ToLower(tileInfo.TvEmac), ":", "", -1)))
		// setting page carousel in redis
		s.RedisConnection.SAdd(fmt.Sprintf("%s:clicked",strings.TrimSpace(strings.Replace(strings.ToLower(tileInfo.TvEmac), ":", "", -1))), resultByteArray)
	}

	return nil
}


func (s *Server) TileSelect(stream pb.UserService_TileSelectServer)  error {

	for {
		tileInfo , err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return status.Error(codes.Internal, fmt.Sprintf("Error while getting data from request %s ", err.Error()))
		}
		ts, _ := ptypes.TimestampProto(time.Now())
		tileInfo.EventTime = ts
		resultByteArray, err := proto.Marshal(tileInfo)
		if err != nil {
			return  status.Error(codes.Internal, fmt.Sprintf("Error while marshaling data %s ", err.Error()))
		}
		// adding emac to major analytics
		s.RedisConnection.SAdd("todaysUser", strings.TrimSpace(strings.Replace(strings.ToLower(tileInfo.TvEmac), ":", "", -1)))
		s.RedisConnection.SAdd(fmt.Sprintf("%s:selected",strings.TrimSpace(strings.Replace(strings.ToLower(tileInfo.TvEmac), ":", "", -1))), resultByteArray)
	}
	// setting page carousel in redis
	return nil
}

