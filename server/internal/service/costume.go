package service

import (
	"context"
	"fmt"
	"log"

	pb "lunar-tear/server/gen/proto"
	"lunar-tear/server/internal/gametime"
	"lunar-tear/server/internal/gameutil"
	"lunar-tear/server/internal/masterdata"
	"lunar-tear/server/internal/model"
	"lunar-tear/server/internal/store"
	"lunar-tear/server/internal/userdata"
)

var costumeDiffTables = []string{
	"IUserCostume",
	"IUserMaterial",
	"IUserConsumableItem",
}

type CostumeServiceServer struct {
	pb.UnimplementedCostumeServiceServer
	users    store.UserRepository
	sessions store.SessionRepository
	catalog  *masterdata.CostumeCatalog
	config   *masterdata.GameConfig
}

func NewCostumeServiceServer(users store.UserRepository, sessions store.SessionRepository, catalog *masterdata.CostumeCatalog, config *masterdata.GameConfig) *CostumeServiceServer {
	return &CostumeServiceServer{users: users, sessions: sessions, catalog: catalog, config: config}
}

func (s *CostumeServiceServer) Enhance(ctx context.Context, req *pb.EnhanceRequest) (*pb.EnhanceResponse, error) {
	log.Printf("[CostumeService] Enhance: uuid=%s materials=%v", req.UserCostumeUuid, req.Materials)

	userId := currentUserId(ctx, s.users, s.sessions)
	nowMillis := gametime.NowMillis()

	snapshot, err := s.users.UpdateUser(userId, func(user *store.UserState) {
		costume, ok := user.Costumes[req.UserCostumeUuid]
		if !ok {
			log.Printf("[CostumeService] Enhance: costume uuid=%s not found", req.UserCostumeUuid)
			return
		}

		cm, ok := s.catalog.Costumes[costume.CostumeId]
		if !ok {
			log.Printf("[CostumeService] Enhance: costume master id=%d not found", costume.CostumeId)
			return
		}

		totalExp := int32(0)
		totalMaterialCount := int32(0)
		for materialId, count := range req.Materials {
			mat, ok := s.catalog.Materials[materialId]
			if !ok {
				log.Printf("[CostumeService] Enhance: material id=%d not found, skipping", materialId)
				continue
			}

			cur := user.Materials[materialId]
			if cur < count {
				log.Printf("[CostumeService] Enhance: insufficient material id=%d have=%d need=%d", materialId, cur, count)
				continue
			}
			user.Materials[materialId] = cur - count
			totalMaterialCount += count

			expPerUnit := mat.EffectValue
			if mat.WeaponType != 0 && mat.WeaponType == cm.SkillfulWeaponType {
				expPerUnit = expPerUnit * s.config.MaterialSameWeaponExpCoefficientPermil / 1000
			}
			totalExp += expPerUnit * count
		}

		if costFunc, ok := s.catalog.EnhanceCostByRarity[cm.RarityType]; ok && totalMaterialCount > 0 {
			goldCost := costFunc.Evaluate(totalMaterialCount)
			user.ConsumableItems[s.config.ConsumableItemIdForGold] -= goldCost
			log.Printf("[CostumeService] Enhance: gold cost=%d (materials=%d)", goldCost, totalMaterialCount)
		}

		costume.Exp += totalExp

		if thresholds, ok := s.catalog.ExpByRarity[cm.RarityType]; ok {
			costume.Level, costume.Exp = gameutil.LevelAndCap(costume.Exp, thresholds)
		}

		costume.LatestVersion = nowMillis
		user.Costumes[req.UserCostumeUuid] = costume
		log.Printf("[CostumeService] Enhance: costumeId=%d +%d exp -> total=%d level=%d", costume.CostumeId, totalExp, costume.Exp, costume.Level)
	})
	if err != nil {
		return nil, fmt.Errorf("costume enhance: %w", err)
	}

	tables := userdata.FullClientTableMap(snapshot)
	diff := userdata.BuildDiffFromTables(userdata.SelectTables(tables, costumeDiffTables))

	return &pb.EnhanceResponse{
		IsGreatSuccess:         false,
		SurplusEnhanceMaterial: map[int32]int32{},
		DiffUserData:           diff,
	}, nil
}

