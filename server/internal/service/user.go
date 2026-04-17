package service

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	pb "lunar-tear/server/gen/proto"
	"lunar-tear/server/internal/gametime"
	"lunar-tear/server/internal/model"
	"lunar-tear/server/internal/store"
	"lunar-tear/server/internal/userdata"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type UserServiceServer struct {
	pb.UnimplementedUserServiceServer
	users    store.UserRepository
	sessions store.SessionRepository
}

func NewUserServiceServer(users store.UserRepository, sessions store.SessionRepository) *UserServiceServer {
	return &UserServiceServer{users: users, sessions: sessions}
}

func setCommonResponseTrailers(ctx context.Context, diff map[string]*pb.DiffData, includeUpdateNames bool) {
	keys := make([]string, 0, len(diff))
	for key := range diff {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var pairs []string
	if includeUpdateNames && len(keys) > 0 {
		pairs = append(pairs, "x-apb-update-user-data-names", keys[0])
		for _, key := range keys[1:] {
			pairs[len(pairs)-1] += "," + key
		}
	}

	if err := grpc.SetTrailer(ctx, metadata.Pairs(pairs...)); err != nil {
		log.Printf("[UserService] failed to set trailers: %v", err)
	}
}

func (s *UserServiceServer) RegisterUser(ctx context.Context, req *pb.RegisterUserRequest) (*pb.RegisterUserResponse, error) {
	userId, err := s.users.CreateUser(req.Uuid)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	user, err := s.users.LoadUser(userId)
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	log.Printf("[UserService] RegisterUser: uuid=%s terminalId=%s -> userId=%d", req.Uuid, req.TerminalId, user.UserId)

	return &pb.RegisterUserResponse{
		UserId:       user.UserId,
		Signature:    fmt.Sprintf("sig_%d_%d", user.UserId, gametime.Now().Unix()),
		DiffUserData: userdata.BuildDiffFromTables(userdata.FirstEntranceClientTableMap(user)),
	}, nil
}

func (s *UserServiceServer) Auth(ctx context.Context, req *pb.AuthUserRequest) (*pb.AuthUserResponse, error) {
	log.Printf("[UserService] Auth: uuid=%s", req.Uuid)

	session, err := s.sessions.CreateSession(req.Uuid, 24*time.Hour)
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}
	user, err := s.users.LoadUser(session.UserId)
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}

	return &pb.AuthUserResponse{
		SessionKey:     session.SessionKey,
		ExpireDatetime: timestamppb.New(session.ExpireAt),
		Signature:      req.Signature,
		UserId:         user.UserId,
		DiffUserData:   userdata.BuildDiffFromTables(userdata.FirstEntranceClientTableMap(user)),
	}, nil
}

func (s *UserServiceServer) GameStart(ctx context.Context, _ *emptypb.Empty) (*pb.GameStartResponse, error) {
	log.Printf("[UserService] GameStart")

	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-apb-session-key"); len(vals) > 0 {
			log.Printf("[UserService] GameStart session: %s", vals[0])
		}
	}

	userId := currentUserId(ctx, s.users, s.sessions)
	user, _ := s.users.UpdateUser(userId, func(user *store.UserState) {
		user.GameStartDatetime = gametime.NowMillis()
	})
	diff := userdata.BuildDiffFromTables(userdata.ProjectTables(user, startedGameStartTables))
	setCommonResponseTrailers(ctx, diff, true)

	return &pb.GameStartResponse{
		// Apply only the starter outgame rows we need after title completion.
		// Keep IUser and other risky core-account rows out of GameStart diff.
		DiffUserData: diff,
	}, nil
}

func (s *UserServiceServer) TransferUser(ctx context.Context, req *pb.TransferUserRequest) (*pb.TransferUserResponse, error) {
	log.Printf("[UserService] TransferUser")
	userId, err := s.users.CreateUser(req.Uuid)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}
	return &pb.TransferUserResponse{
		UserId:       userId,
		Signature:    "transferred-sig",
		DiffUserData: userdata.EmptyDiff(),
	}, nil
}

