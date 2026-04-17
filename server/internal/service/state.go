package service

import (
	"context"

	"lunar-tear/server/internal/store"

	"google.golang.org/grpc/metadata"
)

var startedGameStartTables = []string{
	"IUserProfile",
	"IUserCharacter",
	"IUserCostume",
	"IUserWeapon",
	"IUserWeaponSkill",
	"IUserWeaponAbility",
	"IUserWeaponStory",
	"IUserCompanion",
	"IUserDeckCharacter",
	"IUserDeck",
	"IUserGem",
	"IUserMission",
	"IUserMainQuestFlowStatus",
	"IUserMainQuestMainFlowStatus",
	"IUserMainQuestProgressStatus",
	"IUserMainQuestSeasonRoute",
	"IUserQuest",
	"IUserQuestMission",
	"IUserTutorialProgress",
	"IUserWeaponNote",
	"IUserCostumeActiveSkill",
	"IUserDeckTypeNote",
	"IUserDeckSubWeaponGroup",
	"IUserDeckPartsGroup",
	"IUserConsumableItem",
	"IUserMaterial",
	"IUserImportantItem",
}

var gimmickDiffTables = []string{
	"IUserGimmick",
	"IUserGimmickOrnamentProgress",
	"IUserGimmickSequence",
	"IUserGimmickUnlock",
}

func currentUserId(ctx context.Context, users store.UserRepository, sessions store.SessionRepository) int64 {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if vals := md.Get("x-apb-session-key"); len(vals) > 0 {
			if userId, err := sessions.ResolveUserId(vals[0]); err == nil {
				return userId
			}
		}
	}

	defaultId, _ := users.DefaultUserId()
	return defaultId
}