var awakenDiffTables = []string{
	"IUserCostume",
	"IUserMaterial",
	"IUserConsumableItem",
	"IUserCostumeAwakenStatusUp",
	"IUserThought",
}

func (s *CostumeServiceServer) Awaken(ctx context.Context, req *pb.AwakenRequest) (*pb.AwakenResponse, error) {
	log.Printf("[CostumeService] Awaken: uuid=%s materials=%v", req.UserCostumeUuid, req.Materials)

	userId := currentUserId(ctx, s.users, s.sessions)
	nowMillis := gametime.NowMillis()

	snapshot, err := s.users.UpdateUser(userId, func(user *store.UserState) {
		costume, ok := user.Costumes[req.UserCostumeUuid]
		if !ok {
			log.Printf("[CostumeService] Awaken: costume uuid=%s not found", req.UserCostumeUuid)
			return
		}

		awakenRow, ok := s.catalog.AwakenByCostumeId[costume.CostumeId]
		if !ok {
			log.Printf("[CostumeService] Awaken: no awaken data for costumeId=%d", costume.CostumeId)
			return
		}

		nextStep := costume.AwakenCount + 1

		if gold, ok := s.catalog.AwakenPriceByGroup[awakenRow.CostumeAwakenPriceGroupId]; ok {
			user.ConsumableItems[s.config.ConsumableItemIdForGold] -= gold
			log.Printf("[CostumeService] Awaken: gold cost=%d", gold)
		}

		for materialId, count := range req.Materials {
			cur := user.Materials[materialId]
			if cur < count {
				log.Printf("[CostumeService] Awaken: insufficient material id=%d have=%d need=%d", materialId, cur, count)
				count = cur
			}
			user.Materials[materialId] = cur - count
		}

		costume.AwakenCount = nextStep
		costume.LatestVersion = nowMillis
		user.Costumes[req.UserCostumeUuid] = costume
		log.Printf("[CostumeService] Awaken: costumeId=%d awakenCount=%d", costume.CostumeId, nextStep)

		effectSteps, ok := s.catalog.AwakenEffectsByGroupAndStep[awakenRow.CostumeAwakenEffectGroupId]
		if !ok {
			return
		}
		effect, ok := effectSteps[nextStep]
		if !ok {
			return
		}

		switch model.CostumeAwakenEffectType(effect.CostumeAwakenEffectType) {
		case model.CostumeAwakenEffectTypeStatusUp:
			s.applyAwakenStatusUp(user, req.UserCostumeUuid, effect.CostumeAwakenEffectId, nowMillis)
		case model.CostumeAwakenEffectTypeAbility:
			log.Printf("[CostumeService] Awaken: ability effect id=%d (client-resolved)", effect.CostumeAwakenEffectId)
		case model.CostumeAwakenEffectTypeItemAcquire:
			s.applyAwakenItemAcquire(user, effect.CostumeAwakenEffectId, nowMillis)
		default:
			log.Printf("[CostumeService] Awaken: unknown effect type=%d", effect.CostumeAwakenEffectType)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("costume awaken: %w", err)
	}

	tables := userdata.FullClientTableMap(snapshot)
	diff := userdata.BuildDiffFromTables(userdata.SelectTables(tables, awakenDiffTables))

	return &pb.AwakenResponse{
		DiffUserData: diff,
	}, nil
}

func (s *CostumeServiceServer) applyAwakenStatusUp(user *store.UserState, costumeUuid string, statusUpGroupId int32, nowMillis int64) {
	rows, ok := s.catalog.AwakenStatusUpByGroup[statusUpGroupId]
	if !ok {
		log.Printf("[CostumeService] Awaken: status up group %d not found", statusUpGroupId)
		return
	}

	for _, row := range rows {
		calcType := model.StatusCalculationType(row.StatusCalculationType)
		key := store.CostumeAwakenStatusKey{
			UserCostumeUuid:       costumeUuid,
			StatusCalculationType: calcType,
		}
		state := user.CostumeAwakenStatusUps[key]
		state.UserCostumeUuid = costumeUuid
		state.StatusCalculationType = calcType

		switch model.StatusKindType(row.StatusKindType) {
		case model.StatusKindTypeHp:
			state.Hp += row.EffectValue
		case model.StatusKindTypeAttack:
			state.Attack += row.EffectValue
		case model.StatusKindTypeVitality:
			state.Vitality += row.EffectValue
		case model.StatusKindTypeAgility:
			state.Agility += row.EffectValue
		case model.StatusKindTypeCriticalRatio:
			state.CriticalRatio += row.EffectValue
		case model.StatusKindTypeCriticalAttack:
			state.CriticalAttack += row.EffectValue
		}

		state.LatestVersion = nowMillis
		user.CostumeAwakenStatusUps[key] = state
	}
}

func (s *CostumeServiceServer) applyAwakenItemAcquire(user *store.UserState, itemAcquireId int32, nowMillis int64) {
	acq, ok := s.catalog.AwakenItemAcquireById[itemAcquireId]
	if !ok {
		log.Printf("[CostumeService] Awaken: item acquire id=%d not found", itemAcquireId)
		return
	}

	key := fmt.Sprintf("awaken-thought-%d", acq.PossessionId)
	if _, exists := user.Thoughts[key]; exists {
		return
	}
	user.Thoughts[key] = store.ThoughtState{
		UserThoughtUuid:     key,
		ThoughtId:           acq.PossessionId,
		AcquisitionDatetime: nowMillis,
		LatestVersion:       nowMillis,
	}
	log.Printf("[CostumeService] Awaken: granted thought id=%d", acq.PossessionId)
}

var activeSkillDiffTables = []string{
	"IUserCostumeActiveSkill",
	"IUserMaterial",
	"IUserConsumableItem",
}

func (s *CostumeServiceServer) EnhanceActiveSkill(ctx context.Context, req *pb.EnhanceActiveSkillRequest) (*pb.EnhanceActiveSkillResponse, error) {
	log.Printf("[CostumeService] EnhanceActiveSkill: uuid=%s addLevel=%d", req.UserCostumeUuid, req.AddLevelCount)

	userId := currentUserId(ctx, s.users, s.sessions)
	nowMillis := gametime.NowMillis()

	snapshot, err := s.users.UpdateUser(userId, func(user *store.UserState) {
		costume, ok := user.Costumes[req.UserCostumeUuid]
		if !ok {
			log.Printf("[CostumeService] EnhanceActiveSkill: costume uuid=%s not found", req.UserCostumeUuid)
			return
		}

		cm, ok := s.catalog.Costumes[costume.CostumeId]
		if !ok {
			log.Printf("[CostumeService] EnhanceActiveSkill: costume master id=%d not found", costume.CostumeId)
			return
		}

		groupRows := s.catalog.ActiveSkillGroupsByGroupId[cm.CostumeActiveSkillGroupId]
		enhanceMatId := int32(-1)
		for _, g := range groupRows {
			if g.CostumeLimitBreakCountLowerLimit <= costume.LimitBreakCount {
				enhanceMatId = g.CostumeActiveSkillEnhancementMaterialId
				break
			}
		}
		if enhanceMatId < 0 {
			log.Printf("[CostumeService] EnhanceActiveSkill: no skill group for costumeId=%d groupId=%d lb=%d",
				costume.CostumeId, cm.CostumeActiveSkillGroupId, costume.LimitBreakCount)
			return
		}

		skill := user.CostumeActiveSkills[req.UserCostumeUuid]
		currentLevel := skill.Level

		maxLevelFunc, ok := s.catalog.ActiveSkillMaxLevelByRarity[cm.RarityType]
		if !ok {
			log.Printf("[CostumeService] EnhanceActiveSkill: no max level func for rarity=%d", cm.RarityType)
			return
		}
		maxLevel := maxLevelFunc.Evaluate(1)

		addCount := req.AddLevelCount
		if currentLevel+addCount > maxLevel {
			addCount = maxLevel - currentLevel
		}
		if addCount <= 0 {
			log.Printf("[CostumeService] EnhanceActiveSkill: already at max level %d", currentLevel)
			return
		}

		for lvl := currentLevel; lvl < currentLevel+addCount; lvl++ {
			key := [2]int32{enhanceMatId, lvl}
			mats := s.catalog.ActiveSkillEnhanceMats[key]
			for _, mat := range mats {
				cur := user.Materials[mat.MaterialId]
				cost := mat.Count
				if cur < cost {
					log.Printf("[CostumeService] EnhanceActiveSkill: insufficient material id=%d have=%d need=%d", mat.MaterialId, cur, cost)
					cost = cur
				}
				user.Materials[mat.MaterialId] = cur - cost
			}

			if costFunc, ok := s.catalog.ActiveSkillCostByRarity[cm.RarityType]; ok {
				goldCost := costFunc.Evaluate(lvl + 1)
				user.ConsumableItems[s.config.ConsumableItemIdForGold] -= goldCost
			}
		}

		skill.UserCostumeUuid = req.UserCostumeUuid
		skill.Level = currentLevel + addCount
		skill.LatestVersion = nowMillis
		user.CostumeActiveSkills[req.UserCostumeUuid] = skill
		log.Printf("[CostumeService] EnhanceActiveSkill: costumeId=%d level %d -> %d", costume.CostumeId, currentLevel, skill.Level)
	})
	if err != nil {
		return nil, fmt.Errorf("costume enhance active skill: %w", err)
	}

	tables := userdata.FullClientTableMap(snapshot)
	diff := userdata.BuildDiffFromTables(userdata.SelectTables(tables, activeSkillDiffTables))

	return &pb.EnhanceActiveSkillResponse{
		DiffUserData: diff,
	}, nil
}

func (s *CostumeServiceServer) LimitBreak(ctx context.Context, req *pb.LimitBreakRequest) (*pb.LimitBreakResponse, error) {
	log.Printf("[CostumeService] LimitBreak: uuid=%s materials=%v", req.UserCostumeUuid, req.Materials)

	userId := currentUserId(ctx, s.users, s.sessions)
	nowMillis := gametime.NowMillis()

	snapshot, err := s.users.UpdateUser(userId, func(user *store.UserState) {
		costume, ok := user.Costumes[req.UserCostumeUuid]
		if !ok {
			log.Printf("[CostumeService] LimitBreak: costume uuid=%s not found", req.UserCostumeUuid)
			return
		}

		if costume.LimitBreakCount >= s.config.CostumeLimitBreakAvailableCount {
			log.Printf("[CostumeService] LimitBreak: already at max limit break %d", costume.LimitBreakCount)
			return
		}

		cm, ok := s.catalog.Costumes[costume.CostumeId]
		if !ok {
			log.Printf("[CostumeService] LimitBreak: costume master id=%d not found", costume.CostumeId)
			return
		}

		totalMaterialCount := int32(0)
		for materialId, count := range req.Materials {
			cur := user.Materials[materialId]
			if cur < count {
				log.Printf("[CostumeService] LimitBreak: insufficient material id=%d have=%d need=%d", materialId, cur, count)
				count = cur
			}
			user.Materials[materialId] = cur - count
			totalMaterialCount += count
		}

		if costFunc, ok := s.catalog.LimitBreakCostByRarity[cm.RarityType]; ok && totalMaterialCount > 0 {
			goldCost := costFunc.Evaluate(totalMaterialCount)
			user.ConsumableItems[s.config.ConsumableItemIdForGold] -= goldCost
			log.Printf("[CostumeService] LimitBreak: gold cost=%d", goldCost)
		}

		costume.LimitBreakCount++
		costume.LatestVersion = nowMillis
		user.Costumes[req.UserCostumeUuid] = costume
		log.Printf("[CostumeService] LimitBreak: costumeId=%d limitBreak -> %d", costume.CostumeId, costume.LimitBreakCount)
	})
	if err != nil {
		return nil, fmt.Errorf("costume limit break: %w", err)
	}

	tables := userdata.FullClientTableMap(snapshot)
	diff := userdata.BuildDiffFromTables(userdata.SelectTables(tables, costumeDiffTables))

	return &pb.LimitBreakResponse{
		DiffUserData: diff,
	}, nil
}