func (s *UserServiceServer) SetUserName(ctx context.Context, req *pb.SetUserNameRequest) (*pb.SetUserNameResponse, error) {
	log.Printf("[UserService] SetUserName: %s", req.Name)
	userId := currentUserId(ctx, s.users, s.sessions)
	user, _ := s.users.UpdateUser(userId, func(user *store.UserState) {
		nowMillis := gametime.NowMillis()
		user.Profile.Name = req.Name
		user.Profile.NameUpdateDatetime = nowMillis
	})
	return &pb.SetUserNameResponse{
		DiffUserData: userdata.BuildDiffFromTables(userdata.ProjectTables(user, []string{"IUserProfile"})),
	}, nil
}

func (s *UserServiceServer) SetUserMessage(ctx context.Context, req *pb.SetUserMessageRequest) (*pb.SetUserMessageResponse, error) {
	log.Printf("[UserService] SetUserMessage: %s", req.Message)
	userId := currentUserId(ctx, s.users, s.sessions)
	user, _ := s.users.UpdateUser(userId, func(user *store.UserState) {
		nowMillis := gametime.NowMillis()
		user.Profile.Message = req.Message
		user.Profile.MessageUpdateDatetime = nowMillis
	})
	return &pb.SetUserMessageResponse{
		DiffUserData: userdata.BuildDiffFromTables(userdata.ProjectTables(user, []string{"IUserProfile"})),
	}, nil
}

func (s *UserServiceServer) SetUserFavoriteCostumeId(ctx context.Context, req *pb.SetUserFavoriteCostumeIdRequest) (*pb.SetUserFavoriteCostumeIdResponse, error) {
	log.Printf("[UserService] SetUserFavoriteCostumeId: %d", req.FavoriteCostumeId)
	userId := currentUserId(ctx, s.users, s.sessions)
	user, _ := s.users.UpdateUser(userId, func(user *store.UserState) {
		nowMillis := gametime.NowMillis()
		user.Profile.FavoriteCostumeId = req.FavoriteCostumeId
		user.Profile.FavoriteCostumeIdUpdateDatetime = nowMillis
	})
	return &pb.SetUserFavoriteCostumeIdResponse{
		DiffUserData: userdata.BuildDiffFromTables(userdata.ProjectTables(user, []string{"IUserProfile"})),
	}, nil
}

func (s *UserServiceServer) GetUserProfile(ctx context.Context, req *pb.GetUserProfileRequest) (*pb.GetUserProfileResponse, error) {
	log.Printf("[UserService] GetUserProfile: playerId=%d", req.PlayerId)
	userId := req.PlayerId
	if userId == 0 {
		userId = currentUserId(ctx, s.users, s.sessions)
	}
	user, err := s.users.LoadUser(userId)
	if err != nil {
		return &pb.GetUserProfileResponse{DiffUserData: userdata.EmptyDiff()}, nil
	}

	deckCharacters := []*pb.ProfileDeckCharacter{}
	if deck, ok := user.Decks[store.DeckKey{DeckType: model.DeckTypeQuest, UserDeckNumber: 1}]; ok && deck.UserDeckCharacterUuid01 != "" {
		if deckCharacter, ok := user.DeckCharacters[deck.UserDeckCharacterUuid01]; ok {
			costumeId := int32(0)
			if costume, ok := user.Costumes[deckCharacter.UserCostumeUuid]; ok {
				costumeId = costume.CostumeId
			}
			mainWeaponId := int32(0)
			mainWeaponLevel := int32(0)
			if weapon, ok := user.Weapons[deckCharacter.MainUserWeaponUuid]; ok {
				mainWeaponId = weapon.WeaponId
				mainWeaponLevel = weapon.Level
			}
			deckCharacters = append(deckCharacters, &pb.ProfileDeckCharacter{
				CostumeId:       costumeId,
				MainWeaponId:    mainWeaponId,
				MainWeaponLevel: mainWeaponLevel,
			})
		}
	}

	return &pb.GetUserProfileResponse{
		Level:             user.Status.Level,
		Name:              user.Profile.Name,
		FavoriteCostumeId: user.Profile.FavoriteCostumeId,
		Message:           user.Profile.Message,
		IsFriend:          false,
		LatestUsedDeck: &pb.ProfileDeck{
			Power:         100,
			DeckCharacter: deckCharacters,
		},
		PvpInfo: &pb.ProfilePvpInfo{},
		GamePlayHistory: &pb.GamePlayHistory{
			HistoryItem:              []*pb.PlayHistoryItem{},
			HistoryCategoryGraphItem: []*pb.PlayHistoryCategoryGraphItem{},
		},
		DiffUserData: userdata.EmptyDiff(),
	}, nil
}

func (s *UserServiceServer) SetBirthYearMonth(ctx context.Context, req *pb.SetBirthYearMonthRequest) (*pb.SetBirthYearMonthResponse, error) {
	log.Printf("[UserService] SetBirthYearMonth: %d/%d", req.BirthYear, req.BirthMonth)
	userId := currentUserId(ctx, s.users, s.sessions)
	_, _ = s.users.UpdateUser(userId, func(user *store.UserState) {
		user.BirthYear = req.BirthYear
		user.BirthMonth = req.BirthMonth
	})
	return &pb.SetBirthYearMonthResponse{DiffUserData: userdata.EmptyDiff()}, nil
}

func (s *UserServiceServer) GetBirthYearMonth(ctx context.Context, _ *emptypb.Empty) (*pb.GetBirthYearMonthResponse, error) {
	userId := currentUserId(ctx, s.users, s.sessions)
	user, err := s.users.LoadUser(userId)
	if err != nil {
		return &pb.GetBirthYearMonthResponse{BirthYear: 2000, BirthMonth: 1, DiffUserData: userdata.EmptyDiff()}, nil
	}
	return &pb.GetBirthYearMonthResponse{BirthYear: user.BirthYear, BirthMonth: user.BirthMonth, DiffUserData: userdata.EmptyDiff()}, nil
}

func (s *UserServiceServer) GetChargeMoney(ctx context.Context, _ *emptypb.Empty) (*pb.GetChargeMoneyResponse, error) {
	userId := currentUserId(ctx, s.users, s.sessions)
	user, err := s.users.LoadUser(userId)
	if err != nil {
		return &pb.GetChargeMoneyResponse{ChargeMoneyThisMonth: 0, DiffUserData: userdata.EmptyDiff()}, nil
	}
	return &pb.GetChargeMoneyResponse{ChargeMoneyThisMonth: user.ChargeMoneyThisMonth, DiffUserData: userdata.EmptyDiff()}, nil
}

func (s *UserServiceServer) SetUserSetting(ctx context.Context, req *pb.SetUserSettingRequest) (*pb.SetUserSettingResponse, error) {
	log.Printf("[UserService] SetUserSetting: isNotifyPurchaseAlert=%v", req.IsNotifyPurchaseAlert)
	userId := currentUserId(ctx, s.users, s.sessions)
	user, _ := s.users.UpdateUser(userId, func(user *store.UserState) {
		user.Setting.IsNotifyPurchaseAlert = req.IsNotifyPurchaseAlert
	})
	return &pb.SetUserSettingResponse{
		DiffUserData: userdata.BuildDiffFromTables(userdata.ProjectTables(user, []string{"IUserSetting"})),
	}, nil
}

func (s *UserServiceServer) GetAndroidArgs(ctx context.Context, req *pb.GetAndroidArgsRequest) (*pb.GetAndroidArgsResponse, error) {
	return &pb.GetAndroidArgsResponse{Nonce: "Mama", ApiKey: "1234567890", DiffUserData: userdata.EmptyDiff()}, nil
}

func (s *UserServiceServer) GetBackupToken(ctx context.Context, req *pb.GetBackupTokenRequest) (*pb.GetBackupTokenResponse, error) {
	userId := currentUserId(ctx, s.users, s.sessions)
	user, err := s.users.LoadUser(userId)
	if err != nil {
		return &pb.GetBackupTokenResponse{BackupToken: "mock-backup-token", DiffUserData: userdata.EmptyDiff()}, nil
	}
	return &pb.GetBackupTokenResponse{BackupToken: user.BackupToken, DiffUserData: userdata.EmptyDiff()}, nil
}

func (s *UserServiceServer) CheckTransferSetting(ctx context.Context, _ *emptypb.Empty) (*pb.CheckTransferSettingResponse, error) {
	return &pb.CheckTransferSettingResponse{DiffUserData: userdata.EmptyDiff()}, nil
}

func (s *UserServiceServer) GetUserGamePlayNote(ctx context.Context, req *pb.GetUserGamePlayNoteRequest) (*pb.GetUserGamePlayNoteResponse, error) {
	return &pb.GetUserGamePlayNoteResponse{DiffUserData: userdata.EmptyDiff()}, nil
}
